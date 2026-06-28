#!/usr/bin/env bash
set -euo pipefail

# Sync observability stack to Timeweb ~/grafana and reload containers.
# Source of truth in repo: my-utils-api/observability/
# Server layout: ~/grafana (user freedeeml → /home/freedeeml/grafana)
# Does NOT overwrite server .env or docker-compose secrets block.

HOST="${OBSERVABILITY_HOST:-Timeweb}"
REMOTE_DIR="${OBSERVABILITY_REMOTE_DIR:-~/grafana}"
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

echo "Sync config/ → ${HOST}:${REMOTE_DIR}/config/"
rsync -avz \
  --exclude '.env' \
  "${SCRIPT_DIR}/config/" "${HOST}:${REMOTE_DIR}/config/"

echo "Ensure Promtail can read Docker logs..."
ssh "${HOST}" "grep -q 'docker.sock' ${REMOTE_DIR}/docker-compose.yml || sed -i '/\\/var\\/log:\\/var\\/log/a\\      - /var/run/docker.sock:/var/run/docker.sock:ro' ${REMOTE_DIR}/docker-compose.yml"

echo "Ensure Tempo service exists in docker-compose.yml..."
ssh "${HOST}" "grep -q '^  tempo:' ${REMOTE_DIR}/docker-compose.yml" || ssh "${HOST}" "REMOTE_DIR='${REMOTE_DIR}' python3 - <<'PY'
import os
from pathlib import Path

path = Path(os.environ['REMOTE_DIR']).expanduser() / 'docker-compose.yml'
text = path.read_text()
if '  tempo:' in text:
    raise SystemExit(0)
block = '''
  tempo:
    image: grafana/tempo:2.7.2
    command: [\"-config.file=/etc/tempo/tempo-config.yml\"]
    volumes:
      - ./config/tempo:/etc/tempo
      - tempo_data:/var/tempo
    restart: unless-stopped
    network_mode: host
'''
if 'volumes:' not in text:
    text = text.rstrip() + block + '''
volumes:
  tempo_data:
'''
else:
    text = text.replace('volumes:', block + 'volumes:', 1)
    if 'tempo_data:' not in text:
        text = text.rstrip() + '  tempo_data:\n'
path.write_text(text)
PY"

echo "Reload stack (grafana, loki, promtail, prometheus, tempo)..."
ssh "${HOST}" "cd ${REMOTE_DIR} && docker compose up -d grafana loki promtail prometheus tempo"

echo "Apply Metal Discord notification template (optional)..."
if [[ -f "${SCRIPT_DIR}/scripts/apply-metal-discord-template.py" ]]; then
  rsync -avz "${SCRIPT_DIR}/scripts/" "${HOST}:${REMOTE_DIR}/scripts/"
  ssh "${HOST}" "set -a; [ -f ${REMOTE_DIR}/.env ] && source ${REMOTE_DIR}/.env; set +a; GRAFANA_URL=http://127.0.0.1:3500/grafana python3 ${REMOTE_DIR}/scripts/apply-metal-discord-template.py" || true
fi

echo ""
echo "Done."
echo "  Logs:    https://utils.alexeyav.ru/grafana/d/myutils-api-logs/my-utils-api-logs"
echo "  Metrics: https://utils.alexeyav.ru/grafana/d/myutils-api-metrics/my-utils-api-metrics"
echo "  Traces:  https://utils.alexeyav.ru/grafana/d/myutils-agent-traces/myutils-agent-traces"
echo "  Explore: {app=\"my-utils-api\"}"
