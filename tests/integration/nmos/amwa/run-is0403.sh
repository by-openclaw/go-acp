#!/usr/bin/env bash
# Run AMWA IS-04-03 (Peer-to-Peer Node) for one minor against the
# dhs-node container. Restart of dhs-node + mDNS settle is the caller's
# responsibility — this script just stages the JSON body, POSTs, and
# saves the result.
set -euo pipefail
ver="${1:-v1.3}"
out="/root/amwa-test/results/is04-03-${ver}.json"

cat > /root/amwa-test/req.json <<EOF
{"suite":"IS-04-03","host":["dhs-node"],"port":[18080],"version":["${ver}"],"output":"json"}
EOF

curl -s \
    -H 'Content-Type: application/json' \
    -d @/root/amwa-test/req.json \
    -o "$out" \
    http://127.0.0.1:5000/api

python3 /root/amwa-test/summarise.py "$out"
