#!/usr/bin/env bash
set -euo pipefail

repo_dir=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
cd "$repo_dir"

if [[ ! -f .env ]]; then
  printf '%s\n' 'ModelSays: .env is missing; run make pi-install first.' >&2
  exit 1
fi

set -a
# .env is an operator-owned file containing simple KEY=value assignments.
# shellcheck disable=SC1091
source .env
set +a

docker compose up -d postgres
"$repo_dir/bin/goose" -dir backend/migrations postgres "$DATABASE_URL" up

backend_pid=''
client_pid=''
stop_children() {
  [[ -z "$backend_pid" ]] || kill "$backend_pid" 2>/dev/null || true
  [[ -z "$client_pid" ]] || kill "$client_pid" 2>/dev/null || true
  wait "$backend_pid" "$client_pid" 2>/dev/null || true
}
trap stop_children INT TERM EXIT

"$repo_dir/bin/modelsays" &
backend_pid=$!
npm --prefix client run serve:pi &
client_pid=$!

wait -n "$backend_pid" "$client_pid"

