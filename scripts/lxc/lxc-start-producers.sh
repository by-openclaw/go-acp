#!/bin/sh
set -e
for pid in $(find /proc -maxdepth 2 -name exe -lname "/usr/local/bin/dhs" 2>/dev/null | awk -F/ '{print $3}'); do
  kill -9 "$pid" || true
done
sleep 1
nohup /usr/local/bin/dhs producer acp1 serve --tree /var/lib/dhs/demo_frame.json  --host 0.0.0.0 --port 2071 > /var/log/dhs/acp1.log 2>&1 &
nohup /usr/local/bin/dhs producer acp2 serve --tree /var/lib/dhs/demo_device.json --host 0.0.0.0 --port 2072 > /var/log/dhs/acp2.log 2>&1 &
sleep 2
echo --- listeners ---
ss -tunlp 2>&1 | grep -E '2071|2072' || true
echo --- pids ---
pgrep -af dhs | grep -v avahi | grep -v lxc-start || true
