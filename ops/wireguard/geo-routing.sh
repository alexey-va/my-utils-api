#!/usr/bin/env bash
set -euo pipefail

ENV_FILE="${GEO_ROUTING_ENV_FILE:-/etc/my-utils/geo-routing.env}"
if [[ ! -r "$ENV_FILE" ]]; then
  echo "Geo routing environment is not readable: $ENV_FILE" >&2
  exit 1
fi

# shellcheck source=/dev/null
source "$ENV_FILE"

required=(GEO_ROUTING_CLIENT_CIDR GEO_ROUTING_INGRESS_INTERFACE GEO_ROUTING_DIRECT_EGRESS_INTERFACE GEO_ROUTING_STATUS_FILE)
for name in "${required[@]}"; do
  if [[ -z "${!name:-}" ]]; then
    echo "Missing required geo routing setting: $name" >&2
    exit 1
  fi
done

mark=0x51890
mark_mask=0xffffffff
priority=1088
filter_chain=MYUTILS-WG-RU
nat_chain=MYUTILS-WG-RU-NAT

write_awg_only_status() {
  local status_dir status_tmp
  status_dir="$(dirname "$GEO_ROUTING_STATUS_FILE")"
  install -d -m 755 "$status_dir"
  status_tmp="$(mktemp "$status_dir/.geo-routing-status.XXXXXX")"
  printf '{"mode":"AWG_ONLY","ruPrefixCount":0,"updatedAt":"%s"}\n' "$(date -u +%Y-%m-%dT%H:%M:%SZ)" >"$status_tmp"
  chmod 644 "$status_tmp"
  mv -f -- "$status_tmp" "$GEO_ROUTING_STATUS_FILE"
}

start() {
  nft list table ip myutils_wg_geo >/dev/null 2>&1 || nft -f - <<EOF
table ip myutils_wg_geo {
  set ru_ipv4 {
    type ipv4_addr
    flags interval
    auto-merge
  }
  chain prerouting {
    type filter hook prerouting priority mangle; policy accept;
    iifname "$GEO_ROUTING_INGRESS_INTERFACE" ip saddr $GEO_ROUTING_CLIENT_CIDR ip daddr @ru_ipv4 meta mark set $mark
  }
}
EOF

  while ip rule del priority "$priority" 2>/dev/null; do :; done
  ip rule add priority "$priority" fwmark "$mark/$mark_mask" lookup main

  iptables -N "$filter_chain" 2>/dev/null || true
  iptables -F "$filter_chain"
  iptables -A "$filter_chain" -i "$GEO_ROUTING_INGRESS_INTERFACE" -s "$GEO_ROUTING_CLIENT_CIDR" -o "$GEO_ROUTING_DIRECT_EGRESS_INTERFACE" -m mark --mark "$mark/$mark_mask" -j ACCEPT
  iptables -A "$filter_chain" -i "$GEO_ROUTING_DIRECT_EGRESS_INTERFACE" -d "$GEO_ROUTING_CLIENT_CIDR" -m conntrack --ctstate ESTABLISHED,RELATED -j ACCEPT
  iptables -A "$filter_chain" -j RETURN
  iptables -C FORWARD -j "$filter_chain" 2>/dev/null || iptables -I FORWARD 1 -j "$filter_chain"

  iptables -t nat -N "$nat_chain" 2>/dev/null || true
  iptables -t nat -F "$nat_chain"
  iptables -t nat -A "$nat_chain" -s "$GEO_ROUTING_CLIENT_CIDR" -o "$GEO_ROUTING_DIRECT_EGRESS_INTERFACE" -m mark --mark "$mark/$mark_mask" -j MASQUERADE
  iptables -t nat -A "$nat_chain" -j RETURN
  iptables -t nat -C POSTROUTING -j "$nat_chain" 2>/dev/null || iptables -t nat -I POSTROUTING 1 -j "$nat_chain"

  [[ -s "$GEO_ROUTING_STATUS_FILE" ]] || write_awg_only_status
}

stop() {
  iptables -t nat -D POSTROUTING -j "$nat_chain" 2>/dev/null || true
  iptables -t nat -F "$nat_chain" 2>/dev/null || true
  iptables -t nat -X "$nat_chain" 2>/dev/null || true
  iptables -D FORWARD -j "$filter_chain" 2>/dev/null || true
  iptables -F "$filter_chain" 2>/dev/null || true
  iptables -X "$filter_chain" 2>/dev/null || true
  while ip rule del priority "$priority" 2>/dev/null; do :; done
  nft delete table ip myutils_wg_geo 2>/dev/null || true
  write_awg_only_status
}

case "${1:-start}" in
  start) start ;;
  stop) stop ;;
  *) echo "Usage: $0 start|stop" >&2; exit 2 ;;
esac
