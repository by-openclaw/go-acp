#!/bin/sh
for pid in $(pgrep -f 'dhs producer acp2'); do
  kill -9 "$pid" || true
done
sleep 1
pgrep -af 'dhs producer acp2' || echo no-acp2-running
