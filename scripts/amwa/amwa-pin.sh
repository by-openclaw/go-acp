#!/bin/bash
set -e
cd /root/amwa-test
docker compose down 2>&1 | tail -2 || true
sed -i 's|amwa/nmos-testing:latest|amwa/nmos-testing:master-902dd5d|' docker-compose.yml
grep image docker-compose.yml
docker compose up -d --build 2>&1 | tail -8
sleep 12
echo --- nmos-testing logs ---
docker logs nmos-testing 2>&1 | tail -8
echo --- dhs logs ---
docker logs dhs-node 2>&1 | tail -5
echo --- API probe ---
curl -s -o /dev/null -w "GET 5000: %{http_code}\n" http://localhost:5000/
curl -s -X POST http://localhost:5000/api -H 'Content-Type: application/json' -d '{}' | head -c 250
