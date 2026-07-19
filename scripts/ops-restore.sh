#!/usr/bin/env bash
set -euo pipefail

source_url=${1:?DATABASE_URL is required}
backup_file=${2:?BACKUP_FILE is required}
restore_database=${3:-modelsays_restore_drill}
confirmation=${4:-}

if [[ ! "$restore_database" =~ ^modelsays_restore_[a-zA-Z0-9_]+$ ]]; then
  printf '%s\n' 'RESTORE_DATABASE must start with modelsays_restore_ and contain only letters, digits, and underscores' >&2
  exit 1
fi
if [[ "$confirmation" != "$restore_database" ]]; then
  printf 'Dry run: would recreate %s from %s. Re-run with CONFIRM_RESTORE=%s.\n' "$restore_database" "$backup_file" "$restore_database"
  exit 0
fi

if command -v pg_restore >/dev/null && command -v psql >/dev/null; then
  admin_url=${source_url%/*}/postgres
  dropdb --if-exists --force --maintenance-db="$admin_url" "$restore_database"
  createdb --maintenance-db="$admin_url" "$restore_database"
  restore_url=${source_url%/*}/$restore_database
  pg_restore --no-owner --no-acl --dbname="$restore_url" "$backup_file"
  psql "$restore_url" -v ON_ERROR_STOP=1 -Atc \
    "SELECT 'rooms=' || COUNT(*) FROM rooms UNION ALL SELECT 'games=' || COUNT(*) FROM games UNION ALL SELECT 'migration=' || MAX(version_id) FROM goose_db_version WHERE is_applied;"
else
  container_file=/tmp/modelsays-ops-restore.dump
  docker compose cp "$backup_file" "postgres:$container_file" >/dev/null
  docker compose exec -T postgres dropdb --if-exists --force -U postgres "$restore_database"
  docker compose exec -T postgres createdb -U postgres "$restore_database"
  docker compose exec -T postgres pg_restore --no-owner --no-acl -U postgres -d "$restore_database" "$container_file"
  docker compose exec -T postgres psql -U postgres -d "$restore_database" -v ON_ERROR_STOP=1 -Atc \
    "SELECT 'rooms=' || COUNT(*) FROM rooms UNION ALL SELECT 'games=' || COUNT(*) FROM games UNION ALL SELECT 'migration=' || MAX(version_id) FROM goose_db_version WHERE is_applied;"
  docker compose exec -T postgres rm -f "$container_file"
fi
