#!/bin/bash
echo ">>> Cerebrum: dhs nodes registered"
curl -s http://10.100.0.5:8080/x-nmos/query/v1.3/nodes | grep -oE '"label":"[^"]*"' | sort -u

echo ""
echo ">>> avahi-browse _nmos-node._tcp from this host"
timeout 4 avahi-browse -r -t -p _nmos-node._tcp 2>/dev/null | grep '^=' | awk -F';' '{print $4 " @ " $8 ":" $9}'

echo ""
echo ">>> Direct curl /x-nmos/node/v1.3/self on each peer"
for host in dhs-debian.local dhs-ubuntu.local dhs-rocky.local; do
  label=$(curl -sf --max-time 2 "http://${host}:18080/x-nmos/node/v1.3/self" 2>/dev/null | grep -oE '"label":"[^"]*"' | head -1)
  echo "  ${host}: ${label:-UNREACHABLE}"
done
