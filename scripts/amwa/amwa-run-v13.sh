#!/bin/bash
set -e
mkdir -p /root/amwa-test/results
cd /root/amwa-test
echo ">>> Running IS-04-01 v1.3 against dhs-node:18080 (this takes ~2-3 min)"
curl -s -X POST http://localhost:5000/api \
  -H 'Content-Type: application/json' \
  -d '{"suite":"IS-04-01","host":["dhs-node"],"port":[18080],"version":["v1.3"],"output":"json"}' \
  > results/is04-01-v1.3.json
echo "Result file size: $(wc -c < results/is04-01-v1.3.json) bytes"
echo ""
echo ">>> Summary"
python3 -c "
import json
with open('results/is04-01-v1.3.json') as f:
    r = json.load(f)
results = r.get('result', [])
counts = {}
for t in results:
    counts[t.get('state','?')] = counts.get(t.get('state','?'), 0) + 1
print('total:', len(results))
for k,v in sorted(counts.items()):
    print(f'  {k}: {v}')
print()
print('Fails:')
for t in results:
    if t.get('state') == 'Fail':
        print(' ', t.get('name'), '-', t.get('detail','')[:80])
"
