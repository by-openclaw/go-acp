#!/bin/sh
for pid in $(pgrep -f 'dhs producer acp1'); do
  kill -9 "$pid" || true
done
sleep 1
nohup /usr/local/bin/dhs producer acp1 serve \
  --tree /opt/dhs/tree/empty_frame_32.json \
  --dm-library /opt/dhs/dmlib \
  --preload "0=axon/synapse/RRS18-1601/acp1,1=axon/synapse/2GS110-2728/acp1,2=axon/synapse/2HF110-4326/acp1,3=axon/synapse/GED130-2522/acp1,4=axon/synapse/SDB20-0806/acp1,5=axon/synapse/2GS110-2929/acp1,6=axon/synapse/GJA840-0101/acp1,7=axon/synapse/HPD130-2120/acp1,8=axon/synapse/HRB990-1010/acp1" \
  --host 0.0.0.0 --port 2071 \
  > /var/log/dhs/acp1.log 2>&1 &
sleep 3
echo --- acp1 log ---
head -20 /var/log/dhs/acp1.log
echo --- port ---
ss -tunlp 2>/dev/null | grep 2071 || echo NOT_LISTENING
