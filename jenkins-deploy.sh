#!/usr/bin/env bash
set -euo pipefail

cd "${WORKSPACE}"

COMPOSE="docker compose -f docker-compose.yml -f docker-compose.jenkins.yml"

${COMPOSE} up -d postgres redis
${COMPOSE} up -d --build api
