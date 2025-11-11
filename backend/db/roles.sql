-- Roles and minimal privileges for BrokerFlow
-- Usage:
--   psql "$DATABASE_URL" -v ON_ERROR_STOP=1 -f backend/db/roles.sql

DO $$
BEGIN
  IF NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'bf_migrator') THEN
    CREATE ROLE bf_migrator LOGIN PASSWORD 'change-me' NOSUPERUSER;
  END IF;
  IF NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'bf_app') THEN
    CREATE ROLE bf_app LOGIN PASSWORD 'change-me' NOSUPERUSER;
  END IF;
  IF NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'bf_readonly') THEN
    CREATE ROLE bf_readonly LOGIN PASSWORD 'change-me' NOSUPERUSER;
  END IF;
END$$;

-- Restrict public schema creation by PUBLIC to reduce attack surface.
REVOKE CREATE ON SCHEMA public FROM PUBLIC;

-- Grant schema usage to app and readonly roles
GRANT USAGE ON SCHEMA public TO bf_app, bf_readonly, bf_migrator;

-- Migrator: full DDL/DML on public schema (no superuser)
GRANT CREATE, USAGE ON SCHEMA public TO bf_migrator;
GRANT ALL PRIVILEGES ON ALL TABLES IN SCHEMA public TO bf_migrator;
GRANT ALL PRIVILEGES ON ALL SEQUENCES IN SCHEMA public TO bf_migrator;
ALTER DEFAULT PRIVILEGES IN SCHEMA public GRANT ALL PRIVILEGES ON TABLES TO bf_migrator;
ALTER DEFAULT PRIVILEGES IN SCHEMA public GRANT ALL PRIVILEGES ON SEQUENCES TO bf_migrator;

-- Application: minimal DML on tables; EXECUTE on functions
-- Note: keep direct table grants minimal; prefer SECURITY DEFINER functions + RLS for sensitive data.
GRANT SELECT, INSERT, UPDATE, DELETE ON ALL TABLES IN SCHEMA public TO bf_app;
ALTER DEFAULT PRIVILEGES IN SCHEMA public GRANT SELECT, INSERT, UPDATE, DELETE ON TABLES TO bf_app;

-- Allow app to use sequences
GRANT USAGE, SELECT, UPDATE ON ALL SEQUENCES IN SCHEMA public TO bf_app;
ALTER DEFAULT PRIVILEGES IN SCHEMA public GRANT USAGE, SELECT, UPDATE ON SEQUENCES TO bf_app;

-- Readonly: read-only access
GRANT SELECT ON ALL TABLES IN SCHEMA public TO bf_readonly;
ALTER DEFAULT PRIVILEGES IN SCHEMA public GRANT SELECT ON TABLES TO bf_readonly;

-- Functions the app must execute (grant broadly; refine as needed)
GRANT EXECUTE ON ALL FUNCTIONS IN SCHEMA public TO bf_app, bf_migrator;

-- Optional: limit app DML surface by revoking on PII tables; access via functions only
-- REVOKE ALL ON TABLE pii_contacts FROM bf_app, bf_readonly;
-- GRANT SELECT ON TABLE pii_contacts TO bf_app; -- if needed for accessor; prefer SECURITY DEFINER

-- Note: set strong passwords or move to SCRAM with secret management in prod.

