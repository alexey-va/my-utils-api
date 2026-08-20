#!/usr/bin/env bash
set -euo pipefail

umask 077

env_file=${AWG_FAILOVER_ENV_FILE:-/etc/my-utils/awg-failover.env}
[[ -r "$env_file" ]] || { echo "AWG failover environment is not readable: $env_file" >&2; exit 1; }
# shellcheck source=/dev/null
source "$env_file"

required=(
  AWG_PRIMARY_INTERFACE AWG_PRIMARY_EXPECTED_EGRESS
  AWG_SECONDARY_INTERFACE AWG_SECONDARY_EXPECTED_EGRESS
  AWG_ROUTE_TABLE AWG_FAILURE_THRESHOLD AWG_RECOVERY_THRESHOLD
  AWG_HANDSHAKE_MAX_AGE AWG_PROBE_URL AWG_STATE_FILE AWG_STATUS_FILE
)
for name in "${required[@]}"; do
  [[ -n "${!name:-}" ]] || { echo "Missing AWG failover setting: $name" >&2; exit 1; }
done
for command in awg curl flock install ip jq mktemp python3; do
  command -v "$command" >/dev/null || { echo "Required command is missing: $command" >&2; exit 1; }
done
for interface in "$AWG_PRIMARY_INTERFACE" "$AWG_SECONDARY_INTERFACE"; do
  [[ "$interface" =~ ^[a-zA-Z0-9_.-]+$ && ${#interface} -le 15 ]]
done
[[ "$AWG_PRIMARY_INTERFACE" != "$AWG_SECONDARY_INTERFACE" ]]
for expected_egress in "$AWG_PRIMARY_EXPECTED_EGRESS" "$AWG_SECONDARY_EXPECTED_EGRESS"; do
  [[ "$expected_egress" =~ ^([0-9]{1,3}\.){3}[0-9]{1,3}$ ]]
done
for value in "$AWG_ROUTE_TABLE" "$AWG_FAILURE_THRESHOLD" "$AWG_RECOVERY_THRESHOLD" "$AWG_HANDSHAKE_MAX_AGE"; do
  [[ "$value" =~ ^[0-9]+$ ]]
done
((AWG_FAILURE_THRESHOLD >= 1 && AWG_FAILURE_THRESHOLD <= 20))
((AWG_RECOVERY_THRESHOLD >= 1 && AWG_RECOVERY_THRESHOLD <= 20))
((AWG_HANDSHAKE_MAX_AGE >= 30 && AWG_HANDSHAKE_MAX_AGE <= 900))
[[ "$AWG_PROBE_URL" =~ ^https://[^[:space:]]+$ ]]

exec 9>/run/my-utils-awg-failover.lock
flock -n 9 || exit 0

tmp_dir=$(mktemp -d /run/my-utils-awg-failover.XXXXXX)
cleanup() { rm -rf -- "$tmp_dir"; }
trap cleanup EXIT INT TERM

probe_exit() {
  local exit_id=$1 interface=$2 expected_egress=$3
  local now handshake age body latency_seconds observed_egress reason handshake_output
  now=$(date +%s)
  body=$tmp_dir/$exit_id.egress
  reason=""
  handshake=0
  age=null
  latency_seconds=""
  observed_egress=""

  if ! ip link show dev "$interface" >/dev/null 2>&1; then
    reason=interface_missing
  else
    if ! handshake_output=$(awg show "$interface" latest-handshakes 2>/dev/null); then
      reason=interface_missing
    else
      handshake=$(awk 'BEGIN { max=0 } { if ($2 > max) max=$2 } END { print max }' <<<"$handshake_output")
      [[ "$handshake" =~ ^[0-9]+$ ]] || handshake=0
    fi
    if [[ -z "$reason" ]] && ((handshake == 0)); then
      reason=handshake_missing
    elif [[ -z "$reason" ]]; then
      age=$((now - handshake))
      if ((age < 0 || age > AWG_HANDSHAKE_MAX_AGE)); then
        reason=handshake_stale
      elif ! latency_seconds=$(curl --fail --silent --show-error \
        --connect-timeout 2 --max-time 5 --interface "$interface" \
        --output "$body" --write-out '%{time_total}' "$AWG_PROBE_URL"); then
        reason=egress_probe_failed
      else
        observed_egress=$(tr -d '\r\n' <"$body")
        if [[ "$observed_egress" != "$expected_egress" ]]; then
          reason=unexpected_egress
        fi
      fi
    fi
  fi

  jq -n \
    --arg id "$exit_id" \
    --arg interface "$interface" \
    --arg expectedEgress "$expected_egress" \
    --arg observedEgress "$observed_egress" \
    --arg reason "$reason" \
    --arg latency "$latency_seconds" \
    --argjson handshake "$handshake" \
    --argjson age "$age" \
    '{
      id: $id,
      interface: $interface,
      healthy: ($reason == ""),
      reason: (if $reason == "" then null else $reason end),
      expectedEgressIp: $expectedEgress,
      observedEgressIp: (if $observedEgress == "" then null else $observedEgress end),
      handshakeAtEpoch: $handshake,
      handshakeAgeSeconds: $age,
      latencyMs: (if $latency == "" then null else (($latency | tonumber) * 1000 | round) end)
    }'
}

probe_exit primary "$AWG_PRIMARY_INTERFACE" "$AWG_PRIMARY_EXPECTED_EGRESS" >"$tmp_dir/primary.json"
probe_exit secondary "$AWG_SECONDARY_INTERFACE" "$AWG_SECONDARY_EXPECTED_EGRESS" >"$tmp_dir/secondary.json"
jq -n --slurpfile primary "$tmp_dir/primary.json" --slurpfile secondary "$tmp_dir/secondary.json" \
  '{primary: $primary[0], secondary: $secondary[0]}' >"$tmp_dir/probes.json"

state_dir=$(dirname -- "$AWG_STATE_FILE")
status_dir=$(dirname -- "$AWG_STATUS_FILE")
install -d -m 700 "$state_dir"
install -d -m 755 "$status_dir"
if ! jq -e '
  (.active == null or .active == "primary" or .active == "secondary") and
  (.counters | type == "object")
' "$AWG_STATE_FILE" >/dev/null 2>&1; then
  route_interface=$(ip -4 route show table "$AWG_ROUTE_TABLE" | awk '$1 == "default" && $2 == "dev" { print $3; exit }')
  case "$route_interface" in
    "$AWG_PRIMARY_INTERFACE") initial_active=primary ;;
    "$AWG_SECONDARY_INTERFACE") initial_active=secondary ;;
    *) initial_active=null ;;
  esac
  printf '{"active":%s,"counters":{}}\n' \
    "$([[ "$initial_active" == null ]] && printf null || printf '"%s"' "$initial_active")" >"$tmp_dir/initial-state.json"
  state_input=$tmp_dir/initial-state.json
else
  state_input=$AWG_STATE_FILE
fi

script_dir=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)
python3 "$script_dir/decide-failover.py" \
  --state "$state_input" \
  --probes "$tmp_dir/probes.json" \
  --failure-threshold "$AWG_FAILURE_THRESHOLD" \
  --recovery-threshold "$AWG_RECOVERY_THRESHOLD" >"$tmp_dir/decision.json"

desired_id=$(jq -r '.active // ""' "$tmp_dir/decision.json")
desired_interface=""
case "$desired_id" in
  primary) desired_interface=$AWG_PRIMARY_INTERFACE ;;
  secondary) desired_interface=$AWG_SECONDARY_INTERFACE ;;
  "")
    for interface in "$AWG_PRIMARY_INTERFACE" "$AWG_SECONDARY_INTERFACE"; do
      ip route del default dev "$interface" table "$AWG_ROUTE_TABLE" 2>/dev/null || true
    done
    ;;
  *) echo "Invalid failover decision" >&2; exit 1 ;;
esac
if [[ -n "$desired_interface" ]]; then
  if ip link show dev "$desired_interface" >/dev/null 2>&1; then
    ip route replace default dev "$desired_interface" table "$AWG_ROUTE_TABLE" metric 10
  else
    for interface in "$AWG_PRIMARY_INTERFACE" "$AWG_SECONDARY_INTERFACE"; do
      ip route del default dev "$interface" table "$AWG_ROUTE_TABLE" 2>/dev/null || true
    done
  fi
fi
ip route replace unreachable default table "$AWG_ROUTE_TABLE" metric 32767

state_tmp=$(mktemp "$state_dir/.awg-failover-state.XXXXXX")
install -m 600 "$tmp_dir/decision.json" "$state_tmp"
mv -f -- "$state_tmp" "$AWG_STATE_FILE"

checked_at=$(date -u +%Y-%m-%dT%H:%M:%SZ)
if [[ -z "$desired_id" ]]; then
  overall_status=DOWN
elif jq -e '[.primary.healthy, .secondary.healthy] | all' "$tmp_dir/probes.json" >/dev/null; then
  overall_status=HEALTHY
else
  overall_status=DEGRADED
fi
jq -n \
  --arg checkedAt "$checked_at" \
  --arg overallStatus "$overall_status" \
  --arg activeExit "$desired_id" \
  --arg activeInterface "$desired_interface" \
  --slurpfile probes "$tmp_dir/probes.json" \
  --slurpfile decision "$tmp_dir/decision.json" \
  '{
    schemaVersion: 1,
    checkedAt: $checkedAt,
    overallStatus: $overallStatus,
    activeExit: (if $activeExit == "" then null else $activeExit end),
    activeInterface: (if $activeInterface == "" then null else $activeInterface end),
    changed: $decision[0].changed,
    counters: $decision[0].counters,
    exits: $probes[0]
  }' >"$tmp_dir/exit-health.json"
status_tmp=$(mktemp "$status_dir/.exit-health.XXXXXX")
install -m 644 "$tmp_dir/exit-health.json" "$status_tmp"
mv -f -- "$status_tmp" "$AWG_STATUS_FILE"
