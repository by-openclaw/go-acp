#!/bin/bash
set -e
docker rm -f nmos-testing 2>/dev/null || true
# The latest image's default CMD runs the Façade. Override to launch
# the Node tester (nmos-test.py) which serves the IS-04/IS-05/etc API
# we already drive via POST /api.
docker run -d --name nmos-testing --network host \
  --entrypoint python3 \
  amwa/nmos-testing:latest \
  /home/nmos-testing/nmos-test.py
sleep 6
docker logs nmos-testing 2>&1 | tail -8
echo "---"
curl -s -o /dev/null -w "GET /: %{http_code}\n" http://localhost:5000/
curl -s -X POST http://localhost:5000/api -H 'Content-Type: application/json' -d '{}' | head -c 200
