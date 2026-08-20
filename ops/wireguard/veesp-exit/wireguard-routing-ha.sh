#!/usr/bin/env bash
set -euo pipefail

env_file=${WIREGUARD_ROUTING_ENV_FILE:-/etc/my-utils/wireguard-routing-ha.env}
[[ -r "$env_file" ]] || { echo "WireGuard routing environment is not readable: $env_file" >&2; exit 1; }
# shellcheck source=/dev/null
source "$env_file"

required=(
  WIREGUARD_CLIENT_CIDR WIREGUARD_INGRESS_INTERFACE
  WIREGUARD_PRIMARY_EXIT WIREGUARD_SECONDARY_EXIT WIREGUARD_EXIT_PATTERN
  WIREGUARD_ROUTE_TABLE WIREGUARD_ROUTE_PRIORITY WIREGUARD_LISTEN_PORT
)
for name in "${required[@]}"; do
  [[ -n "${!name:-}" ]] || { echo "Missing WireGuard routing setting: $name" >&2; exit 1; }
done

chain=MYUTILS-WG-USERS

start() {
  local current_managed_default
  sysctl -q -w net.ipv4.ip_forward=1
  ip route replace unreachable default table "$WIREGUARD_ROUTE_TABLE" metric 32767
  current_managed_default=$(ip -4 route show table "$WIREGUARD_ROUTE_TABLE" | awk \
    -v primary="$WIREGUARD_PRIMARY_EXIT" -v secondary="$WIREGUARD_SECONDARY_EXIT" \
    '$1 == "default" && $2 == "dev" && ($3 == primary || $3 == secondary) { print $3; exit }')
  if [[ -z "$current_managed_default" ]]; then
    ip route replace default dev "$WIREGUARD_PRIMARY_EXIT" table "$WIREGUARD_ROUTE_TABLE" metric 10
  fi
  ip rule show | grep -Fq "from $WIREGUARD_CLIENT_CIDR lookup $WIREGUARD_ROUTE_TABLE" || \
    ip rule add priority "$WIREGUARD_ROUTE_PRIORITY" from "$WIREGUARD_CLIENT_CIDR" table "$WIREGUARD_ROUTE_TABLE"

  iptables -N "$chain" 2>/dev/null || true
  iptables -F "$chain"
  iptables -A "$chain" -i "$WIREGUARD_INGRESS_INTERFACE" -s "$WIREGUARD_CLIENT_CIDR" -o "$WIREGUARD_EXIT_PATTERN" -j ACCEPT
  iptables -A "$chain" -i "$WIREGUARD_EXIT_PATTERN" -d "$WIREGUARD_CLIENT_CIDR" -m conntrack --ctstate ESTABLISHED,RELATED -j ACCEPT
  iptables -A "$chain" -i "$WIREGUARD_INGRESS_INTERFACE" -s "$WIREGUARD_CLIENT_CIDR" -j REJECT --reject-with icmp-admin-prohibited
  iptables -A "$chain" -j RETURN
  iptables -C FORWARD -j "$chain" 2>/dev/null || iptables -I FORWARD 1 -j "$chain"
  iptables -C INPUT -p udp --dport "$WIREGUARD_LISTEN_PORT" -j ACCEPT 2>/dev/null || \
    iptables -I INPUT 1 -p udp --dport "$WIREGUARD_LISTEN_PORT" -j ACCEPT
}

stop() {
  iptables -D INPUT -p udp --dport "$WIREGUARD_LISTEN_PORT" -j ACCEPT 2>/dev/null || true
  iptables -D FORWARD -j "$chain" 2>/dev/null || true
  iptables -F "$chain" 2>/dev/null || true
  iptables -X "$chain" 2>/dev/null || true
  ip rule del priority "$WIREGUARD_ROUTE_PRIORITY" from "$WIREGUARD_CLIENT_CIDR" table "$WIREGUARD_ROUTE_TABLE" 2>/dev/null || true
  ip route flush table "$WIREGUARD_ROUTE_TABLE" 2>/dev/null || true
}

case "${1:-start}" in
  start) start ;;
  stop) stop ;;
  *) echo "Usage: $0 start|stop" >&2; exit 2 ;;
esac
