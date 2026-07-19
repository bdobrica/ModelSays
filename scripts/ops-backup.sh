#!/usr/bin/env bash
set -euo pipefail

database_url=${1:?DATABASE_URL is required}
backup_dir=${2:-backups}
mkdir -p "$backup_dir"
backup_file="$backup_dir/modelsays-$(date -u +%Y%m%dT%H%M%SZ).dump"
if command -v pg_dump >/dev/null && command -v pg_restore >/dev/null; then
  pg_dump --format=custom --no-owner --no-acl --file="$backup_file" "$database_url"
  pg_restore --list "$backup_file" >/dev/null
else
  container_file=/tmp/modelsays-ops-backup.dump
  docker compose exec -T postgres pg_dump --format=custom --no-owner --no-acl --file="$container_file" "$database_url"
  docker compose exec -T postgres pg_restore --list "$container_file" >/dev/null
  docker compose cp "postgres:$container_file" "$backup_file" >/dev/null
  docker compose exec -T postgres rm -f "$container_file"
fi
printf '%s\n' "$backup_file"
