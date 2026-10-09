#!/usr/bin/env bash
#
# Knot — stop local development infrastructure (PostgreSQL + Redis + MinIO).
#
# Stops and removes the containers. Named volumes are PRESERVED, so database and
# cache contents survive. Use scripts/dev-reset.sh to wipe them.
#
# Usage:
#   scripts/dev-down.sh

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"
COMPOSE_FILE="${REPO_ROOT}/infrastructure/docker/docker-compose.yml"
ENV_FILE="${REPO_ROOT}/.env"

log() { printf '[dev-down] %s\n' "$1"; }
fail() {
  printf '[dev-down] ERROR: %s\n' "$1" >&2
  exit 1
}

compose() {
  if [ -f "${ENV_FILE}" ]; then
    docker compose -f "${COMPOSE_FILE}" --env-file "${ENV_FILE}" "$@"
  else
    docker compose -f "${COMPOSE_FILE}" "$@"
  fi
}

command -v docker >/dev/null 2>&1 ||
  fail "Docker CLI not found. Install Docker Desktop and ensure 'docker' is on your PATH."
[ -f "${COMPOSE_FILE}" ] || fail "Compose file not found: ${COMPOSE_FILE}"

log "Stopping PostgreSQL, Redis, and MinIO..."
compose down

log "Stopped. Named volumes were preserved (data is still on disk)."
log "Wipe all local data with:  scripts/dev-reset.sh"
