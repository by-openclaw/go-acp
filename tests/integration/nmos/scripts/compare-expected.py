#!/usr/bin/env python3
"""compare-expected.py — gate AMWA NMOS Testing JSON results against
an expected.json baseline.

Usage:
    compare-expected.py --result results/is-04-01-<RUN>.json \\
        --expected is-04-01/expected.json

expected.json is a list of objects:
    [
      {"id": "test_01", "outcome": "Pass"},
      {"id": "test_02", "outcome": "Could Not Test"},
      ...
    ]

The script exits 0 iff every expected entry's outcome matches the
result. Any mismatch (test moved from Pass to Fail, or new test
introduced that wasn't in expected) is reported and exits non-zero.
"""

import argparse
import json
import sys
from pathlib import Path


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("--result", required=True, type=Path)
    parser.add_argument("--expected", required=True, type=Path)
    args = parser.parse_args()

    try:
        result = json.loads(args.result.read_text(encoding="utf-8"))
    except (OSError, json.JSONDecodeError) as e:
        print(f"compare-expected: cannot read result {args.result}: {e}",
              file=sys.stderr)
        return 1

    try:
        expected = json.loads(args.expected.read_text(encoding="utf-8"))
    except (OSError, json.JSONDecodeError) as e:
        print(f"compare-expected: cannot read expected {args.expected}: {e}",
              file=sys.stderr)
        return 1

    # AMWA tool result format (v1.0.x): {"name":"...", "results":[
    #   {"name":"test_01","state":{"name":"Pass"}, ...},
    #   ...
    # ]}
    if "results" not in result:
        print("compare-expected: result has no 'results' key — expected"
              " AMWA NMOS Testing JSON format", file=sys.stderr)
        return 1

    by_id = {entry.get("name"): entry for entry in result["results"]
             if isinstance(entry, dict) and "name" in entry}

    failed = []
    for want in expected:
        tid = want.get("id")
        want_outcome = want.get("outcome")
        got = by_id.get(tid)
        if got is None:
            failed.append((tid, "MISSING in result", want_outcome))
            continue
        got_outcome = (got.get("state") or {}).get("name", "?")
        if got_outcome != want_outcome:
            failed.append((tid, got_outcome, want_outcome))

    if not failed:
        print(f"compare-expected: all {len(expected)} expected outcomes match")
        return 0

    print("compare-expected: outcome regressions:", file=sys.stderr)
    for tid, got, want in failed:
        print(f"  {tid}: got={got!r} want={want!r}", file=sys.stderr)
    return 1


if __name__ == "__main__":
    sys.exit(main())
