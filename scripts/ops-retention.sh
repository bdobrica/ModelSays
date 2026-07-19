#!/usr/bin/env bash
set -euo pipefail

database_url=${1:?DATABASE_URL is required}
days=${2:-30}
apply=${3:-}
if [[ ! "$days" =~ ^[0-9]+$ ]] || (( days < 1 )); then
  printf '%s\n' 'RETENTION_DAYS must be a positive integer' >&2
  exit 1
fi

where="completed_at < NOW() - INTERVAL '$days days'"
run_sql() {
  if command -v psql >/dev/null; then
    psql "$database_url" -v ON_ERROR_STOP=1 -c "$1"
  else
    docker compose exec -T postgres psql "$database_url" -v ON_ERROR_STOP=1 -c "$1"
  fi
}
run_sql "SELECT COUNT(*) AS provider_audits_eligible FROM provider_call_audits WHERE $where;"
if [[ "$apply" != yes ]]; then
  printf '%s\n' 'Dry run only. Re-run with APPLY_RETENTION=yes after separate authorization.'
  exit 0
fi
run_sql "DELETE FROM provider_call_audits WHERE $where;"
