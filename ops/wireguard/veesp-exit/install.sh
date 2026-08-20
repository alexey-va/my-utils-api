#!/usr/bin/env bash
set -euo pipefail

mode=plan
replace=false
config_file=""
stack_dir=/opt/my-utils-awg-exit
project=my-utils-awg-exit
network=my-utils-awg-exit

usage() {
  echo "Usage: $0 --config FILE [--apply] [--replace]" >&2
}

while (($#)); do
  case "$1" in
    --config) config_file=${2:-}; shift 2 ;;
    --apply) mode=apply; shift ;;
    --replace) replace=true; shift ;;
    *) usage; exit 2 ;;
  esac
done

[[ $EUID -eq 0 ]] || { echo "Run as root" >&2; exit 1; }
for command in docker stat ip; do
  command -v "$command" >/dev/null || { echo "Required command is missing: $command" >&2; exit 1; }
done
docker compose version >/dev/null
[[ -f "$config_file" ]] || { echo "AWG config is missing" >&2; exit 1; }
[[ "$(stat -c '%a' "$config_file")" == 600 ]] || { echo "AWG config must have mode 600" >&2; exit 1; }
for marker in '[Interface]' 'PrivateKey = ' 'ListenPort = 42697' '[Peer]' 'PresharedKey = ' 'AllowedIPs = ' '10.89.0.0/24'; do
  grep -Fq "$marker" "$config_file" || { echo "AWG config is incomplete" >&2; exit 1; }
done
server_address=$(sed -n 's/^Address[[:space:]]*=[[:space:]]*//p' "$config_file")
tunnel_client_ip=$(sed -n 's/^AllowedIPs[[:space:]]*=[[:space:]]*\([^,]*\),.*/\1/p' "$config_file")
[[ "$server_address" =~ ^10\.8\.([0-9]{1,3})\.1/24$ ]]
overlay_octet=${BASH_REMATCH[1]}
((10#$overlay_octet >= 1 && 10#$overlay_octet <= 254))
[[ "$tunnel_client_ip" == "10.8.$((10#$overlay_octet)).250/32" ]]

source_dir=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)
for file in .dockerignore compose.yml Dockerfile.awg Dockerfile.tinyproxy awg-entrypoint.sh tinyproxy.conf; do
  [[ -f "$source_dir/$file" ]] || { echo "Installer asset is missing: $file" >&2; exit 1; }
done

for container in my-utils-awg-exit my-utils-tinyproxy; do
  if docker container inspect "$container" >/dev/null 2>&1; then
    owner=$(docker inspect -f '{{index .Config.Labels "com.docker.compose.project"}}' "$container")
    [[ "$owner" == "$project" ]] || { echo "Refusing foreign container: $container" >&2; exit 1; }
  fi
done

if docker network inspect "$network" >/dev/null 2>&1; then
  owner=$(docker network inspect -f '{{index .Labels "com.docker.compose.project"}}' "$network")
  subnet=$(docker network inspect -f '{{(index .IPAM.Config 0).Subnet}}' "$network")
  bridge=$(docker network inspect -f '{{index .Options "com.docker.network.bridge.name"}}' "$network")
  [[ "$owner" == "$project" && "$subnet" == 172.29.172.0/24 && "$bridge" == amn0 ]] || {
    echo "Refusing foreign Docker network: $network" >&2
    exit 1
  }
elif ip route show 172.29.172.0/24 | grep -q .; then
  echo "Refusing route conflict for 172.29.172.0/24" >&2
  exit 1
fi

if [[ -e "$stack_dir" && ! -f "$stack_dir/.managed-by-my-utils" ]]; then
  echo "Refusing unmanaged stack directory: $stack_dir" >&2
  exit 1
fi
if [[ -e "$stack_dir/state/awg0.conf" && "$replace" != true ]]; then
  echo "Refusing to replace existing AWG config without --replace" >&2
  exit 1
fi

echo "Plan: install an isolated AWG + tinyproxy Compose stack on $network"
if [[ "$mode" != apply ]]; then
  echo "Plan only; no host changes were made"
  exit 0
fi

install -d -m 700 "$stack_dir" "$stack_dir/state"
touch "$stack_dir/.managed-by-my-utils"
for file in .dockerignore compose.yml Dockerfile.awg Dockerfile.tinyproxy awg-entrypoint.sh tinyproxy.conf; do
  install -m 644 "$source_dir/$file" "$stack_dir/$file"
done
chmod 755 "$stack_dir/awg-entrypoint.sh"
if [[ -e "$stack_dir/state/awg0.conf" ]]; then
  cp -a "$stack_dir/state/awg0.conf" "$stack_dir/state/awg0.conf.backup.$(date -u +%Y%m%dT%H%M%SZ)"
fi
install -m 600 "$config_file" "$stack_dir/state/awg0.conf"
printf 'TUNNEL_CLIENT_IP=%s\n' "$tunnel_client_ip" >"$stack_dir/.env"
chmod 600 "$stack_dir/.env"

cd "$stack_dir"
docker compose config --quiet
docker compose build --pull
docker compose up -d

for _ in $(seq 1 30); do
  awg_health=$(docker inspect -f '{{if .State.Health}}{{.State.Health.Status}}{{else}}none{{end}}' my-utils-awg-exit 2>/dev/null || true)
  proxy_health=$(docker inspect -f '{{if .State.Health}}{{.State.Health.Status}}{{else}}none{{end}}' my-utils-tinyproxy 2>/dev/null || true)
  [[ "$awg_health" == healthy && "$proxy_health" == healthy ]] && break
  sleep 1
done
[[ "$(docker inspect -f '{{.State.Health.Status}}' my-utils-awg-exit)" == healthy ]]
[[ "$(docker inspect -f '{{.State.Health.Status}}' my-utils-tinyproxy)" == healthy ]]
[[ "$(docker inspect -f '{{json .HostConfig.PortBindings}}' my-utils-tinyproxy)" == '{}' ]]
docker network inspect "$network" >/dev/null
echo "Isolated AWG exit stack is healthy"
