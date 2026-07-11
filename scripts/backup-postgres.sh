#!/usr/bin/env bash
# Daily backup of host Postgres myutils DB (port 15432).
set -euo pipefail

BACKUP_DIR="${BACKUP_DIR:-$HOME/backups/postgres}"
RETENTION_DAYS="${RETENTION_DAYS:-14}"
STAMP="$(date +%Y%m%d-%H%M)"
OUT="${BACKUP_DIR}/myutils-${STAMP}.sql.gz"

mkdir -p "${BACKUP_DIR}"

# shellcheck source=/dev/null
source "${HOME}/load-secrets.sh" myutilsapi 2>/dev/null || true

export PGPASSWORD="${POSTGRES_PASSWORD:-myutils}"
pg_dump -h 127.0.0.1 -p 15432 -U "${POSTGRES_USER:-myutils}" "${POSTGRES_DB:-myutils}" | gzip >"${OUT}"
find "${BACKUP_DIR}" -name 'myutils-*.sql.gz' -mtime +"${RETENTION_DAYS}" -delete
echo "backup ok: ${OUT} ($(du -h "${OUT}" | awk '{print $1}'))"
