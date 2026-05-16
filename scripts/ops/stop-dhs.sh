#!/bin/bash
# Hard-stop dhs on all 3 LXCs by walking /proc/*/exe to avoid matching
# our own ssh/pgrep commands.
HOSTS=("10.100.0.102" "10.100.0.103" "10.100.0.104")
KILL_CMD='for pid in $(find /proc -maxdepth 2 -name exe -lname "/usr/local/bin/dhs" 2>/dev/null | awk -F/ "{print \$3}"); do echo "killing $pid"; kill -9 $pid; done; sleep 1; remaining=$(find /proc -maxdepth 2 -name exe -lname "/usr/local/bin/dhs" 2>/dev/null | wc -l); echo "remaining: $remaining"'
for ip in "${HOSTS[@]}"; do
  echo ">>> $ip"
  ssh -o StrictHostKeyChecking=accept-new "root@$ip" "$KILL_CMD"
done
