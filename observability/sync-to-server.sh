#!/usr/bin/env bash
set -euo pipefail

# Sync observability configs to Timeweb and reload Promtail + Grafana.
# Preserves server .env (passwords). Run from repo root or utils/observability.

HOST="${OBSERVABILITY_HOST:-Timeweb}"
REMOTE_DIR="${OBSERVABILITY_REMOTE_DIR:-/home/freedeeml/grafana}"
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

rsync -avz \
  --exclude '.env' \
  "${SCRIPT_DIR}/config/" "${HOST}:${REMOTE_DIR}/config/"

# Ensure Promtail can read Docker logs (do not overwrite server compose secrets).
ssh "${HOST}" "grep -q 'docker.sock' ${REMOTE_DIR}/docker-compose.yml || sed -i '/\\/var\\/log:\\/var\\/log/a\\      - /var/run/docker.sock:/var/run/docker.sock:ro' ${REMOTE_DIR}/docker-compose.yml"

ssh "${HOST}" "cd ${REMOTE_DIR} && docker compose up -d promtail grafana loki"

echo "Done. Dashboard: /grafana/d/myutils-api-logs/my-utils-api-logs"
echo "Explore: {app=\"my-utils-api\"}"
