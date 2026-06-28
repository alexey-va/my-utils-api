#!/usr/bin/env bash
# Allow Docker containers on utils host to reach Tempo OTLP on the host network.
# Required when Tempo uses network_mode: host and my-utils-api runs in docker-compose.
set -euo pipefail

if ! command -v ufw >/dev/null; then
  echo "ufw not installed, skipping"
  exit 0
fi

sudo ufw allow from 172.16.0.0/12 to any port 4318 proto tcp comment 'OTLP Tempo HTTP from docker'
sudo ufw allow from 172.16.0.0/12 to any port 4317 proto tcp comment 'OTLP Tempo gRPC from docker'

echo "UFW rules for Tempo OTLP applied."
