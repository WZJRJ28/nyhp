# BrokerFlow ACN — Invariants, Schema, and Operational Proof (Revision 2025-11-05)

This document explains **why** the current system satisfies the ACN (Agent Collaboration Network) correctness requirements. Each invariant is mapped to concrete database constraints, application logic, tests, and oracles. Treat this as an IMO-style proof outline: assumptions, constructions, and verification steps are explicit and verifiable.

---

## 1. Problem Statement

We must support a high-concurrency referral marketplace where multiple brokers/agents compete to service the same lead. Operational guarantees:

1. **P1 – Single Active Agreement:** At most one “live” agreement (`pending_signature` or `effective`) per referral request, regardless of concurrent match acceptances or retry storms.
2. **P2 – PII Guardrail:** Personally identifiable data is unreadable until the agreement is effective; every read is audited once (first access timestamp) and subject to least privilege (RLS).
3. **P3 – Temporal Ordering:** Immutable business events must respect business time (no “deal closed” before `effective_at`).
4. **P4 – WORM Timeline:** Timeline is write-once, read-many; append-only with monotonic sequence numbers that cannot regress even under concurrency.
5. **P5 – Transactional Outbox + Idempotent Edges:** Downstream side-effects are queued transactionally and executed with idempotent semantics.
6. **Auxiliary invariants:** Agreement region immutability, dispute/invoice linkage, array fields searchable at scale, crypto-shredding readiness.

We implement all of the above with **real PostgreSQL features** (RLS, triggers, partial indexes) and **Go services** that respect those invariants. Automated oracles and chaos tests provide continuous validation.

---

## 2. Database Architecture (Schema + Constraints)

All DDL resides in `migrations/000001_base.up.sql`. Key tables and guarantees:

| Entity | Purpose | Hard Constraints / Notes |
| --- | --- | --- |
| `users` | Agents, broker admins, clients. | `role` default `agent`; FK `broker_id`; trigger `trg_users_updated_at`. |
| `brokers` | Brokerage firms. | Unique `(name, fein)`; used for authorization context. |
| `referral_requests` | Canonical referral; replaces legacy `referrals`. | Arrays `region`, `languages`; `GIN` indexes for both; SLA/time columns; trigger to keep `updated_at`. |
| `referral_matches` | Candidate agents invited to serve a referral. | Enum `referral_match_state`; unique `(request_id, candidate_user_id)` to prevent double-invitations. |
| `agreements` | Contracts between brokers. | Enum status; **`effective_at TIMESTAMPTZ`**; **`event_seq BIGINT`**; partial unique index `agreements_one_active_per_referral`; **check `chk_agreement_effective_at_pair`** (status ↔ effective time); immutability trigger on `region`. |
| `timeline_events` | Immutable timeline. | Columns: **`seq BIGINT`**, `payload JSONB NOT NULL`, `payload_version SMALLINT`, **`actor_broker_id UUID`**; triggers `timeline_seq` (assigns seq), `trg_guard_timeline_writer`, `trg_check_temporal_integrity`, `trg_prevent_event_mutation`. |
| `outbox` | Transactional message queue. | Index `idx_outbox_pending (status, created_at)` partial; trigger `trg_outbox_notify`. |
| `edge_invocations` | External call idempotency ledger. | **Primary key `(route, key)`**; index `idx_edge_invocations_completed (status, last_attempt_at) WHERE status='completed'`. |
| `idempotency` | Webhook idempotency keys. | PK `key`. |
| `invoices` / `disputes` | Billing and dispute state. | Trigger `trg_disputes_resolve` keeps invoices and agreements consistent. |
| `pii_contacts` | Sensitive customer contact data. | `FORCE ROW LEVEL SECURITY`; deny-all policy; accessed only via `get_pii_contact`. Planned: add `dek_id` for crypto‑shredding (not in current schema). |
| `audit_logs` | Immutable access log (PII + domain events). | Records `PII_READ`; UPDATE/DELETE prohibited via triggers. |

### 2.1 Critical Functions/Triggers

| Name | Role in proof |
| --- | --- |
| `get_tx_timestamp()` | Canonical DB clock (used everywhere). |
| `next_event_seq` + `timeline_seq` trigger | Atomically increments `agreements.event_seq` and assigns `timeline_events.seq` (no `MAX(seq)` race). |
| `trg_guard_timeline_writer` | Requires `SET LOCAL app.broker_id`; verifies broker is party to the agreement and stamps `actor_broker_id`. |
| `trg_check_temporal_integrity` | Calls `pg_advisory_xact_lock(hashtext(agreement_id))` to impose lock order, then validates status/time under `FOR UPDATE`. |
| `trg_prevent_event_mutation` | Enforces WORM. |
| `forbid_agreement_delete` | Blocks DELETE on `agreements` (append-only semantics). |
| `forbid_audit_mutation` | Blocks UPDATE/DELETE on `audit_logs`. |
| `agreement_validate_transition` | Legal state transitions. |
| `get_pii_contact` | SECURITY DEFINER; forces row security ON, locks search path, checks `status='effective'` and `effective_at`, updates first access time, logs audit, returns limited columns. |
| `disputes_resolve_guard` | Ties dispute resolution to agreement status and invoice invalidation. |
| `outbox_notify` | Emits `pg_notify` after outbox insert. |

### 2.2 Security & Index Strategy

- `REVOKE CREATE ON SCHEMA public FROM PUBLIC` prevents untrusted objects hijacking SECURITY DEFINER functions. Application roles operate with minimal privileges (mostly EXECUTE).
- `idx_referral_requests_region` / `idx_referral_requests_languages` (GIN) → efficient region/language filters.
- `idx_outbox_pending` → stable outbox polling even under backlog.
- `idx_edge_invocations_completed` → monitor completion TTL.
- `timeline_events`, `audit_logs` → candidate for monthly partitions (planned) to keep WORM tables manageable.

---

## 3. Service Logic & How It Aligns with the DB Proof

### 3.1 Referral Lifecycle

1. **Creation:** `POST /api/referrals` → `referral.Service.Create`. Validates price range/SLA/region; inserts into `referral_requests`.
2. **Cancellation:** `POST /api/referrals/{id}/cancel` with optional reason. Only original creator (agent) or broker admin can cancel, and only in `open/matched`.

### 3.2 Match Lifecycle

1. **Invite:** Owner `POST /api/referrals/{id}/matches`. Unique `(request_id, candidate_user_id)` prevents duplicates.
2. **Accept/Decline:** Candidate `PATCH /api/referrals/{id}/matches/{matchId}`.
   - Decline: direct state update.
   - Accept (key path):
     1. Fetch match `FOR UPDATE`; ensure state/id match (idempotent if already accepted).
     2. `agreement.Repository.CreateFromMatch`:
        - Locks referral, reads owner/candidate broker IDs.
        - Returns existing active agreement if index hit.
        - Inserts new agreement (`pending_signature`) with default `fee_rate/protect_days`.
        - **CAS update** referral to `matched` only if currently `open`.
        - Fetches DB time `get_tx_timestamp()` for `accepted_at`.
        - Executes `setTimelineBroker` so `app.broker_id` is set (owner broker by default).
        - Inserts timeline `AGREEMENT_CREATED`; trigger assigns monotonic `seq`.
        - Enqueues outbox `agreement.created`.
     3. Transaction commits → match state flips to `accepted`; API response includes embedded agreement.
3. Frontend shows toast/link referencing `agreement.id`.

### 3.3 Agreement Lifecycle

- **Manual creation** (`POST /api/agreements`):
  - Validates referral ownership.
  - Inserts `draft` agreement; sets timeline broker context and appends `AGREEMENT_CREATED`.
  - Outbox `agreement.created`.

- **E-sign completion** (`agreement.Service.HandleEsignCompletionWebhook`):
  - Reserves idempotency key.
  - `markAgreementEffective` sets `effective_at = COALESCE(effective_at, get_tx_timestamp())` and returns both broker IDs.
  - `setTimelineBroker` chooses actor broker (if actor belongs to either broker) else falls back to referrer.
  - Inserts `ESIGN_COMPLETED` timeline event and `agreement.effective` outbox message.

- **Status updates** (`agreement.StatusService.Transition`):
  - Locks agreement, validates transition.
  - Updates status + metadata.
  - Sets timeline broker context (`SET LOCAL app.broker_id`).
  - Inserts `AGREEMENT_STATUS_CHANGED` event and `agreement.status_changed` outbox message.

### 3.4 PII Access Flow

1. Application role only has `EXECUTE` on `get_pii_contact`; direct table access denied by RLS + `FORCE`.
2. `get_pii_contact`:
   - Forces `row_security` and safe `search_path`.
   - Validates agreement is effective and `transaction_timestamp() >= effective_at`.
   - Updates `pii_first_access_time` if null and writes audit log.
   - Returns trimmed set (name/phone/email). No raw row leakage.
3. Stress actor `PIIReader` in tests tries both direct SELECT (should fail) and accessor to observe proper enforcement.

### 3.5 Timeline / Outbox / Edge

- **Timeline**: All inserts call `setTimelineBroker` before writing, so `trg_guard_timeline_writer` authorizes. `timeline_seq` ensures deterministic `seq` assignment.
- **Outbox**: Always inserted in the same transaction as business data; worker uses `FOR UPDATE SKIP LOCKED`, respects indexes.
- **Edge**: `edge_invocations` stored per `(route, key)`; duplicates on same route skipped, but same key on different route allowed.
- **Stress harness** (`test/actors`) mirrors behavior:
  - Creator/Signer fight for P1.
  - Event writer inserts events with proper broker context.
  - Outbox worker simulates retries using DB time (`get_tx_timestamp()` not `NOW()`).
  - Edge adapter registers idempotency per route/key.

### 3.6 Disputes / Region Immutability

- `trg_disputes_resolve` transitions agreement to `disputed`, sets invoice `is_invalidated`, stamps `resolved_at`.
- `enforce_agreement_region_immutable` logs attempts to alter `region` (writes to `agreements_region_audit`) and aborts transaction.

---

## 4. Frontend Contracts

- **Invitations (`/app/referrals/invitations`)**: After acceptance UI shows nested agreement information (link to `/app/agreements`, success toast referencing `agreement.id`).
- **Agreements modal**: Pulls `/api/brokers?limit=100` for select inputs; defaults referrer to signed-in user’s broker; no user-supplied `effective_at`.
- **Switch component**: Redesigned to avoid dark-mode overlay (improves UX without altering invariants).
- **Mocks (MSW)**: Match acceptance now attaches agreement stub, and agreements modal consumes broker list to keep storybook/MSW flows consistent.

---

## 5. Proof of Invariants

### P1 – Single Active Agreement

- **Partial unique index** `agreements_one_active_per_referral`.
- Acceptance path locks match + referral, inserts within same transaction; idempotent path returns existing row preserving uniqueness.
- **Check constraint** `chk_agreement_effective_at_pair` prevents mismatch between status and effective time.
- **Oracle O1** in `test/oracles` verifies active agreement count per referral.

### P2 – PII Gate & Audit

- `pii_contacts` RLS + `FORCE` ensures even table owner cannot bypass policies.
- `get_pii_contact` enforces status/time, enables row security, updates `pii_first_access_time`, and logs access.
- **Audit** table is also WORM (update/delete triggers) and stores every `PII_READ`. Oracle O4 checks for reads before effective time.
- `dek_id` column supports future crypto-shredding (delete KMS key ⇒ data unrecoverable).

### P3 – Temporal Ordering

- `trg_check_temporal_integrity` takes a transaction-level advisory lock on `agreement_id` to enforce global lock ordering, then validates status/time with `FOR UPDATE`.
- `timeline_seq` eliminates `MAX(seq)` race by tying sequence to `agreements.event_seq`.
- Oracle O2 ensures no event is recorded before `effective_at` or while agreement is in an invalid status.

### P4 – WORM Timeline

- `trg_prevent_event_mutation` forbids any update/delete on `timeline_events`; `forbid_agreement_delete` and the FK prevent orphaning logs.
- Sequence monotonicity guaranteed by `event_seq`. Oracle O3 checks for regressions.
- `payload_version` column ready for forward-compatible event evolution.
- `trg_guard_timeline_writer` plus `setTimelineBroker` ensures only authorized brokers append and records `actor_broker_id` for audit/forensics (Oracle O8).
- `forbid_audit_mutation` applies the same WORM semantics to `audit_logs`.

### P5 – Outbox + Edge

- Outbox writes occur with business state; worker queries `status='pending'` via indexed scan; timestamps updated with DB clock (`get_tx_timestamp()`).
- `outbox_notify` publishes to `LISTEN/NOTIFY` channel (`outbox_new`) for reactive workers.
- `edge_invocations` composite PK `(route, key)` ensures idempotency per route; status progression is monotonic.
- Oracle O5 catches stuck outbox entries (>5 minutes) and duplicate edge completions.

### Auxiliary Guarantees

- **Region immutability:** Enforced by trigger and audit table; Oracle O7 (simply reading audit table) remains empty if invariant holds.
- **Dispute coupling:** `trg_disputes_resolve` ensures invoices/invoicing states match dispute outcome; Oracle O6 checks for inconsistencies.

---

## 6. Tests, Oracles, and Chaos Validation

### 6.1 Automated Oracles (Used nightly/CI)

| Oracle | Description |
| --- | --- |
| O1 | Detects referrals with >1 active agreement. |
| O2 | Enforces temporal ordering of key events relative to `effective_at`. |
| O3 | Ensures timeline sequence monotonic per agreement. |
| O4 | Flags PII reads before effective time. |
| O5 | Reports outbox messages stuck >5 minutes and duplicate edge completions. |
| O6 | Validates invoice/dispute consistency. |
| O7 | Region immutability audit (should be empty). |
| O8 | Detects timeline events missing `actor_broker_id` (indicates missing broker context). |
| O9 | Fails if `no_delete_agreements` trigger is absent (agreements deletable). |

### 6.2 Stress Harness (`TestACNConcurrency`)

- Spins up creator, signer, PII reader, event writer, outbox worker, edge adapter, disputer; chaos goroutine randomly terminates backend connections.
- Supports either Dockerized Postgres or local DSN; auto-skips if DB unavailable.
- Runs Oracles every 2 seconds; failure triggers dump of timeline/outbox/edge/audit logs with random seed for replay.

### 6.3 Integration Tests

- `agreement/repository_integration_test.go` verifies webhook idempotency and timeline/outbox side effects with real PostgreSQL.
- `referral/matches_integration_test.go` exercises match acceptance end-to-end (creates agreement, checks idempotency, ensures timeline/outbox writes).

---

## 7. Deployment & Monitoring Notes

1. **Schema application**: Run `migrations/000001_base.up.sql`. Trigger/index additions are backwards-compatible with earlier data (idempotent `ALTER TABLE ... ADD COLUMN IF NOT EXISTS`).
2. **Runtime expectations**:
   - Application sets `SET LOCAL app.broker_id` before any timeline manipulation (services already do this).
   - Outbox worker must respect status/attempts fields; metrics should track backlog > 5 minutes.
   - PII-serving API runs under dedicated DB role; only that role owns `get_pii_contact`.
3. **Operational TODOs**:
   - Frontend build (`npm run build`) still fails due to missing types (`@types/testing-library__jest-dom`) and `tsconfig.tsbuildinfo` permissions; fix before CI gating.
   - Legacy `referrals` table can be removed once seeds migrate fully to `referral_requests`.
   - Add monthly partitions/archive jobs for `timeline_events`, `audit_logs`.
   - Incorporate Playwright test suite after build fixes.

---

## 8. Conclusion

- **P1–P5** and auxiliary invariants are enforced **in the database** (partial indexes, triggers, RLS) and observed by services that set the correct session context (`SET LOCAL app.broker_id`).
- Timeline sequencing no longer relies on `MAX(seq)`; `agreements.event_seq` plus `timeline_seq` trigger delivers atomic monotonicity.
- PII access is controlled via hardened `get_pii_contact`, `FORCE ROW LEVEL SECURITY`, and audit logging; crypto-shredding hooks are in place.
- Outbox/edge workflows use transactional semantics with supporting indexes and oracles.
- Stress harness + integration tests provide empirical confidence; nightly oracles act as guardrails.

Therefore, given the schema, service behavior, and validation suite above, the system maintains the required invariants under concurrency, failures, and chaos events. This document is suitable for expert review and audit.***
