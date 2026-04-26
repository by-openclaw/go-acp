# AMWA NMOS Testing UserConfig — template for dhs integration phases.
#
# Each phase copies this file into its own
# tests/integration/nmos/<NN-suite>/UserConfig.py and tweaks the keys
# below. Values here are conservative defaults shared across phases;
# they assume the tool runs alongside dhs inside the isolated-bridge
# docker-compose network defined in docker-compose.yml.
#
# Keys reference the AMWA testing tool's documented configuration:
#   https://github.com/AMWA-TV/nmos-testing#configuration

# Stay unicast: the harness lives on an isolated docker bridge, so
# mDNS multicast (UDP 5353) cannot reach hosts outside the bridge.
# Tools and SUT communicate via unicast DNS-SD against a stub
# resolver, OR — when the suite asks for direct registration — via
# explicit URLs configured per-phase.
DNS_SD_MODE = "unicast"

# When DNS_SD_MODE='unicast', the tool needs a resolver it controls.
# Phase 1 step #1 ships the harness skeleton only and does not
# exercise IS-04; later phases override these.
DNS_SD_BROWSE_TIMEOUT = 5
DNS_SD_ADVERT_TIMEOUT = 5

# Where the tool reaches dhs SUT(s). Each phase sets:
#   QUERY_API_HOST  — IS-04 Query API URL (Phase 1 #4)
#   NODE_API_HOST   — IS-04 Node API URL  (Phase 1 #3)
QUERY_API_HOST = ""
NODE_API_HOST = ""

# Persist test reports for archival; the suite runner copies these
# under tests/integration/nmos/<phase>/results/ once tests finish.
DEFAULT_OUTPUT_PATH = "/results"
