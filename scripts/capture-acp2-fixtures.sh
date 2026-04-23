#!/usr/bin/env bash
# scripts/capture-acp2-fixtures.sh — drive dhs producer + consumer on
# loopback while tshark records, producing a single pcapng that covers
# every ACP2 per-type fixture path (object types, the four functions,
# error stats reachable from the CLI, and an announce).
#
# Usage:
#   scripts/capture-acp2-fixtures.sh [out-pcap]
#
# Defaults:
#   out-pcap  bin/acp2_fixtures.pcapng
#   port      2072 (override with PORT env var)
#   tree      internal/acp2/testdata/protocol_types/fixture_tree.json
#   tshark    /c/Program Files/Wireshark/tshark.exe (override TSHARK)
#   iface     \Device\NPF_Loopback (override IFACE)
#
# The script prints every frame number. Fixturisation (per-type slim +
# tshark.tree freeze) runs from the Makefile `fixtures-acp2` target,
# which calls scripts/fixturize.sh once per frame.

set -euo pipefail

OUT="${1:-bin/acp2_fixtures.pcapng}"
PORT="${PORT:-2072}"
TREE="${TREE:-internal/acp2/testdata/protocol_types/fixture_tree.json}"
TSHARK="${TSHARK:-/c/Program Files/Wireshark/tshark.exe}"
DHS="${DHS:-./bin/dhs.exe}"
IFACE="${IFACE:-\\Device\\NPF_Loopback}"

mkdir -p "$(dirname "$OUT")"

if [[ ! -x "$DHS" ]]; then
    echo "error: $DHS not found; run 'make build' first" >&2
    exit 1
fi
if [[ ! -x "$TSHARK" ]]; then
    echo "error: tshark not found at $TSHARK" >&2
    exit 1
fi

cleanup() {
    if [[ -n "${PROD_PID:-}" ]]; then kill -9 "$PROD_PID" 2>/dev/null || true; fi
    if [[ -n "${WATCH_PID:-}" ]]; then kill -9 "$WATCH_PID" 2>/dev/null || true; fi
    if [[ -n "${CAP_PID:-}"  ]]; then kill -2 "$CAP_PID"  2>/dev/null || true; fi
    wait 2>/dev/null || true
}
trap cleanup EXIT

# Kill any orphaned dhs that might still be holding port 2072.
taskkill.exe //F //IM dhs.exe >/dev/null 2>&1 || true
sleep 1

echo ">>> starting tshark capture -> $OUT"
"$TSHARK" -i "$IFACE" -f "tcp port $PORT" -w "$OUT" -q >/dev/null 2>&1 &
CAP_PID=$!
sleep 2

echo ">>> starting acp2 producer on port $PORT"
"$DHS" producer acp2 serve --tree "$TREE" --port "$PORT" --log-level info >/tmp/acp2-producer.log 2>&1 &
PROD_PID=$!
sleep 2

echo ">>> watch slot 1 (keeps a session with EnableProtocolEvents alive for announce)"
"$DHS" consumer acp2 watch 127.0.0.1 --port "$PORT" --slot 1 >/tmp/acp2-watch.log 2>&1 &
WATCH_PID=$!
sleep 1

echo ">>> walk slot 0 (get_version + get_object on rack controller)"
"$DHS" consumer acp2 walk 127.0.0.1 --port "$PORT" --slot 0 || true

echo ">>> walk slot 1 (get_object on every fixture leaf — string/enum/number/ipv4/node)"
"$DHS" consumer acp2 walk 127.0.0.1 --port "$PORT" --slot 1 || true

echo ">>> get slot 1 GainS32 by label (get_property, numeric leaf)"
"$DHS" consumer acp2 get 127.0.0.1 --port "$PORT" --slot 1 --label GainS32 || true

echo ">>> set slot 1 GainS32 to 3 (set_property + announce via watch session)"
"$DHS" consumer acp2 set 127.0.0.1 --port "$PORT" --slot 1 --label GainS32 --value 3 || true
sleep 1

echo ">>> set slot 1 Mode to 1 (set_property on enum, RW)"
"$DHS" consumer acp2 set 127.0.0.1 --port "$PORT" --slot 1 --label Mode --value 1 || true

echo ">>> set slot 1 UserLabel (set_property on string, RW) — slot 1 has two UserLabel rows (IDENTITY + ROOT); --id 3 pins the IDENTITY one"
"$DHS" consumer acp2 set 127.0.0.1 --port "$PORT" --slot 1 --id 3 --value Updated || true

echo ">>> set slot 1 Gateway (RO target -> error stat=4 no_access)"
"$DHS" consumer acp2 set 127.0.0.1 --port "$PORT" --slot 1 --label Gateway --value 10.0.0.1 || true

echo ">>> set slot 1 Mode=99 (enum out-of-range -> error stat=5 invalid_value)"
"$DHS" consumer acp2 set 127.0.0.1 --port "$PORT" --slot 1 --label Mode --value 99 || true

echo ">>> diag slot 99 (raw probes -> several error frames incl. stat=0 / stat=1 / stat=3)"
"$DHS" consumer acp2 diag 127.0.0.1 --port "$PORT" --slot 99 || true

echo ">>> diag slot 1 (covers stat=3 invalid_pid on unknown pid probe)"
"$DHS" consumer acp2 diag 127.0.0.1 --port "$PORT" --slot 1 || true

sleep 1
echo ">>> stopping watch + producer + tshark"
kill -9 "$WATCH_PID" 2>/dev/null || true
WATCH_PID=""
kill -9 "$PROD_PID" 2>/dev/null || true
PROD_PID=""
sleep 1
kill -2 "$CAP_PID" 2>/dev/null || true
CAP_PID=""
wait 2>/dev/null || true
sleep 2

if [[ ! -s "$OUT" ]]; then
    echo "error: capture produced no bytes" >&2
    exit 1
fi

echo ">>> capture complete: $(du -b "$OUT" | awk '{print $1}') bytes"
echo ">>> frame summary:"
"$TSHARK" -r "$OUT" -Y "tcp.port==$PORT" 2>/dev/null | head -200
