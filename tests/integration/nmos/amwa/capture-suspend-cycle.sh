#!/usr/bin/env bash
# Capture a complete announce → register → suspend → goodbye cycle
# without involving the AMWA tool. Setup:
#
#   1. Bring up dhs-registry alone (advertises _nmos-register._tcp).
#   2. Start tshark on the docker bridge (capture window: 30 s).
#   3. Start dhs-node — it announces _nmos-node._tcp (with ver_*),
#      then RegistryWatcher discovers dhs-registry, registers,
#      suspend fires → goodbye should hit the wire.
#   4. Stop tshark, dump packet summary.
set -euo pipefail
ver="${1:-v1.0}"

cd /root/amwa-test
docker compose down 2>&1 | tail -2
docker compose up -d nmos-testing dhs-registry 2>&1 | tail -3
sleep 6  # dhs-registry mDNS settle

# Derive the bridge name from the freshly-created docker network so
# we don't have to pass it in (it changes on every `compose up`).
netid=$(docker network inspect amwa-test_nmos --format '{{.Id}}' | head -c 12)
iface="br-${netid}"
echo "bridge: ${iface}"
sleep 2

rm -f /tmp/mdns.pcap
echo "starting tshark on ${iface} for 30s..."
tshark -i "${iface}" -w /tmp/mdns.pcap -f 'udp port 5353' -a duration:30 >/tmp/tshark.log 2>&1 &
TSHARK_PID=$!
sleep 2

echo "starting dhs-node DHS_API_VER=${ver} ..."
DHS_API_VER="${ver}" docker compose up -d --force-recreate dhs >/dev/null 2>&1

wait $TSHARK_PID || true
echo "capture done."
ls -la /tmp/mdns.pcap
capinfos /tmp/mdns.pcap 2>&1 | head -5
