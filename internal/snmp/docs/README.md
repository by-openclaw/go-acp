# SNMP — v1 / v2c / v3

| Role | Doc | Status |
|---|---|---|
| Operator runbook | [runbook.md](runbook.md) | ✓ shipping |
| Consumer (the manager) | [consumer.md](consumer.md) | ✓ shipping — v1/v2c polling, traps in every version |
| Provider (the agent) | [provider.md](provider.md) | ✓ shipping — serves DHS-MIB, answers GET/GETNEXT/GETBULK/SET, emits traps |
| Wire format + scope | [../CLAUDE.md](../CLAUDE.md) | authoritative for this protocol |
| Dissector notes | [../wireshark/README.md](../wireshark/README.md) | how the Lua was verified against the IRD |

## What this connector is

Two halves of one protocol, the same split every connector here has:

- **consumer** — a manager. It polls an agent and receives what the
  agent sends unasked. `dhs consumer snmp get|walk|set|trap-listen`,
  and the same session behind the neutral verbs (`info`, `tree`,
  `export`, `watch`, `alarm`) with no SNMP in the command line.
- **provider** — an agent. It serves *our* MIB under BY-SYSTEMS' IANA
  enterprise number 54981, answers someone else's manager, and emits
  our own notifications. `dhs producer snmp serve|trap|mib`.

## Packages

| Package | What it is |
|---|---|
| [`codec/`](../codec) | the wire: v1/v2c/v3 envelopes, every PDU, the v1 Trap-PDU, the USM parameters blob. stdlib-only (ADR-0006) |
| [`usm/`](../usm) | RFC 3414 + 7860 + 3826 — key derivation against the published vectors, digests, DES and AES |
| [`mib/`](../mib) | the OID vocabulary, compiled offline into `tables.tsv.gz`; never parsed at runtime |
| [`smi/`](../smi) + [`tools/mibc`](../../../tools/mibc) | the offline MIB compiler that produces that table |
| [`consumer/`](../consumer) | the manager session, and `plugin.go` — the same session as a neutral `consumer.Protocol` |
| [`provider/`](../provider) | the agent: a served MIB, the request rules, traps in any version |
| [`mibgen/`](../mibgen) | writes DHS-MIB from the served tree, so the module and the agent cannot drift |
| [`monitor/`](../monitor) | the ADR-0030 device monitor profile |

## MIBs

MIB source lives in **`github.com/by-protocol/mib`**, not in this tree
(the Snell set is the exception — it arrived with the snell-rollcall
asset drop). A device that serves its own MIB is the best source of
all: the ATEME Kyrion DR5000 publishes
`http://<ip>/ATEME-DR5000-MIB.smi`, which by construction matches the
firmware in front of you. See [`docs/testbed.md`](../../../docs/testbed.md).

Compiling is a development step, reviewed in a PR:

```
go run ./tools/mibc -out internal/snmp/mib/tables.tsv.gz \
    <by-protocol/mib>/ird <by-protocol/mib>/standard \
    internal/snell-rollcall/assets/Protocol/SNMP/SNMP_MIBs \
    internal/snmp/mib
```

## v3 is the default

The agent answers v3 out of the box, as user `dhs` — `serve` needs no
v3 flags to speak it, and `--v3-user=""` is how you turn it off. With
no passphrase that user is noAuthNoPriv, which identifies a manager
without protecting the exchange; `--v3-auth`/`--v3-priv` raise it, and
the agent then refuses anything less for that user.

v1 and v2c stay on beside it, because half the devices in this lab
predate v3 and a connector that refused them would simply not monitor
them. On the manager side the preference runs the other way: a
configured `SNMP_V3_USER` makes the connector try v3 *first*, since v1
and v2c put their password in clear in every datagram.

Complete as of 2026-09-24: v3 polling (discovery, all three security
levels, re-discovery when an agent reboots mid-session), v3
notifications both ways, and InformRequest both ways.
