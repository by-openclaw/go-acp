#!/bin/sh
set -e
chmod +x /usr/local/bin/dhs

for pid in $(pgrep -f 'dhs producer acp1'); do
  kill -9 "$pid" || true
done
sleep 1

cd /root
nohup /usr/local/bin/dhs producer acp1 serve \
  --manifest /var/lib/dhs/synapse-test.json \
  --cache-dir /root/.cache \
  --host 0.0.0.0 --port 2071 \
  > /var/log/dhs/acp1.log 2>&1 &
sleep 3
echo --- acp1 log ---
head -15 /var/log/dhs/acp1.log
echo --- port ---
ss -tunlp 2>/dev/null | grep 2071 || echo NOT_LISTENING
