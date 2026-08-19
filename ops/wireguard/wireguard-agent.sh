#!/usr/bin/env bash
set -euo pipefail

umask 077

ENV_FILE="${WIREGUARD_AGENT_ENV_FILE:-/etc/my-utils/wireguard-agent.env}"
if [[ ! -r "$ENV_FILE" ]]; then
  echo "WireGuard agent environment is not readable: $ENV_FILE" >&2
  exit 1
fi

# shellcheck source=/dev/null
source "$ENV_FILE"

required=(WIREGUARD_API_BASE_URL WIREGUARD_RELAY_ID WIREGUARD_AGENT_TOKEN WIREGUARD_INTERFACE WIREGUARD_PUBLIC_ENDPOINT)
for name in "${required[@]}"; do
  if [[ -z "${!name:-}" ]]; then
    echo "Missing required agent setting: $name" >&2
    exit 1
  fi
done

if [[ ! "$WIREGUARD_RELAY_ID" =~ ^[0-9a-fA-F-]{36}$ ]]; then
  echo "WIREGUARD_RELAY_ID is invalid" >&2
  exit 1
fi
if [[ ! "$WIREGUARD_INTERFACE" =~ ^[a-zA-Z0-9_=+.-]{1,15}$ ]]; then
  echo "WIREGUARD_INTERFACE is invalid" >&2
  exit 1
fi
if [[ ! "$WIREGUARD_API_BASE_URL" =~ ^https?://[^[:space:]]+$ ]]; then
  echo "WIREGUARD_API_BASE_URL is invalid" >&2
  exit 1
fi
if [[ "$WIREGUARD_PUBLIC_ENDPOINT" == *$'\n'* || "$WIREGUARD_PUBLIC_ENDPOINT" == *$'\r'* ]]; then
  echo "WIREGUARD_PUBLIC_ENDPOINT is invalid" >&2
  exit 1
fi
for command in curl jq wg mktemp; do
  command -v "$command" >/dev/null || {
    echo "Required command is missing: $command" >&2
    exit 1
  }
done
if [[ ! -d "/sys/class/net/$WIREGUARD_INTERFACE" ]]; then
  echo "WireGuard interface is not active: $WIREGUARD_INTERFACE" >&2
  exit 1
fi

tmp_dir="$(mktemp -d /run/my-utils-wireguard-agent.XXXXXX)"
cleanup() {
  rm -rf -- "$tmp_dir"
}
trap cleanup EXIT INT TERM

curl_config="$tmp_dir/curl.conf"
cat >"$curl_config" <<EOF
silent
show-error
fail-with-body
connect-timeout = 10
max-time = 30
header = "X-WireGuard-Agent-Token: ${WIREGUARD_AGENT_TOKEN}"
EOF

desired_json="$tmp_dir/desired.json"
curl --config "$curl_config" \
  --output "$desired_json" \
  "${WIREGUARD_API_BASE_URL%/}/api/internal/wireguard/relays/${WIREGUARD_RELAY_ID}/desired"

jq -e --arg interface "$WIREGUARD_INTERFACE" '
  type == "object" and
  (.revision | type == "number" and . >= 0 and floor == .) and
  .interfaceName == $interface and
  (.peers | type == "array") and
  all(.peers[];
    (.publicKey | type == "string" and test("^[A-Za-z0-9+/]{43}=$")) and
    (.allowedIp | type == "string" and test("^(10|172\\.(1[6-9]|2[0-9]|3[01])|192\\.168)\\.[0-9]{1,3}\\.[0-9]{1,3}/32$"))
  ) and
  (([.peers[].publicKey] | length) == ([.peers[].publicKey] | unique | length)) and
  (([.peers[].allowedIp] | length) == ([.peers[].allowedIp] | unique | length))
' "$desired_json" >/dev/null

sync_conf="$tmp_dir/sync.conf"
private_key="$(wg show "$WIREGUARD_INTERFACE" private-key)"
listen_port="$(wg show "$WIREGUARD_INTERFACE" listen-port)"
if [[ -z "$private_key" || "$private_key" == "(none)" || ! "$listen_port" =~ ^[0-9]+$ ]]; then
  echo "WireGuard interface has no usable private key or listen port" >&2
  exit 1
fi
{
  printf '[Interface]\nPrivateKey = %s\nListenPort = %s\n' "$private_key" "$listen_port"
  jq -r '.peers[] | "\n[Peer]\nPublicKey = \(.publicKey)\nAllowedIPs = \(.allowedIp)"' "$desired_json"
} >"$sync_conf"
unset private_key

wg syncconf "$WIREGUARD_INTERFACE" "$sync_conf"

counters_json="$tmp_dir/counters.json"
wg show "$WIREGUARD_INTERFACE" dump |
  tail -n +2 |
  jq -Rn '[
    inputs |
    split("\t") |
    {
      publicKey: .[0],
      latestHandshakeEpochSeconds: (.[4] | tonumber),
      receiveBytes: (.[5] | tonumber),
      transmitBytes: (.[6] | tonumber)
    }
  ]' >"$counters_json"

heartbeat_json="$tmp_dir/heartbeat.json"
jq -n \
  --arg serverPublicKey "$(wg show "$WIREGUARD_INTERFACE" public-key)" \
  --arg publicEndpoint "$WIREGUARD_PUBLIC_ENDPOINT" \
  --argjson appliedRevision "$(jq '.revision' "$desired_json")" \
  --slurpfile peers "$counters_json" \
  '{
    serverPublicKey: $serverPublicKey,
    publicEndpoint: $publicEndpoint,
    appliedRevision: $appliedRevision,
    peers: $peers[0]
  }' >"$heartbeat_json"

curl --config "$curl_config" \
  --header 'Content-Type: application/json' \
  --request POST \
  --data-binary "@$heartbeat_json" \
  --output /dev/null \
  "${WIREGUARD_API_BASE_URL%/}/api/internal/wireguard/relays/${WIREGUARD_RELAY_ID}/heartbeat"
