#!/usr/bin/env bash
set -euo pipefail

mode=plan

usage() {
  echo "Usage: $0 [--apply]" >&2
}

while (($#)); do
  case "$1" in
    --apply) mode=apply; shift ;;
    *) usage; exit 2 ;;
  esac
done

[[ $EUID -eq 0 ]] || { echo "Run as root" >&2; exit 1; }
[[ -r /etc/os-release ]] || { echo "Missing /etc/os-release" >&2; exit 1; }
# shellcheck source=/dev/null
source /etc/os-release
[[ ${ID:-} == ubuntu ]] || { echo "Only Ubuntu is supported" >&2; exit 1; }

echo "Plan: install official AmneziaWG tooling and protected relay-side key directories"
if [[ "$mode" != apply ]]; then
  echo "Plan only; no host changes were made"
  exit 0
fi

export DEBIAN_FRONTEND=noninteractive
apt-get update
apt-get -o Dpkg::Options::=--force-confold install -y software-properties-common ca-certificates
if ! grep -Rqs '^deb .*ppa.launchpadcontent.net/amnezia/ppa' /etc/apt/sources.list /etc/apt/sources.list.d; then
  add-apt-repository -y ppa:amnezia/ppa
fi
apt-get update
apt-get -o Dpkg::Options::=--force-confold install -y amneziawg curl jq python3 wireguard-tools iptables nftables
command -v awg >/dev/null || { echo "amneziawg package did not install awg" >&2; exit 1; }
command -v awg-quick >/dev/null || { echo "amneziawg package did not install awg-quick" >&2; exit 1; }
install -d -m 700 /etc/my-utils/awg-identities /etc/my-utils/awg-params
echo "Relay host AWG prerequisites are installed"
