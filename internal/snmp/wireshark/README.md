# dhs_snmp.lua — the SNMP dissector

Decodes BER itself rather than delegating to Wireshark's built-in
`snmp`, so the `Protocol | Info` shape matches every other dhs
connector (root `CLAUDE.md`, "Wireshark dissectors"). Field abbrevs
and the proto name carry the `dhs_` prefix so nothing clashes with the
built-in `snmp.*`.

Covers v1, v2c and v3 (USM), every PDU tag including the v1 Trap-PDU
and the v2 notification kinds, every value tag this repo encodes, and
the RFC 3416 exception markers.

## Verified against a real agent

The committed fixtures are frames from the ATEME Kyrion DR5000
(`10.6.255.114`, SNMP v1, firmware 1.3.1.1), captured while the real
`dhs consumer snmp` verbs drove it on 2026-09-24:

```bash
cd internal/snmp
tshark -X lua_script:wireshark/dhs_snmp.lua -r testdata/fixtures/dr5000-snmp.pcapng
```

All 36 frames decode, request ids pair up, and the two refusals the
device sent read as refusals:

```
1 10.6.250.101 → 10.6.255.114 SNMPv1 community="public" GetRequest req=1472231541 1.3.6.1.2.1.1.1.0=NULL (+5 more)
2 10.6.255.114 → 10.6.250.101 SNMPv1 community="public" Response   req=1472231541 1.3.6.1.2.1.1.1.0="Linux dp2 2.6.37 …" (+5 more)
4 10.6.255.114 → 10.6.250.101 SNMPv1 community="public" Response   req=2019679109 err=noSuchName@1 1.3.6.1.4.1.27338.5.5.3.3.1=NULL
```

Per PDU kind, `testdata/protocol_types/<kind>/wire.pcapng` holds one
capture each — the same frames `internal/snmp/codec` replays offline.

## Known limit

The Info column prints numeric OIDs, not MIB names: the Lua dissector
cannot read the compiled table in `internal/snmp/mib`, and shipping a
second copy of 75 000 names beside it would be a second source of
truth (ADR-0015). Wireshark's own MIB loader can supply names if a
site wants them — point it at the modules in `github.com/by-protocol/mib`.

## Install

| OS | Personal Lua plugin dir |
|---|---|
| Windows | `%APPDATA%\Wireshark\plugins\` |
| macOS / Linux | `~/.local/lib/wireshark/plugins/` |

Or deploy to the fleet with `ansible/playbooks/deploy-dissector.yml`.
