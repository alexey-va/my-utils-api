#!/usr/bin/env bash
set -euo pipefail

GH="${GH:-/opt/homebrew/bin/gh}"
OWNER="${GITHUB_OWNER:-alexey-va}"

if ! "${GH}" auth status >/dev/null 2>&1; then
  echo "Сначала: ${GH} auth login"
  exit 1
fi

push_repo() {
  local dir="$1"
  local name="$2"
  local desc="$3"

  echo "=== ${name} ==="
  cd "${dir}"

  if "${GH}" repo view "${OWNER}/${name}" >/dev/null 2>&1; then
    echo "Репозиторий ${OWNER}/${name} уже есть"
  else
    "${GH}" repo create "${name}" --public --description "${desc}"
  fi

  if ! git remote get-url origin >/dev/null 2>&1; then
    git remote add origin "https://github.com/${OWNER}/${name}.git"
  fi

  git push -u origin HEAD
  echo "OK: https://github.com/${OWNER}/${name}"
}

ROOT="$(cd "$(dirname "$0")" && pwd)"

push_repo "${ROOT}/my-utils-api" "my-utils-api" "Spring Boot API for my-utils"
push_repo "${ROOT}/my-utils" "my-utils" "Refine + Vite frontend for my-utils"

echo "Готово."
