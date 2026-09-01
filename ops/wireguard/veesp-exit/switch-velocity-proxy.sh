#!/usr/bin/env bash
set -euo pipefail

umask 077

config_file=/etc/wireguard/wg-utils.conf
interface=wg-utils
proxy_selector_ip=91.197.0.191
old_tunnel_proxy_ip=172.29.172.1
new_tunnel_proxy_ip=172.29.172.3
proxy_port=8888
source_address=10.89.0.7
expected_egress=""

usage() {
  echo "Usage: $0 --expected-egress IPv4 [--config FILE]" >&2
}

while (($#)); do
  case "$1" in
    --config) config_file=${2:-}; shift 2 ;;
    --expected-egress) expected_egress=${2:-}; shift 2 ;;
    *) usage; exit 2 ;;
  esac
done

[[ $EUID -eq 0 ]] || { echo "Run as root" >&2; exit 1; }
for command in curl install ip iptables stat wg wg-quick; do
  command -v "$command" >/dev/null || { echo "Required command is missing: $command" >&2; exit 1; }
done
[[ -f "$config_file" ]]
[[ "$(stat -c '%a' "$config_file")" == 600 ]] || { echo "wg-utils config must have mode 600" >&2; exit 1; }
[[ "$expected_egress" =~ ^([0-9]{1,3}\.){3}[0-9]{1,3}$ ]]
ip link show "$interface" >/dev/null

verify_proxy() {
  local egress
  egress=$(curl --fail --silent --show-error --max-time 20 \
    --proxy "http://$proxy_selector_ip:$proxy_port" http://api.ipify.org)
  [[ "$egress" == "$expected_egress" ]]
}

if grep -Fq "$new_tunnel_proxy_ip" "$config_file" && ! grep -Fq "$old_tunnel_proxy_ip" "$config_file"; then
  verify_proxy
  echo "Velocity proxy route is already current and verified"
  exit 0
fi

grep -Fq "$old_tunnel_proxy_ip" "$config_file"
! grep -Fq "$new_tunnel_proxy_ip" "$config_file"

config_dir=$(dirname -- "$config_file")
staging_dir=$(mktemp -d "$config_dir/.wg-utils-switch.XXXXXX")
staging="$staging_dir/wg-utils.conf"
backup="$config_file.backup.$(date -u +%Y%m%dT%H%M%SZ)"
switched=false

cleanup() {
  rm -f "$staging"
  rmdir "$staging_dir" 2>/dev/null || true
}

delete_rule() {
  local table=$1 chain=$2
  shift 2
  while iptables -t "$table" -D "$chain" "$@" 2>/dev/null; do :; done
}

add_rule() {
  local table=$1 chain=$2
  shift 2
  iptables -t "$table" -C "$chain" "$@" 2>/dev/null || iptables -t "$table" -I "$chain" 1 "$@"
}

rollback() {
  local status=$?
  trap - ERR
  if [[ "$switched" == true && -f "$backup" ]]; then
    wg syncconf wg-utils <(wg-quick strip "$backup") || true
    add_rule nat OUTPUT -p tcp -d "$proxy_selector_ip" --dport "$proxy_port" -j DNAT --to-destination "$old_tunnel_proxy_ip:$proxy_port" || true
    add_rule nat POSTROUTING -o "$interface" -p tcp -d "$old_tunnel_proxy_ip" --dport "$proxy_port" -j SNAT --to-source "$source_address" || true
    delete_rule nat OUTPUT -p tcp -d "$proxy_selector_ip" --dport "$proxy_port" -j DNAT --to-destination "$new_tunnel_proxy_ip:$proxy_port"
    delete_rule nat POSTROUTING -o "$interface" -p tcp -d "$new_tunnel_proxy_ip" --dport "$proxy_port" -j SNAT --to-source "$source_address"
    ip route replace "$old_tunnel_proxy_ip/32" dev "$interface" || true
    ip route del "$new_tunnel_proxy_ip/32" dev "$interface" 2>/dev/null || true
    install -m 600 "$backup" "$config_file"
    echo "Velocity proxy switch failed; previous route restored" >&2
  fi
  cleanup
  exit "$status"
}
trap rollback ERR
trap cleanup EXIT

sed "s/$old_tunnel_proxy_ip/$new_tunnel_proxy_ip/g" "$config_file" >"$staging"
chmod 600 "$staging"
wg-quick strip "$staging" >/dev/null
cp -a "$config_file" "$backup"
switched=true

ip route replace "$new_tunnel_proxy_ip/32" dev "$interface"
add_rule nat OUTPUT -p tcp -d "$proxy_selector_ip" --dport "$proxy_port" -j DNAT --to-destination "$new_tunnel_proxy_ip:$proxy_port"
add_rule nat POSTROUTING -o "$interface" -p tcp -d "$new_tunnel_proxy_ip" --dport "$proxy_port" -j SNAT --to-source "$source_address"
wg syncconf wg-utils <(wg-quick strip "$staging")
delete_rule nat OUTPUT -p tcp -d "$proxy_selector_ip" --dport "$proxy_port" -j DNAT --to-destination "$old_tunnel_proxy_ip:$proxy_port"
delete_rule nat POSTROUTING -o "$interface" -p tcp -d "$old_tunnel_proxy_ip" --dport "$proxy_port" -j SNAT --to-source "$source_address"
ip route del "$old_tunnel_proxy_ip/32" dev "$interface" 2>/dev/null || true
install -m 600 "$staging" "$config_file"

verify_proxy
switched=false
trap - ERR
cleanup
trap - EXIT
echo "Velocity proxy route switched live and verified"
