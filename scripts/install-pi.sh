#!/usr/bin/env bash
set -euo pipefail

go_version=1.25.13
node_version=22.23.2
repo_dir=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
install_user=${SUDO_USER:-$(id -un)}
install_group=$(id -gn "$install_user")

if [[ $(uname -s) != Linux || $(uname -m) != aarch64 ]]; then
  printf '%s\n' 'This installer currently supports 64-bit Raspberry Pi OS/Linux (aarch64).' >&2
  exit 1
fi
if ! command -v sudo >/dev/null; then
  printf '%s\n' 'sudo is required to install toolchains and the boot service.' >&2
  exit 1
fi

printf '%s\n' 'Installing ModelSays prerequisites...'
sudo apt-get update
sudo apt-get install -y ca-certificates curl git make xz-utils

tmp_dir=$(mktemp -d)
cleanup() { rm -rf "$tmp_dir"; }
trap cleanup EXIT

if ! command -v go >/dev/null || [[ $(go env GOVERSION 2>/dev/null || true) != "go${go_version}" ]]; then
  go_archive="go${go_version}.linux-arm64.tar.gz"
  curl -fsSL "https://go.dev/dl/?mode=json" -o "$tmp_dir/go-releases.json"
  go_checksum=$(awk -v file="$go_archive" '
    index($0, "\"filename\": \"" file "\"") { found=1 }
    found && /"sha256":/ { gsub(/[",]/, "", $2); print $2; exit }
  ' "$tmp_dir/go-releases.json")
  [[ $go_checksum =~ ^[0-9a-f]{64}$ ]] || { printf '%s\n' 'Could not obtain the official Go checksum.' >&2; exit 1; }
  curl -fL "https://go.dev/dl/$go_archive" -o "$tmp_dir/$go_archive"
  printf '%s  %s\n' "$go_checksum" "$tmp_dir/$go_archive" | sha256sum -c -
  sudo rm -rf /usr/local/go
  sudo tar -C /usr/local -xzf "$tmp_dir/$go_archive"
  sudo ln -sf /usr/local/go/bin/go /usr/local/bin/go
  sudo ln -sf /usr/local/go/bin/gofmt /usr/local/bin/gofmt
fi

if ! command -v node >/dev/null || [[ $(node --version 2>/dev/null || true) != "v${node_version}" ]]; then
  node_archive="node-v${node_version}-linux-arm64.tar.xz"
  curl -fsSL "https://nodejs.org/dist/v${node_version}/SHASUMS256.txt" -o "$tmp_dir/node-shasums"
  node_checksum=$(awk -v file="$node_archive" '$2 == file { print $1 }' "$tmp_dir/node-shasums")
  [[ $node_checksum =~ ^[0-9a-f]{64}$ ]] || { printf '%s\n' 'Could not obtain the official Node.js checksum.' >&2; exit 1; }
  curl -fL "https://nodejs.org/dist/v${node_version}/$node_archive" -o "$tmp_dir/$node_archive"
  printf '%s  %s\n' "$node_checksum" "$tmp_dir/$node_archive" | sha256sum -c -
  sudo mkdir -p /opt/nodejs
  sudo tar -C /opt/nodejs -xJf "$tmp_dir/$node_archive"
  for executable in node npm npx corepack; do
    sudo ln -sf "/opt/nodejs/node-v${node_version}-linux-arm64/bin/$executable" "/usr/local/bin/$executable"
  done
fi

if ! command -v docker >/dev/null || ! docker compose version >/dev/null 2>&1; then
  printf '%s\n' 'Docker Engine with Compose v2 is required. Install Docker, then rerun this command.' >&2
  exit 1
fi

lan_ip=${MODELSAYS_HOST_IP:-$(hostname -I | awk '{print $1}')}
[[ -n $lan_ip ]] || { printf '%s\n' 'Could not determine the Pi LAN address; set MODELSAYS_HOST_IP.' >&2; exit 1; }
[[ $lan_ip =~ ^[A-Za-z0-9][A-Za-z0-9.-]*$ ]] || { printf '%s\n' 'The detected host address is invalid; set MODELSAYS_HOST_IP to an IPv4 address or hostname.' >&2; exit 1; }

if [[ ! -f "$repo_dir/.env" ]]; then
  cp "$repo_dir/.env.example" "$repo_dir/.env"
fi
set_env() {
  local key=$1 value=$2 file=$3
  if grep -q "^${key}=" "$file"; then
    sed -i "s|^${key}=.*|${key}=${value}|" "$file"
  else
    printf '%s=%s\n' "$key" "$value" >> "$file"
  fi
}
set_env CORS_ALLOWED_ORIGINS "http://${lan_ip}:5173" "$repo_dir/.env"
set_env VITE_API_BASE_URL "http://${lan_ip}:8080" "$repo_dir/.env"

printf '%s\n' 'Building ModelSays (the first build can take several minutes)...'
cd "$repo_dir"
make pi-build
chmod +x scripts/install-pi.sh scripts/run-pi.sh

escaped_repo=${repo_dir//&/\\&}
escaped_user=${install_user//&/\\&}
escaped_group=${install_group//&/\\&}
sed -e "s|@REPO_DIR@|$escaped_repo|g" -e "s|@USER@|$escaped_user|g" -e "s|@GROUP@|$escaped_group|g" \
  deploy/modelsays.service.in > "$tmp_dir/modelsays.service"
sudo install -o root -g root -m 0644 "$tmp_dir/modelsays.service" /etc/systemd/system/modelsays.service
sudo systemctl daemon-reload
sudo systemctl enable modelsays.service
sudo systemctl restart modelsays.service

for _ in {1..120}; do
  if curl -fsS --max-time 2 "http://127.0.0.1:8080/readyz" >/dev/null 2>&1 && \
     curl -fsS --max-time 2 "http://127.0.0.1:5173/" >/dev/null 2>&1; then
    printf '\nModelSays is ready at http://%s:5173\n' "$lan_ip"
    printf '%s\n' 'Logs: journalctl -u modelsays -f'
    exit 0
  fi
  sleep 2
done

printf '%s\n' 'ModelSays did not become ready. Check: journalctl -u modelsays -n 100' >&2
exit 1
