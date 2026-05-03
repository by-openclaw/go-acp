#!/usr/bin/env bash
# Cross-suite regression sweep: AMWA IS-04-01 + IS-04-02 + IS-04-03
# for every minor (v1.0 / v1.1 / v1.2 / v1.3). Used after each
# IS-04 conformance PR-pre-merge run to confirm no prior pass
# regressed. Output a one-line tally per (suite, minor).
#
# Topology requirement: dhs-node + dhs-registry + nmos-testing all
# live on the same docker bridge so mDNS multicast flows between
# them. IS-04-03 needs dhs-registry STOPPED so the Node enters
# Mode-D peer-to-peer; this script handles the start/stop cycles.
set -euo pipefail

cd /root/amwa-test

for ver in v1.0 v1.1 v1.2 v1.3 ; do
    echo
    echo "========================================================================"
    echo "  ${ver}"
    echo "========================================================================"

    echo "--- IS-04-01 (Node API) ${ver} — registry STOPPED, node uses AMWA Mock cascade ---"
    # IS-04-01 cascade-failover tests (test_15/test_16) probe whether
    # the Node fails over between AMWA Mock Registries on ports
    # 5101-5106. Our `dhs-registry` advertises pri=0 so the Node would
    # always prefer it over the higher-pri Mocks → contaminated test.
    # Stop dhs-registry for IS-04-01.
    docker compose stop dhs-registry >/dev/null 2>&1 || true
    DHS_API_VER="${ver}" docker compose up -d --force-recreate dhs >/dev/null
    sleep 8
    /root/amwa-test/run-is0401.sh "${ver}" || echo "  IS-04-01 ${ver}: HARNESS ERROR"

    echo "--- IS-04-02 (Registry) ${ver} — bringing dhs-registry UP ---"
    docker compose start dhs-registry >/dev/null
    sleep 8
    /root/amwa-test/run-is0402.sh "${ver}" | head -1 || echo "  IS-04-02 ${ver}: HARNESS ERROR"

    echo "--- IS-04-03 (P2P Node) ${ver} — stopping registry, node falls back to P2P ---"
    docker compose stop dhs-registry >/dev/null
    sleep 8
    /root/amwa-test/run-is0403.sh "${ver}" | head -1 || echo "  IS-04-03 ${ver}: HARNESS ERROR"
done

echo
echo "=== sweep complete; per-minor result JSONs in /root/amwa-test/results/ ==="
