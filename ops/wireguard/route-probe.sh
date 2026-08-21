#!/usr/bin/env bash
set -euo pipefail

if (($# != 2)); then
  echo "Usage: $0 INTERFACE TARGET[,TARGET...]" >&2
  exit 2
fi

interface=$1
targets_csv=$2
if [[ ! "$interface" =~ ^[a-zA-Z0-9_=+.-]{1,15}$ ]]; then
  echo "Route probe interface is invalid" >&2
  exit 1
fi

valid_ipv4() {
  local value=$1 a b c d
  IFS=. read -r a b c d <<<"$value"
  for octet in "$a" "$b" "$c" "$d"; do
    [[ "$octet" =~ ^(0|[1-9][0-9]{0,2})$ ]] && ((10#$octet <= 255)) || return 1
  done
}

probe_target() {
  local target=$1 output=$2 loss rtt
  ping -n -q -c 3 -W 2 -I "$interface" "$target" >"$output.ping" 2>&1 || true
  loss="$(awk -F', ' '
    /packet loss/ {
      for (i=1; i<=NF; i++) if ($i ~ /packet loss/) {
        gsub(/% packet loss.*/, "", $i)
        gsub(/^[[:space:]]*/, "", $i)
        print $i
      }
    }
  ' "$output.ping")"
  rtt="$(awk -F' = ' '/^(rtt|round-trip)/ { split($2, values, "/"); print values[2] }' "$output.ping")"
  [[ "$loss" =~ ^[0-9]+([.][0-9]+)?$ ]] || loss=100
  [[ "$rtt" =~ ^[0-9]+([.][0-9]+)?$ ]] || rtt=""
  printf '%s\t%s\t%s\n' "$target" "$loss" "$rtt" >"$output"
}

IFS=, read -r -a raw_targets <<<"$targets_csv"
targets=()
declare -A seen_targets=()
for raw_target in "${raw_targets[@]}"; do
  target="${raw_target//[[:space:]]/}"
  valid_ipv4 "$target" || {
    echo "Route probe target is invalid" >&2
    exit 1
  }
  if [[ -n "${seen_targets[$target]:-}" ]]; then
    echo "Route probe targets must be distinct" >&2
    exit 1
  fi
  seen_targets[$target]=1
  targets+=("$target")
done
if ((${#targets[@]} == 0 || ${#targets[@]} % 2 == 0 || ${#targets[@]} > 5)); then
  echo "Route probe requires one, three, or five targets" >&2
  exit 1
fi

tmp_dir="$(mktemp -d)"
cleanup() {
  rm -rf -- "$tmp_dir"
}
trap cleanup EXIT INT TERM

pids=()
for index in "${!targets[@]}"; do
  probe_target "${targets[$index]}" "$tmp_dir/$index.tsv" &
  pids+=("$!")
done
for pid in "${pids[@]}"; do
  wait "$pid"
done

sort -t $'\t' -k2,2n -k3,3n "$tmp_dir"/*.tsv >"$tmp_dir/sorted.tsv"
median_line=$(((${#targets[@]} / 2) + 1))
IFS=$'\t' read -r target loss rtt < <(sed -n "${median_line}p" "$tmp_dir/sorted.tsv")
if [[ -n "$rtt" ]]; then
  printf '{"target":"%s","packetLossPercent":%s,"averageRttMs":%s}\n' "$target" "$loss" "$rtt"
else
  printf '{"target":"%s","packetLossPercent":%s,"averageRttMs":null}\n' "$target" "$loss"
fi
