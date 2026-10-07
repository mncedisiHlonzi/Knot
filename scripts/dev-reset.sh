#!/usr/bin/env bash
#
# Knot — DESTRUCTIVE reset of local development infrastructure.
#
# Stops PostgreSQL and Redis and DELETES their named volumes. Every local
# database and cache entry is lost. There is no undo.
#
# Usage:
#   scripts/dev-reset.sh          # asks for confirmation
#   scripts/dev-reset.sh --yes    # no prompt (CI / scripted use)
#   scripts/dev-reset.sh --help

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"
COMPOSE_FILE="${REPO_ROOT}/infrastructure/docker/docker-compose.yml"
ENV_FILE="${REPO_ROOT}/.env"

log() { printf '[dev-reset] %s\n' "$1"; }
fail() {
  printf '[dev-reset] ERROR: %s\n' "$1" >&2
  exit 1
}

usage() {
  cat <<'EOF'
Usage: scripts/dev-reset.sh [--yes]

  --yes, -y   Skip the confirmation prompt.
  --help, -h  Show this help and exit.

Deletes the local PostgreSQL and Redis named volumes. All local data is lost.
EOF
}

compose() {
  if [ -f "${ENV_FILE}" ]; then
    docker compose -f "${COMPOSE_FILE}" --env-file "${ENV_FILE}" "$@"
  else
    docker compose -f "${COMPOSE_FILE}" "$@"
  fi
}

assume_yes=0
for arg in "$@"; do
  case "${arg}" in
    -y | --yes) assume_yes=1 ;;
    -h | --help)
      usage
      exit 0
      ;;
    *) fail "Unknown argument: ${arg} (try --help)" ;;
  esac
done

command -v docker >/dev/null 2>&1 ||
  fail "Docker CLI not found. Install Docker Desktop and ensure 'docker' is on your PATH."
[ -f "${COMPOSE_FILE}" ] || fail "Compose file not found: ${COMPOSE_FILE}"

if [ "${assume_yes}" -ne 1 ]; then
  printf 'This will DELETE all local Knot PostgreSQL and Redis data (named volumes).\n'
  printf 'There is no undo. Type "yes" to continue: '
  if ! read -r reply; then
    fail "No input available. Re-run with --yes to confirm non-interactively."
  fi
  if [ "${reply}" != "yes" ]; then
    log "Aborted. Nothing was deleted."
    exit 0
  fi
fi

log "Stopping containers and deleting named volumes..."
compose down --volumes --remove-orphans

log "Reset complete. Local data volumes were deleted."
log "Start again with:  scripts/dev-up.sh"
