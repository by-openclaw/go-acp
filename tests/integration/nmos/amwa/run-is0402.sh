#!/usr/bin/env bash
# Run AMWA IS-04-02 (Registration + Query APIs) for one minor against
# the dhs-registry container. Registry advertises every codec it has
# compiled in; the version under test is the api["version"] passed by
# the harness.
set -euo pipefail
ver="${1:-v1.3}"
out="/root/amwa-test/results/is04-02-${ver}.json"

cat > /root/amwa-test/req.json <<EOF
{"suite":"IS-04-02","host":["dhs-registry","dhs-registry"],"port":[8235,8235],"version":["${ver}","${ver}"],"output":"json"}
EOF

curl -s \
    -H 'Content-Type: application/json' \
    -d @/root/amwa-test/req.json \
    -o "$out" \
    http://127.0.0.1:5000/api

python3 /root/amwa-test/summarise.py "$out"
