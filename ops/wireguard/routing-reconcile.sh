#!/usr/bin/env bash
set -euo pipefail

libexec_dir=${WIREGUARD_ROUTING_LIBEXEC_DIR:-/usr/local/libexec}
lock_file=${WIREGUARD_ROUTING_RECONCILE_LOCK_FILE:-/run/my-utils-wireguard-routing-reconcile.lock}
failover_lock_file=${WIREGUARD_FAILOVER_LOCK_FILE:-/run/my-utils-awg-failover.lock}

for command in flock ip systemctl; do
  command -v "$command" >/dev/null || { echo "Required command is missing: $command" >&2; exit 1; }
done

unit_active() {
  systemctl is-active --quiet "$1"
}

routing_rules=$(ip -4 rule show)
routing_routes=$(ip -4 route show table 51889)

repair_main=false
repair_geo=false
repair_proxy=false

if unit_active my-utils-wireguard-routing.service && {
  ! grep -Eq '(^|[[:space:]])1089:.*from 10\.89\.0\.0/24 lookup 51889([[:space:]]|$)' <<<"$routing_rules" ||
  ! grep -Eq '^10\.89\.0\.0/24 dev wg-users([[:space:]]|$)' <<<"$routing_routes" ||
  ! grep -Eq '^unreachable default([[:space:]]|$)' <<<"$routing_routes";
}; then
  repair_main=true
fi
if unit_active my-utils-geo-routing.service &&
  ! grep -Eq '(^|[[:space:]])1088:.*fwmark 0x51890 lookup main([[:space:]]|$)' <<<"$routing_rules"; then
  repair_geo=true
fi
if unit_active my-utils-api-proxy-routing.service &&
  ! grep -Eq '(^|[[:space:]])1087:.*fwmark 0x51891 lookup 51889([[:space:]]|$)' <<<"$routing_rules"; then
  repair_proxy=true
fi

if [[ "$repair_main" != true && "$repair_geo" != true && "$repair_proxy" != true ]]; then
  exit 0
fi

exec 9>"$lock_file"
flock -w 10 9
exec 8>"$failover_lock_file"
flock -w 10 8

# Another invocation may have repaired the rules while this process waited.
routing_rules=$(ip -4 rule show)
routing_routes=$(ip -4 route show table 51889)
if unit_active my-utils-wireguard-routing.service &&
  grep -Eq '(^|[[:space:]])1089:.*from 10\.89\.0\.0/24 lookup 51889([[:space:]]|$)' <<<"$routing_rules" &&
  grep -Eq '^10\.89\.0\.0/24 dev wg-users([[:space:]]|$)' <<<"$routing_routes" &&
  grep -Eq '^unreachable default([[:space:]]|$)' <<<"$routing_routes"; then
  repair_main=false
fi
if unit_active my-utils-geo-routing.service &&
  grep -Eq '(^|[[:space:]])1088:.*fwmark 0x51890 lookup main([[:space:]]|$)' <<<"$routing_rules"; then
  repair_geo=false
fi
if unit_active my-utils-api-proxy-routing.service &&
  grep -Eq '(^|[[:space:]])1087:.*fwmark 0x51891 lookup 51889([[:space:]]|$)' <<<"$routing_rules"; then
  repair_proxy=false
fi

if [[ "$repair_main" == true ]]; then
  echo "Restoring lost WireGuard policy-routing state" >&2
  "$libexec_dir/my-utils-wireguard-routing" start
  # The main helper rebuilds filter chains, so reapply active dependents too.
  unit_active my-utils-geo-routing.service && repair_geo=true
  unit_active my-utils-api-proxy-routing.service && repair_proxy=true
fi
if [[ "$repair_geo" == true ]]; then
  echo "Restoring lost RU-direct policy rule" >&2
  "$libexec_dir/my-utils-geo-routing" start
fi
if [[ "$repair_proxy" == true ]]; then
  echo "Restoring lost API-proxy policy rule" >&2
  "$libexec_dir/my-utils-api-proxy-routing" start
fi

routing_rules=$(ip -4 rule show)
routing_routes=$(ip -4 route show table 51889)
if unit_active my-utils-wireguard-routing.service; then
  grep -Eq '(^|[[:space:]])1089:.*from 10\.89\.0\.0/24 lookup 51889([[:space:]]|$)' <<<"$routing_rules"
  grep -Eq '^10\.89\.0\.0/24 dev wg-users([[:space:]]|$)' <<<"$routing_routes"
  grep -Eq '^unreachable default([[:space:]]|$)' <<<"$routing_routes"
fi
if unit_active my-utils-geo-routing.service; then
  grep -Eq '(^|[[:space:]])1088:.*fwmark 0x51890 lookup main([[:space:]]|$)' <<<"$routing_rules"
fi
if unit_active my-utils-api-proxy-routing.service; then
  grep -Eq '(^|[[:space:]])1087:.*fwmark 0x51891 lookup 51889([[:space:]]|$)' <<<"$routing_rules"
fi
