-- 000_all.sql
-- Super Concurrency Test schema enforcing hard invariants under stress.

BEGIN;

CREATE EXTENSION IF NOT EXISTS pgcrypto;

-- Enumerations to constrain states. Keeping as ENUM-like CHECKs for flexibility.
CREATE DOMAIN agreement_state AS TEXT
    CHECK (VALUE IN ('draft','pending_signature','effective','success','void','disputed','closed'));

CREATE DOMAIN timeline_event_type AS TEXT
    CHECK (VALUE IN (
        'agreement_created',
        'offer_made',
        'contract_signed',
        'lease_signed',
        'closed',
        'correction',
        'amendment',
        'pii_viewed',
        'effective_notified',
        'edge_call'
    ));

-- Core tables ---------------------------------------------------------------

CREATE TABLE IF NOT EXISTS users (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    email TEXT NOT NULL UNIQUE,
    full_name TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS brokers (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    legal_name TEXT NOT NULL UNIQUE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS broker_memberships (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    broker_id UUID NOT NULL REFERENCES brokers(id) ON DELETE CASCADE,
    role TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (user_id, broker_id)
);

CREATE TABLE IF NOT EXISTS agent_licenses (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    region TEXT NOT NULL,
    license_no TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (user_id, region)
);

CREATE TABLE IF NOT EXISTS referral_requests (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    requester_id UUID NOT NULL REFERENCES users(id),
    region TEXT NOT NULL,
    status TEXT NOT NULL DEFAULT 'open',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS referral_matches (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    request_id UUID NOT NULL REFERENCES referral_requests(id) ON DELETE CASCADE,
    broker_id UUID NOT NULL REFERENCES brokers(id) ON DELETE CASCADE,
    status TEXT NOT NULL DEFAULT 'offered',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (request_id, broker_id)
);

CREATE TABLE IF NOT EXISTS workflow_definitions (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name TEXT NOT NULL UNIQUE,
    config JSONB NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS agreements (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    request_id UUID NOT NULL REFERENCES referral_requests(id),
    from_broker_id UUID NOT NULL REFERENCES brokers(id),
    to_broker_id UUID NOT NULL REFERENCES brokers(id),
    state agreement_state NOT NULL DEFAULT 'draft',
    region TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    effective_at TIMESTAMPTZ,
    pii_first_access_at TIMESTAMPTZ,
    UNIQUE (id, region)
);

CREATE INDEX IF NOT EXISTS idx_agreements_request_id ON agreements(request_id);

-- P1: uniqueness of active agreements
CREATE UNIQUE INDEX IF NOT EXISTS agreements_one_active_per_request
    ON agreements(request_id)
    WHERE state IN ('pending_signature','effective');

CREATE TABLE IF NOT EXISTS invoices (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    agreement_id UUID NOT NULL REFERENCES agreements(id) ON DELETE CASCADE,
    amount NUMERIC(12,2) NOT NULL,
    status TEXT NOT NULL DEFAULT 'open',
    is_invalidated BOOLEAN NOT NULL DEFAULT FALSE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS timeline_events (
    id BIGSERIAL PRIMARY KEY,
    agreement_id UUID NOT NULL REFERENCES agreements(id) ON DELETE CASCADE,
    seq INTEGER NOT NULL,
    type timeline_event_type NOT NULL,
    payload JSONB,
    actor_id UUID,
    ts TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT timeline_events_unique_seq UNIQUE (agreement_id, seq)
);

CREATE TABLE IF NOT EXISTS audit_logs (
    id BIGSERIAL PRIMARY KEY,
    agreement_id UUID,
    actor_id UUID,
    action TEXT NOT NULL,
    metadata JSONB,
    ts TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS pii_contacts (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    agreement_id UUID NOT NULL UNIQUE REFERENCES agreements(id) ON DELETE CASCADE,
    client_name TEXT NOT NULL,
    client_email TEXT NOT NULL,
    client_phone TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS outbox (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    topic TEXT NOT NULL,
    payload JSONB NOT NULL,
    status TEXT NOT NULL DEFAULT 'pending',
    attempts INTEGER NOT NULL DEFAULT 0,
    last_attempt TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS edge_invocations (
    key TEXT PRIMARY KEY,
    route TEXT NOT NULL,
    status TEXT NOT NULL DEFAULT 'pending',
    first_attempt_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    last_attempt_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    response_code INTEGER,
    error TEXT
);

CREATE TABLE IF NOT EXISTS disputes (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    agreement_id UUID NOT NULL REFERENCES agreements(id) ON DELETE CASCADE,
    status TEXT NOT NULL DEFAULT 'under_review',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    resolved_at TIMESTAMPTZ
);

-- Functions and triggers ----------------------------------------------------

CREATE OR REPLACE FUNCTION set_updated_at()
RETURNS TRIGGER LANGUAGE plpgsql AS $$
BEGIN
    NEW.updated_at := NOW();
    RETURN NEW;
END;
$$;

CREATE OR REPLACE FUNCTION enforce_agreement_region_immutable()
RETURNS TRIGGER LANGUAGE plpgsql AS $$
BEGIN
    IF NEW.region <> OLD.region THEN
        RAISE EXCEPTION 'Agreement region is immutable';
    END IF;
    RETURN NEW;
END;
$$;

CREATE OR REPLACE FUNCTION timeline_set_seq()
RETURNS TRIGGER LANGUAGE plpgsql AS $$
DECLARE
    next_seq INTEGER;
BEGIN
    SELECT seq + 1 INTO next_seq
    FROM timeline_events
    WHERE agreement_id = NEW.agreement_id
    ORDER BY seq DESC
    LIMIT 1
    FOR UPDATE;

    IF next_seq IS NULL THEN
        next_seq := 1;
    END IF;

    IF NEW.seq IS NULL THEN
        NEW.seq := next_seq;
    END IF;

    NEW.seq := next_seq;
    NEW.ts := NOW();
    RETURN NEW;
END;
$$;

CREATE OR REPLACE FUNCTION timeline_prevent_mutation()
RETURNS TRIGGER LANGUAGE plpgsql AS $$
BEGIN
    RAISE EXCEPTION 'timeline_events are append-only';
END;
$$;

CREATE OR REPLACE FUNCTION enforce_timeline_temporal()
RETURNS TRIGGER LANGUAGE plpgsql AS $$
DECLARE
    ag_state agreement_state;
    ag_effective_at TIMESTAMPTZ;
BEGIN
    IF NEW.ts IS NULL THEN
        NEW.ts := NOW();
    END IF;
    SELECT state, effective_at INTO ag_state, ag_effective_at
    FROM agreements
    WHERE id = NEW.agreement_id
    FOR UPDATE;

    IF ag_state IS NULL THEN
        RAISE EXCEPTION 'Agreement % does not exist', NEW.agreement_id;
    END IF;

    IF NEW.type IN ('offer_made','contract_signed','lease_signed','closed') THEN
        IF ag_state NOT IN ('effective','success','disputed') THEN
            RAISE EXCEPTION 'Temporal violation: agreement % state % cannot accept event %', NEW.agreement_id, ag_state, NEW.type;
        END IF;

        IF ag_effective_at IS NULL OR NEW.ts < ag_effective_at THEN
            RAISE EXCEPTION 'Temporal violation: event precedes effective time for agreement %', NEW.agreement_id;
        END IF;
    END IF;

    IF NEW.type = 'correction' THEN
        IF NEW.payload->>'corrected_event_id' IS NULL THEN
            RAISE EXCEPTION 'Correction event must reference corrected_event_id';
        END IF;
        IF NOT EXISTS (
            SELECT 1
            FROM timeline_events te
            WHERE te.id::text = NEW.payload->>'corrected_event_id'
              AND te.agreement_id = NEW.agreement_id
        ) THEN
            RAISE EXCEPTION 'Correction references event outside agreement';
        END IF;
    END IF;

    IF NEW.type = 'amendment' THEN
        IF NEW.payload->>'amendment' IS NULL THEN
            RAISE EXCEPTION 'Amendment event requires amendment payload';
        END IF;
    END IF;

    RETURN NEW;
END;
$$;

CREATE OR REPLACE FUNCTION pii_access_guard()
RETURNS TRIGGER LANGUAGE plpgsql AS $$
BEGIN
    RAISE EXCEPTION 'Direct access to pii_contacts denied';
END;
$$;

CREATE OR REPLACE FUNCTION log_outbox_notify()
RETURNS TRIGGER LANGUAGE plpgsql AS $$
BEGIN
    PERFORM pg_notify('outbox_new', row_to_json(NEW)::text);
    RETURN NEW;
END;
$$;

CREATE OR REPLACE FUNCTION disputes_resolve_guard()
RETURNS TRIGGER LANGUAGE plpgsql AS $$
BEGIN
    IF OLD.status <> 'resolved' AND NEW.status = 'resolved' THEN
        UPDATE agreements SET state = 'disputed', updated_at = NOW()
        WHERE id = NEW.agreement_id AND state <> 'disputed';

        IF NOT EXISTS (
            SELECT 1 FROM agreements WHERE id = NEW.agreement_id AND state = 'disputed'
        ) THEN
            RAISE EXCEPTION 'Agreement % must be disputed before resolving dispute', NEW.agreement_id;
        END IF;

        UPDATE invoices
        SET is_invalidated = TRUE
        WHERE agreement_id = NEW.agreement_id
          AND status NOT IN ('paid','written_off');
        NEW.resolved_at := NOW();
    END IF;
    NEW.updated_at := NOW();
    RETURN NEW;
END;
$$;

CREATE OR REPLACE FUNCTION audit_pii_access(p_agreement_id UUID, p_actor UUID, p_metadata JSONB)
RETURNS VOID LANGUAGE plpgsql AS $$
BEGIN
    INSERT INTO audit_logs(agreement_id, actor_id, action, metadata, ts)
    VALUES (p_agreement_id, p_actor, 'PII_READ', p_metadata, NOW());
END;
$$;

CREATE OR REPLACE FUNCTION get_pii_contact(p_agreement_id UUID, p_actor UUID)
RETURNS SETOF pii_contacts
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = public
AS $$
DECLARE
    ag RECORD;
BEGIN
    SELECT id, state, effective_at, pii_first_access_at INTO ag
    FROM agreements
    WHERE id = p_agreement_id;

    IF ag.id IS NULL THEN
        RAISE EXCEPTION 'Agreement % not found', p_agreement_id;
    END IF;

    IF ag.state <> 'effective' THEN
        RAISE EXCEPTION 'Agreement % not effective', p_agreement_id;
    END IF;

    IF ag.effective_at IS NULL OR NOW() < ag.effective_at THEN
        RAISE EXCEPTION 'Agreement % not yet effective at current time', p_agreement_id;
    END IF;

    UPDATE agreements
    SET pii_first_access_at = NOW()
    WHERE id = p_agreement_id AND pii_first_access_at IS NULL;

    PERFORM audit_pii_access(p_agreement_id, p_actor, jsonb_build_object('source','get_pii_contact'));

    RETURN QUERY
    SELECT * FROM pii_contacts WHERE agreement_id = p_agreement_id;
END;
$$;

-- Triggers -----------------------------------------------------------------

CREATE TRIGGER trg_agreements_updated
BEFORE UPDATE ON agreements
FOR EACH ROW EXECUTE FUNCTION set_updated_at();

CREATE TRIGGER trg_agreements_region_immutable
BEFORE UPDATE ON agreements
FOR EACH ROW EXECUTE FUNCTION enforce_agreement_region_immutable();

CREATE TRIGGER trg_timeline_set_seq
BEFORE INSERT ON timeline_events
FOR EACH ROW EXECUTE FUNCTION timeline_set_seq();

CREATE TRIGGER trg_timeline_enforce_temporal
BEFORE INSERT ON timeline_events
FOR EACH ROW EXECUTE FUNCTION enforce_timeline_temporal();

CREATE TRIGGER trg_timeline_no_update
BEFORE UPDATE ON timeline_events
FOR EACH ROW EXECUTE FUNCTION timeline_prevent_mutation();

CREATE TRIGGER trg_timeline_no_delete
BEFORE DELETE ON timeline_events
FOR EACH ROW EXECUTE FUNCTION timeline_prevent_mutation();

CREATE TRIGGER trg_outbox_notify
AFTER INSERT ON outbox
FOR EACH ROW EXECUTE FUNCTION log_outbox_notify();

CREATE TRIGGER trg_disputes_resolve
BEFORE UPDATE ON disputes
FOR EACH ROW EXECUTE FUNCTION disputes_resolve_guard();

-- Row level security configuration for PII ---------------------------------

ALTER TABLE pii_contacts ENABLE ROW LEVEL SECURITY;

REVOKE ALL ON pii_contacts FROM PUBLIC;
GRANT USAGE, SELECT ON SEQUENCE audit_logs_id_seq TO PUBLIC;

CREATE POLICY pii_deny_all ON pii_contacts
    USING (false)
    WITH CHECK (false);

COMMIT;
