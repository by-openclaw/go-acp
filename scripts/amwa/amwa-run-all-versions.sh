#!/bin/bash
set -e
mkdir -p /root/amwa-test/results
cd /root/amwa-test
for ver in v1.0 v1.1 v1.2; do
  echo "==========================="
  echo ">>> RESET dhs container for clean api_ver=$ver"
  echo "==========================="
  # Recreate dhs container with the new api_ver via env override.
  DHS_API_VER=$ver docker compose up -d --force-recreate --no-deps dhs 2>&1 | tail -3
  sleep 4
  echo ">>> Run IS-04-01 $ver"
  curl -s -X POST http://localhost:5000/api \
    -H 'Content-Type: application/json' \
    -d "{\"suite\":\"IS-04-01\",\"host\":[\"dhs-node\"],\"port\":[18080],\"version\":[\"$ver\"],\"output\":\"json\"}" \
    > results/is04-01-$ver.json
  echo "Result file: $(wc -c < results/is04-01-$ver.json) bytes"
  bash /tmp/sum.sh results/is04-01-$ver.json
  echo ""
done
