# Cross-connector audit — the producer needs a third-party oracle

Date: 2026-09-07 · Author: by-rune (agent) · Status: **proposal**
Trigger: the RollCall producer validation in
[`internal/snell-rollcall/docs/audit-2026-09-07.md`](../../../internal/snell-rollcall/docs/audit-2026-09-07.md) §7.5C-E
Binding refs: ADR-0025 (definition of done) · ADR-0021 (wire trace) ·
`docs/logging.md` (syslog contract) · root `CLAUDE.md` (dissector rule)

## This tier was already proposed — and lost

**Not a new finding.** [`repo-review-2026-06-07.md`](repo-review-2026-06-07.md)
§7 proposed a **four**-tier taxonomy and asked to "extend ADR-0025 with this":

| Tier (June proposal) | Oracle |
|---|---|
| 1 Unit | spec bytes |
| 2 Consumer integration | vendor emulator + real device (never our provider) |
| **3 Provider integration** | **our provider serves a real controller — e.g. Lawo VSM** |
| 4 Loopback regression | our consumer ↔ our provider, **only after 2+3 pass** |

ADR-0025's Revisions trailer records that on 2026-06-07 the taxonomy was added.
But it landed as **three** tiers — Unit / Smoke / Integration. **Tier 3,
provider-against-a-real-controller, did not survive the fold.** Loopback was
kept and correctly demoted; the tier that would have caught its weakness was
dropped.

The RollCall result below is independent evidence that the dropped tier was the
load-bearing one.

## The gap as it stands today

ADR-0025's test taxonomy names the oracle for the **consumer** side only:

> **Integration** | wire behaviour | **vendor emulator + real device — never our own provider**

Deliverable 2 is a strict-to-spec **producer**, but nothing in the ADR requires
a **third-party client** to drive it. In practice a producer is validated by our
own consumer over loopback — which the same ADR already demotes to
"regression only" — plus Ansible plays we also wrote.

**That is a closed loop.** If our consumer and producer share a
misunderstanding, both agree and every test is green.

## Evidence that this is not theoretical

A RollCall producer was built to our documented model and passed every test
written against it. Pointed at **RollCall Control Panel 4.12.48**, it failed
three times, each a defect our own tooling could not see:

| Defect | Why our tests missed it |
| --- | --- |
| Replies carried `cSrc.rIndex = 0` instead of the session index | our consumer never checked the source index |
| Back-channel pushes addressed with **our** index, not the client's sticky index — client answered `SP_INVSESS`, **every push silently discarded** | our consumer accepted any push regardless of addressing |
| `SP_DISPDATA` pushed onto a `SV_MAP` session | our consumer subscribed on control sessions only, so never saw it |

The second is the dangerous one: the server logged "pushed to 1 session" and
looked healthy while **nothing was delivered**. A closed loop cannot find that
class of bug, by construction.

## Proposed amendment to ADR-0025

Add a **client-side oracle** row to the taxonomy, symmetric with the existing
device-side rule:

| Tier | Proves | Oracle |
|---|---|---|
| **Integration (consumer)** | our consumer against a real wire | vendor emulator + real device — never our own provider |
| **Integration (producer)** | our producer against a real controller | **a third-party client driving our producer — never our own consumer** |

Acceptance for deliverable 2 becomes: *a vendor or third-party client
discovers, renders and drives our producer end to end*, evidenced by a captured
session. Loopback stays regression-only on both sides.

## Most connectors already have the tool

The client-side oracle is largely sitting unused in `assets/`:

| Connector | Client-side oracle | State |
| --- | --- | --- |
| snell-rollcall | RollCall Control Panel 4.12.48 | **used — found 3 defects** |
| emberplus | `EmberPlusView-1.6.2` (viewer/client) | present, unused against our provider |
| probel-sw08p | `Commie.exe` (Probel controller) | present, unused against our provider |
| tsl | `TSL IP Emulator`, miranda agent | present, role to confirm |
| amwa | AMWA NMOS Testing tool (Docker, `dhs-tools`) | used for the Node API — closest existing match |
| acp1 | Synapse Simulator (device side) | client-side tool to identify |
| **acp2** | **Lawo vsmStudio** — `internal/acp2/CLAUDE.md` already names "Lawo VSM Axon Neuron driver" as the viewer under test | controller exists, not yet driven against our provider |
| **probel-sw02p / sw08p** | **vsmStudio** + `Commie.exe`; `verb-tests.md` already specifies "vs Commie/TS emulator + VSM" | specified, not yet run against our provider |
| **osc** | **EVS Cerebrum — OSC over UDP *and* TCP** (owner, 2026-09-07) | controller exists; **not recorded in any doc** — see below |
| **cerebrum-nb** | **EVS Cerebrum** (`10.6.250.5`, NB `:40009`) | see below — provider is currently N/A |

**Correction to an earlier draft of this table:** none of these four is
oracle-less. **Lawo vsmStudio** and **EVS Cerebrum** are real controllers
already on the fabric and already cited across the repo — ADR-0014 lists
"Commie / VSM / live device captures" as real-peer evidence, and
`internal/emberplus/CLAUDE.md` documents VSM dialect quirks in detail. The tool
is not missing; it has simply never been pointed at our **provider**.

### Cerebrum speaks OSC too — and no tracked doc says so

The owner reports (2026-09-07) that **Cerebrum drives OSC over both UDP and
TCP**. That is not recorded anywhere, and it leaves three tracked files wrong
by omission:

| File | Gap |
| --- | --- |
| `docs/testbed.md` §"Cerebrum is a multi-protocol peer, not only the NB API" | lists syslog and SNMP; **omits OSC** |
| `docs/testbed.md` producer port plan | has **no OSC rows at all**, though `internal/osc/CLAUDE.md` fixes UDP 8000, TCP-LP 8000, TCP-SLIP 8001 |
| `internal/osc/CLAUDE.md` real-world peers | names QLab and others; **omits Cerebrum** |

This matters beyond bookkeeping, because OSC is our **only three-transport
connector**: `osc-v10` is UDP plus TCP with an int32 length prefix, `osc-v11` is
UDP plus TCP with SLIP framing. So one open question decides how much a Cerebrum
run actually proves:

> **Which TCP framing does Cerebrum use — length-prefix (1.0) or SLIP (1.1)?**

If length-prefix, it validates `osc-v10` and the SLIP path still has no
third-party oracle. If SLIP, the reverse. If both, OSC gains the most complete
producer oracle of any connector. Worth answering before the test is scheduled,
because it changes what the result licenses us to claim.

Those three doc edits are factual and small, but they belong in a PR by the
owner or with the ports confirmed — `docs/testbed.md` is a binding inventory
(ADR-0015) and this audit should not silently amend it.

**cerebrum-nb is the one genuine exception.** `internal/cerebrum-nb/CLAUDE.md`
records the provider as **N/A, not deferred** — "there is no 'serve Cerebrum'
role to implement". The owner's position (2026-09-07) is that we *could* build
one, i.e. impersonate the Cerebrum Northbound API so a third-party system
consumes us as it would consume Cerebrum. That is a scope decision, not a test
gap: if the provider is ever built, its oracle is whatever consumes Cerebrum NB,
and the N/A ruling in that CLAUDE.md must be revisited first.

## Also transferable from the same exercise

1. **Decoded frame trace + flight recorder.** All three defects were
   *addressing* defects, invisible in a hex capture. One `codec.Describe()` per
   protocol, rendered at `--log-level debug`, reused by `validate` text output
   and by the dissector's Info column. On a fault (decode error, `SP_INVSESS`,
   timeout, NAK) dump the last N frames as **one** bounded event. Detail in the
   RollCall audit §7.5D. Fits inside `docs/logging.md` unchanged: high-rate
   frames stay in the per-connector local file, the remote syslog sink caps at
   info.
2. **A rejected push is a fault, not an acknowledgement.** Any provider that
   pushes must treat a non-`ACK` answer as an error and surface it. Ours counted
   `SP_INVSESS` as delivery.
3. **Mine a vendor dissector when one exists.** `rollcall.dll` yielded 72 type
   names and 163 field abbrevs; we keep the suffixes and change only the prefix
   (`dhs_<proto>.`), so vendor-trained engineers' filters translate by
   substitution and any missing field is visible as a gap. Also gives a
   concrete superset test — ours must decode what theirs marks
   "Payload Decode Not Available".
4. **Read-only first, correctly encoded.** Probe with well-formed frames before
   writing anything. A malformed probe crashed a vendor simulator four times;
   three of those were one defect of ours (`rIndex` encoded as `-1` rather than
   255).

## What does NOT generalise

The throwaway producer spike was the right tool for a connector whose producer
had **never** been driven by anything. It is not worth rebuilding where a
producer already has integration coverage — there, point the vendor client at
the **real Go producer** instead. Mandate the *outcome* (a third-party client
drives our producer), not the spike.

## Suggested next steps

1. Owner decision on the ADR-0025 amendment above.
2. Cheapest first proof, in this order:
   - `EmberPlusView` and `Commie.exe` against `dhs producer emberplus serve` /
     `dhs producer probel-sw08p serve` — both tools are in the repo, both
     producers exist. Hours, not days.
   - **vsmStudio** against the acp2 and probel producers, and **Cerebrum**
     against the **osc** producer over UDP and TCP. These are the controllers
     the repo already treats as real-peer evidence, and the ones a customer
     will actually use.
   - Confirm first **which TCP framing Cerebrum's OSC uses** (length-prefix or
     SLIP), since that decides whether the run validates `osc-v10`, `osc-v11`
     or both.
3. If any turns up defects, the amendment is settled on evidence rather than on
   one connector's experience.
4. Separately, decide whether the **cerebrum-nb provider** stays N/A. That is a
   product-scope question, not a testing one.

## Note on scope discipline

This proposal changes *when a producer may be called done*, not what it does.
It adds no verbs, no wire behaviour and no dependencies. The cost is one test
run per connector against a controller that is already installed.
