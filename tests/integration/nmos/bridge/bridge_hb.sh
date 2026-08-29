#!/bin/sh
while true; do
  for id in $(curl -s --max-time 5 http://127.0.0.1:8235/x-nmos/query/v1.3/nodes | python3 -c "import json,sys; [print(n['id']) for n in json.load(sys.stdin)]" 2>/dev/null); do
    curl -s -o /dev/null -X POST -d "" "http://10.6.250.5:8080/x-nmos/registration/v1.3/health/nodes/$id" --max-time 5
  done
  sleep 4
done
