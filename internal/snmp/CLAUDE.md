# CLAUDE.md — SNMP (v1 / v2c / v3)

Atomic per-protocol context for the SNMP connector. Read the root `CLAUDE.md`
first for cross-cutting rules; this file holds the SNMP-specific scope.

> **STATUS: STARTED 2026-09-10 on the codeowner's go.** The park is lifted.
> What exists now, all at 100% with CI floors:
>
> | package | what it is |
> | --- | --- |
> | `codec` | the wire: v1/v2c/v3 envelopes, every PDU, the v1 Trap-PDU, the USM parameters blob |
> | `usm` | RFC 3414 + RFC 7860 + RFC 3826 — key derivation (published vectors asserted), digests, DES and AES, the authoritative engine |
> | `mib` | the OID vocabulary: the standard groups, and BOTH vendor roots |
> | `provider` | the agent: a served MIB, the request rules, trap emission in any version |
> | `consumer` | the manager: get / getnext / getbulk / set / walk, and the trap receiver |
>
> CLI: `dhs consumer snmp get|walk|set|trap-listen` and
> `dhs producer snmp serve|trap`.
>
> **Ours, not gosnmp.** The codeowner's call, and the reasoning is worth
> keeping: gosnmp and k-sone/snmpgo are both MANAGER libraries with no
> agent — no MIB tree, no GETNEXT to serve, no SET handling — so the
> agent half is ours whichever way the PDU goes; and neither exposes its
> marshalling as a codec, so "buy the PDU, build the agent" is not on the
> menu. See the measured dependency assessment further down, which
> stands.
>
> **Still open**: v3 POLLING (a manager is authoritative for nothing, so
> it must discover the agent's engine first — its own unit); InformRequest
> in both directions; and the offline MIB compiler under `tools/`, which
> is what turns the 232-file Snell set into committed Go tables.

---

## Identity numbers

- **IANA Private Enterprise Number: 54981**, BY-SYSTEMS SPRL (contact
  Boujraf Youssef), found 2026-09-10 in
  `https://www.iana.org/assignments/enterprise-numbers.txt`. It is
  `usm.Enterprise` and the root of `mib.DHS` (`1.3.6.1.4.1.54981`): every
  engine ID and the `sysObjectID` our agent answers are built from it.
  Arcs under it are ours to assign — in the generated DHS MIB, never ad
  hoc in code.
- **DHS-MIB** is that module: `internal/snmp/mib/DHS-MIB.mib`, written by
  `dhs producer snmp mib` (`internal/snmp/mibgen` from
  `provider.DHSModule`) and kept current by `TestDHSMIBIsCurrent`. Arcs:
  `54981.1` dhsProducts; `54981.1.1` dhsAgent — the agent's sysObjectID and
  its v1 trap enterprise, with notifications at `dhsAgent.0.n` so the RFC
  3584 mapping lands on a defined name; `54981.2` dhsMIB, objects at `.2.1`,
  conformance at `.2.2`. A published arc is never reused; a changed
  definition is a new REVISION in `provider/dhsmib.go`. Checked by our own
  compiler (round-trip test) and by net-snmp `snmptranslate` on the tools
  host (needs SNMPv2-SMI/TC/CONF beside it; Debian ships none).
- **IEEE MAC block: none.** OUIs are sold by the IEEE Registration
  Authority; software here uses locally administered addresses (`02:…`).
- **IEEE MAC address block: none.** BY-SYSTEMS appears in none of the five
  IEEE registries (MA-L, MA-M, MA-S, IAB, CID) as of 2026-09-10. Blocks
  come from the IEEE Registration Authority
  (`https://standards.ieee.org/products-programs/regauth/`) and, unlike a
  PEN, are paid. Software does not need one: an emulated device or a
  container can use locally-administered addresses (the second-lowest
  bit of the first octet set, e.g. `02:…`), which are free and can never
  collide with a vendor's burned-in address. A block is only needed for
  hardware that ships with its own MAC.

## Scope — both roles, like every other connector

| Role | What it does |
| --- | --- |
| **consumer** | poll an agent (GET / GETNEXT / GETBULK) and listen for traps + informs |
| **provider** | BE an agent: serve OUR OWN MIB, answer polls, emit our own traps |

The provider half is the same idea as the Ember+ and ACP1 providers serving
their trees — a device we present to someone else's manager, not a mock.

Versions **v1, v2c and v3** are all in scope. v1 traps are structurally
different from v2c notifications (enterprise / generic-trap / specific-trap
fields versus a plain varbind list), so they are two encoders, not one with a
flag.

## Where the wire work goes

SNMP is **ASN.1 BER over UDP**, and this repo already owns a BER codec at
`internal/emberplus/codec/ber`. So `codec/` here stays stdlib-only per
ADR-0006 like every other codec, and there is no argument for a dependency to
put bytes on the wire.

## Where the MIBs live

**`github.com/by-protocol/mib`** — its own repository, not this tree.
**It exists and is populated** (confirmed 2026-09-10; private, reachable
with the `gh` CLI from the desk — a plain `git clone` from the fleet has
no credentials). Layout:

```
ird/TT1260/   AccessControl · AlarmTrap · Base · CFG · G703 · IPstreamer
              RX-DUAL-GIGE-EX · TRAP · TT1260-MIB · Types · ip · rxDualGigE
ird/RX1290/   AlarmTrap · Base · CFG · RX-DUAL-GIGE-EX · RX1290-8VSB-MIB
              RX1290-MIB · Types · ip-RX1290 · rxDualGigE
ird/RX8200/   ~20 files, one per functional area
standard/     RFC-1212 · RFC-1215 · RFC1155-SMI · RFC1213-MIB
              SNMPv2-CONF · SNMPv2-SMI · SNMPv2-TC
```

`Base.mib` is the root every product MIB imports:
`mibEricssonTelevision ::= { enterprises 1773 }`,
`elementManagementMIB ::= { 1773 1 }`, then `general 1` / `content 2` /
`modules 3`. A product hangs off `modules` — `tt1260 ::= { modules 200 }`
— which is how `sysObjectID 1.3.6.1.4.1.1773.1.3.200` decodes.

**The Snell set is already in THIS tree**, not in `by-protocol/mib`: 232
files at `internal/snell-rollcall/assets/Protocol/SNMP/SNMP_MIBs`, with
vendor tools beside them under `assets/Tools/SNMP_Support_Tools`. They
arrived with the snell-rollcall connector's asset drop and are tracked.

So the compiler has to take **two** source roots, not one: a checkout of
`by-protocol/mib` for the IRDs, and that in-tree directory for the Snell
frames. Whether the Snell set should later move to `by-protocol/mib` for
ADR-0015's single-source rule is the codeowner's call; it is not a
blocker, and moving 232 tracked files is a separate change from
compiling them. Per
device under `ird/` (TT1260, RX1290, RX8200), plus `standard/` carrying the
six IETF base modules every vendor MIB imports and no vendor ships.

There are **TWO** vendor trees in this plant, not one — code that assumed a
single vendor root would work against the IRDs and quietly mis-address the
Snell frames:

- **Ericsson Television Limited** (formerly Tandberg Television), IANA
  enterprise **1773** — the IRDs.
- **Snell & Wilcox**, IANA enterprise **7995** — the RollCall frames.
  `SNELL-WILCOX-SMI.mib` puts `snellWilcoxProductReg` at `7995.1` and a
  shared generic sub-tree beside it. The frame at 10.6.255.113 answers
  `sysObjectID 1.3.6.1.4.1.7995.1.3.1` and serves 9 867 objects.

For the IRDs, the vendor is **Ericsson Television Limited**,
IANA enterprise **1773**. `ETV-Base-MIB` defines
`mibEricssonTelevision ::= { enterprises 1773 }` and
`elementManagementMIB ::= { mibEricssonTelevision 1 }`, which is exactly the
branch the testbed devices answer on.

The import graph is shallow and shared:

    ird/<device>/*.mib
      ├── ETV-Base-MIB   (Base.mib)    enterprises 1773
      ├── ETV-Types-TC   (Types.mib)   Uint8 / Uint16 / Uint32 / PIDNumber
      └── standard/      RFC1155-SMI · RFC-1212 · RFC-1215
                         SNMPv2-SMI · SNMPv2-TC · RFC1213-MIB

Note the SMIv1/SMIv2 mix: the product MIBs import RFC1155-SMI and RFC-1212
(SMIv1) while the trap MIB imports SNMPv2-SMI and SNMPv2-TC (SMIv2). A
compiler that only handles one of the two dialects will not get through this
set.

## MIB parsing happens OFFLINE, never in the binary

This is the load-bearing decision. Parsing MIB source is a compiler problem —
lexer, grammar, IMPORTS resolution across files — and dragging that into the
shipped binary is how a connector acquires a dependency tail.

Instead: `tools/mibc` (front end in `internal/snmp/smi`) compiles the MIBs
into one committed table, `internal/snmp/mib/tables.tsv.gz`, and the
connector reads that. The compile is a development step whose output is
reviewed in a PR; the runtime knows only numbers, names and types. The roots
are a checkout of `by-protocol/mib` (the IRDs and the IETF standard modules)
and the Snell set already in this repo:

    go run ./tools/mibc -out internal/snmp/mib/tables.tsv.gz \
        <by-protocol/mib>/ird <by-protocol/mib>/standard \
        internal/snell-rollcall/assets/Protocol/SNMP/SNMP_MIBs \
        internal/snmp/mib

(the last root is our own DHS-MIB, so the agent's names resolve too)

Findings (duplicate modules and which copy won, names that did not resolve)
are expected and listed with `-v`; they are not failures. Two rules decide
between copies: the newest LAST-UPDATED wins, and `-pin` overrides it where
the codeowner chose (IP-MIB is the RX1290's, the newest product). A vendor
defect only the vendor's own files can prove is corrected in
`tools/mibc/patches.txt`, each line with its evidence. Symbols no MIB anywhere
defines (the Snell QUASAR/IQDLY21 registrations) stay unresolved and
reported, never invented.

One OID can carry two names: the TT1260 and RX1290 report the same
sysObjectID and differ at 34 OIDs. Both rows are kept; `--mib MODULE` on the
consumer verbs (or `prefer` in `mib.Describe`) picks the device's.

Measured for the fork set the codeowner already made under
`github.com/by-protocol` (per ADR-0005's build-graph rule — what enters OUR
build, not what a go.mod lists):

- **gosnmp** — effectively runtime-dependency-free (BSD; its four requires
  look test-only). The candidate if we ever want one.
- **gosmi** — pulls `participle`, which pulls a 7-deep tail. Acceptable in
  `tools/` where nothing ships; not acceptable in the binary.
- **snmpquery** — adds a deprecated `pkg/errors` for little. Not worth taking.

## Test peers — no additional hardware needed

`docs/testbed.md` ("Cerebrum is a multi-protocol peer") has the authoritative
ports and communities. In short, Cerebrum closes the loop in both directions:
its **agent** is what our consumer polls, and its **manager + trap receiver**
is what our provider answers and emits to.

The **IRD satellite receivers** are the vendor-device oracle ADR-0025 Tier 3
wants, and they are **SNMP-only for our purposes**: they expose no REST API,
and the raw vendor protocol they also speak is deliberately out of scope (the
same call as Probel and ACP1/ACP2). Their HTTP management page is expected to
work and is useful for cross-checking a value by eye and for setting the
community and trap destination — it is not a connector target. Their MIBs
belong in `assets/mibs/`.

Do not restate ports here — read `docs/testbed.md`, per ADR-0015.

## Polling policy

Prefer notification over polling. Where polling is unavoidable, group OIDs
into per-interval buckets rather than walking everything on one timer — a
receiver fleet polled naively is a traffic source of its own.

## What NOT to do

- Do NOT start implementing before the codeowner's go. This connector is
  parked.
- Do NOT import `dhs/*` from `internal/snmp/codec/` — codec is stdlib-only
  (ADR-0006), and SNMP has no excuse: the BER primitives already exist here.
- Do NOT parse MIB source at runtime. Offline compiler, committed tables.
- Do NOT treat a v1 trap as a v2c notification with different fields — the
  PDUs differ in structure, not just in content.
- Do NOT assume port 161 for an agent. Cerebrum's own agent answers on 1161,
  and vendor devices vary.
