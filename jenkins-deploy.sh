#!/usr/bin/env bash
set -euo pipefail

cd "${WORKSPACE}"

# Public URL of the frontend (browser Origin header). Comma-separated for several hosts.
export MYUTILS_CORS_ALLOWED_ORIGINS="${MYUTILS_CORS_ALLOWED_ORIGINS:-http://utils.alexeyav.ru,https://utils.alexeyav.ru,http://127.0.0.1:13082}"

COMPOSE="docker compose -f docker-compose.yml -f docker-compose.jenkins.yml"

${COMPOSE} up -d postgres redis
${COMPOSE} up -d --build api
