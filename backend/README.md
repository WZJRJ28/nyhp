# BrokerFlow 后端基础设施（阶段一）

本目录包含 BrokerFlow 在“协议签署完成”流程中的首批数据库与业务代码。遵循“架构即代码，公理即约束”理念，关键业务不变量完全由 PostgreSQL 强制执行。

## 已实现内容

1. **数据库迁移脚本**（`migrations/000001_base.up.sql`）
   - 定义 `agreement_status`、`event_type` 枚举和统一时间函数 `get_tx_timestamp()`。
   - 建立 `users`、`brokers`、`referrals`、`agreements` 等核心表，并通过局部唯一索引落实公理 A1（单一活跃协议）。新增 `role` 列默认值 `agent`，供权限控制使用。
   - 创建不可变 `timeline_events` 日志及对应触发器，覆盖时间完整性与修改禁止（公理 A3 / A4）。
   - 通过 `pii_data` + RLS + `get_pii_contact` 函数实现 PII 隔离与访问审计（公理 A2 / A7）。
   - 引入事务性 `outbox` 与 `idempotency` 表以支持外部集成和幂等处理（公理 A5 / A6）。

2. **Go 业务层**
   - `agreement/repository.go`：在单个事务中完成协议生效、时间线追加和 outbox 入队。
   - `agreement/match_acceptance.go`：将“候选经纪接受邀约”投射为真实协议＋时间线＋ outbox，保证 P1/P3/P5。
   - `agreement/service.go`：处理 e-sign webhook，包含幂等校验、事务管理和核心业务调用。
   - `referral/service.go` / `referral/matches.go`：提供转介需求 CRUD、候选经纪匹配与接受/拒绝流程。
   - `dispute/service.go`：封装争议创建与解决流程，触发数据库触发器联动协议与账单状态。
   - `cmd/api/main.go`：暴露 `/auth/*`、`/api/referrals`、`/api/referrals/{id}/matches`、`/api/agreements`、`/api/events`、`/api/brokers/{id}`、`/api/disputes` 等 REST 入口，并在启动时自动检测/补齐数据库 schema（含补充缺少的列、触发器、RLS 策略）。
   - `db/conn.go`：提供 `pgxpool` 连接池构建函数。

3. **单元测试**
   - `agreement/service_test.go` 覆盖幂等重放与正常流程两条路径，利用接口化的伪实现隔离数据库依赖。

## 本地开发指南

### 初始化依赖

```bash
cd backend
export https_proxy=http://127.0.0.1:7890
export http_proxy=http://127.0.0.1:7890
export all_proxy=socks5://127.0.0.1:7890
go mod tidy
```

### 执行数据库迁移

迁移文件兼容 `golang-migrate/migrate`，示例命令：

```bash
migrate -path migrations -database "$DATABASE_URL" up
```

注意：`pgcrypto` 扩展由应用启动时自动创建（见 `cmd/api/main.go:1072` 的 `ensurePgcrypto`）。如果仅用脚本或外部工具执行 SQL 迁移，需确保目标数据库已存在该扩展，或在迁移前后手动执行：

```sql
CREATE EXTENSION IF NOT EXISTS pgcrypto;
```

### 建表 / 迁移流程概览

- 迁移脚本目录：`backend/migrations/`（当前为单一基线文件 `000001_base.up.sql`，可重复执行）。
- 应用启动时（`cmd/api/main.go`）：
  - `ensureSchema` 检测核心表是否存在；
    - 已存在：通过 `ensureColumn` 按需补齐缺失列/索引，然后顺序执行 `migrations/*.sql`；
    - 未存在：确保 `pgcrypto` 存在后，顺序执行 `migrations/*.sql` 完成建表；
  - 该流程为幂等设计，便于本地/CI 环境拉起。
- 手工执行：`backend/scripts/dev_migrate.sh` 使用 `psql` 按文件名顺序执行所有 `.sql`。
- 生产建议：采用版本化迁移（如 `golang-migrate`/Flyway）在发布管道中执行，并限制应用在生产环境进行结构性变更（仅做存在性检查）。

### 运行单元测试

默认构建缓存目录若不可写，可先创建本地缓存目录（推荐写入 `.gocache/`，仓库已忽略该目录）：

```bash
mkdir -p .gocache
GOCACHE=$(pwd)/.gocache go test ./...
```

### 运行示例程序

```bash
export DATABASE_URL="postgres://user:pass@localhost:5432/brokerflow?sslmode=disable"
go run ./cmd/api
```

首次启动会自动检测 `referral_requests` 等核心表是否存在，若尚未迁移，会在程序内自动执行 `migrations/` 下的 SQL。

此外，若数据库已有旧版 schema，程序会按需补齐缺失列与索引（见 `ensureSchema`/`ensureColumn`），从而保持幂等升级。

### 压测与并发正确性套件

`go test ./test -run TestACNConcurrency` 默认会尝试：

1. 复用 `-dsn` flag 或环境变量 `STRESS_TEST_PG_DSN` 指定的已有 Postgres；
2. 若未指定，则检测 Docker 是否可用，自动拉起 Postgres 16；
3. 在无 Docker 且本地 Postgres 未运行时自动 `t.Skip`，不会中断整个 `go test ./...`。

因此 CI 或本地无数据库时不会失败，想要强制执行可显式提供 DSN，例如：

```bash
go test ./test -run TestACNConcurrency -dsn 'postgres://user:pass@127.0.0.1:5432/acn_stress?sslmode=disable'
```

压测结束后会调用 Oracles 校验公理，失败时输出随机种子与最近的事件日志。

## 后续建议

1. 编写 Down Migration 以支持回滚。
2. 实现实际的 HTTP handler，将 webhook 请求映射为 `EsignCompletionRequest`。
3. 为 match acceptance、referral cancel 等关键路径补充 API/服务层测试。
4. 把 `go test ./...`、迁移校验与 Playwright 集成到 CI/CD 流程。
