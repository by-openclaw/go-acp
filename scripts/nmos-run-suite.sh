#!/usr/bin/env bash
# nmos-run-suite.sh — drive the AMWA NMOS Testing tool against dhs.
#
# Usage:
#   scripts/nmos-run-suite.sh <suite-dir>
#
# Where <suite-dir> is e.g. tests/integration/nmos/01-discovery and
# contains a docker-compose.yml + UserConfig.py customised for that
# phase's tests.
#
# The script:
#   1. Boots the compose stack on an isolated bridge.
#   2. Polls the AMWA tool's HTTP API until ready.
#   3. Drives the requested test suite via the API.
#   4. Pulls the JSON report into <suite-dir>/results/.
#   5. Tears down the stack (trap on exit — even on error).
#   6. Exits non-zero on any Fail OR Could-Not-Test result.
#
# Phase 1 step #1 ships only the harness skeleton — there is no real
# IS-* suite wired up yet. Run `scripts/nmos-run-suite.sh` against
# tests/integration/nmos/_template to smoke-test the docker compose
# bring-up; later phases add real per-suite directories.

set -euo pipefail

if [[ "${1:-}" == "" ]]; then
  echo "usage: $0 <suite-dir>" >&2
  exit 2
fi

SUITE_DIR="$1"
if [[ ! -f "$SUITE_DIR/docker-compose.yml" ]]; then
  echo "error: $SUITE_DIR/docker-compose.yml not found" >&2
  exit 2
fi

cd "$SUITE_DIR"

cleanup() {
  echo ">>> cleanup: docker compose down"
  docker compose down --remove-orphans --volumes >/dev/null 2>&1 || true
}
trap cleanup EXIT

echo ">>> boot:    docker compose up -d"
docker compose up -d

echo ">>> wait:    AMWA tool ready"
# Phase 1 step #1 just verifies the image starts. Later phases poll
# the AMWA tool's /api endpoint and POST a test selection.
for i in $(seq 1 30); do
  if docker compose ps nmos-testing | grep -q "running\|Up"; then
    break
  fi
  sleep 1
done

echo ">>> drive:   skeleton — no suite wired (Phase 1 step #1)"
# When a real suite lands the runner does:
#   curl -X POST http://localhost:5000/api -d '{...}'
#   curl    http://localhost:5000/api > results/report.json

mkdir -p results

echo ">>> assert:  no Fail / Could-Not-Test"
# Real assertion lands with the first concrete suite in Phase 1 #2.
echo "(Phase 1 step #1 — assertion is no-op; harness boot succeeded.)"

exit 0
