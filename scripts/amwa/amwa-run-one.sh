#!/bin/bash
# usage: amwa-run-one.sh v1.0  (or v1.1, v1.2, v1.3)
set -e
VER="${1:?ver required}"
mkdir -p /root/amwa-test/results
cd /root/amwa-test
DHS_API_VER="$VER" docker compose up -d --build --force-recreate --no-deps dhs 2>&1 | tail -3
sleep 5
curl -s -X POST http://localhost:5000/api \
  -H 'Content-Type: application/json' \
  -d "{\"suite\":\"IS-04-01\",\"host\":[\"dhs-node\"],\"port\":[18080],\"version\":[\"$VER\"],\"output\":\"json\"}" \
  > "results/is04-01-${VER}.json"
echo "Result: $(wc -c < results/is04-01-${VER}.json) bytes"
bash /tmp/sum.sh "results/is04-01-${VER}.json"
