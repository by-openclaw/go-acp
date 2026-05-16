#!/bin/bash
# Run on the controller (Ubuntu LXC) — deploys + restarts dhs on all 3 LXCs.
set -e
HOSTS=("dhs-debian:10.100.0.102" "dhs-ubuntu:10.100.0.103" "dhs-rocky:10.100.0.104")

for entry in "${HOSTS[@]}"; do
  hostname="${entry%%:*}"
  ip="${entry##*:}"
  echo ">>> Deploying to ${hostname} (${ip})"
  ssh -o StrictHostKeyChecking=accept-new "root@${ip}" "pkill dhs 2>/dev/null; sleep 1; nohup /usr/local/bin/dhs producer nmos serve --config=/etc/dhs/node.json --bind=:18080 --advertise-host=${hostname}.local:18080 --mdns --api-ver=v1.3 > /var/log/dhs.log 2>&1 & sleep 2; pgrep -fa 'dhs producer' | head -1"
done

echo ""
echo ">>> avahi-browse _nmos-node._tcp on the LAN"
sleep 2
timeout 5 avahi-browse -t -p _nmos-node._tcp 2>/dev/null | grep '^=' | sort -u
