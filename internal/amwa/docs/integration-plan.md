# NMOS — integration test plan

How to verify dhs is wire-compliant against real NMOS peers, and
what status each spec is at today.

This is the canonical companion to
[`sequenced-tasks.md`](sequenced-tasks.md) (development order) and
[`conformance.md`](conformance.md) (AMWA NMOS Testing harness).

---

## The codec self-test trap (read first)

dhs unit tests round-trip every codec through *its own* encoder +
decoder. **That proves nothing about wire compliance.** A bug that
mis-encodes AND mis-decodes symmetrically passes every loopback test.

The only ways to verify the codec actually matches the AMWA spec
text on the wire are:

1. **Drive against a real third-party peer** that emits real bytes
   produced by an independent implementation.
2. **Decode bytes captured from real peers** and assert structural
   correctness against the spec.
3. **Run the AMWA NMOS Testing tool** — it acts as Mock Registry /
   Mock Node / probe-client and exercises every spec it claims.

Status grades in the table below distinguish these:

| Grade | Meaning |
|---|---|
| **green** | Codec landed AND verified live against a real third-party peer (Cerebrum, Lawo VSM, AMWA Testing tool, etc.). |
| **yellow** | Codec landed, only loopback (dhs-encoder ↔ dhs-decoder) tested. Real-peer bytes not yet exercised. |
| **pending** | Code missing. Tracker issue open. |
| **partial** | Some surfaces shipped, others still pending. See notes. |
| **n/a** | Role does not exist for this spec (e.g. internal-only API). |

> **No "skipped" status.** Every NMOS-suite row is owed; if it is
> not green or yellow it is **pending** with a tracker issue. Defers
> happen only on explicit user approval.

---

## Three integration test recipes

### Recipe A — register dhs Node against EVS Cerebrum (10.100.0.5:8080)

Cerebrum is a real third-party Registry / Controller running at
**10.100.0.5:8080** in the dhs lab. It speaks IS-04 v1.3 over HTTP.

1. Build dhs:

   ```bash
   go build -o bin/dhs ./cmd/dhs
   ```

2. Start a dhs Node pointed at Cerebrum (no mDNS — direct unicast):

   ```bash
   bin/dhs producer nmos serve \
       --no-mdns \
       --registry http://10.100.0.5:8080 \
       --bind 0.0.0.0:8080 \
       --advertise-host <your-host>:8080
   ```

3. Watch the dhs logs for the `POST /resource` + `POST /health/...`
   round trip — Cerebrum should respond `201 Created` then keep
   accepting heartbeats. A 4xx response anywhere proves a wire-shape
   mismatch in `is04` codec encode.

4. From a second shell, walk the Cerebrum Query API as a Controller:

   ```bash
   bin/dhs consumer nmos walk http://10.100.0.5:8080 --api-ver v1.3
   ```

   The dhs Node we just registered should appear in the catalogue.
   Round-trip via `walk` exercises the `is04` codec **decode** path
   on bytes Cerebrum produced — independent of dhs's own encode.
   That's what closes the codec self-test loophole.

5. Compliance events fire to stderr for any deviation. Capture with
   `--log-format json | tee` for grep / Loki ingestion.

### Recipe B — AMWA NMOS Testing tool against dhs

The AMWA tool is the canonical conformance harness — Docker, Python,
acts as Mock Registry / Mock Node / probe-client. Per
[`conformance.md`](conformance.md):

```bash
# From the dhs devcontainer shell (Docker-out-of-Docker enabled):
tests/integration/nmos/scripts/run-suite.sh is-04-01
```

This brings up an isolated docker-compose bridge with `dhs:dev` +
`amwa/nmos-testing`, runs the suite non-interactively, scrapes the
JSON results, tears down the bridge (even on Ctrl-C), and exits
non-zero if any expected outcome regresses. See
[`tests/integration/nmos/README.md`](../../../tests/integration/nmos/README.md)
for the full per-suite layout + image-digest pinning recipe.

**Suites shipped today:** is-04-01 (Node API), is-04-02 (Registry),
is-05-01 (Connection), is-07-01 (Events), is-09-02 (Discovery). More
suites land per
[`conformance.md`](conformance.md) §"Suite catalogue" as the host
plugin layer ships.

The harness lives behind PR #183 and is opt-in until you've pinned
the AMWA tool image digest + populated each suite's expected.json
from a first real run.

### Recipe C — capture real peer bytes, decode through dhs

For specs where a live peer exchange is not always possible (lab
hardware availability, NDA), capture the wire bytes once and store
them as fixtures. Decoders run against the captured bytes in unit
tests; the test asserts the decoded struct matches the spec.

```
internal/amwa/codec/<spec>/testdata/peer-captures/<peer>/<resource>.json
internal/amwa/codec/<spec>/peer_captures_test.go
```

This is the same dogfood pattern the project already uses for ACP1 /
ACP2 / Probel (see [feedback_fixture_dogfood] in memory). For NMOS
the real peers we have access to are EVS Cerebrum, Lawo VSM, and
the AMWA Testing tool — each gives us one independent byte stream
per resource shape.

Captures land per resource as the integration test sweep runs. PRs
that touch a codec MUST refresh the matching peer-capture fixture
when the resource shape changes.

---

## Per-spec status

Driving order: each row's preconditions are everything in the rows
above it.

The **AMWA result** column tracks the AMWA NMOS Testing tool
(<https://specs.amwa.tv/nmos-testing/>) outcome — the canonical
third-party conformance signal. Format
`Pass:N Fail:N CNT:N (digest <sha>; YYYY-MM-DD)`. `not run` until
PR #183 has been approved + Recipe B has run; `n/a` for rows
without a matching AMWA suite (BCPs without their own suite,
discovery layer, dissector).

| Seq | Spec | Provider side | Provider | Controller side | Controller | AMWA suite | AMWA result | Notes |
|---:|---|---|---|---|---|---|---|---|
| 1 | **IS-09** v1.0.0 System | `dhs registry nmos serve` — `/x-nmos/system/v1.0/global` | green | Node bootstrap reads `/global` at startup | green | IS-09-02 | not run | PR #153 + retrofit #161; verified live against Cerebrum (memory: project_nmos_plugin.md). |
| 2 | **IS-04** Discovery (DNS-SD) | mDNS announce of `_nmos-{register,query,system,node}._tcp` | green | mDNS browse + unicast SRV/TXT fallback | green | covered by IS-04-0x | not run | Verified live against Cerebrum P2P + Mode B unicast. |
| 3 | **IS-04** v1.1/v1.2/v1.3 Node API | `dhs producer nmos serve` — `/x-nmos/node/<ver>/{self,devices,sources,flows,senders,receivers}` | green | `dhs consumer nmos walk` enumerates the resource graph | green | IS-04-01 v1.3 | **56 Pass / 1 Fail / 1 Warning / 1 Manual** (round 25, 2026-05-01) | Verified Mode B against Cerebrum 10.100.0.5:8080. AMWA harness at `tests/integration/nmos/amwa/` — single Fail = test_16 (Docker Desktop Windows cascade timing race; same code passes on Linux Docker). Per-row caveats in [`tests/integration/nmos/amwa/NOTES.md`](../../../tests/integration/nmos/amwa/NOTES.md). v1.0/v1.1/v1.2 rounds pending. |
| 4 | **IS-04** Registration API | `dhs registry nmos serve` — Node POSTs `/resource` + `/health/...` | yellow | n/a (Node is the client; Controller never touches this surface) | n/a | IS-04-02 | not run | Server side loopback-tested only. Recipe A pending against Cerebrum-as-client. |
| 5 | **IS-04** Query API | `dhs registry nmos serve` — Query face | yellow | `dhs consumer nmos walk <reg-host>` | green | IS-04-02 | not run | Controller verified against Cerebrum; dhs Registry server side only loopback-tested. |
| 6 | **IS-04** Query API WS subscriptions | Registry WS subscription endpoint | yellow | `dhs consumer nmos watch <reg-host>` (subscription grain consumer) | pending | IS-04-04 | not run | Server tested in #157 loopback; client `watch` verb pending tracker. |
| 7 | **IS-04** Node P2P fallback | Node mDNS direct-advertise (Mode D) | green | Direct-Node CSV mode (Mode C) | green | IS-04-03 | not run | EVS Cerebrum P2P round-trip verified. |
| 8 | **IS-05** v1.0.2 single endpoints | Node `/single/{senders,receivers}/{id}/{staged,active,transportfile}` | pending | `dhs consumer nmos connect` PATCHes `/staged` + activation | pending | IS-05-01 v1.0 | not run | Codec landed (#174); plugin tracker #163. |
| 9 | **IS-05** v1.1.2 bulk endpoints | Node `/bulk/{senders,receivers}` | pending | Controller bulk PATCH | pending | IS-05-01 v1.1 | not run | Codec landed (#174); plugin tracker #163. |
| 10 | **IS-05** Controller orchestration | n/a | n/a | full stage → activate flow over many Senders | pending | IS-05-03 | not run | tracker #163. |
| 11 | **IS-08** v1.0.1 Audio Channel Mapping | Node `/x-nmos/channelmapping/v1.0/{io,active,staged,activations}` | pending | Controller POSTs `/map/activations` (immediate + scheduled) | pending | IS-08-01 | not run | Codec landed (#178); plugin tracker #165 (reopened). |
| 12 | **IS-07** v1.0.1 Event & Tally over WebSocket | `dhs producer nmos events serve` — fanout state events to subscribed sources | yellow | `dhs consumer nmos events watch --sources <uuid,uuid>` | yellow | IS-07-01 | not run | Codec + WS layer landed (#176, #177); only loopback tested. Real-peer verification pending. |
| 13 | **IS-07** v1.0.1 Event & Tally over MQTT | MQTT bridge over Publisher | pending | MQTT subscriber | pending | IS-07-01 | not run | Tracker #185. WS done; MQTT is parallel transport per spec, OWED not skipped. |
| 14 | **MS-05-01** Control Architecture | n/a (architecture document, no wire) | n/a | n/a | n/a | covered by IS-12-01 | n/a | PR #180 ships datatypes; informs IS-12. |
| 15 | **MS-05-02** v1.0.0 Control Framework | Node hosts NcObject tree + ClassManager + SubscriptionManager | pending | Controller marshals NcMethodResult / NcDescriptor responses | pending | covered by IS-12-01 | not run | Codec landed (#180); class registry + subscription manager pending tracker #166. |
| 16 | **IS-12** v1.0.1 Control Protocol | Node WS server dispatching `Command` / emitting `Notification` | pending | Controller WS client sending `Command` + tracking `Subscription` | pending | IS-12-01 (invasive) | not run | Codec landed (#179); WS server + client pending tracker #166. |
| 17 | **BCP-002-01** Natural Grouping | Node attaches `urn:x-nmos:tag:grouping/v1.0` on Source/Sender/Receiver/Flow | pending | Controller honours `grouphint` when offering routes | pending | (none — JSON-shape rule) | n/a | Validator landed (#181); attach/honour behaviour layers onto IS-04 plugin. |
| 18 | **BCP-002-02** Asset Distinguishing Information | Node attaches `urn:x-nmos:tag:asset/v1.0` on Sender/Receiver | pending | Controller surfaces asset tags in UI | pending | (none) | n/a | Validator landed (#181). |
| 19 | **BCP-004-01** Receiver Capabilities | Node advertises `caps.constraint_sets` on Receiver | pending | Controller filters Senders against Receiver caps | pending | (none) | n/a | Validator landed (#181). |
| 20 | **BCP-004-02** Sender Capabilities | Node advertises `caps.constraint_sets` on Sender | pending | Controller filters Receivers against Sender caps | pending | (none) | n/a | Validator landed (#181). |
| 21 | **BCP-006-01** NMOS With JPEG XS | Node sets `flow.media_type=video/jxsv` + IS-05 transport_params | pending | Controller honours JPEG XS constraints | pending | (none) | n/a | Validator landed (#181). |
| 22 | **BCP-006-04** NMOS Support for MPEG TS | Node sets `flow.media_type=video/MP2T` + mux format URN | pending | Controller honours MPEG TS constraints | pending | (none) | n/a | Validator landed (#181). |
| 23 | **BCP-008-01** Receiver Status Monitoring | Node exposes `NcReceiverMonitor` class via IS-12 | pending | Controller subscribes to monitor properties | pending | BCP-008-01 | not run | Validator landed (#181); class registration depends on MS-05-02 plugin. |
| 24 | **BCP-008-02** Sender Status Monitoring | Node exposes `NcSenderMonitor` class via IS-12 | pending | Controller subscribes to monitor properties | pending | BCP-008-02 | not run | Validator landed (#181); class registration depends on MS-05-02 plugin. |
| 25 | **AMWA NMOS Testing harness** | n/a | n/a | n/a | n/a | (meta) | n/a | PR #183 wires the harness; awaits user manual approval after first real run pins image digest + populates per-suite expected.json. |
| 26 | **Wireshark dissector** | n/a — passive observer | green | n/a — passive observer | green | (meta) | n/a | `internal/amwa/wireshark/dhs_nmos.lua` decodes mDNS, HTTP `/x-nmos/`, WebSocket text frames. PR #182. |

---

## How "yellow" rows turn green

Each yellow row needs at least one of:

- **Real-peer Recipe A**: register dhs against Cerebrum (or Lawo
  VSM, or any other third-party Registry) and verify a successful
  exchange. Logs go in
  `tests/integration/nmos/results/<recipe>-<RUN_ID>.log`.
- **AMWA Recipe B**: matching suite passes (Pass count =
  expected.json baseline; no new Could-Not-Test).
- **Recipe C fixture**: peer bytes captured into
  `internal/amwa/codec/<spec>/testdata/peer-captures/` + matching
  unit test decoder run against them.

Any combination of the three flips a yellow row to green. The first
real-peer evidence wins; subsequent evidence layers in as a
regression net.

---

## Pending plugin work that unlocks integration testing

1. **IS-04 Controller `watch` verb** (seq 6) — WS subscription
   consumer layered on top of `dhs consumer nmos walk`. Reuses
   `internal/amwa/session/http` WebSocket client + the IS-04 Query
   grain envelope.
2. **IS-05 plugin** (seq 8 / 9 / 10, tracker #163) — Node-side
   single + bulk handlers, Controller stage/activate orchestration,
   `dhs consumer nmos connect` verb.
3. **IS-08 plugin** (seq 11, tracker #165 reopened) — Node-side
   `/map/...` handlers, Controller mapping diff tool.
4. **IS-12 + MS-05-02 plugin layer** (seq 15 / 16, tracker #166) —
   Node WS server, ClassManager, SubscriptionManager, Controller
   WS client.
5. **IS-07 MQTT bridge** (seq 13, tracker #185) — parallel transport
   per IS-07 §6.

Each ships behind the existing `feedback_per_spec_issue.md` rule —
one PR per sub-tracker, auto-merge on green CI per
`feedback_nmos_auto_merge.md`, manual approval owed only on PR #183
(integration-test gate).

---

## Driving the test sweep

Per-suite invocation:

```bash
tests/integration/nmos/scripts/run-suite.sh <suite-id>
```

Per-Recipe-A invocation:

```bash
bin/dhs producer nmos serve --no-mdns --registry http://10.100.0.5:8080 --advertise-host <host>:8080
bin/dhs consumer nmos walk http://10.100.0.5:8080 --api-ver v1.3
```

See [`tests/integration/nmos/README.md`](../../../tests/integration/nmos/README.md)
for the bring-up + bump-pin recipe; see
[`conformance.md`](conformance.md) for the gating rules.
