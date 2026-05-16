#!/bin/bash
set -e
docker rm -f nmos-testing dhs-node 2>/dev/null || true
cd /root/amwa-test
docker compose down --remove-orphans 2>&1 | tail -3 || true
chmod +x dhs
docker compose up -d --build 2>&1 | tail -8
sleep 15
echo --- containers ---
docker ps -f label=com.docker.compose.project=amwa-test
echo --- nmos-testing logs ---
docker logs nmos-testing 2>&1 | tail -8
echo --- dhs-node logs ---
docker logs dhs-node 2>&1 | tail -5
echo --- API probe ---
curl -s -o /dev/null -w "GET 5000: %{http_code}\n" http://localhost:5000/
echo "POST 5000 /api with empty body:"
curl -s -X POST http://localhost:5000/api -H 'Content-Type: application/json' -d '{}' | head -c 200
