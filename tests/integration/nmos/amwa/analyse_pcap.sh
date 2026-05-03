#!/usr/bin/env bash
# Walk a tshark pcap of the AMWA docker bridge and emit:
#   1. Every mDNS frame referencing `_nmos-node._tcp` (T_relative, src,
#      qry/resp, name, type, TTL).
#   2. Count of TXT records on `_nmos-node._tcp` per TTL bucket — the
#      goodbye signal is TTL=0; non-zero TTL is a normal announce or
#      cache-flush update.
set -euo pipefail
pcap="${1:-/tmp/mdns.pcap}"

echo "=== ALL _nmos-node._tcp frames ==="
tshark -r "$pcap" -Y 'dns.qry.name contains "_nmos-node" or dns.resp.name contains "_nmos-node"' \
       -T fields -e frame.time_relative -e ip.src -e dns.flags.response -e dns.qry.name -e dns.resp.name -e dns.resp.type -e dns.resp.ttl 2>/dev/null | head -80

echo
echo "=== TXT records on _nmos-node._tcp by TTL ==="
tshark -r "$pcap" -Y 'dns.flags.response==1 and dns.resp.type==16 and dns.resp.name contains "_nmos-node"' \
       -T fields -e frame.time_relative -e dns.resp.ttl 2>/dev/null

echo
echo "=== PTR records on _nmos-node._tcp.local by TTL ==="
tshark -r "$pcap" -Y 'dns.flags.response==1 and dns.resp.type==12 and dns.resp.name contains "_nmos-node"' \
       -T fields -e frame.time_relative -e dns.resp.ttl 2>/dev/null

echo
echo "=== goodbye check: any TTL=0 _nmos-node._tcp records? ==="
tshark -r "$pcap" -Y 'dns.flags.response==1 and dns.resp.ttl==0 and dns.resp.name contains "_nmos-node"' \
       -T fields -e frame.time_relative -e dns.resp.type -e dns.resp.ttl 2>/dev/null | head -20
