#!/usr/bin/env bash
set -euo pipefail

mode=plan
client_cidr=""
ingress_interface=wg-users
resolver_address=""

usage() {
  echo "Usage: $0 --client-cidr CIDR --resolver-address IPv4 [--ingress-interface IFACE] [--apply]" >&2
}

valid_interface() {
  [[ "$1" =~ ^[a-zA-Z0-9_=+.-]{1,15}$ ]]
}

ipv4_number() {
  local value=$1 a b c d
  IFS=. read -r a b c d <<<"$value"
  for octet in "$a" "$b" "$c" "$d"; do
    [[ "$octet" =~ ^(0|[1-9][0-9]{0,2})$ ]] && ((10#$octet <= 255)) || return 1
  done
  printf '%u\n' "$(((10#$a << 24) | (10#$b << 16) | (10#$c << 8) | 10#$d))"
}

validate_network() {
  local cidr=$1 resolver=$2 ip prefix network_number resolver_number host_bits mask broadcast
  [[ "$cidr" == */* ]] || return 1
  ip=${cidr%/*}; prefix=${cidr#*/}
  [[ "$prefix" =~ ^[0-9]+$ ]] && ((prefix >= 16 && prefix <= 29)) || return 1
  network_number=$(ipv4_number "$ip") || return 1
  resolver_number=$(ipv4_number "$resolver") || return 1
  host_bits=$((32 - prefix)); mask=$(((0xffffffff << host_bits) & 0xffffffff))
  (( (network_number & mask) == network_number )) || return 1
  broadcast=$((network_number | (0xffffffff ^ mask)))
  ((resolver_number > network_number && resolver_number < broadcast && (resolver_number & mask) == network_number))
  ((network_number >> 24 == 10 || (network_number >> 24 == 172 && ((network_number >> 16) & 255) >= 16 && ((network_number >> 16) & 255) <= 31) || (network_number >> 24 == 192 && ((network_number >> 16) & 255) == 168)))
}

while (($#)); do
  case "$1" in
    --client-cidr) client_cidr="${2:-}"; shift 2 ;;
    --ingress-interface) ingress_interface="${2:-}"; shift 2 ;;
    --resolver-address) resolver_address="${2:-}"; shift 2 ;;
    --apply) mode=apply; shift ;;
    *) usage; exit 2 ;;
  esac
done

valid_interface "$ingress_interface" || { echo "Invalid ingress interface" >&2; exit 1; }
validate_network "$client_cidr" "$resolver_address" || { echo "Resolver must be a usable address inside a network-aligned private IPv4 /16 through /29" >&2; exit 1; }

echo "Plan: bind a caching resolver only to $resolver_address on $ingress_interface"
echo "Plan: intercept TCP and UDP DNS only from $client_cidr so existing profiles remain valid"
echo "Plan: send resolver upstream traffic through the ordinary utils host route, independent of AWG"
if [[ "$mode" != apply ]]; then
  echo "Plan only; no host changes were made"
  exit 0
fi

[[ $EUID -eq 0 ]] || { echo "Run --apply as root" >&2; exit 1; }
if [[ ! -f /etc/os-release ]] || ! grep -q '^ID=ubuntu$' /etc/os-release; then
  echo "Only Ubuntu is supported" >&2
  exit 1
fi
[[ -d "/sys/class/net/$ingress_interface" ]] || { echo "Interface is not active: $ingress_interface" >&2; exit 1; }

export DEBIAN_FRONTEND=noninteractive
apt-get update
apt-get install -y dnsmasq dnsutils iptables

script_dir=$(cd -- "$(dirname -- "$0")" && pwd)
install -d -m 700 /etc/my-utils
install -d -m 755 /usr/local/libexec /etc/dnsmasq.d /etc/systemd/system/dnsmasq.service.d
install -m 755 -o root -g root "$script_dir/client-dns.sh" /usr/local/libexec/my-utils-wireguard-dns
install -m 644 -o root -g root "$script_dir/systemd/my-utils-wireguard-dns.service" /etc/systemd/system/

env_tmp=$(mktemp)
dnsmasq_tmp=$(mktemp)
cleanup() { rm -f -- "$env_tmp" "$dnsmasq_tmp"; }
trap cleanup EXIT INT TERM
cat >"$env_tmp" <<EOF
WIREGUARD_DNS_CLIENT_CIDR=$client_cidr
WIREGUARD_DNS_INGRESS_INTERFACE=$ingress_interface
WIREGUARD_DNS_RESOLVER_ADDRESS=$resolver_address
EOF
install -m 600 -o root -g root "$env_tmp" /etc/my-utils/client-dns.env

cat >"$dnsmasq_tmp" <<EOF
interface=$ingress_interface
listen-address=$resolver_address
bind-interfaces
no-dhcp-interface=$ingress_interface
no-resolv
server=77.88.8.8
server=1.1.1.1
strict-order
cache-size=1000
domain-needed
stop-dns-rebind
EOF
install -m 644 -o root -g root "$dnsmasq_tmp" /etc/dnsmasq.d/my-utils-wireguard.conf

cat >"$dnsmasq_tmp" <<EOF
[Unit]
After=wg-quick@$ingress_interface.service
Requires=wg-quick@$ingress_interface.service
EOF
install -m 644 -o root -g root "$dnsmasq_tmp" /etc/systemd/system/dnsmasq.service.d/my-utils-wireguard.conf

dnsmasq --test
systemctl daemon-reload
systemctl restart dnsmasq.service
systemctl enable --now my-utils-wireguard-dns.service

ss -H -lun "sport = :53" | grep -Fq "$resolver_address:53"
iptables -t nat -C PREROUTING -j MYUTILS-WG-DNS
dig +time=3 +tries=1 +short @"$resolver_address" example.com A | grep -Eq '^([0-9]{1,3}\.){3}[0-9]{1,3}$'
ip route get 1.1.1.1 | grep -vq 'dev awg-exit'
echo "WireGuard client DNS is active and independent from AWG"
