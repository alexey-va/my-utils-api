#!/usr/bin/env bash
set -euo pipefail

client_public_key_file=""
server_config=""
client_params=""
endpoint=""
listen_port=42697
server_address=10.8.1.1/24
client_address=10.8.1.250/32

usage() {
  echo "Usage: $0 --client-public-key-file FILE --server-config FILE --client-params FILE --endpoint HOST:PORT [--server-address 10.8.N.1/24] [--client-address 10.8.N.250/32]" >&2
}

while (($#)); do
  case "$1" in
    --client-public-key-file) client_public_key_file=${2:-}; shift 2 ;;
    --server-config) server_config=${2:-}; shift 2 ;;
    --client-params) client_params=${2:-}; shift 2 ;;
    --endpoint) endpoint=${2:-}; shift 2 ;;
    --server-address) server_address=${2:-}; shift 2 ;;
    --client-address) client_address=${2:-}; shift 2 ;;
    *) usage; exit 2 ;;
  esac
done

for command in wg shuf stat; do
  command -v "$command" >/dev/null || { echo "Required command is missing: $command" >&2; exit 1; }
done
[[ -f "$client_public_key_file" ]]
[[ -n "$server_config" && -n "$client_params" ]]
[[ "$endpoint" =~ ^[^[:space:]:]+:([0-9]{1,5})$ ]]
listen_port=${BASH_REMATCH[1]}
((10#$listen_port >= 1 && 10#$listen_port <= 65535))
[[ ! -e "$server_config" && ! -e "$client_params" ]]
[[ "$server_address" =~ ^10\.8\.([0-9]{1,3})\.1/24$ ]]
overlay_octet=${BASH_REMATCH[1]}
((10#$overlay_octet >= 1 && 10#$overlay_octet <= 254))
[[ "$client_address" == "10.8.$((10#$overlay_octet)).250/32" ]]

client_public=$(tr -d '\r\n' <"$client_public_key_file")
[[ "$client_public" =~ ^[A-Za-z0-9+/]{43}=$ ]]

umask 077
tmp_dir=$(mktemp -d)
trap 'rm -rf "$tmp_dir"' EXIT
server_private=$tmp_dir/server.key
server_public=$tmp_dir/server.pub
preshared_key=$tmp_dir/peer.psk
wg genkey >"$server_private"
wg pubkey <"$server_private" >"$server_public"
wg genpsk >"$preshared_key"

jc=$(shuf -i 3-5 -n 1)
jmin=$(shuf -i 40-80 -n 1)
jmax=$(shuf -i 900-1200 -n 1)
s1=$(shuf -i 10-30 -n 1)
s2=$(shuf -i 10-30 -n 1)
h1=$(shuf -i 100000000-999999999 -n 1)
h2=$(shuf -i 100000000-999999999 -n 1)
h3=$(shuf -i 100000000-999999999 -n 1)
h4=$(shuf -i 100000000-999999999 -n 1)

cat >"$server_config" <<EOF
[Interface]
Address = $server_address
PrivateKey = $(<"$server_private")
ListenPort = $listen_port
Jc = $jc
Jmin = $jmin
Jmax = $jmax
S1 = $s1
S2 = $s2
H1 = $h1
H2 = $h2
H3 = $h3
H4 = $h4
MTU = 1380
Table = off

[Peer]
PublicKey = $client_public
PresharedKey = $(<"$preshared_key")
AllowedIPs = $client_address, 10.89.0.0/24
EOF

cat >"$client_params" <<EOF
SERVER_PUBLIC_KEY=$(<"$server_public")
PRESHARED_KEY=$(<"$preshared_key")
ENDPOINT=$endpoint
CLIENT_ADDRESS=$client_address
JC=$jc
JMIN=$jmin
JMAX=$jmax
S1=$s1
S2=$s2
H1=$h1
H2=$h2
H3=$h3
H4=$h4
EOF
chmod 600 "$server_config" "$client_params"
echo "Generated protected server and client parameter files"
