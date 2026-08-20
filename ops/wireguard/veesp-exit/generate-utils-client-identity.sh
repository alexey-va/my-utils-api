#!/usr/bin/env bash
set -euo pipefail

umask 077

mode=plan
private_key_file=""
public_key_file=""

usage() {
  echo "Usage: $0 --private-key-file FILE --public-key-file FILE [--apply]" >&2
}

while (($#)); do
  case "$1" in
    --private-key-file) private_key_file=${2:-}; shift 2 ;;
    --public-key-file) public_key_file=${2:-}; shift 2 ;;
    --apply) mode=apply; shift ;;
    *) usage; exit 2 ;;
  esac
done

command -v awg >/dev/null || { echo "Required command is missing: awg" >&2; exit 1; }
[[ -n "$private_key_file" && -n "$public_key_file" ]]
[[ ! -e "$private_key_file" && ! -e "$public_key_file" ]] || {
  echo "Refusing to replace an existing client identity" >&2
  exit 1
}
[[ -d "$(dirname -- "$private_key_file")" && -d "$(dirname -- "$public_key_file")" ]]

echo "Plan: generate a protected AWG client identity"
if [[ "$mode" != apply ]]; then
  echo "Plan only; no key files were created"
  exit 0
fi

tmp_dir=$(mktemp -d)
cleanup() { rm -rf -- "$tmp_dir"; }
trap cleanup EXIT INT TERM
awg genkey >"$tmp_dir/private.key"
awg pubkey <"$tmp_dir/private.key" >"$tmp_dir/public.key"
install -m 600 "$tmp_dir/private.key" "$private_key_file"
install -m 600 "$tmp_dir/public.key" "$public_key_file"
chmod 600 "$private_key_file" "$public_key_file"
echo "Generated protected AWG client identity"
