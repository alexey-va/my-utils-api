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

WIREGUARD_ROUTING_STATUS_FILE="${WIREGUARD_ROUTING_STATUS_FILE:-/var/lib/my-utils-wireguard/geo-routing-status.json}"
WIREGUARD_EXIT_HEALTH_FILE="${WIREGUARD_EXIT_HEALTH_FILE:-/var/lib/my-utils-wireguard/exit-health.json}"
WIREGUARD_EXIT_PREFERENCE_FILE="${WIREGUARD_EXIT_PREFERENCE_FILE:-/var/lib/my-utils-wireguard/exit-preference}"
WIREGUARD_AWG_INTERFACE="${WIREGUARD_AWG_INTERFACE:-awg-exit}"
WIREGUARD_AWG_INTERFACE_PATTERN="${WIREGUARD_AWG_INTERFACE_PATTERN:-awg-exit+}"
WIREGUARD_DIRECT_INTERFACE="${WIREGUARD_DIRECT_INTERFACE:-eth0}"
WIREGUARD_DIRECT_PROBE_TARGET="${WIREGUARD_DIRECT_PROBE_TARGET:-77.88.8.8}"
WIREGUARD_TRAFFIC_CHAIN="${WIREGUARD_TRAFFIC_CHAIN:-MYUTILS-WG-TRAFFIC}"
WIREGUARD_ROUTING_MARK="${WIREGUARD_ROUTING_MARK:-0x51890}"
WIREGUARD_DNS_RESOLVER_ADDRESS="${WIREGUARD_DNS_RESOLVER_ADDRESS:-10.89.0.1}"

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
for interface in "$WIREGUARD_AWG_INTERFACE" "$WIREGUARD_DIRECT_INTERFACE"; do
  if [[ ! "$interface" =~ ^[a-zA-Z0-9_=+.-]{1,15}$ ]]; then
    echo "WireGuard route interface is invalid: $interface" >&2
    exit 1
  fi
done
if [[ ! "$WIREGUARD_AWG_INTERFACE_PATTERN" =~ ^[a-zA-Z0-9_=+.-]{1,15}$ ]]; then
  echo "WireGuard route interface pattern is invalid: $WIREGUARD_AWG_INTERFACE_PATTERN" >&2
  exit 1
fi
if [[ ! "$WIREGUARD_TRAFFIC_CHAIN" =~ ^[a-zA-Z0-9_-]{1,28}$ ]]; then
  echo "WIREGUARD_TRAFFIC_CHAIN is invalid" >&2
  exit 1
fi
if [[ ! "$WIREGUARD_ROUTING_MARK" =~ ^0x[0-9a-fA-F]{1,8}$ ]]; then
  echo "WIREGUARD_ROUTING_MARK is invalid" >&2
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
for command in awg awk curl date dig grep ip iptables jq mktemp nft ping systemctl wg; do
  command -v "$command" >/dev/null || {
    echo "Required command is missing: $command" >&2
    exit 1
  }
done
if [[ ! -d "/sys/class/net/$WIREGUARD_INTERFACE" ]]; then
  echo "WireGuard interface is not active: $WIREGUARD_INTERFACE" >&2
  exit 1
fi
for interface in "$WIREGUARD_DIRECT_INTERFACE"; do
  if [[ ! -d "/sys/class/net/$interface" ]]; then
    echo "WireGuard route interface is not active: $interface" >&2
    exit 1
  fi
done

valid_ipv4() {
  local value=$1 a b c d
  IFS=. read -r a b c d <<<"$value"
  for octet in "$a" "$b" "$c" "$d"; do
    [[ "$octet" =~ ^(0|[1-9][0-9]{0,2})$ ]] && ((10#$octet <= 255)) || return 1
  done
}

traffic_rule_bytes() {
  local marker="myutils:$1:$2" rules
  if ! rules="$(iptables -t mangle -L "$WIREGUARD_TRAFFIC_CHAIN" -vnx 2>/dev/null)"; then
    printf '0\n'
    return
  fi
  awk -v marker="$marker" '
      index($0, marker) { print $2; found=1; exit }
      END { if (!found) print 0 }
    ' <<<"$rules"
}

configure_traffic_counters() {
  local peer_ip
  iptables -t mangle -N "$WIREGUARD_TRAFFIC_CHAIN" 2>/dev/null || true
  iptables -t mangle -C FORWARD -j "$WIREGUARD_TRAFFIC_CHAIN" 2>/dev/null ||
    iptables -t mangle -I FORWARD 1 -j "$WIREGUARD_TRAFFIC_CHAIN"
  iptables -t mangle -F "$WIREGUARD_TRAFFIC_CHAIN"
  while IFS= read -r peer_ip; do
    iptables -t mangle -A "$WIREGUARD_TRAFFIC_CHAIN" \
      -i "$WIREGUARD_INTERFACE" -s "$peer_ip" \
      -m mark --mark "$WIREGUARD_ROUTING_MARK/0xffffffff" \
      -m comment --comment "myutils:$peer_ip:ru-upload" -j RETURN
    iptables -t mangle -A "$WIREGUARD_TRAFFIC_CHAIN" \
      -i "$WIREGUARD_INTERFACE" -s "$peer_ip" \
      -m comment --comment "myutils:$peer_ip:non-ru-upload" -j RETURN
    iptables -t mangle -A "$WIREGUARD_TRAFFIC_CHAIN" \
      -i "$WIREGUARD_DIRECT_INTERFACE" -o "$WIREGUARD_INTERFACE" -d "$peer_ip" \
      -m comment --comment "myutils:$peer_ip:ru-download" -j RETURN
    iptables -t mangle -A "$WIREGUARD_TRAFFIC_CHAIN" \
      -i "$WIREGUARD_AWG_INTERFACE_PATTERN" -o "$WIREGUARD_INTERFACE" -d "$peer_ip" \
      -m comment --comment "myutils:$peer_ip:non-ru-download" -j RETURN
  done < <(jq -r '.peers[].allowedIp | sub("/32$"; "")' "$desired_json")
}

route_probe() {
  local interface=$1 target=$2 output=$3 loss rtt
  ping -n -q -c 3 -W 2 -I "$interface" "$target" >"$output" 2>&1 || true
  loss="$(awk -F', ' '
    /packet loss/ {
      for (i=1; i<=NF; i++) if ($i ~ /packet loss/) {
        gsub(/% packet loss.*/, "", $i)
        gsub(/^[[:space:]]*/, "", $i)
        print $i
      }
    }
  ' "$output")"
  rtt="$(awk -F' = ' '/^(rtt|round-trip)/ { split($2, values, "/"); print values[2] }' "$output")"
  [[ "$loss" =~ ^[0-9]+([.][0-9]+)?$ ]] || loss=100
  if [[ "$rtt" =~ ^[0-9]+([.][0-9]+)?$ ]]; then
    jq -n --arg target "$target" --argjson loss "$loss" --argjson rtt "$rtt" \
      '{target: $target, packetLossPercent: $loss, averageRttMs: $rtt}'
  else
    jq -n --arg target "$target" --argjson loss "$loss" \
      '{target: $target, packetLossPercent: $loss, averageRttMs: null}'
  fi
}

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
  (.exitPreference == "AUTO" or .exitPreference == "PRIMARY" or .exitPreference == "SECONDARY") and
  (.peers | type == "array") and
  all(.peers[];
    (.publicKey | type == "string" and test("^[A-Za-z0-9+/]{43}=$")) and
    (.allowedIp | type == "string" and test("^(10\\.[0-9]{1,3}|172\\.(1[6-9]|2[0-9]|3[01])|192\\.168)\\.[0-9]{1,3}\\.[0-9]{1,3}/32$"))
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

preference_dir="$(dirname -- "$WIREGUARD_EXIT_PREFERENCE_FILE")"
install -d -m 700 "$preference_dir"
preference_tmp="$(mktemp "$preference_dir/.exit-preference.XXXXXX")"
jq -r '.exitPreference' "$desired_json" >"$preference_tmp"
chmod 644 "$preference_tmp"
mv -f -- "$preference_tmp" "$WIREGUARD_EXIT_PREFERENCE_FILE"
if systemctl is-active --quiet my-utils-awg-failover.timer; then
  systemctl start my-utils-awg-failover.service
fi

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

route_counters_json="$tmp_dir/route-counters.json"
while IFS=$'\t' read -r public_key allowed_ip; do
  peer_ip="${allowed_ip%/32}"
  jq -n \
    --arg publicKey "$public_key" \
    --argjson ruDownloadBytes "$(traffic_rule_bytes "$peer_ip" ru-download)" \
    --argjson ruUploadBytes "$(traffic_rule_bytes "$peer_ip" ru-upload)" \
    --argjson nonRuDownloadBytes "$(traffic_rule_bytes "$peer_ip" non-ru-download)" \
    --argjson nonRuUploadBytes "$(traffic_rule_bytes "$peer_ip" non-ru-upload)" \
    '{
      key: $publicKey,
      value: {
        ruDownloadBytes: $ruDownloadBytes,
        ruUploadBytes: $ruUploadBytes,
        nonRuDownloadBytes: $nonRuDownloadBytes,
        nonRuUploadBytes: $nonRuUploadBytes
      }
    }'
done < <(jq -r '.peers[] | [.publicKey, .allowedIp] | @tsv' "$desired_json") |
  jq -s 'from_entries' >"$route_counters_json"

jq --slurpfile routeCounters "$route_counters_json" '
  map(. + {
    routingTraffic: ($routeCounters[0][.publicKey] // {
      ruDownloadBytes: 0,
      ruUploadBytes: 0,
      nonRuDownloadBytes: 0,
      nonRuUploadBytes: 0
    })
  })
' "$counters_json" >"$tmp_dir/counters-with-routes.json"
mv "$tmp_dir/counters-with-routes.json" "$counters_json"

routing_status_json="$tmp_dir/routing-status.json"
printf 'null\n' >"$routing_status_json"
if [[ -r "$WIREGUARD_ROUTING_STATUS_FILE" ]]; then
  if jq -e '
    type == "object" and
    ((keys | sort) == ["mode", "ruPrefixCount", "updatedAt"]) and
    (.mode == "AWG_ONLY" or .mode == "RU_DIRECT_AWG_DEFAULT") and
    (.ruPrefixCount | type == "number" and floor == . and . >= 0 and . <= 100000) and
    (.updatedAt | type == "string" and test("^[0-9]{4}-[0-9]{2}-[0-9]{2}T[0-9]{2}:[0-9]{2}:[0-9]{2}Z$")) and
    ((.mode == "AWG_ONLY" and .ruPrefixCount == 0) or (.mode == "RU_DIRECT_AWG_DEFAULT" and .ruPrefixCount > 0))
  ' "$WIREGUARD_ROUTING_STATUS_FILE" >/dev/null; then
    routingHealthy=false
    routing_rules="$(ip -4 rule show)"
    routing_table_routes="$(ip -4 route show table 51889)"
    routing_mode="$(jq -r '.mode' "$WIREGUARD_ROUTING_STATUS_FILE")"
    if systemctl is-active --quiet my-utils-wireguard-routing.service &&
      grep -Eq '(^|[[:space:]])1089:.*from 10\.89\.0\.0/24 lookup 51889([[:space:]]|$)' <<<"$routing_rules" &&
      grep -Eq '^10\.89\.0\.0/24 dev wg-users([[:space:]]|$)' <<<"$routing_table_routes"; then
      routingHealthy=true
    fi
    dns_answer=""
    if valid_ipv4 "$WIREGUARD_DNS_RESOLVER_ADDRESS" &&
      systemctl is-active --quiet my-utils-wireguard-dns.service &&
      iptables -C INPUT -j MYUTILS-WG-DNS-IN 2>/dev/null &&
      iptables -t nat -C PREROUTING -j MYUTILS-WG-DNS 2>/dev/null; then
      dns_answer="$(dig +time=2 +tries=1 +short @"$WIREGUARD_DNS_RESOLVER_ADDRESS" example.com A 2>/dev/null || true)"
    fi
    if ! grep -Eq '^([0-9]{1,3}\.){3}[0-9]{1,3}$' <<<"$dns_answer"; then
      routingHealthy=false
    fi
    if [[ "$routing_mode" == "RU_DIRECT_AWG_DEFAULT" ]] && {
      ! systemctl is-active --quiet my-utils-geo-routing.service ||
      ! grep -Eq '(^|[[:space:]])1088:.*fwmark 0x51890 lookup main([[:space:]]|$)' <<<"$routing_rules" ||
      ! nft list set ip myutils_wg_geo ru_ipv4 >/dev/null 2>&1;
    }; then
      routingHealthy=false
    fi
    jq \
      --argjson healthy "$routingHealthy" \
      --arg checkedAt "$(date -u +%Y-%m-%dT%H:%M:%SZ)" \
      '{mode, ruPrefixCount, updatedAt, healthy: $healthy, checkedAt: $checkedAt}' \
      "$WIREGUARD_ROUTING_STATUS_FILE" >"$routing_status_json"
  else
    echo "Ignoring invalid WireGuard routing status file" >&2
  fi
fi

exit_health_json="$tmp_dir/exit-health.json"
printf 'null\n' >"$exit_health_json"
if systemctl is-active --quiet my-utils-awg-failover.timer && [[ -r "$WIREGUARD_EXIT_HEALTH_FILE" ]]; then
  if jq -e '
    type == "object" and
    .schemaVersion == 1 and
    (.checkedAt | type == "string" and test("^[0-9]{4}-[0-9]{2}-[0-9]{2}T[0-9]{2}:[0-9]{2}:[0-9]{2}Z$")) and
    (.overallStatus == "HEALTHY" or .overallStatus == "DEGRADED" or .overallStatus == "DOWN") and
    (.counters | type == "object" and (keys | sort) == ["primary", "secondary"]) and
    (.exits | type == "object" and (keys | sort) == ["primary", "secondary"])
  ' "$WIREGUARD_EXIT_HEALTH_FILE" >/dev/null; then
    jq '.' "$WIREGUARD_EXIT_HEALTH_FILE" >"$exit_health_json"
  else
    echo "Ignoring invalid AWG exit health file" >&2
  fi
fi

route_quality_json="$tmp_dir/route-quality.json"
printf 'null\n' >"$route_quality_json"
active_awg_interface="$(jq -r '.activeInterface // empty' "$exit_health_json")"
if [[ -z "$active_awg_interface" ]]; then
  active_awg_interface="$WIREGUARD_AWG_INTERFACE"
fi
awg_endpoint=""
if [[ "$active_awg_interface" =~ ^[a-zA-Z0-9_=+.-]{1,15}$ && -d "/sys/class/net/$active_awg_interface" ]]; then
  awg_endpoint="$(
    awg show "$active_awg_interface" endpoints 2>/dev/null |
      awk 'NR == 1 { endpoint=$2; sub(/:[0-9]+$/, "", endpoint); print endpoint }' || true
  )"
fi
if valid_ipv4 "$WIREGUARD_DIRECT_PROBE_TARGET" && valid_ipv4 "$awg_endpoint"; then
  route_probe "$WIREGUARD_DIRECT_INTERFACE" "$WIREGUARD_DIRECT_PROBE_TARGET" "$tmp_dir/direct-probe.txt" >"$tmp_dir/direct-probe.json"
  route_probe "$WIREGUARD_DIRECT_INTERFACE" "$awg_endpoint" "$tmp_dir/veesp-probe.txt" >"$tmp_dir/veesp-probe.json"
  jq -n \
    --arg measuredAt "$(date -u +%Y-%m-%dT%H:%M:%SZ)" \
    --slurpfile direct "$tmp_dir/direct-probe.json" \
    --slurpfile veesp "$tmp_dir/veesp-probe.json" \
    '{measuredAt: $measuredAt, direct: $direct[0], veesp: $veesp[0]}' >"$route_quality_json"
else
  echo "Skipping route quality probes because a target is invalid" >&2
fi

heartbeat_json="$tmp_dir/heartbeat.json"
jq -n \
  --arg serverPublicKey "$(wg show "$WIREGUARD_INTERFACE" public-key)" \
  --arg publicEndpoint "$WIREGUARD_PUBLIC_ENDPOINT" \
  --argjson appliedRevision "$(jq '.revision' "$desired_json")" \
  --slurpfile peers "$counters_json" \
  --slurpfile routingStatus "$routing_status_json" \
  --slurpfile routeQuality "$route_quality_json" \
  --slurpfile exitHealth "$exit_health_json" \
  '({
      serverPublicKey: $serverPublicKey,
      publicEndpoint: $publicEndpoint,
      appliedRevision: $appliedRevision,
      peers: $peers[0]
    } +
    (if $routingStatus[0] == null then {} else {routingStatus: $routingStatus[0]} end) +
    (if $routeQuality[0] == null then {} else {routeQuality: $routeQuality[0]} end) +
    (if $exitHealth[0] == null then {} else {exitHealth: $exitHealth[0]} end))' >"$heartbeat_json"

curl --config "$curl_config" \
  --header 'Content-Type: application/json' \
  --request POST \
  --data-binary "@$heartbeat_json" \
  --output /dev/null \
  "${WIREGUARD_API_BASE_URL%/}/api/internal/wireguard/relays/${WIREGUARD_RELAY_ID}/heartbeat"

# Reset interval counters only after the heartbeat is accepted; failed uploads
# keep accumulating bytes for the next run instead of losing observations.
configure_traffic_counters
