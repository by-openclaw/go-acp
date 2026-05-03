#!/usr/bin/env bash
# Run AMWA IS-04-01 (Node API) for one minor. dhs-node must already
# be running with DHS_API_VER=$ver and dhs-registry must be UP so the
# Node enters Mode-A registered mode for IS-04-01 cascade timing
# tests (test_05/15/16 cascade).
set -euo pipefail
ver="${1:-v1.3}"
out="/root/amwa-test/results/is04-01-${ver}.json"

cat > /root/amwa-test/req.json <<EOF
{"suite":"IS-04-01","host":["dhs-node"],"port":[18080],"version":["${ver}"],"output":"json"}
EOF

curl -s \
    -H 'Content-Type: application/json' \
    -d @/root/amwa-test/req.json \
    -o "$out" \
    http://127.0.0.1:5000/api

python3 -c "
import json, sys
d = json.load(open('$out'))
results = d.get('results', [])
counts = {}
for r in results:
    counts[r['state']] = counts.get(r['state'], 0) + 1
print(f\"suite={d.get('suite')} duration={d.get('duration')} totals={counts}\")
"
