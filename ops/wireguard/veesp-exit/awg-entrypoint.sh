#!/usr/bin/env bash
set -euo pipefail

interface=${AWG_INTERFACE:-awg0}
config=${AWG_CONFIG:-/config/awg0.conf}
client_cidr=${CLIENT_CIDR:-10.89.0.0/24}
tunnel_client_ip=${TUNNEL_CLIENT_IP:-10.8.1.250/32}
tinyproxy_ip=${TINYPROXY_IP:-172.29.172.3}
container_ip=${CONTAINER_IP:-172.29.172.2}
forward_chain=MYUTILS_AWG_FORWARD
nat_chain=MYUTILS_AWG_NAT
awg_pid=""
kernel_mode=false

case "$interface" in
  *[!a-zA-Z0-9_.-]*|'') echo "Invalid AWG interface" >&2; exit 1 ;;
esac
[[ "$client_cidr" == 10.89.0.0/24 ]]
[[ "$tunnel_client_ip" == 10.8.1.250/32 ]]
[[ "$tinyproxy_ip" == 172.29.172.3 ]]
[[ "$container_ip" == 172.29.172.2 ]]
[[ -r "$config" ]]

cleanup() {
  iptables -w -t nat -D POSTROUTING -j "$nat_chain" 2>/dev/null || true
  iptables -w -t nat -F "$nat_chain" 2>/dev/null || true
  iptables -w -t nat -X "$nat_chain" 2>/dev/null || true
  iptables -w -D FORWARD -j "$forward_chain" 2>/dev/null || true
  iptables -w -F "$forward_chain" 2>/dev/null || true
  iptables -w -X "$forward_chain" 2>/dev/null || true
  ip link delete "$interface" 2>/dev/null || true
  if [[ -n "$awg_pid" ]]; then
    kill "$awg_pid" 2>/dev/null || true
    wait "$awg_pid" 2>/dev/null || true
  fi
}
trap cleanup EXIT INT TERM

ip link delete "$interface" 2>/dev/null || true
mkdir -p /run/amneziawg
rm -f "/run/amneziawg/$interface.sock"
if ip link add "$interface" type amneziawg; then
  kernel_mode=true
else
  amneziawg-go -f "$interface" &
  awg_pid=$!
  for _ in $(seq 1 50); do
    [[ -S "/run/amneziawg/$interface.sock" ]] && break
    kill -0 "$awg_pid" 2>/dev/null || { echo "amneziawg-go exited" >&2; exit 1; }
    sleep 0.1
  done
  [[ -S "/run/amneziawg/$interface.sock" ]]
fi

awg setconf "$interface" <(awg-quick strip "$config")
address=$(sed -n 's/^Address[[:space:]]*=[[:space:]]*//p' "$config" | head -n 1)
mtu=$(sed -n 's/^MTU[[:space:]]*=[[:space:]]*//p' "$config" | head -n 1)
[[ -n "$address" && -n "$mtu" ]]
ip address add "$address" dev "$interface"
ip link set dev "$interface" mtu "$mtu" up
ip route replace "$client_cidr" dev "$interface"

iptables -w -N "$forward_chain"
iptables -w -A "$forward_chain" -i "$interface" -o eth0 -s "$client_cidr" -j ACCEPT
iptables -w -A "$forward_chain" -i "$interface" -o eth0 -s "$tunnel_client_ip" -j ACCEPT
iptables -w -A "$forward_chain" -i eth0 -o "$interface" -d "$client_cidr" -m conntrack --ctstate RELATED,ESTABLISHED -j ACCEPT
iptables -w -A "$forward_chain" -i eth0 -o "$interface" -d "$tunnel_client_ip" -m conntrack --ctstate RELATED,ESTABLISHED -j ACCEPT
iptables -w -A "$forward_chain" -j RETURN
iptables -w -I FORWARD 1 -j "$forward_chain"

iptables -w -t nat -N "$nat_chain"
iptables -w -t nat -A "$nat_chain" -s "$client_cidr" -d "$tinyproxy_ip/32" -o eth0 -j SNAT --to-source "$container_ip"
iptables -w -t nat -A "$nat_chain" -s "$tunnel_client_ip" -d "$tinyproxy_ip/32" -o eth0 -j SNAT --to-source "$container_ip"
iptables -w -t nat -A "$nat_chain" -s "$client_cidr" '!' -d 172.29.172.0/24 -o eth0 -j MASQUERADE
iptables -w -t nat -A "$nat_chain" -s "$tunnel_client_ip" '!' -d 172.29.172.0/24 -o eth0 -j MASQUERADE
iptables -w -t nat -A "$nat_chain" -j RETURN
iptables -w -t nat -I POSTROUTING 1 -j "$nat_chain"

if [[ "$kernel_mode" == true ]]; then
  while sleep 3600; do :; done
else
  wait "$awg_pid"
fi
