#!/bin/bash
set -e
docker rm -f nmos-testing dhs-node 2>/dev/null || true
docker network rm amwa-test_nmos 2>/dev/null || true

# Run AMWA Testing tool on host network so it can reach the LXC dhs Nodes.
docker run -d --name nmos-testing --network host \
  amwa/nmos-testing:master-902dd5d
sleep 10
echo --- container status ---
docker ps -f name=nmos-testing
echo --- logs ---
docker logs nmos-testing 2>&1 | tail -6
echo --- API probe ---
curl -s -o /dev/null -w "GET /: %{http_code}\n" http://localhost:5000/
curl -s -X POST http://localhost:5000/api \
  -H 'Content-Type: application/json' \
  -d '{}' | head -c 200
