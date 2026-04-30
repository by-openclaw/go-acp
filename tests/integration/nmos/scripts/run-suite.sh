#!/usr/bin/env bash
#
# run-suite.sh — invoke one AMWA NMOS Testing suite against dhs.
#
# Usage: run-suite.sh <suite-id>
#
# <suite-id> matches a directory under tests/integration/nmos/ —
# e.g. is-04-01, is-05-01, is-07-01.
#
# The script:
#   1. Spins up the per-suite docker-compose bridge (dhs + AMWA tool).
#   2. Polls the AMWA tool's /api endpoint until it's ready.
#   3. Runs the suite via the AMWA tool's non-interactive runner.
#   4. Scrapes the JSON result + writes it to results/<suite>-<RUN_ID>.json.
#   5. Compares against expected.json — exits 0 if every expected test
#      is Pass; non-zero otherwise.
#   6. Tears down the bridge (trap on EXIT covers success / failure / ^C).

set -euo pipefail

if [ $# -lt 1 ]; then
    echo "usage: $0 <suite-id>" >&2
    exit 64
fi

SUITE="$1"
SUITE_DIR="$(cd "$(dirname "$0")/.." && pwd)/${SUITE}"
RESULTS_DIR="$(cd "$(dirname "$0")/.." && pwd)/results"

if [ ! -d "$SUITE_DIR" ]; then
    echo "no suite dir at $SUITE_DIR" >&2
    exit 65
fi

mkdir -p "$RESULTS_DIR"

RUN_ID="$(date +%s)-$$"
PROJECT="dhs_nmos_${SUITE//-/_}_${RUN_ID}"
RESULT_FILE="$RESULTS_DIR/${SUITE}-${RUN_ID}.json"

export PHASE="$SUITE"
export RUN_ID

cleanup() {
    echo "tearing down compose project $PROJECT"
    docker compose -p "$PROJECT" -f "$SUITE_DIR/docker-compose.yml" down --volumes --remove-orphans >/dev/null 2>&1 || true
}
trap cleanup EXIT

echo "==> bringing up $PROJECT"
docker compose -p "$PROJECT" -f "$SUITE_DIR/docker-compose.yml" up -d

echo "==> waiting for AMWA NMOS Testing tool to become ready"
for i in $(seq 1 60); do
    if docker compose -p "$PROJECT" -f "$SUITE_DIR/docker-compose.yml" \
        exec -T amwa-nmos-testing curl -sf http://localhost:5000/ >/dev/null 2>&1; then
        echo "    ready"
        break
    fi
    sleep 2
done

echo "==> running suite $SUITE"
RAW_RESULT="$(docker compose -p "$PROJECT" -f "$SUITE_DIR/docker-compose.yml" \
    exec -T amwa-nmos-testing python3 /home/nmos-testing/run-suite.py \
    --suite "$SUITE" --target dhs-under-test --json 2>&1 || true)"

echo "$RAW_RESULT" > "$RESULT_FILE"
echo "==> result written to $RESULT_FILE"

if [ -f "$SUITE_DIR/expected.json" ]; then
    echo "==> comparing against expected.json"
    if python3 "$(dirname "$0")/compare-expected.py" \
        --result "$RESULT_FILE" --expected "$SUITE_DIR/expected.json"; then
        echo "==> $SUITE PASS"
        exit 0
    else
        echo "==> $SUITE FAIL — see $RESULT_FILE" >&2
        exit 1
    fi
fi

echo "==> no expected.json — raw result only"
exit 0
