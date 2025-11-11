-- 000001_base.up.sql
-- Combined baseline schema for BrokerFlow.

BEGIN;

REVOKE CREATE ON SCHEMA public FROM PUBLIC;

-- CREATE EXTENSION IF NOT EXISTS pgcrypto;

DO $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM pg_type WHERE typname = 'agreement_status') THEN
        CREATE TYPE agreement_status AS ENUM (
            'draft',
            'pending_signature',
            'effective',
            'success',
            'void',
            'disputed',
            'closed'
        );
    END IF;
END;
$$;

DO $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM pg_type WHERE typname = 'event_type') THEN
        CREATE TYPE event_type AS ENUM (
            'AGREEMENT_CREATED',
            'ESIGN_REQUESTED',
            'ESIGN_COMPLETED',
            'PII_VIEWED',
            'CLIENT_CONTACTED',
            'PROPERTY_SHOWN',
            'OFFER_MADE',
            'DEAL_CLOSED',
            'CORRECTION_ADDED'
        );
    END IF;
END;
$$;

DROP FUNCTION IF EXISTS get_tx_timestamp() CASCADE;

CREATE OR REPLACE FUNCTION get_tx_timestamp() RETURNS TIMESTAMPTZ AS $$
BEGIN
    RETURN transaction_timestamp();
END;
$$ LANGUAGE plpgsql;

DROP FUNCTION IF EXISTS update_updated_at_column() CASCADE;

CREATE OR REPLACE FUNCTION update_updated_at_column() RETURNS TRIGGER AS $$
BEGIN
    NEW.updated_at := get_tx_timestamp();
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

ALTER TABLE IF EXISTS brokers ALTER COLUMN created_at SET DEFAULT get_tx_timestamp();
ALTER TABLE IF EXISTS users ALTER COLUMN created_at SET DEFAULT get_tx_timestamp();
ALTER TABLE IF EXISTS users ALTER COLUMN updated_at SET DEFAULT get_tx_timestamp();
ALTER TABLE IF EXISTS referrals ALTER COLUMN created_at SET DEFAULT get_tx_timestamp();
ALTER TABLE IF EXISTS referral_requests ALTER COLUMN created_at SET DEFAULT get_tx_timestamp();
ALTER TABLE IF EXISTS referral_requests ALTER COLUMN updated_at SET DEFAULT get_tx_timestamp();
ALTER TABLE IF EXISTS referral_matches ALTER COLUMN created_at SET DEFAULT get_tx_timestamp();
ALTER TABLE IF EXISTS agreements ALTER COLUMN created_at SET DEFAULT get_tx_timestamp();
ALTER TABLE IF EXISTS agreements ALTER COLUMN updated_at SET DEFAULT get_tx_timestamp();
ALTER TABLE IF EXISTS agreements ALTER COLUMN status_updated_at SET DEFAULT get_tx_timestamp();
ALTER TABLE IF EXISTS agreements_region_audit ALTER COLUMN changed_at SET DEFAULT get_tx_timestamp();
ALTER TABLE IF EXISTS timeline_events ALTER COLUMN created_at SET DEFAULT get_tx_timestamp();
ALTER TABLE IF EXISTS timeline_events ALTER COLUMN ts SET DEFAULT get_tx_timestamp();
ALTER TABLE IF EXISTS pii_contacts ALTER COLUMN created_at SET DEFAULT get_tx_timestamp();
ALTER TABLE IF EXISTS audit_logs ALTER COLUMN ts SET DEFAULT get_tx_timestamp();
ALTER TABLE IF EXISTS outbox ALTER COLUMN created_at SET DEFAULT get_tx_timestamp();
ALTER TABLE IF EXISTS idempotency ALTER COLUMN created_at SET DEFAULT get_tx_timestamp();
ALTER TABLE IF EXISTS edge_invocations ALTER COLUMN first_attempt_at SET DEFAULT get_tx_timestamp();
ALTER TABLE IF EXISTS edge_invocations ALTER COLUMN last_attempt_at SET DEFAULT get_tx_timestamp();
ALTER TABLE IF EXISTS invoices ALTER COLUMN created_at SET DEFAULT get_tx_timestamp();
ALTER TABLE IF EXISTS disputes ALTER COLUMN created_at SET DEFAULT get_tx_timestamp();
ALTER TABLE IF EXISTS disputes ALTER COLUMN updated_at SET DEFAULT get_tx_timestamp();

CREATE TABLE IF NOT EXISTS brokers (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name TEXT NOT NULL UNIQUE,
    fein TEXT NOT NULL UNIQUE,
    verified BOOLEAN NOT NULL DEFAULT FALSE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT get_tx_timestamp()
);

CREATE TABLE IF NOT EXISTS users (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    email TEXT NOT NULL UNIQUE,
    full_name TEXT NOT NULL,
    password_hash TEXT,
    phone TEXT,
    languages TEXT[] NOT NULL DEFAULT '{}'::text[],
    role TEXT NOT NULL DEFAULT 'agent',
    broker_id UUID REFERENCES brokers(id),
    rating NUMERIC(3,2) NOT NULL DEFAULT 0 CHECK (rating >= 0 AND rating <= 5),
    created_at TIMESTAMPTZ NOT NULL DEFAULT get_tx_timestamp(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT get_tx_timestamp()
);

CREATE INDEX IF NOT EXISTS idx_users_email ON users(email);

DROP TRIGGER IF EXISTS trg_users_updated_at ON users;

CREATE TRIGGER trg_users_updated_at
BEFORE UPDATE ON users
FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();

CREATE TABLE IF NOT EXISTS referrals (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    created_by_user_id UUID NOT NULL REFERENCES users(id),
    status TEXT NOT NULL DEFAULT 'open',
    cancel_reason TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT get_tx_timestamp()
);

CREATE TABLE IF NOT EXISTS referral_requests (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    created_by_user_id UUID NOT NULL REFERENCES users(id),
    region TEXT[] NOT NULL,
    price_min BIGINT NOT NULL CHECK (price_min > 0),
    price_max BIGINT NOT NULL CHECK (price_max > 0 AND price_max > price_min),
    property_type TEXT NOT NULL,
    deal_type TEXT NOT NULL,
    languages TEXT[] NOT NULL DEFAULT '{}'::text[],
    sla_hours INTEGER NOT NULL CHECK (sla_hours > 0),
    status TEXT NOT NULL DEFAULT 'open',
    cancel_reason TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT get_tx_timestamp(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT get_tx_timestamp()
);

CREATE INDEX IF NOT EXISTS idx_referral_requests_creator ON referral_requests(created_by_user_id);
CREATE INDEX IF NOT EXISTS idx_referral_requests_status ON referral_requests(status);
CREATE INDEX IF NOT EXISTS idx_referral_requests_deal_type ON referral_requests(deal_type);
CREATE INDEX IF NOT EXISTS idx_referral_requests_created_at ON referral_requests(created_at DESC);
CREATE INDEX IF NOT EXISTS idx_referral_requests_region ON referral_requests USING GIN(region);
CREATE INDEX IF NOT EXISTS idx_referral_requests_languages ON referral_requests USING GIN(languages);

DROP TRIGGER IF EXISTS trg_referral_requests_updated ON referral_requests;

CREATE TRIGGER trg_referral_requests_updated
BEFORE UPDATE ON referral_requests
FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();

DO $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM pg_type WHERE typname = 'referral_match_state') THEN
        CREATE TYPE referral_match_state AS ENUM ('invited','accepted','declined');
    END IF;
END;
$$;

CREATE TABLE IF NOT EXISTS referral_matches (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    request_id UUID NOT NULL REFERENCES referral_requests(id) ON DELETE CASCADE,
    candidate_user_id UUID NOT NULL REFERENCES users(id),
    state referral_match_state NOT NULL DEFAULT 'invited',
    score NUMERIC(4,2) NOT NULL DEFAULT 0 CHECK (score >= 0 AND score <= 1),
    created_at TIMESTAMPTZ NOT NULL DEFAULT get_tx_timestamp(),
    UNIQUE (request_id, candidate_user_id)
);

CREATE INDEX IF NOT EXISTS idx_referral_matches_request ON referral_matches(request_id);

CREATE TABLE IF NOT EXISTS agreements (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    referral_id UUID NOT NULL REFERENCES referral_requests(id),
    from_broker_id UUID NOT NULL REFERENCES brokers(id),
    to_broker_id UUID NOT NULL REFERENCES brokers(id),
    status agreement_status NOT NULL DEFAULT 'draft',
    effective_at TIMESTAMPTZ,
    pii_first_access_time TIMESTAMPTZ,
    version INTEGER NOT NULL DEFAULT 1,
    created_at TIMESTAMPTZ NOT NULL DEFAULT get_tx_timestamp(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT get_tx_timestamp(),
    region TEXT NOT NULL DEFAULT 'us-ea',
    fee_rate NUMERIC(5,2) NOT NULL DEFAULT 0,
    protect_days INTEGER NOT NULL DEFAULT 0,
    status_updated_at TIMESTAMPTZ NOT NULL DEFAULT get_tx_timestamp(),
    status_updated_by UUID,
    event_seq BIGINT NOT NULL DEFAULT 0,
    CONSTRAINT chk_agreement_effective_at_pair CHECK (
        (status IN ('effective','success','disputed') AND effective_at IS NOT NULL)
        OR
        (status IN ('draft','pending_signature','void','closed') AND effective_at IS NULL)
    )
);

ALTER TABLE agreements
    ADD COLUMN IF NOT EXISTS fee_rate NUMERIC(5,2) NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS protect_days INTEGER NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS status_updated_at TIMESTAMPTZ NOT NULL DEFAULT get_tx_timestamp(),
    ADD COLUMN IF NOT EXISTS status_updated_by UUID,
    ADD COLUMN IF NOT EXISTS event_seq BIGINT NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS effective_at TIMESTAMPTZ;

UPDATE agreements
SET effective_at = COALESCE(effective_at, get_tx_timestamp())
WHERE status IN ('effective','success','disputed') AND effective_at IS NULL;

UPDATE agreements
SET effective_at = NULL
WHERE status IN ('draft','pending_signature','void','closed') AND effective_at IS NOT NULL;

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1
        FROM pg_constraint
        WHERE conname = 'chk_agreement_effective_at_pair'
          AND conrelid = 'agreements'::regclass
    ) THEN
        ALTER TABLE agreements
            ADD CONSTRAINT chk_agreement_effective_at_pair CHECK (
                (status IN ('effective','success','disputed') AND effective_at IS NOT NULL)
                OR
                (status IN ('draft','pending_signature','void','closed') AND effective_at IS NULL)
            );
    END IF;
END;
$$;

DROP INDEX IF EXISTS agreements_one_active_per_referral;
CREATE UNIQUE INDEX IF NOT EXISTS agreements_one_active_per_referral
    ON agreements(referral_id)
    WHERE status IN ('pending_signature','effective');

CREATE TABLE IF NOT EXISTS agreements_region_audit (
    id BIGSERIAL PRIMARY KEY,
    agreement_id UUID NOT NULL,
    old_region TEXT NOT NULL,
    new_region TEXT NOT NULL,
    changed_at TIMESTAMPTZ NOT NULL DEFAULT get_tx_timestamp()
);

CREATE TABLE IF NOT EXISTS timeline_events (
    id BIGSERIAL PRIMARY KEY,
    agreement_id UUID NOT NULL REFERENCES agreements(id),
    seq BIGINT,
    type event_type NOT NULL,
    payload JSONB NOT NULL DEFAULT '{}'::jsonb,
    payload_version SMALLINT NOT NULL DEFAULT 1,
    actor_id UUID REFERENCES users(id),
    actor_broker_id UUID,
    created_at TIMESTAMPTZ NOT NULL DEFAULT get_tx_timestamp(),
    ts TIMESTAMPTZ NOT NULL DEFAULT get_tx_timestamp(),
    CONSTRAINT timeline_events_agreement_id_seq_unique UNIQUE(agreement_id, seq)
);

ALTER TABLE timeline_events
    ADD COLUMN IF NOT EXISTS actor_broker_id UUID;

DROP TRIGGER IF EXISTS timeline_seq ON timeline_events;
DROP FUNCTION IF EXISTS trg_event_seq() CASCADE;
DROP FUNCTION IF EXISTS next_event_seq(UUID) CASCADE;

CREATE OR REPLACE FUNCTION next_event_seq(p_agreement UUID)
RETURNS BIGINT LANGUAGE plpgsql AS $$
DECLARE
    v BIGINT;
BEGIN
    UPDATE agreements
       SET event_seq = event_seq + 1
     WHERE id = p_agreement
     RETURNING event_seq INTO v;
    RETURN v;
END;
$$;

CREATE OR REPLACE FUNCTION trg_event_seq() RETURNS TRIGGER AS $$
BEGIN
    IF NEW.seq IS NULL THEN
        NEW.seq := next_event_seq(NEW.agreement_id);
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER timeline_seq
BEFORE INSERT ON timeline_events
FOR EACH ROW EXECUTE FUNCTION trg_event_seq();

DROP FUNCTION IF EXISTS trg_guard_timeline_writer() CASCADE;

CREATE OR REPLACE FUNCTION trg_guard_timeline_writer() RETURNS TRIGGER AS $$
DECLARE
    caller_broker TEXT;
BEGIN
    caller_broker := NULLIF(current_setting('app.broker_id', true), '');
    IF caller_broker IS NULL THEN
        RAISE EXCEPTION 'broker context required';
    END IF;

    IF NOT EXISTS (
        SELECT 1 FROM agreements a
        WHERE a.id = NEW.agreement_id
          AND caller_broker::uuid IN (a.from_broker_id, a.to_broker_id)
    ) THEN
        RAISE EXCEPTION 'unauthorized to append timeline for this agreement';
    END IF;

    NEW.actor_broker_id := caller_broker::uuid;

    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS trg_guard_timeline_writer ON timeline_events;
CREATE TRIGGER trg_guard_timeline_writer
BEFORE INSERT ON timeline_events
FOR EACH ROW EXECUTE FUNCTION trg_guard_timeline_writer();

DROP FUNCTION IF EXISTS prevent_event_mutation() CASCADE;

CREATE OR REPLACE FUNCTION prevent_event_mutation() RETURNS TRIGGER AS $$
BEGIN
    RAISE EXCEPTION 'timeline_events are immutable';
END;
$$ LANGUAGE plpgsql;

DROP FUNCTION IF EXISTS check_temporal_integrity() CASCADE;

CREATE OR REPLACE FUNCTION check_temporal_integrity() RETURNS TRIGGER AS $$
DECLARE
    agreement_eff_time TIMESTAMPTZ;
    agreement_state agreement_status;
    event_timestamp TIMESTAMPTZ;
BEGIN
    PERFORM pg_advisory_xact_lock(hashtext(NEW.agreement_id::text));
    event_timestamp := COALESCE(NEW.ts, get_tx_timestamp());

    SELECT effective_at, status INTO agreement_eff_time, agreement_state
    FROM agreements WHERE id = NEW.agreement_id FOR UPDATE;

    IF NEW.type IN ('OFFER_MADE','ESIGN_COMPLETED','DEAL_CLOSED') THEN
        IF agreement_state NOT IN ('effective','success','disputed') THEN
            RAISE EXCEPTION 'Temporal violation: agreement % state % cannot accept event %', NEW.agreement_id, agreement_state, NEW.type;
        END IF;
        IF agreement_eff_time IS NULL OR event_timestamp < agreement_eff_time THEN
            RAISE EXCEPTION 'Temporal violation: event precedes effective time for agreement %', NEW.agreement_id;
        END IF;
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS trg_check_temporal_integrity ON timeline_events;

CREATE TRIGGER trg_check_temporal_integrity
BEFORE INSERT ON timeline_events
FOR EACH ROW EXECUTE FUNCTION check_temporal_integrity();

DROP TRIGGER IF EXISTS trg_prevent_event_mutation ON timeline_events;

CREATE TRIGGER trg_prevent_event_mutation
BEFORE UPDATE OR DELETE ON timeline_events
FOR EACH ROW EXECUTE FUNCTION prevent_event_mutation();

CREATE TABLE IF NOT EXISTS pii_data (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    referral_id UUID NOT NULL UNIQUE REFERENCES referrals(id),
    client_name TEXT NOT NULL,
    client_email TEXT NOT NULL,
    client_phone TEXT
);

CREATE TABLE IF NOT EXISTS pii_contacts (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    agreement_id UUID NOT NULL UNIQUE REFERENCES agreements(id) ON DELETE CASCADE,
    client_name TEXT NOT NULL,
    client_email TEXT NOT NULL,
    client_phone TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT get_tx_timestamp()
);

CREATE TABLE IF NOT EXISTS audit_logs (
    id BIGSERIAL PRIMARY KEY,
    agreement_id UUID,
    actor_id UUID,
    action TEXT NOT NULL,
    metadata JSONB,
    ts TIMESTAMPTZ NOT NULL DEFAULT get_tx_timestamp()
);

DROP FUNCTION IF EXISTS audit_pii_access(UUID, UUID, JSONB) CASCADE;

CREATE OR REPLACE FUNCTION audit_pii_access(p_agreement_id UUID, p_actor UUID, p_metadata JSONB)
RETURNS VOID LANGUAGE plpgsql AS $$
BEGIN
    INSERT INTO audit_logs(agreement_id, actor_id, action, metadata, ts)
    VALUES (p_agreement_id, p_actor, 'PII_READ', p_metadata, get_tx_timestamp());
END;
$$;

DROP FUNCTION IF EXISTS can_view_pii(UUID) CASCADE;

CREATE OR REPLACE FUNCTION can_view_pii(p_referral_id UUID)
RETURNS BOOLEAN LANGUAGE plpgsql STABLE AS $$
BEGIN
    RETURN EXISTS (
        SELECT 1 FROM agreements
        WHERE referral_id = p_referral_id
          AND status = 'effective'
    );
END;
$$;

DROP FUNCTION IF EXISTS get_pii_contact(UUID, UUID) CASCADE;

CREATE OR REPLACE FUNCTION get_pii_contact(p_agreement_id UUID, p_actor UUID)
RETURNS TABLE(client_name TEXT, client_phone TEXT, client_email TEXT)
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = pg_catalog, public
AS $$
DECLARE
    ag RECORD;
BEGIN
    PERFORM set_config('row_security', 'on', true);

    SELECT id, status, effective_at, pii_first_access_time INTO ag
    FROM agreements WHERE id = p_agreement_id;

    IF ag.id IS NULL THEN
        RAISE EXCEPTION 'Agreement % not found', p_agreement_id;
    END IF;
    IF ag.status <> 'effective' THEN
        RAISE EXCEPTION 'Agreement % not effective', p_agreement_id;
    END IF;
    IF ag.effective_at IS NULL OR get_tx_timestamp() < ag.effective_at THEN
        RAISE EXCEPTION 'Agreement % not yet effective at current time', p_agreement_id;
    END IF;

    UPDATE agreements
    SET pii_first_access_time = get_tx_timestamp()
    WHERE id = p_agreement_id AND pii_first_access_time IS NULL;

    PERFORM audit_pii_access(p_agreement_id, p_actor, jsonb_build_object('source','get_pii_contact'));

    RETURN QUERY
    SELECT c.client_name, c.client_phone, c.client_email
    FROM pii_contacts c
    WHERE c.agreement_id = p_agreement_id;
END;
$$;

ALTER TABLE pii_data ENABLE ROW LEVEL SECURITY;
ALTER TABLE pii_contacts ENABLE ROW LEVEL SECURITY;
ALTER TABLE pii_contacts FORCE ROW LEVEL SECURITY;

REVOKE ALL ON pii_contacts FROM PUBLIC;
DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM pg_policies WHERE schemaname = CURRENT_SCHEMA AND tablename = 'pii_contacts' AND policyname = 'pii_contacts_deny_all') THEN
        EXECUTE 'DROP POLICY pii_contacts_deny_all ON pii_contacts';
    END IF;
END;
$$;

CREATE POLICY pii_contacts_deny_all ON pii_contacts USING (false) WITH CHECK (false);

DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM pg_policies WHERE schemaname = CURRENT_SCHEMA AND tablename = 'pii_data' AND policyname = 'policy_pii_access') THEN
        EXECUTE 'DROP POLICY policy_pii_access ON pii_data';
    END IF;
END;
$$;

CREATE POLICY policy_pii_access ON pii_data
    FOR SELECT USING (can_view_pii(referral_id));

CREATE TABLE IF NOT EXISTS outbox (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    topic TEXT NOT NULL,
    payload JSONB,
    status TEXT NOT NULL DEFAULT 'pending',
    attempts INTEGER NOT NULL DEFAULT 0,
    last_attempt TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT get_tx_timestamp()
);

DROP FUNCTION IF EXISTS outbox_notify() CASCADE;

CREATE OR REPLACE FUNCTION outbox_notify()
RETURNS TRIGGER LANGUAGE plpgsql AS $$
BEGIN
    PERFORM pg_notify('outbox_new', row_to_json(NEW)::text);
    RETURN NEW;
END;
$$;

DROP TRIGGER IF EXISTS trg_outbox_notify ON outbox;

CREATE TRIGGER trg_outbox_notify
AFTER INSERT ON outbox
FOR EACH ROW EXECUTE FUNCTION outbox_notify();

CREATE TABLE IF NOT EXISTS idempotency (
    key TEXT PRIMARY KEY,
    created_at TIMESTAMPTZ NOT NULL DEFAULT get_tx_timestamp()
);

CREATE TABLE IF NOT EXISTS edge_invocations (
    key TEXT NOT NULL,
    route TEXT NOT NULL,
    status TEXT NOT NULL DEFAULT 'pending',
    first_attempt_at TIMESTAMPTZ NOT NULL DEFAULT get_tx_timestamp(),
    last_attempt_at TIMESTAMPTZ NOT NULL DEFAULT get_tx_timestamp(),
    response_code INTEGER,
    error TEXT,
    PRIMARY KEY (route, key)
);

CREATE INDEX IF NOT EXISTS idx_outbox_pending ON outbox (status, created_at) WHERE status = 'pending';
CREATE INDEX IF NOT EXISTS idx_edge_invocations_completed ON edge_invocations (status, last_attempt_at) WHERE status = 'completed';

ALTER TABLE timeline_events
    DROP CONSTRAINT IF EXISTS fk_timeline_agreement;

ALTER TABLE timeline_events
    ADD CONSTRAINT fk_timeline_agreement
    FOREIGN KEY (agreement_id) REFERENCES agreements(id) ON DELETE RESTRICT;

DROP FUNCTION IF EXISTS forbid_agreement_delete() CASCADE;

CREATE OR REPLACE FUNCTION forbid_agreement_delete() RETURNS trigger AS $$
BEGIN
    RAISE EXCEPTION 'agreements are non-deletable';
END;
$$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS no_delete_agreements ON agreements;
CREATE TRIGGER no_delete_agreements
BEFORE DELETE ON agreements
FOR EACH ROW EXECUTE FUNCTION forbid_agreement_delete();

DROP FUNCTION IF EXISTS forbid_audit_mutation() CASCADE;

CREATE OR REPLACE FUNCTION forbid_audit_mutation() RETURNS trigger AS $$
BEGIN
    RAISE EXCEPTION 'audit_logs are append-only';
END;
$$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS audit_no_update ON audit_logs;
CREATE TRIGGER audit_no_update
BEFORE UPDATE ON audit_logs
FOR EACH ROW EXECUTE FUNCTION forbid_audit_mutation();

DROP TRIGGER IF EXISTS audit_no_delete ON audit_logs;
CREATE TRIGGER audit_no_delete
BEFORE DELETE ON audit_logs
FOR EACH ROW EXECUTE FUNCTION forbid_audit_mutation();

CREATE TABLE IF NOT EXISTS invoices (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    agreement_id UUID NOT NULL REFERENCES agreements(id) ON DELETE CASCADE,
    amount NUMERIC(12,2) NOT NULL DEFAULT 0,
    status TEXT NOT NULL DEFAULT 'open',
    cancel_reason TEXT,
    is_invalidated BOOLEAN NOT NULL DEFAULT FALSE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT get_tx_timestamp()
);

CREATE TABLE IF NOT EXISTS disputes (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    agreement_id UUID NOT NULL REFERENCES agreements(id) ON DELETE CASCADE,
    status TEXT NOT NULL DEFAULT 'under_review',
    created_at TIMESTAMPTZ NOT NULL DEFAULT get_tx_timestamp(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT get_tx_timestamp(),
    resolved_at TIMESTAMPTZ
);

DROP FUNCTION IF EXISTS agreement_validate_transition(agreement_status, agreement_status) CASCADE;

CREATE OR REPLACE FUNCTION agreement_validate_transition(prev agreement_status, next agreement_status)
RETURNS BOOLEAN LANGUAGE plpgsql AS $$
BEGIN
    IF prev = next THEN
        RETURN TRUE;
    END IF;

    IF prev = 'draft' AND next IN ('pending_signature', 'void') THEN
        RETURN TRUE;
    END IF;

    IF prev = 'pending_signature' AND next IN ('effective', 'void') THEN
        RETURN TRUE;
    END IF;

    IF prev = 'effective' AND next IN ('success', 'disputed', 'void', 'closed') THEN
        RETURN TRUE;
    END IF;

    IF prev = 'disputed' AND next IN ('void', 'closed') THEN
        RETURN TRUE;
    END IF;

    IF prev = 'success' AND next = 'closed' THEN
        RETURN TRUE;
    END IF;

    IF prev = 'void' AND next = 'closed' THEN
        RETURN TRUE;
    END IF;

    RETURN FALSE;
END;
$$;

DROP FUNCTION IF EXISTS disputes_resolve_guard() CASCADE;

CREATE OR REPLACE FUNCTION disputes_resolve_guard()
RETURNS TRIGGER LANGUAGE plpgsql AS $$
BEGIN
    IF OLD.status <> 'resolved' AND NEW.status = 'resolved' THEN
        UPDATE agreements SET status = 'disputed'
        WHERE id = NEW.agreement_id AND status <> 'disputed';

        UPDATE invoices
        SET is_invalidated = TRUE
        WHERE agreement_id = NEW.agreement_id
          AND status NOT IN ('paid','written_off');

        NEW.resolved_at := get_tx_timestamp();
    END IF;

    NEW.updated_at := get_tx_timestamp();
    RETURN NEW;
END;
$$;

DROP TRIGGER IF EXISTS trg_disputes_resolve ON disputes;

CREATE TRIGGER trg_disputes_resolve
BEFORE UPDATE ON disputes
FOR EACH ROW EXECUTE FUNCTION disputes_resolve_guard();

DROP FUNCTION IF EXISTS enforce_agreement_region_immutable() CASCADE;

CREATE OR REPLACE FUNCTION enforce_agreement_region_immutable()
RETURNS TRIGGER LANGUAGE plpgsql AS $$
BEGIN
    IF NEW.region <> OLD.region THEN
        INSERT INTO agreements_region_audit(agreement_id, old_region, new_region)
        VALUES (OLD.id, OLD.region, NEW.region);
        RAISE EXCEPTION 'Agreement region is immutable';
    END IF;
    RETURN NEW;
END;
$$;

DROP TRIGGER IF EXISTS trg_agreements_region_immutable ON agreements;

CREATE TRIGGER trg_agreements_region_immutable
BEFORE UPDATE ON agreements
FOR EACH ROW EXECUTE FUNCTION enforce_agreement_region_immutable();

DROP TRIGGER IF EXISTS trg_agreements_updated ON agreements;

CREATE TRIGGER trg_agreements_updated
BEFORE UPDATE ON agreements
FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();

COMMIT;

ALTER TABLE users ADD COLUMN IF NOT EXISTS role TEXT NOT NULL DEFAULT 'agent';
ALTER TABLE referral_requests ADD COLUMN IF NOT EXISTS cancel_reason TEXT;
