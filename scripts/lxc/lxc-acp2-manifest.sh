#!/bin/sh
set -e
chmod +x /usr/local/bin/dhs

# Set up cache layout the producer expects: .cache/dm/acp2/<DM>.json
mkdir -p /root/.cache/dm/acp2
cp -f "/var/lib/dhs/SHPRM1@5.3.5.json" "/root/.cache/dm/acp2/SHPRM1@5.3.5.json"
cp -f "/var/lib/dhs/CONVERT Hybrid@6.7.4.json" "/root/.cache/dm/acp2/CONVERT Hybrid@6.7.4.json"

cd /root
nohup /usr/local/bin/dhs producer acp2 serve \
  --manifest /var/lib/dhs/neuron-test.json \
  --cache-dir /root/.cache \
  --host 0.0.0.0 --port 2072 \
  > /var/log/dhs/acp2.log 2>&1 &
sleep 3
echo --- acp2 log ---
head -20 /var/log/dhs/acp2.log
echo --- port ---
ss -tunlp 2>/dev/null | grep 2072 || echo NOT_LISTENING
