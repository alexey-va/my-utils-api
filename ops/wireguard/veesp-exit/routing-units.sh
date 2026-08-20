#!/usr/bin/env bash

readonly -a awg_dependent_routing_units=(
  my-utils-wireguard-routing.service
  my-utils-geo-routing.service
  my-utils-api-proxy-routing.service
)

capture_active_routing_units() {
  local state_file=$1 unit
  : >"$state_file"
  chmod 600 "$state_file"
  for unit in "${awg_dependent_routing_units[@]}"; do
    if systemctl is-active --quiet "$unit"; then
      printf '%s\n' "$unit" >>"$state_file"
    fi
  done
}

routing_unit_was_active() {
  local state_file=$1 unit=$2
  grep -Fxq -- "$unit" "$state_file"
}

restore_active_routing_units() {
  local state_file=$1

  if routing_unit_was_active "$state_file" my-utils-wireguard-routing.service; then
    systemctl start my-utils-wireguard-routing.service
  fi
  if routing_unit_was_active "$state_file" my-utils-geo-routing.service; then
    systemctl start my-utils-geo-routing.service
    systemctl start my-utils-geo-routing-update.service
  fi
  if routing_unit_was_active "$state_file" my-utils-api-proxy-routing.service; then
    systemctl start my-utils-api-proxy-routing.service
  fi
}
