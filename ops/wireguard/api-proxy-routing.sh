#!/usr/bin/env bash
set -euo pipefail

docker_cidr=172.16.0.0/12
proxy_destination=91.197.0.191/32
tunnel_proxy_destination=172.29.172.3
proxy_port=8888
egress_interface_pattern=awg-exit+
source_address=10.89.0.1
table=51889
priority=1087
mark=0x51891
mark_mask=0xffffffff
mark_chain=MYUTILS-API-PROXY
nat_chain=MYUTILS-API-PROXY-NAT
dnat_chain=MYUTILS-API-PROXY-DNAT

start() {
  ip route show table "$table" | grep -Eq '^default dev awg-exit([[:alnum:]_.-]*) '

  while ip rule del priority "$priority" 2>/dev/null; do :; done
  ip rule add priority "$priority" fwmark "$mark/$mark_mask" lookup "$table"

  iptables -t mangle -N "$mark_chain" 2>/dev/null || true
  iptables -t mangle -F "$mark_chain"
  iptables -t mangle -A "$mark_chain" -s "$docker_cidr" -d "$proxy_destination" -p tcp --dport "$proxy_port" -j MARK --set-xmark "$mark/$mark_mask"
  iptables -t mangle -A "$mark_chain" -j RETURN
  iptables -t mangle -C PREROUTING -j "$mark_chain" 2>/dev/null || iptables -t mangle -I PREROUTING 1 -j "$mark_chain"

  iptables -t nat -N "$dnat_chain" 2>/dev/null || true
  iptables -t nat -F "$dnat_chain"
  iptables -t nat -A "$dnat_chain" -s "$docker_cidr" -d "$proxy_destination" -p tcp --dport "$proxy_port" -j DNAT --to-destination "$tunnel_proxy_destination:$proxy_port"
  iptables -t nat -A "$dnat_chain" -j RETURN
  iptables -t nat -C PREROUTING -j "$dnat_chain" 2>/dev/null || iptables -t nat -I PREROUTING 1 -j "$dnat_chain"

  iptables -t nat -N "$nat_chain" 2>/dev/null || true
  iptables -t nat -F "$nat_chain"
  iptables -t nat -A "$nat_chain" -s "$docker_cidr" -d "$tunnel_proxy_destination/32" -p tcp --dport "$proxy_port" -o "$egress_interface_pattern" -m mark --mark "$mark/$mark_mask" -j SNAT --to-source "$source_address"
  iptables -t nat -A "$nat_chain" -j RETURN
  iptables -t nat -C POSTROUTING -j "$nat_chain" 2>/dev/null || iptables -t nat -I POSTROUTING 1 -j "$nat_chain"
}

stop() {
  while iptables -t nat -D POSTROUTING -j "$nat_chain" 2>/dev/null; do :; done
  iptables -t nat -F "$nat_chain" 2>/dev/null || true
  iptables -t nat -X "$nat_chain" 2>/dev/null || true
  while iptables -t nat -D PREROUTING -j "$dnat_chain" 2>/dev/null; do :; done
  iptables -t nat -F "$dnat_chain" 2>/dev/null || true
  iptables -t nat -X "$dnat_chain" 2>/dev/null || true
  while iptables -t mangle -D PREROUTING -j "$mark_chain" 2>/dev/null; do :; done
  iptables -t mangle -F "$mark_chain" 2>/dev/null || true
  iptables -t mangle -X "$mark_chain" 2>/dev/null || true
  while ip rule del priority "$priority" 2>/dev/null; do :; done
}

case "${1:-start}" in
  start) start ;;
  stop) stop ;;
  *) echo "Usage: $0 start|stop" >&2; exit 2 ;;
esac
