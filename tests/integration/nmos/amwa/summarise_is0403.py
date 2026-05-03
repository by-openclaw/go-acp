#!/usr/bin/env python3
import json, sys
d = json.load(open(sys.argv[1]))
results = d.get("results", [])
counts = {}
for r in results:
    counts[r["state"]] = counts.get(r["state"], 0) + 1
print(f"suite={d.get('suite')} duration={d.get('duration')} totals={counts}")
for r in results:
    detail = (r.get("detail") or "")[:140]
    print(f"  {r['name']:<10} {r['state']:<14} - {detail}")
