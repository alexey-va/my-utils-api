#!/usr/bin/env bash
set -euo pipefail

mode=plan
replace=false
api_base_url=""
relay_id=""
agent_token_file=""
server_private_key_file=""
public_endpoint=""
client_cidr=""
listen_port=51820
interface=wg-users
egress_interface=awg-exit

usage() {
  echo "Usage: $0 --api-base-url URL --relay-id UUID --agent-token-file FILE --public-endpoint HOST:PORT --client-cidr CIDR [--server-private-key-file FILE] [--listen-port PORT] [--apply] [--replace]" >&2
}

valid_private_cidr() {
  local cidr=$1 ip prefix a b c d ip_number host_bits mask
  [[ "$cidr" == */* ]] || return 1
  ip=${cidr%/*}; prefix=${cidr#*/}
  [[ "$prefix" =~ ^[0-9]+$ ]] && ((prefix >= 16 && prefix <= 29)) || return 1
  IFS=. read -r a b c d <<<"$ip"
  for octet in "$a" "$b" "$c" "$d"; do
    [[ "$octet" =~ ^(0|[1-9][0-9]{0,2})$ ]] && ((10#$octet <= 255)) || return 1
  done
  a=$((10#$a)); b=$((10#$b)); c=$((10#$c)); d=$((10#$d))
  ((a == 10 || (a == 172 && b >= 16 && b <= 31) || (a == 192 && b == 168))) || return 1
  ip_number=$(((a << 24) | (b << 16) | (c << 8) | d))
  host_bits=$((32 - prefix)); mask=$(((0xffffffff << host_bits) & 0xffffffff))
  (( (ip_number & mask) == ip_number ))
}

while (($#)); do
  case "$1" in
    --api-base-url) api_base_url="${2:-}"; shift 2 ;;
    --relay-id) relay_id="${2:-}"; shift 2 ;;
    --agent-token-file) agent_token_file="${2:-}"; shift 2 ;;
    --server-private-key-file) server_private_key_file="${2:-}"; shift 2 ;;
    --public-endpoint) public_endpoint="${2:-}"; shift 2 ;;
    --client-cidr) client_cidr="${2:-}"; shift 2 ;;
    --listen-port) listen_port="${2:-}"; shift 2 ;;
    --apply) mode=apply; shift ;;
    --replace) replace=true; shift ;;
    *) usage; exit 2 ;;
  esac
done

if [[ $EUID -ne 0 ]]; then
  echo "Run as root" >&2
  exit 1
fi
if [[ ! "$relay_id" =~ ^[0-9a-fA-F-]{36}$ ]]; then
  echo "Invalid relay id" >&2
  exit 1
fi
if [[ ! "$api_base_url" =~ ^https?://[^[:space:]]+$ ]]; then
  echo "Invalid API base URL" >&2
  exit 1
fi
if [[ ! "$public_endpoint" =~ ^[^[:space:]:]+:[0-9]{1,5}$ ]]; then
  echo "Invalid public endpoint" >&2
  exit 1
fi
endpoint_port=${public_endpoint##*:}
if ((10#$endpoint_port < 1 || 10#$endpoint_port > 65535)); then
  echo "Invalid public endpoint port" >&2
  exit 1
fi
if [[ ! "$listen_port" =~ ^[0-9]+$ ]] || ((listen_port < 1 || listen_port > 65535)); then
  echo "Invalid listen port" >&2
  exit 1
fi
if ! valid_private_cidr "$client_cidr"; then
  echo "Client CIDR must be private IPv4 /16 through /29" >&2
  exit 1
fi
if [[ -z "$agent_token_file" || ! -f "$agent_token_file" || "$(stat -c '%a' "$agent_token_file")" != "600" ]]; then
  echo "Agent token file must exist with mode 600" >&2
  exit 1
fi
if [[ -n "$server_private_key_file" ]]; then
  if [[ ! -f "$server_private_key_file" || "$(stat -c '%a' "$server_private_key_file")" != "600" ]]; then
    echo "WireGuard server private key file must exist with mode 600" >&2
    exit 1
  fi
  server_private_key=$(tr -d '\r\n' <"$server_private_key_file")
  if [[ ! "$server_private_key" =~ ^[A-Za-z0-9+/]{43}=$ ]]; then
    unset server_private_key
    echo "WireGuard server private key file is invalid" >&2
    exit 1
  fi
  unset server_private_key
fi
if [[ ! -f /etc/os-release ]] || ! grep -q '^ID=ubuntu$' /etc/os-release; then
  echo "Only Ubuntu is supported" >&2
  exit 1
fi
command -v systemctl >/dev/null || { echo "systemd is required" >&2; exit 1; }

target=/etc/wireguard/$interface.conf
if [[ -e "$target" && "$replace" != true ]]; then
  echo "Refusing to overwrite $target without --replace" >&2
  exit 1
fi

echo "Plan: install $interface, fail-closed $client_cidr routing through $egress_interface, and the one-minute control-plane agent"
if [[ "$mode" != apply ]]; then
  exit 0
fi
if [[ ! -d "/sys/class/net/$egress_interface" ]]; then
  echo "Required egress interface is not active: $egress_interface" >&2
  exit 1
fi

export DEBIAN_FRONTEND=noninteractive
apt-get update
apt-get install -y wireguard-tools jq curl iptables
install -d -m 700 /etc/wireguard /etc/my-utils
install -d -m 755 /usr/local/libexec

tmp_dir="$(mktemp -d)"
cleanup() { rm -rf -- "$tmp_dir"; }
trap cleanup EXIT INT TERM
if [[ -n "$server_private_key_file" ]]; then
  server_private_key=$(tr -d '\r\n' <"$server_private_key_file")
else
  wg genkey >"$tmp_dir/server.key"
  server_private_key=$(<"$tmp_dir/server.key")
fi
server_address="${client_cidr%/*}"
server_address="${server_address%.*}.1/${client_cidr#*/}"
cat >"$tmp_dir/$interface.conf" <<EOF
[Interface]
Address = $server_address
PrivateKey = $server_private_key
ListenPort = $listen_port
MTU = 1380
SaveConfig = false
EOF
unset server_private_key
if [[ -e "$target" ]]; then
  cp -a -- "$target" "$target.backup.$(date -u +%Y%m%dT%H%M%SZ)"
fi
install -m 600 -o root -g root "$tmp_dir/$interface.conf" "$target"

cat >"$tmp_dir/wireguard-routing" <<EOF
#!/usr/bin/env bash
set -euo pipefail
action="\${1:-start}"
client_cidr='$client_cidr'
ingress='$interface'
egress='$egress_interface'
egress_pattern='awg-exit+'
table=51889
priority=1089
chain=MYUTILS-WG-USERS
listen_port='$listen_port'

start() {
  sysctl -q -w net.ipv4.ip_forward=1
  ip route replace unreachable default table "\$table" metric 32767
  ip route replace "\$client_cidr" dev "\$ingress" table "\$table" scope link
  ip route replace default dev "\$egress" table "\$table" metric 10
  ip rule show | grep -Fq "from \$client_cidr lookup \$table" || ip rule add priority "\$priority" from "\$client_cidr" table "\$table"
  iptables -N "\$chain" 2>/dev/null || true
  iptables -F "\$chain"
  iptables -A "\$chain" -i "\$ingress" -s "\$client_cidr" -o "\$egress_pattern" -j ACCEPT
  iptables -A "\$chain" -i "\$egress_pattern" -d "\$client_cidr" -m conntrack --ctstate ESTABLISHED,RELATED -j ACCEPT
  iptables -A "\$chain" -i "\$ingress" -s "\$client_cidr" -j REJECT --reject-with icmp-admin-prohibited
  iptables -A "\$chain" -j RETURN
  iptables -C FORWARD -j "\$chain" 2>/dev/null || iptables -I FORWARD 1 -j "\$chain"
  iptables -C INPUT -p udp --dport "\$listen_port" -j ACCEPT 2>/dev/null || iptables -I INPUT 1 -p udp --dport "\$listen_port" -j ACCEPT
}

stop() {
  iptables -D INPUT -p udp --dport "\$listen_port" -j ACCEPT 2>/dev/null || true
  iptables -D FORWARD -j "\$chain" 2>/dev/null || true
  iptables -F "\$chain" 2>/dev/null || true
  iptables -X "\$chain" 2>/dev/null || true
  ip rule del priority "\$priority" from "\$client_cidr" table "\$table" 2>/dev/null || true
  ip route flush table "\$table" 2>/dev/null || true
}

case "\$action" in
  start) start ;;
  stop) stop ;;
  *) echo "Usage: \$0 start|stop" >&2; exit 2 ;;
esac
EOF
install -m 755 -o root -g root "$tmp_dir/wireguard-routing" /usr/local/libexec/my-utils-wireguard-routing

cat >"/etc/systemd/system/my-utils-wireguard-routing.service" <<EOF
[Unit]
Description=Fail-closed routing for my-utils WireGuard clients
After=systemd-networkd.service network-online.target my-utils-awg-exit.service wg-quick@$interface.service
Wants=my-utils-awg-exit.service
Requires=wg-quick@$interface.service

[Service]
Type=oneshot
RemainAfterExit=yes
ExecStart=/usr/local/libexec/my-utils-wireguard-routing start
ExecStop=/usr/local/libexec/my-utils-wireguard-routing stop

[Install]
WantedBy=multi-user.target
EOF

install -m 755 -o root -g root "$(dirname "$0")/route-probe.sh" /usr/local/libexec/my-utils-wireguard-route-probe
install -m 755 -o root -g root "$(dirname "$0")/routing-reconcile.sh" /usr/local/libexec/my-utils-wireguard-routing-reconcile
install -m 755 -o root -g root "$(dirname "$0")/wireguard-agent.sh" /usr/local/libexec/my-utils-wireguard-agent
install -m 644 -o root -g root "$(dirname "$0")/systemd/my-utils-wireguard-agent.service" /etc/systemd/system/
install -m 644 -o root -g root "$(dirname "$0")/systemd/my-utils-wireguard-agent.timer" /etc/systemd/system/
install -m 644 -o root -g root "$(dirname "$0")/systemd/my-utils-wireguard-routing-reconcile.service" /etc/systemd/system/
agent_token="$(<"$agent_token_file")"
if [[ ${#agent_token} -lt 40 || "$agent_token" == *$'\n'* || "$agent_token" == *$'\r'* ]]; then
  echo "Agent token file is invalid" >&2
  exit 1
fi
cat >"$tmp_dir/wireguard-agent.env" <<EOF
WIREGUARD_API_BASE_URL=$api_base_url
WIREGUARD_RELAY_ID=$relay_id
WIREGUARD_AGENT_TOKEN=$agent_token
WIREGUARD_INTERFACE=$interface
WIREGUARD_PUBLIC_ENDPOINT=$public_endpoint
WIREGUARD_EXIT_PREFERENCE_FILE=/var/lib/my-utils-wireguard/exit-preference
EOF
unset agent_token
install -m 600 -o root -g root "$tmp_dir/wireguard-agent.env" /etc/my-utils/wireguard-agent.env
cat >/etc/sysctl.d/99-my-utils-wireguard.conf <<EOF
net.ipv4.ip_forward = 1
EOF
sysctl --system >/dev/null

systemctl daemon-reload
systemctl enable --now "wg-quick@$interface.service"
systemctl enable --now my-utils-wireguard-routing.service
systemctl enable --now my-utils-wireguard-routing-reconcile.service
systemctl enable --now my-utils-wireguard-agent.timer
systemctl start my-utils-wireguard-agent.service

wg show "$interface" >/dev/null
ip rule show | grep -Fq "from $client_cidr lookup 51889"
echo "WireGuard relay is active on UDP $listen_port"
