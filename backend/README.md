# BrokerFlow 后端基础设施（阶段一）

本目录包含 BrokerFlow 在“协议签署完成”流程中的首批数据库与业务代码。遵循“架构即代码，公理即约束”理念，关键业务不变量完全由 PostgreSQL 强制执行。

## 已实现内容

1. **数据库迁移脚本**（`migrations/000001_initial_schema.up.sql`）
   - 定义 `agreement_status`、`event_type` 枚举和统一时间函数 `get_tx_timestamp()`。
   - 建立 `users`、`brokers`、`referrals`、`agreements` 等核心表，并通过局部唯一索引落实公理 A1（单一活跃协议）。
   - 创建不可变 `timeline_events` 日志及对应触发器，覆盖时间完整性与修改禁止（公理 A3 / A4）。
   - 通过 `pii_data` + RLS + `access_pii_data` 函数实现 PII 隔离与访问审计（公理 A2 / A7）。
   - 引入事务性 `outbox` 与 `idempotency` 表以支持外部集成和幂等处理（公理 A5 / A6）。

2. **Go 业务层**
   - `agreement/repository.go`：在单个事务中完成协议生效、时间线追加和 outbox 入队。
   - `agreement/service.go`：处理 e-sign webhook，包含幂等校验、事务管理和核心业务调用。
   - `db/conn.go`：提供 `pgxpool` 连接池构建函数。
   - `cmd/api/main.go`：演示如何初始化连接池与 `agreement.Service`。

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

### 运行单元测试

默认构建缓存目录若不可写，可先创建本地缓存目录：

```bash
mkdir -p .gocache
GOCACHE=$(pwd)/.gocache go test ./...
```

### 运行示例程序

```bash
export DATABASE_URL="postgres://user:pass@localhost:5432/brokerflow?sslmode=disable"
go run ./cmd/api
```

程序目前主要负责依赖注入，后续可以在此基础上接入真实的 HTTP 路由与 webhook 解析。

## 后续建议

1. 编写 Down Migration 以支持回滚。
2. 实现实际的 HTTP handler，将 webhook 请求映射为 `EsignCompletionRequest`。
3. 为 repository 层补充数据库集成测试。
4. 把 `go test ./...` 与迁移检查集成到 CI/CD 流程。
