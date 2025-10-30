-- 000002_stress_ext.up.sql
-- Extensions to the initial schema to support concurrency stress testing invariants.

BEGIN;

-- Add region to agreements with an immutable constraint.
ALTER TABLE agreements
    ADD COLUMN IF NOT EXISTS region TEXT NOT NULL DEFAULT 'us-ea';

-- Add event wall-clock ts distinct from created_at if not present.
ALTER TABLE timeline_events
    ADD COLUMN IF NOT EXISTS ts TIMESTAMPTZ NOT NULL DEFAULT get_tx_timestamp();

-- Strengthen temporal enforcement to cover more event types.
DROP TRIGGER IF EXISTS trg_check_temporal_integrity ON timeline_events;
DROP FUNCTION IF EXISTS check_temporal_integrity();

CREATE OR REPLACE FUNCTION check_temporal_integrity()
RETURNS TRIGGER AS $$
DECLARE
    agreement_eff_time TIMESTAMPTZ;
    agreement_state agreement_status;
    event_timestamp TIMESTAMPTZ;
BEGIN
    event_timestamp := COALESCE(NEW.ts, get_tx_timestamp());

    SELECT eff_time, status INTO agreement_eff_time, agreement_state
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

CREATE TRIGGER trg_check_temporal_integrity
BEFORE INSERT ON timeline_events
FOR EACH ROW EXECUTE FUNCTION check_temporal_integrity();

-- Prevent UPDATE/DELETE on timeline_events already exists via trg_prevent_event_mutation.

-- PII contacts table keyed by agreement with RLS deny-all and a SECURITY DEFINER accessor.
CREATE TABLE IF NOT EXISTS pii_contacts (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    agreement_id UUID NOT NULL UNIQUE REFERENCES agreements(id) ON DELETE CASCADE,
    client_name TEXT NOT NULL,
    client_email TEXT NOT NULL,
    client_phone TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT get_tx_timestamp()
);

ALTER TABLE pii_contacts ENABLE ROW LEVEL SECURITY;
REVOKE ALL ON pii_contacts FROM PUBLIC;
CREATE POLICY pii_contacts_deny_all ON pii_contacts USING (false) WITH CHECK (false);

-- Simple audit table for PII reads and other actions.
CREATE TABLE IF NOT EXISTS audit_logs (
    id BIGSERIAL PRIMARY KEY,
    agreement_id UUID,
    actor_id UUID,
    action TEXT NOT NULL,
    metadata JSONB,
    ts TIMESTAMPTZ NOT NULL DEFAULT get_tx_timestamp()
);

CREATE OR REPLACE FUNCTION audit_pii_access(p_agreement_id UUID, p_actor UUID, p_metadata JSONB)
RETURNS VOID LANGUAGE plpgsql AS $$
BEGIN
    INSERT INTO audit_logs(agreement_id, actor_id, action, metadata, ts)
    VALUES (p_agreement_id, p_actor, 'PII_READ', p_metadata, get_tx_timestamp());
END;
$$;

-- SECURITY DEFINER accessor for PII with first-access monotonic update.
CREATE OR REPLACE FUNCTION get_pii_contact(p_agreement_id UUID, p_actor UUID)
RETURNS SETOF pii_contacts
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = public
AS $$
DECLARE
    ag RECORD;
BEGIN
    SELECT id, status, eff_time, pii_first_access_time INTO ag
    FROM agreements WHERE id = p_agreement_id;

    IF ag.id IS NULL THEN
        RAISE EXCEPTION 'Agreement % not found', p_agreement_id;
    END IF;
    IF ag.status <> 'effective' THEN
        RAISE EXCEPTION 'Agreement % not effective', p_agreement_id;
    END IF;
    IF ag.eff_time IS NULL OR get_tx_timestamp() < ag.eff_time THEN
        RAISE EXCEPTION 'Agreement % not yet effective at current time', p_agreement_id;
    END IF;

    UPDATE agreements
    SET pii_first_access_time = get_tx_timestamp()
    WHERE id = p_agreement_id AND pii_first_access_time IS NULL;

    PERFORM audit_pii_access(p_agreement_id, p_actor, jsonb_build_object('source','get_pii_contact'));

    RETURN QUERY SELECT * FROM pii_contacts WHERE agreement_id = p_agreement_id;
END;
$$;

-- Outbox notify trigger to wake workers.
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

-- Edge invocation idempotency registry.
CREATE TABLE IF NOT EXISTS edge_invocations (
    key TEXT PRIMARY KEY,
    route TEXT NOT NULL,
    status TEXT NOT NULL DEFAULT 'pending',
    first_attempt_at TIMESTAMPTZ NOT NULL DEFAULT get_tx_timestamp(),
    last_attempt_at TIMESTAMPTZ NOT NULL DEFAULT get_tx_timestamp(),
    response_code INTEGER,
    error TEXT
);

-- Invoices and disputes with linkage.
CREATE TABLE IF NOT EXISTS invoices (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    agreement_id UUID NOT NULL REFERENCES agreements(id) ON DELETE CASCADE,
    amount NUMERIC(12,2) NOT NULL DEFAULT 0,
    status TEXT NOT NULL DEFAULT 'open',
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

CREATE OR REPLACE FUNCTION disputes_resolve_guard()
RETURNS TRIGGER LANGUAGE plpgsql AS $$
BEGIN
    IF OLD.status <> 'resolved' AND NEW.status = 'resolved' THEN
        UPDATE agreements SET status = 'disputed' WHERE id = NEW.agreement_id AND status <> 'disputed';
        UPDATE invoices SET is_invalidated = TRUE
        WHERE agreement_id = NEW.agreement_id AND status NOT IN ('paid','written_off');
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

-- Region immutability trigger and audit to support oracle.
CREATE TABLE IF NOT EXISTS agreements_region_audit (
    id BIGSERIAL PRIMARY KEY,
    agreement_id UUID NOT NULL,
    old_region TEXT NOT NULL,
    new_region TEXT NOT NULL,
    changed_at TIMESTAMPTZ NOT NULL DEFAULT get_tx_timestamp()
);

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

COMMIT;

