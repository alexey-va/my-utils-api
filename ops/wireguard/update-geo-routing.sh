#!/usr/bin/env bash
set -euo pipefail

umask 077

ENV_FILE="${GEO_ROUTING_ENV_FILE:-/etc/my-utils/geo-routing.env}"
if [[ ! -r "$ENV_FILE" ]]; then
  echo "Geo routing environment is not readable: $ENV_FILE" >&2
  exit 1
fi

# shellcheck source=/dev/null
source "$ENV_FILE"

required=(GEO_ROUTING_SOURCE_URL GEO_ROUTING_STATUS_FILE)
for name in "${required[@]}"; do
  if [[ -z "${!name:-}" ]]; then
    echo "Missing required geo routing setting: $name" >&2
    exit 1
  fi
done
if [[ ! "$GEO_ROUTING_SOURCE_URL" =~ ^https://[^[:space:]]+$ ]]; then
  echo "Geo routing source URL must use HTTPS" >&2
  exit 1
fi
for command in curl nft python3 mktemp install; do
  command -v "$command" >/dev/null || {
    echo "Required command is missing: $command" >&2
    exit 1
  }
done

tmp_dir="$(mktemp -d /run/my-utils-geo-routing.XXXXXX)"
cleanup() { rm -rf -- "$tmp_dir"; }
trap cleanup EXIT INT TERM

zone_file="$tmp_dir/ru.zone"
transaction_file="$tmp_dir/ru.nft"
count_file="$tmp_dir/count"
curl --proto '=https' --tlsv1.2 --location --silent --show-error --fail \
  --connect-timeout 10 --max-time 90 \
  --output "$zone_file" "$GEO_ROUTING_SOURCE_URL"

/usr/local/libexec/my-utils-render-geo-prefixes \
  --count-file "$count_file" \
  <"$zone_file" >"$transaction_file"

prefix_count="$(<"$count_file")"
if [[ ! "$prefix_count" =~ ^[0-9]+$ ]]; then
  echo "Renderer returned an invalid prefix count" >&2
  exit 1
fi
nft list set ip myutils_wg_geo ru_ipv4 >/dev/null
nft --check -f "$transaction_file"
nft -f "$transaction_file"

status_dir="$(dirname "$GEO_ROUTING_STATUS_FILE")"
install -d -m 755 "$status_dir"
status_tmp="$(mktemp "$status_dir/.geo-routing-status.XXXXXX")"
printf '{"mode":"RU_DIRECT_AWG_DEFAULT","ruPrefixCount":%s,"updatedAt":"%s"}\n' \
  "$prefix_count" "$(date -u +%Y-%m-%dT%H:%M:%SZ)" >"$status_tmp"
chmod 644 "$status_tmp"
mv -f -- "$status_tmp" "$GEO_ROUTING_STATUS_FILE"
echo "Loaded $prefix_count validated Russian IPv4 prefixes"
