# Integration Testing

This directory contains Playwright end-to-end tests that exercise the real backend and the React application together.

## Prerequisites

- PostgreSQL 16 running locally and accessible via `postgres://testuser:pass@127.0.0.1:5432/acn_stress`.
  - You can reuse the same database that the Go stress harness uses (it will be dropped and recreated automatically by `InitLocalDatabase`).
- `pnpm` and Node.js ≥ 18 (for the frontend tooling).
- All Go dependencies installed (`go mod tidy` already covers them).

## Running the test suite

```bash
# from the repository root
pnpm install           # installs Playwright and front-end deps (first time only)
pnpm exec playwright install  # installs browser binaries (first time only)

# export any overrides if necessary
export DATABASE_URL='postgres://testuser:pass@127.0.0.1:5432/acn_stress?sslmode=disable'
export JWT_SECRET='integration-secret'

# run the end-to-end tests
pnpm test:integration
```

The Playwright configuration (`../playwright.config.ts`) automatically:

1. Starts the Go API server (`go run ./cmd/api`) with the given environment variables.
2. Starts the Vite dev server with `VITE_USE_MOCKS=false` so requests hit the real backend.
3. Runs the tests located in this folder.
4. Shuts both servers down when the test completes.

### Test data

The login test registers a user (`alex.agent@example.com` / `password`) before running. If the account already exists a `409` response is ignored, making the test idempotent.

You can customise the credentials by exporting:

```bash
export E2E_EMAIL='me@example.com'
export E2E_PASSWORD='MySecret123'
export E2E_FULL_NAME='Me Myself'
```

## Useful scripts

- `pnpm test:integration --headed` – run tests in headed mode for debugging.
- `pnpm test:integration --debug` – start Playwright inspector.
- `pnpm exec playwright show-report` – open the latest HTML report.

