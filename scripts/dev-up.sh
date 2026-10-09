#!/usr/bin/env bash
#
# Knot — start local development infrastructure (PostgreSQL + Redis + MinIO).
#
# Brings up the containers defined in infrastructure/docker/docker-compose.yml.
# The mobile app and the Go backend run on the host, so there is no application
# container to start. This script never touches mobile or Go source.
#
# Usage:
#   scripts/dev-up.sh
#
# Safe to re-run: `docker compose up -d` is idempotent.

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"
COMPOSE_FILE="${REPO_ROOT}/infrastructure/docker/docker-compose.yml"
ENV_FILE="${REPO_ROOT}/.env"

log() { printf '[dev-up] %s\n' "$1"; }
fail() {
  printf '[dev-up] ERROR: %s\n' "$1" >&2
  exit 1
}

# Wraps `docker compose`, adding --env-file only when a local .env exists.
compose() {
  if [ -f "${ENV_FILE}" ]; then
    docker compose -f "${COMPOSE_FILE}" --env-file "${ENV_FILE}" "$@"
  else
    docker compose -f "${COMPOSE_FILE}" "$@"
  fi
}

command -v docker >/dev/null 2>&1 ||
  fail "Docker CLI not found. Install Docker Desktop and ensure 'docker' is on your PATH."
docker compose version >/dev/null 2>&1 ||
  fail "The 'docker compose' plugin is unavailable. Update Docker Desktop."
[ -f "${COMPOSE_FILE}" ] || fail "Compose file not found: ${COMPOSE_FILE}"

if [ -f "${ENV_FILE}" ]; then
  log "Using environment file: ${ENV_FILE}"
else
  log "No .env found — falling back to the defaults in docker-compose.yml."
  log "To customise, run: cp .env.example .env"
fi

log "Starting PostgreSQL, Redis, and MinIO..."
compose up -d

log "Current status:"
compose ps

log "Local infrastructure is up. Connection details come from .env"
log "(defaults: postgres on localhost:5433, redis on localhost:6379, minio on localhost:9000,"
log " minio console on localhost:9001)."
log "Stop it with:  scripts/dev-down.sh"
