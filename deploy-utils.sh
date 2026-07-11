#!/usr/bin/env bash
set -euo pipefail

cd "$(dirname "$0")"

export MYUTILS_CORS_ALLOWED_ORIGINS="${MYUTILS_CORS_ALLOWED_ORIGINS:-http://utils.alexeyav.ru,https://utils.alexeyav.ru,http://127.0.0.1:13082}"

if [[ -f /etc/mcfine/load-secrets.sh ]]; then
  # shellcheck source=/etc/mcfine/load-secrets.sh
  source /etc/mcfine/load-secrets.sh myutilsapi
elif [[ -f /home/freedeeml/.secrets/myutilsapi.env ]]; then
  set -a
  # shellcheck source=/dev/null
  source /home/freedeeml/.secrets/myutilsapi.env
  set +a
else
  echo "No secrets source found (Infisical or ~/.secrets/myutilsapi.env)" >&2
  exit 1
fi

export TELEGRAM_ALLOWED_USER_IDS="${TELEGRAM_ALLOWED_USER_IDS:-303179278}"
export MYUTILS_TELEGRAM_ENABLED="${MYUTILS_TELEGRAM_ENABLED:-true}"
export OPENROUTER_PROXY_ENABLED=true
export OPENROUTER_PROXY_HOST=185.242.106.81
export OPENROUTER_PROXY_PORT=8888
export MYUTILS_TEMPORAL_ENABLED=true
export TEMPORAL_TARGET=temporal:7233

export DOCKER_BUILDKIT=1
export COMPOSE_DOCKER_CLI_BUILD=1
export GIT_COMMIT="$(git rev-parse HEAD 2>/dev/null || echo local)"

COMPOSE="docker compose -f docker-compose.yml -f docker-compose.utils.yml"

for port in 15432 16379; do
  if ! timeout 1 bash -c "cat < /dev/null > /dev/tcp/127.0.0.1/${port}" 2>/dev/null; then
    echo "Postgres/Redis not listening on 127.0.0.1:${port}" >&2
    exit 1
  fi
done

${COMPOSE} up -d temporal-postgresql temporal temporal-ui
${COMPOSE} up -d --build --force-recreate api
