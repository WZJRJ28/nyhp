#!/usr/bin/env bash
set -euo pipefail

if [[ $# -ge 1 ]]; then
  DB_URL="$1"
else
  DB_URL="${DATABASE_URL:-postgres://testuser:pass@127.0.0.1:5432/acn_stress?sslmode=disable}"
fi

if [[ -z "${DB_URL}" ]]; then
  echo "DATABASE_URL not set"
  exit 1
fi

echo "Applying migrations to ${DB_URL}" >&2
for file in $(ls migrations/*.sql | sort); do
  echo "\n-- running ${file}" >&2
  psql "${DB_URL}" -v ON_ERROR_STOP=1 -f "${file}"
done

echo "Done" >&2
