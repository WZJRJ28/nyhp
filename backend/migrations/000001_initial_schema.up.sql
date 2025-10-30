-- 000001_initial_schema.up.sql
-- BrokerFlow Phase 1 schema establishing database-enforced axioms.

-- =============================================================================
-- SECTION 0: PREREQUISITES
-- =============================================================================
-- Ensure gen_random_uuid() is available for primary key defaults.
-- CREATE EXTENSION IF NOT EXISTS pgcrypto;

-- =============================================================================
-- SECTION 1: PRELIMINARIES (ENUMS & HELPERS)
-- =============================================================================
-- Enumerations capture the finite state machines for agreements and events.
CREATE TYPE agreement_status AS ENUM (
    'draft',
    'pending_signature',
    'effective',
    'success',
    'void',
    'disputed',
    'closed'
);

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

-- Centralized access to the current transaction timestamp keeps temporal logic consistent (Axiom A3).
CREATE OR REPLACE FUNCTION get_tx_timestamp() RETURNS TIMESTAMPTZ AS $$
BEGIN
    RETURN transaction_timestamp();
END;
$$ LANGUAGE plpgsql;

-- =============================================================================
-- SECTION 2: CORE ENTITY TABLES (Axiom A1 Anchor)
-- =============================================================================
CREATE TABLE users (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    email TEXT NOT NULL UNIQUE,
    full_name TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT get_tx_timestamp()
);

CREATE TABLE brokers (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name TEXT NOT NULL UNIQUE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT get_tx_timestamp()
);

CREATE TABLE referrals (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    created_by_user_id UUID NOT NULL REFERENCES users(id),
    status TEXT NOT NULL DEFAULT 'open',
    created_at TIMESTAMPTZ NOT NULL DEFAULT get_tx_timestamp()
);

CREATE TABLE agreements (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    referral_id UUID NOT NULL REFERENCES referrals(id),
    from_broker_id UUID NOT NULL REFERENCES brokers(id),
    to_broker_id UUID NOT NULL REFERENCES brokers(id),
    status agreement_status NOT NULL DEFAULT 'draft',
    eff_time TIMESTAMPTZ,
    pii_first_access_time TIMESTAMPTZ,
    version INTEGER NOT NULL DEFAULT 1,
    created_at TIMESTAMPTZ NOT NULL DEFAULT get_tx_timestamp()
);

-- Axiom A1: allow at most one pending/effective agreement per referral via a partial unique index.
CREATE UNIQUE INDEX agreements_one_active_per_referral
    ON agreements (referral_id)
    WHERE status IN ('pending_signature', 'effective');

-- =============================================================================
-- SECTION 3: IMMUTABLE EVENT LEDGER (Axioms A3 & A4)
-- =============================================================================
CREATE TABLE timeline_events (
    id BIGSERIAL PRIMARY KEY,
    agreement_id UUID NOT NULL REFERENCES agreements(id),
    seq INTEGER NOT NULL,
    type event_type NOT NULL,
    payload JSONB,
    actor_id UUID REFERENCES users(id),
    created_at TIMESTAMPTZ NOT NULL DEFAULT get_tx_timestamp(),
    CONSTRAINT timeline_events_agreement_id_seq_unique UNIQUE (agreement_id, seq)
);

-- Prevent mutation of historical events (Axiom A4).
CREATE OR REPLACE FUNCTION prevent_event_mutation()
RETURNS TRIGGER AS $$
BEGIN
    RAISE EXCEPTION 'Axiom A4 violation: timeline_events are immutable.';
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER trg_prevent_event_mutation
BEFORE UPDATE OR DELETE ON timeline_events
FOR EACH ROW EXECUTE FUNCTION prevent_event_mutation();

-- Enforce temporal integrity for closing events (Axiom A3).
CREATE OR REPLACE FUNCTION check_temporal_integrity()
RETURNS TRIGGER AS $$
DECLARE
    agreement_eff_time TIMESTAMPTZ;
    event_timestamp TIMESTAMPTZ;
BEGIN
    event_timestamp := COALESCE(NEW.created_at, get_tx_timestamp());

    IF NEW.type = 'DEAL_CLOSED' THEN
        SELECT eff_time INTO agreement_eff_time
        FROM agreements
        WHERE id = NEW.agreement_id
        FOR UPDATE;

        IF agreement_eff_time IS NULL THEN
            RAISE EXCEPTION 'Axiom A3 violation: cannot log a deal-closing event for a non-effective agreement (%).', NEW.agreement_id;
        END IF;

        IF event_timestamp < agreement_eff_time THEN
            RAISE EXCEPTION 'Axiom A3 violation: event time (%) precedes agreement effective time (%).', event_timestamp, agreement_eff_time;
        END IF;
    END IF;

    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER trg_check_temporal_integrity
BEFORE INSERT ON timeline_events
FOR EACH ROW EXECUTE FUNCTION check_temporal_integrity();

-- =============================================================================
-- SECTION 4: PII SEGREGATION & ACCESS (Axioms A2 & A7)
-- =============================================================================
CREATE TABLE pii_data (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    referral_id UUID NOT NULL UNIQUE REFERENCES referrals(id),
    client_name TEXT NOT NULL,
    client_email TEXT NOT NULL,
    client_phone TEXT
);

ALTER TABLE pii_data ENABLE ROW LEVEL SECURITY;
ALTER TABLE pii_data FORCE ROW LEVEL SECURITY;

CREATE OR REPLACE FUNCTION can_view_pii(p_referral_id UUID)
RETURNS BOOLEAN AS $$
BEGIN
    RETURN EXISTS (
        SELECT 1
        FROM agreements
        WHERE referral_id = p_referral_id
          AND status = 'effective'
    );
END;
$$ LANGUAGE plpgsql STABLE;

CREATE POLICY policy_pii_access ON pii_data
    FOR SELECT
    USING (can_view_pii(referral_id));

CREATE OR REPLACE FUNCTION access_pii_data(p_referral_id UUID, p_actor_id UUID)
RETURNS SETOF pii_data AS $$
DECLARE
    agreement_rec RECORD;
    next_seq INTEGER;
BEGIN
    PERFORM set_config('search_path', 'public', true);

    SELECT id, pii_first_access_time
    INTO agreement_rec
    FROM agreements
    WHERE referral_id = p_referral_id
      AND status = 'effective';

    IF NOT FOUND THEN
        RAISE EXCEPTION 'Axiom A2 violation: no effective agreement for referral %.', p_referral_id;
    END IF;

    IF agreement_rec.pii_first_access_time IS NULL THEN
        UPDATE agreements
        SET pii_first_access_time = get_tx_timestamp()
        WHERE id = agreement_rec.id;

        SELECT COALESCE(MAX(seq), 0) + 1
        INTO next_seq
        FROM timeline_events
        WHERE agreement_id = agreement_rec.id;

        INSERT INTO timeline_events (agreement_id, seq, type, payload, actor_id)
        VALUES (
            agreement_rec.id,
            next_seq,
            'PII_VIEWED',
            jsonb_build_object(
                'message', 'First PII access by actor',
                'actor_id', p_actor_id,
                'referral_id', p_referral_id
            ),
            p_actor_id
        );
    END IF;

    RETURN QUERY
    SELECT *
    FROM pii_data
    WHERE referral_id = p_referral_id;
END;
$$ LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = public;

-- =============================================================================
-- SECTION 5: OUTBOX & IDEMPOTENCY (Axioms A5 & A6)
-- =============================================================================
CREATE TABLE outbox (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    topic TEXT NOT NULL,
    payload JSONB,
    status TEXT NOT NULL DEFAULT 'pending',
    attempts INTEGER NOT NULL DEFAULT 0,
    last_attempt TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT get_tx_timestamp()
);

CREATE TABLE idempotency (
    key TEXT PRIMARY KEY,
    created_at TIMESTAMPTZ NOT NULL DEFAULT get_tx_timestamp()
);
