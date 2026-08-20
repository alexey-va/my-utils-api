#!/usr/bin/env bash
set -euo pipefail

env_file=${WIREGUARD_DNS_ENV_FILE:-/etc/my-utils/client-dns.env}
[[ -r "$env_file" ]] || { echo "WireGuard DNS environment is not readable: $env_file" >&2; exit 1; }
# shellcheck source=/dev/null
source "$env_file"

client_cidr=${WIREGUARD_DNS_CLIENT_CIDR:?}
ingress_interface=${WIREGUARD_DNS_INGRESS_INTERFACE:?}
resolver_address=${WIREGUARD_DNS_RESOLVER_ADDRESS:?}
chain=MYUTILS-WG-DNS
input_chain=MYUTILS-WG-DNS-IN

start() {
  [[ -d "/sys/class/net/$ingress_interface" ]] || { echo "Interface is not active: $ingress_interface" >&2; exit 1; }
  ip -4 address show dev "$ingress_interface" | grep -Eq "inet ${resolver_address//./\\.}/"

  iptables -t nat -N "$chain" 2>/dev/null || true
  iptables -t nat -F "$chain"
  iptables -t nat -A "$chain" -i "$ingress_interface" -s "$client_cidr" -p udp --dport 53 -j DNAT --to-destination "$resolver_address:53"
  iptables -t nat -A "$chain" -i "$ingress_interface" -s "$client_cidr" -p tcp --dport 53 -j DNAT --to-destination "$resolver_address:53"
  iptables -t nat -A "$chain" -j RETURN
  iptables -t nat -C PREROUTING -j "$chain" 2>/dev/null || iptables -t nat -I PREROUTING 1 -j "$chain"

  iptables -N "$input_chain" 2>/dev/null || true
  iptables -F "$input_chain"
  iptables -A "$input_chain" -i "$ingress_interface" -s "$client_cidr" -d "$resolver_address" -p udp --dport 53 -j ACCEPT
  iptables -A "$input_chain" -i "$ingress_interface" -s "$client_cidr" -d "$resolver_address" -p tcp --dport 53 -j ACCEPT
  iptables -A "$input_chain" -j RETURN
  iptables -C INPUT -j "$input_chain" 2>/dev/null || iptables -I INPUT 1 -j "$input_chain"
}

stop() {
  while iptables -D INPUT -j "$input_chain" 2>/dev/null; do :; done
  iptables -F "$input_chain" 2>/dev/null || true
  iptables -X "$input_chain" 2>/dev/null || true
  while iptables -t nat -D PREROUTING -j "$chain" 2>/dev/null; do :; done
  iptables -t nat -F "$chain" 2>/dev/null || true
  iptables -t nat -X "$chain" 2>/dev/null || true
}

case "${1:-start}" in
  start) start ;;
  stop) stop ;;
  *) echo "Usage: $0 start|stop" >&2; exit 2 ;;
esac
