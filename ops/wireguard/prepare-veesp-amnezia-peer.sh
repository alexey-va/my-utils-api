#!/usr/bin/env bash
set -euo pipefail

mode=plan
container=amnezia-awg
endpoint=""
client_cidr=""
output=""
awg_address=10.8.1.250/32

usage() {
  echo "Usage: $0 --endpoint HOST:PORT --client-cidr CIDR --output FILE [--container amnezia-awg] [--awg-address IP/32] [--apply]" >&2
}

valid_private_cidr() {
  local cidr=$1 ip prefix a b c d ip_number host_bits mask
  [[ "$cidr" == */* ]] || return 1
  ip=${cidr%/*}; prefix=${cidr#*/}
  [[ "$prefix" =~ ^[0-9]+$ ]] && ((prefix >= 16 && prefix <= 29)) || return 1
  IFS=. read -r a b c d <<<"$ip"
  for octet in "$a" "$b" "$c" "$d"; do
    [[ "$octet" =~ ^(0|[1-9][0-9]{0,2})$ ]] && ((10#$octet <= 255)) || return 1
  done
  a=$((10#$a)); b=$((10#$b)); c=$((10#$c)); d=$((10#$d))
  ((a == 10 || (a == 172 && b >= 16 && b <= 31) || (a == 192 && b == 168))) || return 1
  ip_number=$(((a << 24) | (b << 16) | (c << 8) | d))
  host_bits=$((32 - prefix)); mask=$(((0xffffffff << host_bits) & 0xffffffff))
  (( (ip_number & mask) == ip_number ))
}

while (($#)); do
  case "$1" in
    --container) container="${2:-}"; shift 2 ;;
    --endpoint) endpoint="${2:-}"; shift 2 ;;
    --client-cidr) client_cidr="${2:-}"; shift 2 ;;
    --output) output="${2:-}"; shift 2 ;;
    --awg-address) awg_address="${2:-}"; shift 2 ;;
    --apply) mode=apply; shift ;;
    *) usage; exit 2 ;;
  esac
done

for command in docker stat; do
  command -v "$command" >/dev/null || { echo "Required command is missing: $command" >&2; exit 1; }
done
if [[ ! "$container" =~ ^[a-zA-Z0-9_.-]+$ ]]; then
  echo "Invalid container name" >&2
  exit 1
fi
if [[ ! "$endpoint" =~ ^[^[:space:]:]+:[0-9]{1,5}$ ]]; then
  echo "Invalid endpoint" >&2
  exit 1
fi
endpoint_port=${endpoint##*:}
if ((10#$endpoint_port < 1 || 10#$endpoint_port > 65535)); then
  echo "Invalid endpoint port" >&2
  exit 1
fi
if ! valid_private_cidr "$client_cidr"; then
  echo "Client CIDR must be private IPv4 /16 through /29" >&2
  exit 1
fi
if [[ ! "$awg_address" =~ ^10\.8\.1\.[0-9]{1,3}/32$ ]]; then
  echo "AWG address must be a 10.8.1.x/32 address" >&2
  exit 1
fi
awg_host=${awg_address#10.8.1.}; awg_host=${awg_host%/32}
if ((10#$awg_host < 2 || 10#$awg_host > 254)); then
  echo "AWG address host must be between 2 and 254" >&2
  exit 1
fi
if [[ -z "$output" ]]; then
  echo "Output path is required" >&2
  exit 1
fi
if [[ -e "$output" ]]; then
  echo "Refusing to overwrite output: $output" >&2
  exit 1
fi
if [[ "$(docker inspect -f '{{.State.Running}}' "$container" 2>/dev/null)" != true ]]; then
  echo "Expected running container was not found: $container" >&2
  exit 1
fi
if [[ "$(docker inspect -f '{{.Config.Image}}' "$container")" != amnezia-awg ]]; then
  echo "Unexpected container image" >&2
  exit 1
fi
docker exec "$container" test -f /opt/amnezia/awg/wg0.conf
docker exec "$container" sh -c 'command -v awg >/dev/null && command -v awg-quick >/dev/null'
if docker exec "$container" grep -q '^# my-utils-awg-exit$' /opt/amnezia/awg/wg0.conf; then
  echo "Dedicated my-utils AWG peer already exists" >&2
  exit 1
fi

echo "Plan: back up $container, add one route-owning AWG peer for $client_cidr, and write a mode-600 client artifact"
if [[ "$mode" != apply ]]; then
  exit 0
fi

container_output=/opt/amnezia/awg/my-utils-awg-exit.conf
docker exec -i "$container" sh -s -- "$endpoint" "$client_cidr" "$awg_address" "$container_output" <<'CONTAINER_SCRIPT'
set -eu
umask 077
endpoint=$1
client_cidr=$2
awg_address=$3
client_output=$4
config=/opt/amnezia/awg/wg0.conf
timestamp=$(date -u +%Y%m%dT%H%M%SZ)
backup="/opt/amnezia/awg/wg0.conf.backup.$timestamp"
tmp_dir=$(mktemp -d /tmp/my-utils-awg.XXXXXX)
success=false
cleanup() {
  if [ "$success" != true ]; then
    cp "$backup" "$config" 2>/dev/null || true
    awg-quick strip "$config" >"$tmp_dir/restore.conf" 2>/dev/null || true
    awg syncconf wg0 "$tmp_dir/restore.conf" 2>/dev/null || true
    ip route del "$client_cidr" dev wg0 2>/dev/null || true
    iptables -t nat -D POSTROUTING -s "$client_cidr" -o eth0 -j MASQUERADE 2>/dev/null || true
    rm -f "$client_output"
  fi
  rm -rf "$tmp_dir"
}
trap cleanup EXIT INT TERM

cp "$config" "$backup"
chmod 600 "$backup"
awg genkey >"$tmp_dir/client.key"
awg pubkey <"$tmp_dir/client.key" >"$tmp_dir/client.pub"
awg genpsk >"$tmp_dir/client.psk"
client_public=$(cat "$tmp_dir/client.pub")
server_public=$(awg show wg0 public-key)
host_ip=${awg_address%/*}
if awg show wg0 allowed-ips | grep -Fq "$host_ip/32"; then
  echo "Requested AWG address is already allocated" >&2
  exit 1
fi

if ! grep -qF "PostUp = iptables -t nat -A POSTROUTING -s $client_cidr -o eth0 -j MASQUERADE" "$config"; then
  awk -v cidr="$client_cidr" '
    !added && /^\[Peer\]$/ {
      print "PostUp = iptables -t nat -A POSTROUTING -s " cidr " -o eth0 -j MASQUERADE"
      print "PostDown = iptables -t nat -D POSTROUTING -s " cidr " -o eth0 -j MASQUERADE"
      added=1
    }
    { print }
  ' "$config" >"$tmp_dir/server.conf"
  mv "$tmp_dir/server.conf" "$config"
fi
cat >>"$config" <<EOF

# my-utils-awg-exit
[Peer]
PublicKey = $client_public
PresharedKey = $(cat "$tmp_dir/client.psk")
AllowedIPs = $awg_address, $client_cidr
# end my-utils-awg-exit
EOF
chmod 600 "$config"

awg set wg0 peer "$client_public" preshared-key "$tmp_dir/client.psk" allowed-ips "$awg_address,$client_cidr"
ip route replace "$client_cidr" dev wg0
iptables -t nat -C POSTROUTING -s "$client_cidr" -o eth0 -j MASQUERADE 2>/dev/null ||
  iptables -t nat -A POSTROUTING -s "$client_cidr" -o eth0 -j MASQUERADE

{
  echo '[Interface]'
  echo "Address = $awg_address"
  echo "PrivateKey = $(cat "$tmp_dir/client.key")"
  awk -F= '/^(Jc|Jmin|Jmax|S1|S2|H1|H2|H3|H4)[[:space:]]*=/{gsub(/[[:space:]]/, "", $1); sub(/^[^=]*=[[:space:]]*/, "", $0); print $1 " = " $0}' "$config"
  echo 'MTU = 1380'
  echo 'Table = off'
  echo
  echo '[Peer]'
  echo "PublicKey = $server_public"
  echo "PresharedKey = $(cat "$tmp_dir/client.psk")"
  echo "Endpoint = $endpoint"
  echo 'AllowedIPs = 0.0.0.0/0'
  echo 'PersistentKeepalive = 25'
} >"$client_output"
chmod 600 "$client_output"
success=true
CONTAINER_SCRIPT

umask 077
docker cp "$container:$container_output" "$output" >/dev/null
chmod 600 "$output"
docker exec "$container" rm -f "$container_output"
echo "Prepared root-sensitive AmneziaWG client artifact: $output"
