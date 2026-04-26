# agents.md — dhs (Device Hub Systems)

Shared session rules for AI agents working on this project. Read alongside:

- `CLAUDE.md` — cross-cutting Go conventions, registry pattern, compliance
  pattern, error hierarchy, storage rules.
- `internal/<proto>/CLAUDE.md` — atomic per-protocol wire-format context
  (one file per protocol).

The Go module path is `acp` (legacy, kept to avoid churn). The binary,
CLI, and product name are **`dhs`** (Device Hub Systems, locked 2026-04-21).

---

## Repositories

```
acp/     Go module — core library + dhs CLI + planned REST/WS server
acp-ui/  separate React 19 repo — consumes the future dhs-srv REST + WS API
```

Integration point: `acp/api/openapi.yaml` defines the REST + WebSocket
contract. `acp-ui` generates its types from that spec and has zero
knowledge of Go or protocol internals.

---

## Folder layout (at a glance)

```
cmd/dhs/                        single CLI binary (consumer + producer)
internal/protocol/              neutral consumer-plugin registry + iface
internal/provider/              neutral provider-plugin registry + iface
internal/<proto>/               per-protocol self-contained subtree:
  CLAUDE.md                     atomic wire-format context (read this!)
  codec/                        stdlib-only byte codec (lift-ready)
  consumer/                     package <proto> — implements protocol.Protocol
  provider/                     package <proto> — implements provider.Provider
  wireshark/                    dissector_<proto>.lua
  docs/                         consumer / provider / README per protocol
  assets/                       spec PDFs + vendor tools + TS emulators
tests/                          unit / integration / smoke / fixtures
docs/                           cross-cutting architecture, connector, schema
```

`<proto>` ∈ `{acp1, acp2, emberplus, probel-sw08p, probel-sw02p, osc-v10, osc-v11, tsl-v31, tsl-v40, tsl-v50}` on main.
Other feature branches add more: `cerebrum-nb` on `feat/cerebrum-nb-plugin`
(PR #144), `nmos` (scaffold only — design doc + epic #146) on
`feat/nmos-scaffold`. See `memory/project_protocol_backlog.md` for the
full queue.

> **NMOS is the odd one out.** It is a suite of ~14 specs
> (IS-04/05/07/08/09/12/13, MS-05-01/02, BCP-002/004/006/007/008) with a
> 3-role topology — Node + Registry + Controller (Registry is a dual-
> face hybrid: consumer of registrations + provider of catalogue) —
> rather than the 2-role consumer/provider split. Read
> `internal/amwa/CLAUDE.md` and `internal/amwa/docs/architecture.md`
> before touching it.
>
> **Locked scope rules (2026-04-27):**
>
> - **NMOS-strict-spec only.** Implement IS-04/05/07/09/12/MS-05/
>   BCP-002/004/006/008 literally; fire compliance events on peer
>   deviations (see `internal/amwa/docs/matrix-compliance.md` —
>   Lawo VSM verified). NEVER mix with cross-protocol mux concepts.
> - **Cross-protocol mux is parked.** The ingress→canonical→egress
>   matrix (Ember+ ingress fan-out to glow+router egresses) is real
>   architecture but tied to a planned CLI refactor. NO epic, NO PR
>   without explicit user ask. See `memory/project_cross_protocol_mux.md`.
> - **Plugin lift-ability.** Each `internal/<proto>/` subtree must
>   stay extractable to its own Go module repo. Codec rule already in
>   `feedback_codec_isolation.md`; extend to consumer + provider +
>   future registry plugin. Only neutral interfaces cross the seam.
> - **Conformance gate is AMWA NMOS Testing** (Apache-2.0 Python tool,
>   Docker `amwa/nmos-testing`). Per-phase suite mapping in
>   `internal/amwa/docs/conformance.md`; runs via devcontainer +
>   isolated bridge (NEVER `network_mode: host`); pinned by image
>   digest; trap-based cleanup so no garbage.
> - **Reference impl: sony/nmos-cpp** (Apache-2.0, JT-NM Tested) — use
>   as cross-impl byte oracle + interop peer. Same role as `osc.js` for
>   OSC and Commie for Probel.
> - **Real-world testbed peers:** Lawo VSM Studio (NMOS Controller —
>   IS-04 Node API + IS-05 v1.0/1.1, HTTP only, no Query API, no
>   WebSocket, no scheduled activations); EVS Cerebrum (Cerebrum-NB
>   on PR #144; Cerebrum NMOS proxy is a real production cross-proto
>   bridge — interop peer for our NMOS plugin, not a model to copy).
> - **Approval rule:** when an agent asks the user a question, NEVER
>   act in the same turn — wait for explicit approval before any
>   write/commit/PR/issue/memory action. See
>   `memory/feedback_approval_before_action.md`.

The **TSL UMD plugin** registers three wire versions as separate
entries per the `feedback_protocol_versioning.md` Pattern A rule:

- `tsl-v31` — UDP-only (spec §3.0); 18-byte frame; 4 binary tallies + 2-bit brightness; **no colour**
- `tsl-v40` — UDP-only; v3.1 frame + CHKSUM + VBC + XDATA (3-position 2-bit colour for L/R display)
- `tsl-v50` — UDP **and** TCP/DLE-STX (spec §Phy); LH/Text/RH 2-bit colour + 2-bit brightness per DMSG

TSL is push-only one-way: tally-source → MV. CLI shape:

- `dhs consumer tsl-vXX listen [--bind HOST:PORT] [--tcp] [--keepalive DUR]` — bind a UDP (or v5.0 TCP) listener and print every decoded frame.
- `dhs producer tsl-vXX send  --dest HOST:PORT [version flags]` — encode one frame and push.
- `dhs producer tsl-vXX serve --dest HOST:PORT --refresh DUR [version flags]` — same as send, looped on a timer.
- v5.0 grouped multi-DMSG: repeatable `--dmsg "index=N,lh=...,text-tally=...,rh=...,brightness=...,umd=STR"`.
- TCP dead-socket detection via SO_KEEPALIVE 30 s on both producer dialer and consumer listener (TSL spec carries no app-layer heartbeat; pcap audit confirmed VSM never sends one).

Wireshark support: `internal/tsl/wireshark/dhs_tsl.lua` covers all
three versions on UDP + v5.0 TCP/DLE-STX. Auto-registers on UDP
4000 (v3.1) / 4004 (v4.0) / 8901 (v5.0) and TCP 8902 (v5.0 testbed
default; spec is 8901 but UDP binds it). Info column carries
`addr=N T=1234 bright=B UMD="..."` for v3.1/v4.0 and
`PBC=N screen=N dmsgs=N` (+ per-DMSG detail when single DMSG) for
v5.0.

Validated live 2026-04-26 against:
- **Lawo VSM Studio** (controller pushing v3.1, v4.0, v5.0 single-DMSG)
- **Miranda TSL over IP Emulator v1.02** (v5.0 UDP + TCP, single +
  group-display-messages 5-DMSG)

The **OSC plugin** registers two wire versions as separate entries per
the `feedback_protocol_versioning.md` Pattern A rule:

- `osc-v10` — UDP + TCP/int32-length-prefix; core types i/f/s/b
- `osc-v11` — UDP + TCP/SLIP (RFC 1055 double-END); adds T/F/N/I + arrays

Both share `internal/osc/codec/` (stdlib-only). Wireshark support is a
full from-scratch dissector at `internal/osc/wireshark/dhs_osc.lua`
covering UDP + TCP length-prefix (1.0) + TCP SLIP (1.1), every type
tag including 1.1 payload-less (T/F/N/I) and array markers (`[`, `]`),
and recursive bundle decoding. Per-message Info column shows address,
type-tag string, and arg count.

The **probel-sw02p** plugin merged on main 2026-04-25 via PR #106
(closed #105) — 33 command bytes + Wireshark dissector: the salvo
family (10 bytes), the VSM-supported bulk set (14 bytes), and
non-VSM seqs 5, 6, 30-33, 36-38, 39-44 (17 bytes). Every command
OUTSIDE the VSM set needs explicit per-command user approval from
the numbered queue in `memory/project_probel_sw02p_cmd_queue.md`.
Never write code for any non-VSM SW-P-02 command without an
`approve seq N` from the user. See
`internal/probel-sw02p/CLAUDE.md` for the full landed tables +
owner-only protect authority rule + protect-blocks-connect
state-echo deviation. Consumer matrix-config flags
(`--mtx-id --level --dsts --srcs`) + bootstrap rx 01 sweep +
rotating 2 s keep-alive ping landed via PR #132 (closed #128;
mirrors VSM observed behaviour since SW-P-02 has no in-protocol
keep-alive command). PR #132 also added default TCP
`SO_KEEPALIVE` across sw02p / sw08p / osc TCP codecs (#129) and
mirrored matrix-config flags onto sw08p (#130). HA / multi-
instance parked under epic #127 (see
`memory/project_ha_architecture.md`).

---

## CLI surface

```
dhs consumer <proto> <verb> <target> [flags]    outbound — query/control
dhs producer <proto> <verb> [flags]             inbound  — serve a tree
dhs list-protocols
dhs version
```

Consumer verbs for acp1/acp2/emberplus (generic): info, walk, get, set,
watch, export, import, extract, diff, convert, discover, matrix, invoke,
stream, profile, diag.

Probel has its own verb catalogue (interrogate, connect, tally-dump, watch,
…) — see `dhs consumer probel-sw08p -h`.

OSC has its own symmetric-peer verbs:
- `dhs consumer osc-vXX watch --listen <udp|tcp-len|tcp-slip>:<port> [--pattern PAT]`
- `dhs producer osc-vXX send --to HOST:PORT --transport KIND --address /A --types TAGS [args...]`
- `dhs producer osc-vXX fader --to HOST:PORT [--rate N] [--duration D] [--min --max] [--pattern ramp|sine|random]`
- `dhs producer osc-vXX serve --bind <transport>:<port>`

Type-tag tokens for `--types`: `i f s b h d t S c r m T F N I [ ]`. The
watcher's per-frame line shape (`/addr ,tags v1 v2 v3`) matches the
`dhs_osc.lua` Wireshark Info column verbatim, so a live `watch`
terminal and a tshark capture can be diffed line-for-line.

Producer verb is `serve` for the slot-based protocols.

---

## Specs + dissectors (authoritative references)

```
internal/acp1/assets/       AXON-ACP_v1_4.pdf
internal/acp2/assets/       acp2_protocol.pdf + an2_protocol.pdf
internal/emberplus/assets/  Ember+ Documentation.pdf + Ember+ Formulas.pdf
internal/probel-sw08p/assets/probel-sw08p/SW-P-08 Issue 30.doc   (use antiword; the .pdf is corrupted)

internal/<proto>/wireshark/dhs_<proto>.lua         byte-exact reference
```

Naming convention is `dhs_<proto>` for the file, the Proto, and the
field-abbrev prefix (e.g. `dhs_osc.address`). The `dhs_` prefix avoids
clashes with Wireshark built-ins that own bare `<proto>.*` namespaces
(notably `osc.*`). A regression fixture is checked in at
`tests/fixtures/osc/battery.pcapng` (343 KB, 88 frames across all OSC
transports) — `dissector_replay_test.go` runs tshark on it under the
`integration` build tag.

Extract the Probel spec:

```
antiword "internal/probel-sw08p/assets/probel-sw08p/SW-P-08 Issue 30.doc" > swp08.txt
```

SW-P-08 §2 defines the transmission protocol (ACK/NAK flow). Any code or
comment citing "SW-P-88 §3.5" is wrong — the real section is SW-P-08 §2.

Rule: before modifying any codec, read the relevant spec section. When the
spec and a reference implementation disagree, the spec wins.

Exception — "spec vs. every shipping controller" (rare, documented): if two
or more independent production controllers consistently contradict the spec
on the same point, follow the controllers AND fire a compliance event that
names the deviation. Do NOT silently conform to the controllers. Lab
emulators don't count; live interop against real devices does. Example:
SW-P-08 §3.2.30 tells listeners to use cmd 122+123 for salvo tally, but no
shipping controller implements that path — all update from cmd 04 per
§3.2.3, so the matrix must emit cmd 04 on salvo Set + fire
`probel_salvo_emitted_connected` (see
`internal/probel-sw08p/CLAUDE.md` "Known deviations from spec").

---

## Task patterns

### Add a new command for protocol X

1. Codec: add `internal/<proto>/codec/cmd_rxNNN_*.go` (or `txNNN_*`) with
   the byte-exact encoder/decoder. Stdlib only.
2. Consumer: add `internal/<proto>/consumer/cmd_rxNNN_*.go` that wires the
   codec into `Protocol` surface.
3. Provider: add `internal/<proto>/provider/cmd_rxNNN_*.go` handler.
4. Test: table-driven expected-bytes tests in the same folder.
5. Wireshark: extend the dissector if the new command has new fields.
6. Verify against a real capture.

### Add a new protocol

See `CLAUDE.md` → "Adding a new protocol". Copy `internal/protocol/_template/`,
create the `internal/<name>/CLAUDE.md`, register in both consumer + provider
registries, blank-import in `cmd/dhs/main.go`.

### Fix a wire-format bug

1. Write a failing test first with expected bytes from the spec table.
2. Fix the codec.
3. Verify the test passes.
4. `go test ./internal/<proto>/...`.

---

## Testing rules

```
Unit tests (always run, no device needed):
  go test ./internal/...
  MockTransport injected — never real sockets
  Table-driven, expected bytes from spec documents

Integration tests (real device or emulator):
  go test -tags integration ./tests/integration/<proto>/...
  ACP1_TEST_HOST=192.168.1.5  (ACP1 emulator)
  ACP2_TEST_HOST=192.168.1.8  (ACP2 device or VM)
  EMBERPLUS_TEST_HOST=...     (Ember+ provider)
  Skip if env var not set

Protocol captures:
  Use internal/<proto>/wireshark/dissector_<proto>.lua in Wireshark
  Verify byte sequences match test expectations
```

Rule: build always — `go build ./...` before running tests, always into
`bin/`. Never ask the user to build for you. See
[feedback_build](C:\Users\BY-SYSTEMSSRLBoujraf\.claude\projects\c--Users-BY-SYSTEMSSRLBoujraf-Downloads-acp\memory\feedback_build.md).

---

## Git workflow

- Branch-per-issue (`feat/...`, `fix/...`, `refactor/...`).
- Never merge untested: integration-test on VM/real device before PR.
- PR title under 70 chars.
- Put `Closes #N` in the **PR body**, not only the commit body — squash
  drops per-commit lines. See
  [feedback_pr_issue_close](C:\Users\BY-SYSTEMSSRLBoujraf\.claude\projects\c--Users-BY-SYSTEMSSRLBoujraf-Downloads-acp\memory\feedback_pr_issue_close.md).
- Prefer creating new commits over `git commit --amend`.
- Never `--no-verify`; fix the hook failure instead.

Commit message style (observed across the repo):

```
<type>(<scope>): <short subject>

<body — why, not what>

Co-Authored-By: Claude Opus ...
```

Where `<type>` ∈ `{feat, fix, refactor, docs, test, chore}` and `<scope>`
is the protocol or subsystem (`acp1`, `emberplus`, `probel-sw08p`, `dhs`, etc.).

---

## Out of scope — v1

```
ACP1 methods 6-9    (getPresetIdx, setPresetIdx, getPrp, setPrp)
ACP1 File objects   (engineer-mode only on real Synapse; unit-test-only)
ACMP protocol       (AN2 proto=3)
Authentication / TLS
Historical data / retention
Multi-user sessions
Apple notarization
ARM Linux packages
```

---

## acp-ui-specific rules

```
Never call device protocols directly — only through the future dhs-srv REST/WS
Never manually edit src/types/api.ts — regenerate from openapi.json
Never hardcode protocol names — fetch from GET /api/protocols
Never poll REST for live values — use WebSocket announces
TypeScript strict mode everywhere
useOptimistic for all SET operations with rollback on error
```
