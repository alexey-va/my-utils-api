#!/usr/bin/env bash
set -euo pipefail

umask 077

mode=plan
replace=false
interface=""
params_file=""
private_key_file=""
expected_egress=""

usage() {
  echo "Usage: $0 --interface NAME --params FILE --private-key-file FILE --expected-egress IPv4 [--apply] [--replace]" >&2
}

while (($#)); do
  case "$1" in
    --interface) interface=${2:-}; shift 2 ;;
    --params) params_file=${2:-}; shift 2 ;;
    --private-key-file) private_key_file=${2:-}; shift 2 ;;
    --expected-egress) expected_egress=${2:-}; shift 2 ;;
    --apply) mode=apply; shift ;;
    --replace) replace=true; shift ;;
    *) usage; exit 2 ;;
  esac
done

[[ $EUID -eq 0 ]] || { echo "Run as root" >&2; exit 1; }
for command in awg awg-quick curl install ip systemctl stat; do
  command -v "$command" >/dev/null || { echo "Required command is missing: $command" >&2; exit 1; }
done
[[ "$interface" =~ ^[a-zA-Z0-9_.-]+$ && ${#interface} -le 15 && "$interface" != awg-exit ]]
[[ -f "$params_file" && -f "$private_key_file" ]]
[[ "$(stat -c '%a' "$params_file")" == 600 ]]
[[ "$(stat -c '%a' "$private_key_file")" == 600 ]]
[[ "$expected_egress" =~ ^([0-9]{1,3}\.){3}[0-9]{1,3}$ ]]

read_param() {
  local key=$1
  awk -F= -v wanted="$key" '$1 == wanted { sub(/^[^=]*=/, ""); print; found++ } END { exit found == 1 ? 0 : 1 }' "$params_file"
}

private_key=$(tr -d '\r\n' <"$private_key_file")
server_public_key=$(read_param SERVER_PUBLIC_KEY)
preshared_key=$(read_param PRESHARED_KEY)
endpoint=$(read_param ENDPOINT)
client_address=$(read_param CLIENT_ADDRESS)
jc=$(read_param JC)
jmin=$(read_param JMIN)
jmax=$(read_param JMAX)
s1=$(read_param S1)
s2=$(read_param S2)
h1=$(read_param H1)
h2=$(read_param H2)
h3=$(read_param H3)
h4=$(read_param H4)

[[ "$private_key" =~ ^[A-Za-z0-9+/]{43}=$ ]]
[[ "$server_public_key" =~ ^[A-Za-z0-9+/]{43}=$ ]]
[[ "$preshared_key" =~ ^[A-Za-z0-9+/]{43}=$ ]]
[[ "$endpoint" =~ ^[^[:space:]:]+:[0-9]{1,5}$ ]]
[[ "$client_address" =~ ^10\.8\.([0-9]{1,3})\.250/32$ ]]
((10#${BASH_REMATCH[1]} >= 1 && 10#${BASH_REMATCH[1]} <= 254))
for value in "$jc" "$jmin" "$jmax" "$s1" "$s2" "$h1" "$h2" "$h3" "$h4"; do
  [[ "$value" =~ ^[0-9]+$ ]]
done

config_dir=/etc/amnezia/amneziawg
config_file=$config_dir/$interface.conf
service=my-utils-$interface.service
unit_file=/etc/systemd/system/$service
if [[ -e "$config_file" || -e "$unit_file" ]] && [[ "$replace" != true ]]; then
  echo "Refusing to replace the existing reserve client without --replace" >&2
  exit 1
fi

echo "Plan: install and validate reserve interface $interface without changing policy table 51889"
if [[ "$mode" != apply ]]; then
  echo "Plan only; no host changes were made"
  exit 0
fi

install -d -m 700 "$config_dir"
tmp_dir=$(mktemp -d "$config_dir/.reserve-client.XXXXXX")
staging=$tmp_dir/$interface.conf
config_backup=""
unit_backup=""
was_active=false
route_before=$(ip -4 route show table 51889 2>/dev/null || true)

cleanup() { rm -rf -- "$tmp_dir"; }
rollback() {
  local status=$?
  trap - ERR
  systemctl disable --now "$service" 2>/dev/null || true
  if [[ -n "$config_backup" ]]; then install -m 600 "$config_backup" "$config_file"; else rm -f -- "$config_file"; fi
  if [[ -n "$unit_backup" ]]; then install -m 644 "$unit_backup" "$unit_file"; else rm -f -- "$unit_file"; fi
  systemctl daemon-reload || true
  if [[ "$was_active" == true ]]; then systemctl enable --now "$service" || true; fi
  cleanup
  echo "Reserve AWG client installation failed; previous state restored" >&2
  exit "$status"
}
trap rollback ERR
trap cleanup EXIT

if systemctl is-active --quiet "$service"; then was_active=true; fi
timestamp=$(date -u +%Y%m%dT%H%M%SZ)
if [[ -e "$config_file" ]]; then config_backup=$config_file.backup.$timestamp; cp -a "$config_file" "$config_backup"; fi
if [[ -e "$unit_file" ]]; then unit_backup=$unit_file.backup.$timestamp; cp -a "$unit_file" "$unit_backup"; fi

cat >"$staging" <<EOF
[Interface]
Address = $client_address
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
install -m 600 "$staging" "$config_file"
cat >"$unit_file" <<EOF
[Unit]
Description=my-utils reserve AmneziaWG egress ($interface)
After=network-online.target
Wants=network-online.target

[Service]
Type=oneshot
RemainAfterExit=yes
ExecStart=/usr/bin/awg-quick up $config_file
ExecStop=/usr/bin/awg-quick down $config_file

[Install]
WantedBy=multi-user.target
EOF
chmod 644 "$unit_file"
systemctl daemon-reload
switch_started=$(date +%s)
systemctl enable --now "$service"

handshake=0
for _ in $(seq 1 45); do
  handshake=$(awg show "$interface" latest-handshakes | awk 'BEGIN { max=0 } { if ($2 > max) max=$2 } END { print max }')
  ((handshake >= switch_started)) && break
  sleep 1
done
((handshake >= switch_started))
egress=$(curl --fail --silent --show-error --max-time 15 --interface "$interface" https://api.ipify.org)
[[ "$egress" == "$expected_egress" ]]
route_after=$(ip -4 route show table 51889 2>/dev/null || true)
[[ "$route_after" == "$route_before" ]]

trap - ERR
cleanup
trap - EXIT
echo "AWG reserve client is healthy and not selected for policy routing"
