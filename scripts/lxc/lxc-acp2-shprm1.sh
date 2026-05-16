#!/bin/sh
for pid in $(pgrep -f 'dhs producer acp2'); do
  kill -9 "$pid" || true
done
sleep 1
nohup /usr/local/bin/dhs producer acp2 serve --tree /var/lib/dhs/shprm1.json --host 0.0.0.0 --port 2072 > /var/log/dhs/acp2.log 2>&1 &
sleep 2
echo --- producer log head ---
head -10 /var/log/dhs/acp2.log
echo --- port ---
ss -tlnp 2>/dev/null | grep 2072 || echo NOT_LISTENING
