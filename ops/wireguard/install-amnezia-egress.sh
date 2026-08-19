#!/usr/bin/env bash
set -euo pipefail

mode=plan
replace=false
consume=false
config_file=""
interface=awg-exit

usage() {
  echo "Usage: $0 --config FILE [--interface awg-exit] [--apply] [--replace] [--consume]" >&2
}

while (($#)); do
  case "$1" in
    --config) config_file="${2:-}"; shift 2 ;;
    --interface) interface="${2:-}"; shift 2 ;;
    --apply) mode=apply; shift ;;
    --replace) replace=true; shift ;;
    --consume) consume=true; shift ;;
    *) usage; exit 2 ;;
  esac
done

if [[ $EUID -ne 0 ]]; then
  echo "Run as root" >&2
  exit 1
fi
if [[ ! "$interface" =~ ^[a-zA-Z0-9_=+.-]{1,15}$ ]]; then
  echo "Invalid interface name" >&2
  exit 1
fi
if [[ -z "$config_file" || ! -f "$config_file" ]]; then
  echo "AmneziaWG client config is missing" >&2
  exit 1
fi
if [[ "$(stat -c '%a' "$config_file")" != "600" ]]; then
  echo "AmneziaWG client config must have mode 600" >&2
  exit 1
fi
if ! grep -q '^\[Interface\]$' "$config_file" ||
   ! grep -q '^PrivateKey = ' "$config_file" ||
   ! grep -q '^Table = off$' "$config_file" ||
   ! grep -q '^\[Peer\]$' "$config_file" ||
   ! grep -q '^PresharedKey = ' "$config_file" ||
   ! grep -q '^AllowedIPs = 0\.0\.0\.0/0$' "$config_file"; then
  echo "AmneziaWG client config is incomplete or unsafe" >&2
  exit 1
fi
if [[ ! -f /etc/os-release ]] || ! grep -q '^ID=ubuntu$' /etc/os-release; then
  echo "Only Ubuntu is supported" >&2
  exit 1
fi
command -v systemctl >/dev/null || { echo "systemd is required" >&2; exit 1; }

target_dir=/etc/amnezia/amneziawg
target="$target_dir/$interface.conf"
if [[ -e "$target" && "$replace" != true ]]; then
  echo "Refusing to overwrite $target without --replace" >&2
  exit 1
fi

echo "Plan: install official AmneziaWG tooling, protect $target, and enable $interface"
if [[ "$mode" != apply ]]; then
  exit 0
fi

export DEBIAN_FRONTEND=noninteractive
apt-get update
apt-get install -y software-properties-common ca-certificates
if ! grep -Rqs '^deb .*ppa.launchpadcontent.net/amnezia/ppa' /etc/apt/sources.list /etc/apt/sources.list.d; then
  add-apt-repository -y ppa:amnezia/ppa
fi
apt-get update
apt-get install -y amneziawg
command -v awg >/dev/null || { echo "amneziawg package did not install awg" >&2; exit 1; }
command -v awg-quick >/dev/null || { echo "amneziawg package did not install awg-quick" >&2; exit 1; }

install -d -m 700 "$target_dir"
if [[ -e "$target" ]]; then
  cp -a -- "$target" "$target.backup.$(date -u +%Y%m%dT%H%M%SZ)"
fi
install -m 600 -o root -g root "$config_file" "$target"

service_file=/etc/systemd/system/my-utils-awg-exit.service
cat >"$service_file" <<EOF
[Unit]
Description=my-utils AmneziaWG egress
After=network-online.target
Wants=network-online.target

[Service]
Type=oneshot
RemainAfterExit=yes
ExecStart=$(command -v awg-quick) up $target
ExecStop=$(command -v awg-quick) down $target

[Install]
WantedBy=multi-user.target
EOF
chmod 644 "$service_file"
systemctl daemon-reload
systemctl enable --now my-utils-awg-exit.service
awg show "$interface" >/dev/null

if [[ "$consume" == true ]]; then
  rm -f -- "$config_file"
fi
echo "AmneziaWG egress is active on $interface"
