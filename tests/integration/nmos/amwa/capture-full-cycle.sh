#!/usr/bin/env bash
# Capture a full IS-04-01 cycle: announce → register-with-Mock →
# suspend (goodbye) → AMWA test_12 query. The AMWA tool brings up
# Mock Registries on ports 5101-5106 once the test starts, so we
# need tshark running BEFORE the test POST.
set -euo pipefail
iface="${1:-br-c5f9c46dd009}"
ver="${2:-v1.0}"

rm -f /tmp/mdns.pcap

# 90s window: 35s for AMWA suite + ~30s buffer
tshark -i "${iface}" -w /tmp/mdns.pcap -f 'udp port 5353' -a duration:90 >/tmp/tshark.log 2>&1 &
TSHARK_PID=$!
sleep 2

echo "force-recreate dhs (fresh DHS_API_VER=${ver}) ..."
cd /root/amwa-test
DHS_API_VER="${ver}" docker compose up -d --force-recreate dhs >/dev/null 2>&1
sleep 8  # mDNS settle

echo "POST AMWA suite (Mock registries fire up here) ..."
cat > /root/amwa-test/req.json <<EOF
{"suite":"IS-04-01","host":["dhs-node"],"port":[18080],"version":["${ver}"],"output":"json"}
EOF
curl -s -H 'Content-Type: application/json' -d @/root/amwa-test/req.json \
    http://127.0.0.1:5000/api > /root/amwa-test/results/is04-01-${ver}.json &

wait $TSHARK_PID || true
echo "capture done."
ls -la /tmp/mdns.pcap
