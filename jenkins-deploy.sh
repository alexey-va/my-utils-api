#!/usr/bin/env bash
set -euo pipefail

cd "${WORKSPACE}"

# Public URL of the frontend (browser Origin header). Comma-separated for several hosts.
export MYUTILS_CORS_ALLOWED_ORIGINS="${MYUTILS_CORS_ALLOWED_ORIGINS:-http://utils.alexeyav.ru,https://utils.alexeyav.ru,http://127.0.0.1:13082}"

# Telegram bot — TELEGRAM_BOT_TOKEN + OPENROUTER_API_KEY from Jenkins credentials.
export TELEGRAM_BOT_TOKEN="${TELEGRAM_BOT_TOKEN:-}"
export TELEGRAM_ALLOWED_USER_IDS="${TELEGRAM_ALLOWED_USER_IDS:-303179278}"
export MYUTILS_TELEGRAM_ENABLED="${MYUTILS_TELEGRAM_ENABLED:-true}"
export OPENROUTER_API_KEY="${OPENROUTER_API_KEY:-}"
export OPENROUTER_PROXY_ENABLED=true
export OPENROUTER_PROXY_HOST=185.242.106.81
export OPENROUTER_PROXY_PORT=8888

export DOCKER_BUILDKIT=1
export COMPOSE_DOCKER_CLI_BUILD=1

COMPOSE="docker compose -f docker-compose.yml -f docker-compose.jenkins.yml"

export GIT_COMMIT="$(git rev-parse HEAD)"

${COMPOSE} up -d postgres redis temporal-postgresql temporal temporal-ui
${COMPOSE} up -d --build --force-recreate api
