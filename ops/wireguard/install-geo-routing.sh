#!/usr/bin/env bash
set -euo pipefail

mode=plan
client_cidr=""
ingress_interface=wg-users
direct_egress_interface=""
source_url=https://www.ipdeny.com/ipblocks/data/aggregated/ru-aggregated.zone

usage() {
  echo "Usage: $0 --client-cidr CIDR --direct-egress-interface IFACE [--ingress-interface IFACE] [--source-url HTTPS_URL] [--apply]" >&2
}

valid_interface() {
  [[ "$1" =~ ^[a-zA-Z0-9_=+.-]{1,15}$ ]]
}

valid_private_cidr() {
  local cidr=$1 ip prefix a b c d ip_number host_bits mask
  [[ "$cidr" == */* ]] || return 1
  ip=${cidr%/*}; prefix=${cidr#*/}
  [[ "$prefix" =~ ^[0-9]+$ ]] && ((prefix >= 16 && prefix <= 29)) || return 1
  IFS=. read -r a b c d <<<"$ip"
  for octet in "$a" "$b" "$c" "$d"; do
    [[ "$octet" =~ ^(0|[1-9][0-9]{0,2})$ ]] && ((10#$octet <= 255)) || return 1
  done
  a=$((10#$a)); b=$((10#$b)); c=$((10#$c)); d=$((10#$d))
  ((a == 10 || (a == 172 && b >= 16 && b <= 31) || (a == 192 && b == 168))) || return 1
  ip_number=$(((a << 24) | (b << 16) | (c << 8) | d))
  host_bits=$((32 - prefix)); mask=$(((0xffffffff << host_bits) & 0xffffffff))
  (( (ip_number & mask) == ip_number ))
}

while (($#)); do
  case "$1" in
    --client-cidr) client_cidr="${2:-}"; shift 2 ;;
    --ingress-interface) ingress_interface="${2:-}"; shift 2 ;;
    --direct-egress-interface) direct_egress_interface="${2:-}"; shift 2 ;;
    --source-url) source_url="${2:-}"; shift 2 ;;
    --apply) mode=apply; shift ;;
    *) usage; exit 2 ;;
  esac
done

valid_private_cidr "$client_cidr" || { echo "Client CIDR must be a network-aligned private IPv4 /16 through /29" >&2; exit 1; }
valid_interface "$ingress_interface" || { echo "Invalid ingress interface" >&2; exit 1; }
valid_interface "$direct_egress_interface" || { echo "Invalid direct egress interface" >&2; exit 1; }
[[ "$source_url" =~ ^https://[^[:space:]]+$ ]] || { echo "Source URL must use HTTPS" >&2; exit 1; }

echo "Plan: mark validated Russian IPv4 destinations arriving on $ingress_interface from $client_cidr"
echo "Plan: route mark 0x51890 via main at priority 1088 and masquerade only that traffic on $direct_egress_interface"
echo "Plan: unmarked traffic remains on AWG table 51889; failed GeoIP updates keep the last known-good set"
echo "Plan: refresh the atomic nftables interval set daily from $source_url"
if [[ "$mode" != apply ]]; then
  echo "Plan only; no host changes were made"
  exit 0
fi

if [[ $EUID -ne 0 ]]; then
  echo "Run --apply as root" >&2
  exit 1
fi
if [[ ! -f /etc/os-release ]] || ! grep -q '^ID=ubuntu$' /etc/os-release; then
  echo "Only Ubuntu is supported" >&2
  exit 1
fi
for interface in "$ingress_interface" "$direct_egress_interface"; do
  [[ -d "/sys/class/net/$interface" ]] || { echo "Interface is not active: $interface" >&2; exit 1; }
done
command -v systemctl >/dev/null || { echo "systemd is required" >&2; exit 1; }

export DEBIAN_FRONTEND=noninteractive
apt-get update
apt-get install -y curl iproute2 iptables nftables python3

script_dir="$(cd "$(dirname "$0")" && pwd)"
install -d -m 700 /etc/my-utils
install -d -m 755 /usr/local/libexec /var/lib/my-utils-wireguard
install -m 755 -o root -g root "$script_dir/geo-routing.sh" /usr/local/libexec/my-utils-geo-routing
install -m 755 -o root -g root "$script_dir/update-geo-routing.sh" /usr/local/libexec/my-utils-update-geo-routing
install -m 755 -o root -g root "$script_dir/render-geo-prefixes.py" /usr/local/libexec/my-utils-render-geo-prefixes
install -m 644 -o root -g root "$script_dir/systemd/my-utils-geo-routing.service" /etc/systemd/system/
install -m 644 -o root -g root "$script_dir/systemd/my-utils-geo-routing-update.service" /etc/systemd/system/
install -m 644 -o root -g root "$script_dir/systemd/my-utils-geo-routing-update.timer" /etc/systemd/system/

config_tmp="$(mktemp)"
cleanup() { rm -f -- "$config_tmp"; }
trap cleanup EXIT INT TERM
cat >"$config_tmp" <<EOF
GEO_ROUTING_CLIENT_CIDR=$client_cidr
GEO_ROUTING_INGRESS_INTERFACE=$ingress_interface
GEO_ROUTING_DIRECT_EGRESS_INTERFACE=$direct_egress_interface
GEO_ROUTING_SOURCE_URL=$source_url
GEO_ROUTING_STATUS_FILE=/var/lib/my-utils-wireguard/geo-routing-status.json
EOF
install -m 600 -o root -g root "$config_tmp" /etc/my-utils/geo-routing.env

systemctl daemon-reload
systemctl enable --now my-utils-geo-routing.service
systemctl enable --now my-utils-geo-routing-update.timer
systemctl start my-utils-geo-routing-update.service

nft list set ip myutils_wg_geo ru_ipv4 >/dev/null
ip rule show | grep -Fq '1088:'
iptables -C FORWARD -j MYUTILS-WG-RU
iptables -t nat -C POSTROUTING -j MYUTILS-WG-RU-NAT
echo "RU-direct routing is active; all other client destinations remain on AWG"
