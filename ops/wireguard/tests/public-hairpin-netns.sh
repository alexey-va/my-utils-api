#!/usr/bin/env bash
# Run: sudo unshare --net -- bash tests/public-hairpin-netns.sh [helper.py]
# All interfaces, sysctls, rules and listeners are confined to disposable netns.
set -euo pipefail
[[ $(readlink /proc/self/ns/net) != $(readlink /proc/1/ns/net) ]] || {
  echo 'REFUSING: current network namespace is the host namespace' >&2
  exit 2
}
[[ $# -le 1 ]] || { echo "Usage: $0 [helper.py]" >&2; exit 2; }
test_dir=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)
helper_source=${1:-"$test_dir/../public-hairpin-routing.py"}
[[ -r "$helper_source" ]] || { echo "Helper source is not readable: $helper_source" >&2; exit 2; }
task_tmp=$(mktemp -d /tmp/valheim-hairpin-test.XXXXXX)
helper() {
  env WIREGUARD_PUBLIC_ADDRESS=195.133.60.194 WIREGUARD_CLIENT_CIDR=10.89.0.0/24 \
    WIREGUARD_INGRESS_INTERFACE=wg-users WIREGUARD_ROUTE_TABLE=51889 \
    WIREGUARD_HAIRPIN_LOCK_FILE="$task_tmp/helper.lock" \
    WIREGUARD_HAIRPIN_STATE_FILE="$task_tmp/helper-state.json" \
    python3 "$task_tmp/public-hairpin-routing.py" "$@"
}
child_pids=()
cleanup() {
  for child in "${child_pids[@]}"; do kill "$child" 2>/dev/null || true; done
  wait 2>/dev/null || true
  rm -rf -- "$task_tmp"
}
trap cleanup EXIT
cp -- "$helper_source" "$task_tmp/public-hairpin-routing.py"
parent_netns=$(readlink /proc/self/ns/net)
new_ns() {
  unshare --net -- sleep 90 &
  new_pid=$!
  child_pids+=("$new_pid")
  for attempt in {1..50}; do
    [[ $(readlink "/proc/$new_pid/ns/net") != "$parent_netns" ]] && return
    sleep 0.02
  done
  echo 'child netns failed to start' >&2
  exit 3
}
new_ns
client_pid=$new_pid
new_ns
exit_pid=$new_pid
client() { nsenter -t "$client_pid" -n -- "$@"; }
exit_ns() { nsenter -t "$exit_pid" -n -- "$@"; }

ip link set lo up
ip link add wg-users type veth peer name client0
ip link set client0 netns "$client_pid"
ip address add 10.89.0.1/24 dev wg-users
ip link set wg-users up
client ip link set lo up
client ip address add 10.89.0.2/24 dev client0
client ip link set client0 up
client ip route add default via 10.89.0.1

ip link add awg-exit type veth peer name awg0
ip link set awg0 netns "$exit_pid"
ip address add 10.8.1.251/32 dev awg-exit
ip link set awg-exit up
ip route add 10.8.1.1/32 dev awg-exit
exit_ns ip link set lo up
exit_ns ip address add 10.8.1.1/24 dev awg0
exit_ns ip link set awg0 up

ip link add public0 type veth peer name internet0
ip link set internet0 netns "$exit_pid"
ip address add 195.133.60.194/32 dev public0
ip link set public0 up
ip route add 91.197.0.191/32 dev public0
ip route add default dev public0
exit_ns ip address add 91.197.0.191/32 dev internet0
exit_ns ip link set internet0 up
exit_ns ip route add 195.133.60.194/32 dev internet0
exit_ns ip route add default dev internet0

# Only this namespace and the disposable exit namespace are changed.
sysctl -q -w net.ipv4.ip_forward=1
sysctl -q -w net.ipv4.conf.all.rp_filter=2
sysctl -q -w net.ipv4.conf.default.rp_filter=2
sysctl -q -w net.ipv4.conf.all.accept_local=0
sysctl -q -w net.ipv4.conf.awg-exit.accept_local=0
exit_ns sysctl -q -w net.ipv4.ip_forward=1
exit_ns sysctl -q -w net.ipv4.conf.all.rp_filter=2

ip route add default via 10.8.1.1 dev awg-exit table 51889 metric 10
ip route add unreachable default table 51889 metric 32767
ip route add 10.89.0.0/24 dev wg-users table 51889
ip route add default via 10.8.1.1 dev awg-exit table 51986
ip route add 10.89.0.0/24 dev wg-users table 51986
ip rule add priority 1085 oif awg-exit table 51986
ip rule add priority 1086 from 10.8.1.251/32 table 51986
ip rule add priority 1088 fwmark 0x51890 table main
ip rule add priority 1089 from 10.89.0.0/24 table 51889

iptables -t mangle -A PREROUTING -i wg-users -d 195.133.60.194/32 -j MARK --set-mark 0x51890
iptables -A INPUT -i wg-users -d 10.89.0.1 -p udp --dport 53 -j ACCEPT
iptables -A INPUT -i wg-users ! -p icmp -m conntrack --ctstate NEW -j REJECT --reject-with icmp-port-unreachable
iptables -P FORWARD DROP
iptables -A FORWARD -i wg-users -s 10.89.0.0/24 -o awg-exit -j ACCEPT
iptables -A FORWARD -i awg-exit -d 10.89.0.0/24 -m conntrack --ctstate ESTABLISHED,RELATED -j ACCEPT
iptables -t nat -A POSTROUTING -s 10.89.0.0/24 -o awg-exit -j SNAT --to-source 10.8.1.251
exit_ns iptables -t nat -A POSTROUTING -s 10.8.1.251/32 -o internet0 -j SNAT --to-source 91.197.0.191

cat >"$task_tmp/server.py" <<'PY'
import socket, threading, time
def udp(ip, port):
    sock=socket.socket(socket.AF_INET,socket.SOCK_DGRAM)
    sock.bind((ip,port))
    while True:
        payload,peer=sock.recvfrom(1024)
        print('SERVER UDP',ip,port,'peer',peer,flush=True)
        sock.sendto(payload,peer)
def tcp(ip, port):
    sock=socket.socket(socket.AF_INET,socket.SOCK_STREAM)
    sock.setsockopt(socket.SOL_SOCKET,socket.SO_REUSEADDR,1)
    sock.bind((ip,port));sock.listen(8)
    while True:
        conn,peer=sock.accept()
        print('SERVER TCP',ip,port,'peer',peer,flush=True)
        with conn:
            conn.sendall(conn.recv(1024))
for f,args in [(udp,('195.133.60.194',2456)),(tcp,('195.133.60.194',18080)),(tcp,('195.133.60.194',22)),(udp,('10.89.0.1',53))]:
    threading.Thread(target=f,args=args,daemon=True).start()
print('READY',flush=True)
time.sleep(80)
PY
python3 "$task_tmp/server.py" >"$task_tmp/server.log" 2>&1 &
child_pids+=("$!")
for attempt in {1..50}; do
  grep -q READY "$task_tmp/server.log" && break
  sleep 0.02
done
cat >"$task_tmp/client.py" <<'PY'
import socket,sys
protocol,host,port,expected=sys.argv[1:]
sock=socket.socket(socket.AF_INET,socket.SOCK_DGRAM if protocol=='udp' else socket.SOCK_STREAM)
sock.settimeout(1.5)
payload=b'hairpin-proof'
try:
    sock.connect((host,int(port)))
    sock.sendall(payload)
    received=sock.recv(1024)
    success=received==payload
    print('CLIENT',protocol,host,port,'success',success,'peer',sock.getpeername(),flush=True)
except OSError as error:
    success=False
    print('CLIENT',protocol,host,port,'success',False,type(error).__name__,str(error),flush=True)
finally:
    sock.close()
if success!=(expected=='success'):sys.exit(1)
PY
probe() { client python3 "$task_tmp/client.py" "$@"; }

echo 'BASELINE: own-public destination is local and INPUT rejects it'
ip route get 195.133.60.194 from 10.89.0.2 iif wg-users
probe udp 195.133.60.194 2456 failure
exit_ns python3 "$task_tmp/client.py" tcp 195.133.60.194 22 success

echo 'SAME PRIORITY: insertion does not precede existing local rule'
ip rule add priority 0 iif wg-users from 10.89.0.0/24 to 195.133.60.194/32 table 51889
ip -4 rule show
ip route get 195.133.60.194 from 10.89.0.2 iif wg-users
ip rule del priority 0 iif wg-users from 10.89.0.0/24 to 195.133.60.194/32 table 51889

echo 'SAFE APPLY: local2, scoped unreachable1, then remove only old local0'
echo 'ACTUAL HELPER: foreign priority 0/1/2 refusal with unchanged rules/state'
for priority in 0 1 2; do
  ip rule add priority "$priority" fwmark 0x701 lookup 12345 protocol 243
  rules_before=$(ip -j -details -4 rule show)
  if helper start; then
    echo "ERROR: helper accepted foreign priority $priority" >&2
    exit 6
  fi
  [[ $(ip -j -details -4 rule show) == "$rules_before" ]]
  [[ ! -e "$task_tmp/helper-state.json" ]]
  ip rule del priority "$priority" fwmark 0x701 lookup 12345 protocol 243
done
ip rule add priority 2 lookup local protocol kernel
rules_before=$(ip -j -details -4 rule show)
if helper start; then
  echo 'ERROR: helper adopted unowned generic local2 rule' >&2
  exit 7
fi
[[ $(ip -j -details -4 rule show) == "$rules_before" ]]
[[ ! -e "$task_tmp/helper-state.json" ]]
ip rule del priority 2 lookup local protocol kernel
helper plan
helper start
rules_before=$(ip -j -details -4 rule show)
helper start
[[ $(ip -j -details -4 rule show) == "$rules_before" ]]
echo 'RULES JSON AFTER APPLY'
ip -j -details -4 rule show
ip route get 195.133.60.194 from 10.89.0.2 iif wg-users mark 0x51890
echo 'RP_FILTER=2, ACCEPT_LOCAL=0'
sysctl net.ipv4.conf.all.accept_local net.ipv4.conf.awg-exit.accept_local net.ipv4.conf.all.rp_filter
probe udp 195.133.60.194 2456 success
echo 'SERVER LOG AFTER FIRST EXTERNAL-HOP ATTEMPT'
cat "$task_tmp/server.log"

echo 'TCP, UDP AND LOCAL DNS ALL WORK WITHOUT ACCEPT_LOCAL'
probe udp 195.133.60.194 2456 success
probe tcp 195.133.60.194 18080 success
probe udp 10.89.0.1 53 success
echo 'PUBLIC SSH-LIKE TCP ENTRY REMAINS LOCAL AND REACHABLE'
exit_ns python3 "$task_tmp/client.py" tcp 195.133.60.194 22 success
echo 'ICMP ALSO COMPLETES THE EXTERNAL HOP'
client ping -n -c 1 -W 2 195.133.60.194
echo 'UNTARGETED LOCAL DELIVERY'
ip route get 195.133.60.194 from 91.197.0.191 iif public0
ip route get 195.133.60.194
ip route get 10.89.0.1 from 10.89.0.2 iif wg-users
echo 'SERVER LOG AFTER SUCCESSFUL EXTERNAL HOP'
cat "$task_tmp/server.log"
echo 'NAT COUNTERS: RELAY THEN EXIT'
iptables -t nat -L POSTROUTING -nvx
exit_ns iptables -t nat -L POSTROUTING -nvx

echo 'SIMULATED NETWORKD FOREIGN-RULE PURGE: kernel local lookup survives'
ip rule del priority 0 iif wg-users from 10.89.0.0/24 to 195.133.60.194/32 table 51889 protocol 242
ip rule del unreachable priority 1 iif wg-users from 10.89.0.0/24 to 195.133.60.194/32 protocol 242
ip -j -details -4 rule show
probe udp 10.89.0.1 53 success
exit_ns python3 "$task_tmp/client.py" tcp 195.133.60.194 22 success
helper start
probe udp 195.133.60.194 2456 success

echo 'NO EXIT: unreachable fallback prevents local/direct delivery'
ip route del default via 10.8.1.1 dev awg-exit table 51889 metric 10
if ip route get 195.133.60.194 from 10.89.0.2 iif wg-users; then
  echo 'ERROR: expected unreachable route' >&2
  exit 4
fi
probe udp 195.133.60.194 2456 failure
probe udp 10.89.0.1 53 success

echo 'EMPTY TABLE: terminal scoped unreachable still prevents local/direct delivery'
ip route flush table 51889
if ip route get 195.133.60.194 from 10.89.0.2 iif wg-users mark 0x51890; then
  echo 'ERROR: empty managed table escaped scoped unreachable' >&2
  exit 5
fi
probe udp 195.133.60.194 2456 failure
probe udp 10.89.0.1 53 success
exit_ns python3 "$task_tmp/client.py" tcp 195.133.60.194 22 success

echo 'ROLLBACK: restore normal local lookup before dropping scoped rule'
helper stop
rules_before=$(ip -j -details -4 rule show)
helper stop
[[ $(ip -j -details -4 rule show) == "$rules_before" ]]
[[ ! -e "$task_tmp/helper-state.json" ]]
ip -4 rule show
ip route get 195.133.60.194 from 10.89.0.2 iif wg-users
probe udp 195.133.60.194 2456 failure
probe udp 10.89.0.1 53 success
echo 'PASS: isolated routing, NAT return, DNS/public-TCP preservation, fail-closed including empty table, rollback'
