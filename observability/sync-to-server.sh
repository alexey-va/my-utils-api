#!/usr/bin/env bash
set -euo pipefail

# Sync observability stack to utils (utils.alexeyav.ru) and reload containers.
# Source of truth in repo: my-utils-api/observability/
# Server layout: ~/grafana (user freedeeml → /home/freedeeml/grafana)
# Does NOT overwrite server .env or docker-compose secrets block.

HOST="${OBSERVABILITY_HOST:-utils}"
REMOTE_DIR="${OBSERVABILITY_REMOTE_DIR:-~/grafana}"
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

python3 "${SCRIPT_DIR}/scripts/apply-metal-discord-template.py" --check
python3 "${SCRIPT_DIR}/scripts/validate-vpn-alerts.py" --check

echo "Sync config/ → ${HOST}:${REMOTE_DIR}/config/"
rsync -avz \
  --exclude '.env' \
  --exclude 'grafana/provisioning/dashboards/' \
  --exclude 'grafana/dashboards/' \
  "${SCRIPT_DIR}/config/" "${HOST}:${REMOTE_DIR}/config/"

# Grafana's dashboard provider is shared with RusCrafting and is maintained
# on the server. Upload only this repository's My Utils dashboard directory.
echo "Verify shared Grafana dashboard providers..."
ssh "${HOST}" "test -f ${REMOTE_DIR}/config/grafana/provisioning/dashboards/dashboards.yml && grep -q '/dashboards/json/my-utils' ${REMOTE_DIR}/config/grafana/provisioning/dashboards/dashboards.yml && grep -q '/dashboards/json/ruscrafting' ${REMOTE_DIR}/config/grafana/provisioning/dashboards/dashboards.yml"
echo "Sync My Utils dashboards → ${HOST}:${REMOTE_DIR}/config/grafana/provisioning/dashboards/json/my-utils/"
rsync -avz \
  "${SCRIPT_DIR}/config/grafana/provisioning/dashboards/my-utils/" \
  "${HOST}:${REMOTE_DIR}/config/grafana/provisioning/dashboards/json/my-utils/"

echo "Ensure Promtail can read Docker logs..."
ssh "${HOST}" "grep -q 'docker.sock' ${REMOTE_DIR}/docker-compose.yml || sed -i '/\\/var\\/log:\\/var\\/log/a\\      - /var/run/docker.sock:/var/run/docker.sock:ro' ${REMOTE_DIR}/docker-compose.yml"

echo "Ensure Promtail positions survive container restarts..."
ssh "${HOST}" "mkdir -p ${REMOTE_DIR}/data/promtail"
ssh "${HOST}" "grep -q './data/promtail:/var/lib/promtail' ${REMOTE_DIR}/docker-compose.yml || sed -i '/\\.\\/config\\/promtail:\\/etc\\/promtail/a\\      - ./data/promtail:/var/lib/promtail' ${REMOTE_DIR}/docker-compose.yml"

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

echo "Reload stack (grafana, loki, promtail, prometheus, node-exporter, blackbox-exporter, tempo)..."
ssh "${HOST}" "cd ${REMOTE_DIR} && docker compose up -d grafana loki promtail prometheus node-exporter blackbox-exporter tempo"

echo "Apply Metal Discord template..."
if [[ -f "${SCRIPT_DIR}/scripts/apply-metal-discord-template.py" ]]; then
  rsync -avz "${SCRIPT_DIR}/scripts/" "${HOST}:${REMOTE_DIR}/scripts/"
  ssh "${HOST}" "bash -s" <<EOF || true
set -a
[ -f ${REMOTE_DIR}/.env ] && source ${REMOTE_DIR}/.env
export GRAFANA_USER="\${GRAFANA_USER:-freedeeml}"
export GRAFANA_PASSWORD="\${GRAFANA_PASSWORD:-\$(grep GF_SECURITY_ADMIN_PASSWORD ${REMOTE_DIR}/docker-compose.yml | head -1 | sed 's/.*=//')}"
export GRAFANA_URL=http://127.0.0.1:3500/grafana
set +a
for i in 1 2 3 4 5 6 7 8 9 10; do
  curl -sf "\${GRAFANA_URL%/}/api/health" >/dev/null && break
  sleep 2
done
python3 ${REMOTE_DIR}/scripts/apply-metal-discord-template.py
EOF
fi

echo "Retired alert cleanup is a separate explicit operation:"
echo "  python3 ${SCRIPT_DIR}/scripts/retire-ruscrafting-alerts.py --confirm"

echo "Ensure UFW allows Docker → Tempo OTLP on host..."
if [[ -f "${SCRIPT_DIR}/scripts/setup-utils-firewall.sh" ]]; then
  ssh "${HOST}" "bash -s" < "${SCRIPT_DIR}/scripts/setup-utils-firewall.sh" || true
fi

echo ""
echo "Done."
echo "  Metal Status: https://utils.alexeyav.ru/grafana/d/rYdddlPWk/metal-status"
echo "  VPN Health:   https://utils.alexeyav.ru/grafana/d/myutils-vpn-health/vpn-health"
echo "  Logs:        https://utils.alexeyav.ru/grafana/d/myutils-api-logs/my-utils-api-logs"
echo "  Metrics:     https://utils.alexeyav.ru/grafana/d/myutils-api-metrics/my-utils-api-metrics"
echo "  Visitors:    https://utils.alexeyav.ru/grafana/d/workout-visitors/workout-visitors"
echo "  Explore: {app=\"my-utils-api\"}"
