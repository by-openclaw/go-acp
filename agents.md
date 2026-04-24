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

`<proto>` ∈ `{acp1, acp2, emberplus, probel-sw08p, probel-sw02p}`.

The `probel-sw02p` plugin is partial — branch
`feat/probel-sw02p-commands` (PR #106, NOT yet pushed/merged) holds
33 command bytes + Wireshark dissector: the salvo family (10 bytes),
the VSM-supported bulk set (14 bytes), and non-VSM seqs 5, 6, 30-33,
36-38, 39-44 (17 bytes). Every command OUTSIDE the VSM set needs
explicit per-command user approval from the numbered queue in
`memory/project_probel_sw02p_cmd_queue.md`. Never write code for
any non-VSM SW-P-02 command without a `approve seq N` from the
user. See `internal/probel-sw02p/CLAUDE.md` for the full landed
tables + owner-only protect authority rule. Codec hardening gaps
(no auto-retry / reconnect / keepalive) tracked in
`memory/project_probel_sw02p_client_hardening.md`.

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

Producer verb is `serve` for every protocol.

---

## Specs + dissectors (authoritative references)

```
internal/acp1/assets/       AXON-ACP_v1_4.pdf
internal/acp2/assets/       acp2_protocol.pdf + an2_protocol.pdf
internal/emberplus/assets/  Ember+ Documentation.pdf + Ember+ Formulas.pdf
internal/probel-sw08p/assets/probel-sw08p/SW-P-08 Issue 30.doc   (use antiword; the .pdf is corrupted)

internal/<proto>/wireshark/dissector_<proto>.lua   byte-exact reference
```

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
