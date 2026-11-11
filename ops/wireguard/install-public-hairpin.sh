#!/usr/bin/env bash
set -euo pipefail

public_address=""
mode=plan
while (($#)); do
  case "$1" in
    --public-address) public_address="${2:?IPv4 address is required}"; shift 2 ;;
    --apply) mode=apply; shift ;;
    *) echo "Usage: $0 --public-address IPv4 [--apply]" >&2; exit 2 ;;
  esac
done
[[ -n "$public_address" ]] || { echo "--public-address is required" >&2; exit 2; }
python3 -c 'import ipaddress,sys; ipaddress.IPv4Address(sys.argv[1])' "$public_address"

echo "Plan: route VPN requests to $public_address through the existing VPN exit, preserving other local traffic"
if [[ "$mode" == plan ]]; then
  echo "Plan only; no host changes were made"
  exit 0
fi
[[ $EUID -eq 0 ]] || { echo "Run --apply as root" >&2; exit 1; }
systemctl is-active --quiet my-utils-wireguard-routing.service
script_dir="$(cd "$(dirname "$0")" && pwd)"
set -a
# shellcheck source=/dev/null
source /etc/my-utils/wireguard-routing-ha.env
set +a
export WIREGUARD_PUBLIC_ADDRESS="$public_address"
python3 "$script_dir/public-hairpin-routing.py" plan

install_atomic() {
  local source=$1 target=$2 mode=$3
  install -m "$mode" "$source" "$target.new.$$"
  mv -f -- "$target.new.$$" "$target"
}
install_atomic "$script_dir/public-hairpin-routing.py" /usr/local/libexec/my-utils-wireguard-public-hairpin 755
install_atomic "$script_dir/routing-reconcile.sh" /usr/local/libexec/my-utils-wireguard-routing-reconcile 755
install_atomic "$script_dir/systemd/my-utils-wireguard-public-hairpin.service" /etc/systemd/system/my-utils-wireguard-public-hairpin.service 644
config_tmp="$(mktemp)"
trap 'rm -f -- "$config_tmp"' EXIT
printf 'WIREGUARD_PUBLIC_ADDRESS=%s\n' "$public_address" >"$config_tmp"
install_atomic "$config_tmp" /etc/my-utils/wireguard-public-hairpin.env 600
systemctl daemon-reload
systemctl enable --now my-utils-wireguard-public-hairpin.service
