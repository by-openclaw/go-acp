# UserConfig.py — AMWA NMOS Testing tool config for the IS-04-01
# Node API suite running against dhs.
#
# Authoritative knobs documented at
# https://github.com/AMWA-TV/nmos-testing/blob/master/UserConfig.example.py.

# DNS-SD discovery is unicast inside the isolated docker bridge. The
# host LAN never sees mDNS announcements from this run.
DNS_SD_MODE = 'unicast'

# AMWA tool listens on :5000 (suite runner) + :5001 (Mock Reg/Node).
# Both stay inside the isolated bridge — never published to host.

# Target dhs Node — service name resolves on the bridge's internal
# DNS to the dhs-under-test container.
TARGETS = [
    {
        "name": "dhs-under-test",
        "host": "dhs-under-test",
        "port": 8080,
        "protocol": "http",
        "version": "v1.3",
    }
]

# Conformance run config — non-interactive, JSON output.
NON_INTERACTIVE = True
ENABLE_TLS = False
ENABLE_AUTH = False
