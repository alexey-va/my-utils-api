#!/usr/bin/env bash
set -euo pipefail

umask 077

params_file=""
config_file=/etc/amnezia/amneziawg/awg-exit.conf
expected_egress=""
service=my-utils-awg-exit.service
interface=awg-exit

usage() {
  echo "Usage: $0 --params FILE --expected-egress IPv4 [--config FILE]" >&2
}

while (($#)); do
  case "$1" in
    --params) params_file=${2:-}; shift 2 ;;
    --config) config_file=${2:-}; shift 2 ;;
    --expected-egress) expected_egress=${2:-}; shift 2 ;;
    *) usage; exit 2 ;;
  esac
done

[[ $EUID -eq 0 ]] || { echo "Run as root" >&2; exit 1; }
for command in awg awg-quick curl install systemctl stat; do
  command -v "$command" >/dev/null || { echo "Required command is missing: $command" >&2; exit 1; }
done
[[ -f "$params_file" && -f "$config_file" ]]
[[ "$(stat -c '%a' "$params_file")" == 600 ]] || { echo "Client parameters must have mode 600" >&2; exit 1; }
[[ "$expected_egress" =~ ^([0-9]{1,3}\.){3}[0-9]{1,3}$ ]]

read_param() {
  local key=$1
  awk -F= -v wanted="$key" '$1 == wanted { sub(/^[^=]*=/, ""); print; found++ } END { exit found == 1 ? 0 : 1 }' "$params_file"
}

server_public_key=$(read_param SERVER_PUBLIC_KEY)
preshared_key=$(read_param PRESHARED_KEY)
endpoint=$(read_param ENDPOINT)
jc=$(read_param JC)
jmin=$(read_param JMIN)
jmax=$(read_param JMAX)
s1=$(read_param S1)
s2=$(read_param S2)
h1=$(read_param H1)
h2=$(read_param H2)
h3=$(read_param H3)
h4=$(read_param H4)

[[ "$server_public_key" =~ ^[A-Za-z0-9+/]{43}=$ ]]
[[ "$preshared_key" =~ ^[A-Za-z0-9+/]{43}=$ ]]
[[ "$endpoint" =~ ^[^[:space:]:]+:[0-9]{1,5}$ ]]
for value in "$jc" "$jmin" "$jmax" "$s1" "$s2" "$h1" "$h2" "$h3" "$h4"; do
  [[ "$value" =~ ^[0-9]+$ ]]
done

private_key=$(awk -F= '$1 ~ /^[[:space:]]*PrivateKey[[:space:]]*$/ { sub(/^[^=]*=[[:space:]]*/, ""); print; found++ } END { exit found == 1 ? 0 : 1 }' "$config_file")
address=$(awk -F= '$1 ~ /^[[:space:]]*Address[[:space:]]*$/ { sub(/^[^=]*=[[:space:]]*/, ""); print; found++ } END { exit found == 1 ? 0 : 1 }' "$config_file")
[[ "$private_key" =~ ^[A-Za-z0-9+/]{43}=$ ]]
[[ "$address" == 10.8.1.250/32 ]]

config_dir=$(dirname -- "$config_file")
staging_dir=$(mktemp -d "$config_dir/.awg-exit-switch.XXXXXX")
staging="$staging_dir/awg-exit.conf"
backup="$config_file.backup.$(date -u +%Y%m%dT%H%M%SZ)"
switched=false

cleanup() {
  rm -f "$staging"
  rmdir "$staging_dir" 2>/dev/null || true
}

rollback() {
  local status=$?
  trap - ERR
  if [[ "$switched" == true && -f "$backup" ]]; then
    systemctl stop my-utils-awg-exit.service || true
    install -m 600 "$backup" "$config_file"
    systemctl start my-utils-awg-exit.service || true
    echo "New AWG client failed validation; previous config restored" >&2
  fi
  cleanup
  exit "$status"
}
trap rollback ERR
trap cleanup EXIT

cat >"$staging" <<EOF
[Interface]
Address = $address
PrivateKey = $private_key
Jc = $jc
Jmin = $jmin
Jmax = $jmax
S1 = $s1
S2 = $s2
H1 = $h1
H2 = $h2
H3 = $h3
H4 = $h4
MTU = 1380
Table = off

[Peer]
PublicKey = $server_public_key
PresharedKey = $preshared_key
Endpoint = $endpoint
AllowedIPs = 0.0.0.0/0
PersistentKeepalive = 25
EOF
chmod 600 "$staging"
awg-quick strip "$staging" >/dev/null
cp -a "$config_file" "$backup"

switched=true
systemctl stop my-utils-awg-exit.service
install -m 600 "$staging" "$config_file"
switch_started=$(date +%s)
systemctl start my-utils-awg-exit.service

handshake=0
for _ in $(seq 1 45); do
  handshake=$(awg show "$interface" latest-handshakes | awk 'BEGIN { max=0 } { if ($2 > max) max=$2 } END { print max }')
  ((handshake >= switch_started)) && break
  sleep 1
done
((handshake >= switch_started))

egress=$(curl --fail --silent --show-error --max-time 15 --interface "$interface" https://api.ipify.org)
[[ "$egress" == "$expected_egress" ]]

switched=false
trap - ERR
cleanup
trap - EXIT
echo "AWG client switched; handshake and egress verified"
