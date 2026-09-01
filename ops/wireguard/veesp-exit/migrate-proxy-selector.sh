#!/usr/bin/env bash
set -euo pipefail

umask 077

config_file=/etc/wireguard/wg-utils.conf
interface=wg-utils
old_proxy_selector_ip=185.242.106.81
new_proxy_selector_ip=91.197.0.191
tunnel_proxy_ip=172.29.172.3
proxy_port=8888
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
for command in curl ip iptables stat wg-quick; do
  command -v "$command" >/dev/null || { echo "Required command is missing: $command" >&2; exit 1; }
done
[[ -f "$config_file" ]]
[[ "$(stat -c '%a' "$config_file")" == 600 ]] || { echo "wg-utils config must have mode 600" >&2; exit 1; }
[[ "$expected_egress" =~ ^([0-9]{1,3}\.){3}[0-9]{1,3}$ ]]
ip link show "$interface" >/dev/null

verify_proxy() {
  local selector=$1 egress
  egress=$(curl --fail --silent --show-error --max-time 20 \
    --proxy "http://$selector:$proxy_port" http://api.ipify.org)
  [[ "$egress" == "$expected_egress" ]]
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

new_rule_preexisting=false
if iptables -t nat -C OUTPUT -p tcp -d "$new_proxy_selector_ip" --dport "$proxy_port" \
  -j DNAT --to-destination "$tunnel_proxy_ip:$proxy_port" 2>/dev/null; then
  new_rule_preexisting=true
fi

if ! grep -Fq "$old_proxy_selector_ip" "$config_file" && grep -Fq "$new_proxy_selector_ip" "$config_file"; then
  add_rule nat OUTPUT -p tcp -d "$new_proxy_selector_ip" --dport "$proxy_port" \
    -j DNAT --to-destination "$tunnel_proxy_ip:$proxy_port"
  if ! verify_proxy "$new_proxy_selector_ip"; then
    if [[ "$new_rule_preexisting" == false ]]; then
      delete_rule nat OUTPUT -p tcp -d "$new_proxy_selector_ip" --dport "$proxy_port" \
        -j DNAT --to-destination "$tunnel_proxy_ip:$proxy_port"
    fi
    echo "Current Velocity proxy selector failed verification" >&2
    exit 1
  fi
  delete_rule nat OUTPUT -p tcp -d "$old_proxy_selector_ip" --dport "$proxy_port" \
    -j DNAT --to-destination "$tunnel_proxy_ip:$proxy_port"
  echo "Velocity proxy selector is already current and verified"
  exit 0
fi

grep -Fq "$old_proxy_selector_ip" "$config_file"
config_dir=$(dirname -- "$config_file")
staging_dir=$(mktemp -d "$config_dir/.wg-utils-selector.XXXXXX")
staging="$staging_dir/wg-utils.conf"
backup="$config_file.backup.$(date -u +%Y%m%dT%H%M%SZ)"
installed=false

cleanup() {
  rm -f "$staging"
  rmdir "$staging_dir" 2>/dev/null || true
}

rollback() {
  local status=$?
  trap - ERR
  if [[ "$installed" == true && -f "$backup" ]]; then
    cp -a "$backup" "$config_file"
  fi
  add_rule nat OUTPUT -p tcp -d "$old_proxy_selector_ip" --dport "$proxy_port" \
    -j DNAT --to-destination "$tunnel_proxy_ip:$proxy_port" || true
  if [[ "$new_rule_preexisting" == false ]]; then
    delete_rule nat OUTPUT -p tcp -d "$new_proxy_selector_ip" --dport "$proxy_port" \
      -j DNAT --to-destination "$tunnel_proxy_ip:$proxy_port"
  fi
  cleanup
  echo "Velocity proxy selector migration failed; previous selector restored" >&2
  exit "$status"
}
trap rollback ERR
trap cleanup EXIT

sed "s/$old_proxy_selector_ip/$new_proxy_selector_ip/g" "$config_file" >"$staging"
chmod 600 "$staging"
wg-quick strip "$staging" >/dev/null
cp -a "$config_file" "$backup"

add_rule nat OUTPUT -p tcp -d "$new_proxy_selector_ip" --dport "$proxy_port" \
  -j DNAT --to-destination "$tunnel_proxy_ip:$proxy_port"
verify_proxy "$new_proxy_selector_ip"

mv -f "$staging" "$config_file"
installed=true
delete_rule nat OUTPUT -p tcp -d "$old_proxy_selector_ip" --dport "$proxy_port" \
  -j DNAT --to-destination "$tunnel_proxy_ip:$proxy_port"
verify_proxy "$new_proxy_selector_ip"

installed=false
trap - ERR
cleanup
trap - EXIT
echo "Velocity proxy selector migrated live and verified"
