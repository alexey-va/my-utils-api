#!/usr/bin/env bash
set -euo pipefail

mode=plan
replace=false
swap_gib=2
ssh_port=22
trusted_ssh_ips=()

usage() {
  echo "Usage: $0 [--trusted-ssh-ip IPv4]... [--swap-gib N] [--ssh-port PORT] [--apply] [--replace]" >&2
}

valid_ipv4() {
  local value=$1 a b c d
  IFS=. read -r a b c d <<<"$value"
  for octet in "$a" "$b" "$c" "$d"; do
    [[ "$octet" =~ ^(0|[1-9][0-9]{0,2})$ ]] && ((10#$octet <= 255)) || return 1
  done
}

while (($#)); do
  case "$1" in
    --trusted-ssh-ip) trusted_ssh_ips+=("${2:-}"); shift 2 ;;
    --swap-gib) swap_gib=${2:-}; shift 2 ;;
    --ssh-port) ssh_port=${2:-}; shift 2 ;;
    --apply) mode=apply; shift ;;
    --replace) replace=true; shift ;;
    *) usage; exit 2 ;;
  esac
done

[[ $EUID -eq 0 ]] || { echo "Run as root" >&2; exit 1; }
[[ -r /etc/os-release ]] || { echo "Missing /etc/os-release" >&2; exit 1; }
# shellcheck source=/dev/null
source /etc/os-release
[[ ${ID:-} == ubuntu ]] || { echo "Only Ubuntu is supported" >&2; exit 1; }
[[ "$swap_gib" =~ ^[1-9][0-9]?$ ]]
((swap_gib <= 8))
[[ "$ssh_port" =~ ^[0-9]+$ ]] && ((ssh_port >= 1 && ssh_port <= 65535))
for value in "${trusted_ssh_ips[@]}"; do
  valid_ipv4 "$value" || { echo "Invalid trusted SSH IPv4: $value" >&2; exit 1; }
done
[[ -s /root/.ssh/authorized_keys ]] || {
  echo "Refusing SSH key-only hardening without /root/.ssh/authorized_keys" >&2
  exit 1
}

ssh_dropin=/etc/ssh/sshd_config.d/00-my-utils-access.conf
fail2ban_jail=/etc/fail2ban/jail.d/my-utils-sshd.conf
docker_config=/etc/docker/daemon.json
for target in "$ssh_dropin" "$fail2ban_jail"; do
  if [[ -e "$target" ]] && ! grep -Fq 'managed-by-my-utils' "$target" && [[ "$replace" != true ]]; then
    echo "Refusing unmanaged file without --replace: $target" >&2
    exit 1
  fi
done
if [[ -e "$docker_config" && ! -e /etc/docker/.managed-by-my-utils && "$replace" != true ]]; then
  echo "Refusing unmanaged file without --replace: $docker_config" >&2
  exit 1
fi

echo "Plan: add ${swap_gib} GiB swap, Docker Compose, bounded logs, fail2ban, and SSH key-only hardening"
if ((${#trusted_ssh_ips[@]})); then
  echo "Plan: fail2ban will trust ${#trusted_ssh_ips[@]} explicitly supplied SSH source address(es)"
fi
if [[ "$mode" != apply ]]; then
  echo "Plan only; no host changes were made"
  exit 0
fi

export DEBIAN_FRONTEND=noninteractive
apt-get update
apt-get install -y ca-certificates curl jq python3 wireguard-tools iptables nftables \
  docker.io docker-compose-v2 fail2ban

if ! swapon --show=NAME --noheadings | grep -Fxq /swapfile; then
  if [[ ! -e /swapfile ]]; then
    fallocate -l "${swap_gib}G" /swapfile
    chmod 600 /swapfile
    mkswap /swapfile >/dev/null
  fi
  swapon /swapfile
fi
grep -Eq '^/swapfile[[:space:]]+none[[:space:]]+swap[[:space:]]' /etc/fstab || \
  printf '/swapfile none swap sw 0 0\n' >>/etc/fstab
cat >/etc/sysctl.d/90-my-utils-memory.conf <<'EOF'
# managed-by-my-utils
vm.swappiness = 10
EOF
sysctl -q -p /etc/sysctl.d/90-my-utils-memory.conf

install -d -m 755 /etc/systemd/journald.conf.d /etc/docker /etc/ssh/sshd_config.d /etc/fail2ban/jail.d
cat >/etc/systemd/journald.conf.d/my-utils-limits.conf <<'EOF'
# managed-by-my-utils
[Journal]
SystemMaxUse=200M
RuntimeMaxUse=50M
EOF
cat >"$docker_config" <<'EOF'
{
  "log-driver": "local",
  "log-opts": {
    "max-size": "20m",
    "max-file": "3"
  }
}
EOF
# managed-by-my-utils: the marker intentionally lives outside strict JSON.
touch /etc/docker/.managed-by-my-utils

cat >"$ssh_dropin" <<EOF
# managed-by-my-utils
Port $ssh_port
LoginGraceTime 15
MaxAuthTries 10
MaxStartups 20:30:60
PerSourceMaxStartups 2
PermitRootLogin prohibit-password
PasswordAuthentication no
KbdInteractiveAuthentication no
EOF

ignoreip="127.0.0.1/8 ::1"
for value in "${trusted_ssh_ips[@]}"; do
  ignoreip+=" $value"
done
cat >"$fail2ban_jail" <<EOF
# managed-by-my-utils
[DEFAULT]
banaction = nftables-multiport
banaction_allports = nftables-allports
backend = systemd
ignoreip = $ignoreip
bantime = 1d
findtime = 10m
maxretry = 3
bantime.increment = true
bantime.factor = 2
bantime.maxtime = 1w

[sshd]
enabled = true
port = $ssh_port
mode = aggressive
EOF

sshd -t
systemctl daemon-reload
systemctl enable --now docker.service fail2ban.service
systemctl restart docker.service fail2ban.service
systemctl restart systemd-journald.service
systemctl reload ssh.service 2>/dev/null || systemctl reload sshd.service

docker compose version >/dev/null
fail2ban-client status sshd >/dev/null
effective_sshd=$(sshd -T)
grep -Fq 'passwordauthentication no' <<<"$effective_sshd"
grep -Fq 'permitrootlogin prohibit-password' <<<"$effective_sshd"
active_swap=$(swapon --show=NAME --noheadings)
grep -Fxq /swapfile <<<"$active_swap"
echo "Exit host bootstrap completed; SSH is key-only and fail2ban is active"
