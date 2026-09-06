# CLAUDE.md — SNMP (v1 / v2c / v3)

Atomic per-protocol context for the SNMP connector. Read the root `CLAUDE.md`
first for cross-cutting rules; this file holds the SNMP-specific scope.

> **STATUS: PARKED — do not start implementing without an explicit go from
> the codeowner.** What exists today is this file and `assets/`, so MIBs and
> vendor documentation have somewhere to land. No codec, no consumer, no
> provider. The park is a scheduling decision, not a design gap: the shape
> below is agreed, the peers exist, the go does not.

---

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

## MIB parsing happens OFFLINE, never in the binary

This is the load-bearing decision. Parsing MIB source is a compiler problem —
lexer, grammar, IMPORTS resolution across files — and dragging that into the
shipped binary is how a connector acquires a dependency tail.

Instead: a tool under `tools/` compiles the MIBs in `assets/mibs/` into
committed Go OID tables, and the connector reads the generated tables. The
compile is a development step whose output is reviewed in a PR; the runtime
knows only numbers and types.

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
is what our provider answers and emits to. The **IRD satellite receivers** are
the vendor-device oracle ADR-0025 Tier 3 wants; their MIBs belong in
`assets/mibs/`.

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
