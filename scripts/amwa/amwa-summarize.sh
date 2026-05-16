#!/bin/bash
F="${1:-/root/amwa-test/results/is04-01-v1.3.json}"
python3 << EOF
import json
from collections import Counter
d = json.load(open('$F'))
results = d.get('results', [])
print(f"=== {d['suite']} v{d['endpoints'][0]['version']} on {d['endpoints'][0]['host']}:{d['endpoints'][0]['port']} ===")
print(f"duration: {d['duration']:.1f}s   total: {len(results)}")
states = Counter(t['state'] for t in results)
for s, n in sorted(states.items(), key=lambda x: -x[1]):
    print(f"  {s}: {n}")
print()
print("=== Fails ===")
for t in results:
    if t['state'] == 'Fail':
        print(f"  {t['name']}: {t['detail'][:120]}")
print()
print("=== Warnings ===")
for t in results:
    if t['state'] == 'Warning':
        print(f"  {t['name']}: {t['detail'][:120]}")
EOF
