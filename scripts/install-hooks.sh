#!/usr/bin/env bash
#
# Knot — install the repository's git hooks.
#
# Points git at the tracked `.githooks/` directory (via core.hooksPath) so the
# pre-commit duplicate-`package` hygiene sweep runs on every commit. Run this
# once after cloning.
#
# Usage:
#   scripts/install-hooks.sh

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"

cd "${REPO_ROOT}"

git config core.hooksPath .githooks
chmod +x .githooks/pre-commit

printf '[install-hooks] core.hooksPath set to .githooks\n'
printf '[install-hooks] pre-commit hook installed and executable.\n'
