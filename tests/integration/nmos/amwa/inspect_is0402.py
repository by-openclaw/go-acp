#!/usr/bin/env python3
"""Per-version IS-04-02 result inspection: list non-Pass/non-Disabled/non-N/A
entries, plus test_01's specific state, for the round-22 audit."""
import json, sys
for path in sys.argv[1:]:
    d = json.load(open(path))["results"]
    print(f"\n=== {path} ===")
    for r in d:
        st = r["state"]
        if st in ("Fail", "Could Not Test", "Not Implemented", "Warning") or r["name"] == "test_01":
            detail = (r.get("detail") or "")[:140]
            print(f"  {r['name']:<26} {st:<18} - {detail}")
