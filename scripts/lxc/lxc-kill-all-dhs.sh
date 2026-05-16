#!/bin/sh
for pid in $(pgrep -f /usr/local/bin/dhs); do
  kill -9 "$pid" || true
done
sleep 1
pgrep -f /usr/local/bin/dhs || echo no-dhs-running
