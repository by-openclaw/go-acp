#!/usr/bin/env bash
# scripts/fixturize.sh — slim a pcap to specific frame numbers and freeze a tshark -V tree.
#
# Usage:
#   scripts/fixturize.sh <src.pcapng> <dst-dir> <frame-list>
#   Example: scripts/fixturize.sh bin/emberplus_glow_mtx_lua.pcapng \
#              tests/fixtures/protocol_types/emberplus/matrix 41
#
# Layout produced under <dst-dir>:
#   capture.pcapng   slimmed (only <frame-list>)
#   tshark.tree      frozen tshark -V output for the slimmed capture
#
# Requires: editcap + tshark on PATH (ships with Wireshark).

set -euo pipefail

SRC=${1:-}
DST=${2:-}
shift 2 || { echo "usage: $0 <src.pcapng> <dst-dir> <frame-list...>" >&2; exit 2; }

if [[ -z "$SRC" || -z "$DST" || $# -eq 0 ]]; then
    echo "usage: $0 <src.pcapng> <dst-dir> <frame-list...>" >&2
    exit 2
fi

mkdir -p "$DST"
OUT_PCAP="$DST/capture.pcapng"
OUT_TREE="$DST/tshark.tree"

editcap -r "$SRC" "$OUT_PCAP" "$@" >/dev/null

# Freeze the tree. Filter to emberplus/acp1/acp2 frames only; strip volatile
# fields (absolute timestamps) so the freeze stays stable across re-runs.
tshark -r "$OUT_PCAP" -V 2>/dev/null \
  | sed -E '
      s/Arrival Time:.*/Arrival Time: [frozen]/;
      s/UTC Arrival Time:.*/UTC Arrival Time: [frozen]/;
      s/Epoch Arrival Time:.*/Epoch Arrival Time: [frozen]/;
      s/Time shift for this packet:.*/Time shift for this packet: [frozen]/;
      s/Time delta from previous captured frame:.*/Time delta: [frozen]/;
      s/Time delta from previous displayed frame:.*/Time delta: [frozen]/;
      s/Time since reference or first frame:.*/Time since ref: [frozen]/;
    ' > "$OUT_TREE"

n=$(wc -l < "$OUT_TREE" | tr -d ' ')
size=$(wc -c < "$OUT_PCAP" | tr -d ' ')
echo "$DST: $size bytes pcap, $n lines tree, frames: $*"
