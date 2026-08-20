#!/usr/bin/env bash
set -euo pipefail

mode=plan
replace=false
primary_interface=awg-exit
secondary_interface=awg-exit-b
primary_egress=""
secondary_egress=""
client_cidr=10.89.0.0/24
ingress_interface=wg-users
listen_port=51820
api_proxy_script=""

usage() {
  echo "Usage: $0 --primary-egress IPv4 --secondary-egress IPv4 [--primary-interface NAME] [--secondary-interface NAME] [--api-proxy-script FILE] [--apply] [--replace]" >&2
}

while (($#)); do
  case "$1" in
    --primary-interface) primary_interface=${2:-}; shift 2 ;;
    --secondary-interface) secondary_interface=${2:-}; shift 2 ;;
    --primary-egress) primary_egress=${2:-}; shift 2 ;;
    --secondary-egress) secondary_egress=${2:-}; shift 2 ;;
    --api-proxy-script) api_proxy_script=${2:-}; shift 2 ;;
    --apply) mode=apply; shift ;;
    --replace) replace=true; shift ;;
    *) usage; exit 2 ;;
  esac
done

[[ $EUID -eq 0 ]] || { echo "Run as root" >&2; exit 1; }
for command in awg curl flock install ip jq ping python3 systemctl; do
  command -v "$command" >/dev/null || { echo "Required command is missing: $command" >&2; exit 1; }
done
for interface in "$primary_interface" "$secondary_interface"; do
  [[ "$interface" =~ ^[a-zA-Z0-9_.-]+$ && ${#interface} -le 15 ]]
done
[[ "$primary_interface" != "$secondary_interface" ]]
for expected in "$primary_egress" "$secondary_egress"; do
  [[ "$expected" =~ ^([0-9]{1,3}\.){3}[0-9]{1,3}$ ]]
done

source_dir=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)
for file in awg-failover.sh decide-failover.py my-utils-awg-failover.service my-utils-awg-failover.timer wireguard-routing-ha.sh my-utils-wireguard-routing-ha.service; do
  [[ -f "$source_dir/$file" ]] || { echo "Installer asset is missing: $file" >&2; exit 1; }
done
if [[ -z "$api_proxy_script" ]]; then
  api_proxy_script=$source_dir/../api-proxy-routing.sh
fi
[[ -f "$api_proxy_script" ]] || { echo "API proxy routing asset is missing: $api_proxy_script" >&2; exit 1; }
api_proxy_unit=$source_dir/../systemd/my-utils-api-proxy-routing.service
[[ -f "$api_proxy_unit" ]] || { echo "API proxy routing systemd unit is missing: $api_proxy_unit" >&2; exit 1; }
env_file=/etc/my-utils/awg-failover.env
routing_env_file=/etc/my-utils/wireguard-routing-ha.env
if [[ ( -e "$env_file" || -e "$routing_env_file" || -e /usr/local/libexec/my-utils-wireguard-routing ) && "$replace" != true ]]; then
  echo "Refusing to replace existing routing/failover state without --replace" >&2
  exit 1
fi

echo "Plan: monitor $primary_interface and $secondary_interface, prefer primary, and fail closed after hysteresis"
if [[ "$mode" != apply ]]; then
  echo "Plan only; no host changes were made"
  exit 0
fi

for interface in "$primary_interface" "$secondary_interface"; do
  ip link show dev "$interface" >/dev/null
  awg show "$interface" >/dev/null
done
[[ "$(curl -4fsS --max-time 10 --interface "$primary_interface" https://api.ipify.org)" == "$primary_egress" ]]
[[ "$(curl -4fsS --max-time 10 --interface "$secondary_interface" https://api.ipify.org)" == "$secondary_egress" ]]

install -d -m 700 /etc/my-utils /var/lib/my-utils-wireguard
install -d -m 755 /usr/local/libexec
timestamp=$(date -u +%Y%m%dT%H%M%SZ)
for target in /usr/local/libexec/my-utils-wireguard-routing /usr/local/libexec/my-utils-api-proxy-routing /etc/systemd/system/my-utils-wireguard-routing.service /etc/systemd/system/my-utils-api-proxy-routing.service; do
  if [[ -e "$target" ]]; then cp -a -- "$target" "$target.backup.$timestamp"; fi
done
install -m 755 "$source_dir/awg-failover.sh" /usr/local/libexec/my-utils-awg-failover
install -m 755 "$source_dir/decide-failover.py" /usr/local/libexec/decide-failover.py
install -m 755 "$source_dir/wireguard-routing-ha.sh" /usr/local/libexec/my-utils-wireguard-routing
install -m 755 "$api_proxy_script" /usr/local/libexec/my-utils-api-proxy-routing
install -m 644 "$source_dir/my-utils-awg-failover.service" /etc/systemd/system/
install -m 644 "$source_dir/my-utils-awg-failover.timer" /etc/systemd/system/
install -m 644 "$source_dir/my-utils-wireguard-routing-ha.service" /etc/systemd/system/my-utils-wireguard-routing.service
install -m 644 "$api_proxy_unit" /etc/systemd/system/my-utils-api-proxy-routing.service
env_tmp=$(mktemp /etc/my-utils/.awg-failover.XXXXXX)
cat >"$env_tmp" <<EOF
# managed-by-my-utils
AWG_PRIMARY_INTERFACE=$primary_interface
AWG_PRIMARY_EXPECTED_EGRESS=$primary_egress
AWG_SECONDARY_INTERFACE=$secondary_interface
AWG_SECONDARY_EXPECTED_EGRESS=$secondary_egress
AWG_ROUTE_TABLE=51889
AWG_FAILURE_THRESHOLD=3
AWG_RECOVERY_THRESHOLD=2
AWG_HANDSHAKE_MAX_AGE=180
AWG_PROBE_URL=https://api.ipify.org
AWG_LATENCY_TARGET=1.1.1.1
AWG_STATE_FILE=/var/lib/my-utils-wireguard/awg-failover-state.json
AWG_STATUS_FILE=/var/lib/my-utils-wireguard/exit-health.json
AWG_PREFERENCE_FILE=/var/lib/my-utils-wireguard/exit-preference
EOF
chmod 600 "$env_tmp"
mv -f -- "$env_tmp" "$env_file"
if [[ ! -e /var/lib/my-utils-wireguard/exit-preference ]]; then
  printf 'AUTO\n' >/var/lib/my-utils-wireguard/exit-preference
  chmod 644 /var/lib/my-utils-wireguard/exit-preference
fi
routing_env_tmp=$(mktemp /etc/my-utils/.wireguard-routing-ha.XXXXXX)
cat >"$routing_env_tmp" <<EOF
# managed-by-my-utils
WIREGUARD_CLIENT_CIDR=$client_cidr
WIREGUARD_INGRESS_INTERFACE=$ingress_interface
WIREGUARD_PRIMARY_EXIT=$primary_interface
WIREGUARD_SECONDARY_EXIT=$secondary_interface
WIREGUARD_EXIT_PATTERN=awg-exit+
WIREGUARD_ROUTE_TABLE=51889
WIREGUARD_ROUTE_PRIORITY=1089
WIREGUARD_LISTEN_PORT=$listen_port
EOF
chmod 600 "$routing_env_tmp"
mv -f -- "$routing_env_tmp" "$routing_env_file"

systemctl daemon-reload
/usr/local/libexec/my-utils-wireguard-routing start
/usr/local/libexec/my-utils-api-proxy-routing start
systemctl enable --now my-utils-api-proxy-routing.service
systemctl enable --now my-utils-awg-failover.timer
systemctl start my-utils-awg-failover.service
jq -e '.overallStatus == "HEALTHY" and .activeExit == "primary"' /var/lib/my-utils-wireguard/exit-health.json >/dev/null
echo "AWG failover is active with a healthy primary and reserve"
