# CLAUDE.md — dhs (Device Hub Systems)

Read this file completely before touching any code, then read the atomic
per-protocol context under `internal/<proto>/CLAUDE.md` for whichever
protocol you're working on.

> Go module path is `acp` — that's the legacy name and stays put to avoid
> import churn. The binary, CLI, and product name are **`dhs`** (Device Hub
> Systems, locked 2026-04-21).

---

## Project purpose

A Go toolset to connect to, monitor, and control devices across four
protocols: ACP1, ACP2, Ember+, and Probel SW-P-08/SW-P-88.

One binary, two roles:

```
dhs consumer <proto> <verb> <target>    outbound — query/control a device
dhs producer <proto> <verb> [flags]     inbound  — serve a canonical tree
```

A separate repository `acp-ui` (React 19) consumes a future `cmd/dhs-srv`
HTTP/WebSocket API. This repo has zero frontend code.

---

## Folder layout

```
cmd/
  dhs/                        single CLI binary (consumer + producer)

internal/
  protocol/                   neutral consumer-plugin registry + iface
    _template/                copy-and-customize for a new protocol
    compliance/               absorb-and-fire-event spec-deviation hooks
  provider/                   neutral provider-plugin registry + iface
  acp1/                       per-protocol self-contained subtree
    CLAUDE.md                 atomic wire-format context
    consumer/                 package acp1 — implements protocol.Protocol
    provider/                 package acp1 — implements provider.Provider
    wireshark/                dissector_acp1.lua
    docs/                     consumer.md / provider.md / README.md
    assets/                   spec PDFs + vendor tools (Synapse Simulator)
  acp2/
    CLAUDE.md
    consumer/   provider/   wireshark/   docs/   assets/
  emberplus/
    CLAUDE.md
    codec/                    stdlib-only wire codec (ber / glow / s101 / matrix)
    consumer/   provider/   wireshark/   docs/
    assets/                   Ember+ PDFs + TinyEmber+/EmberPlusView tools
                              + BY-RESEARCH TS emulator (assets/smh/)
  probel-sw08p/               Probel SW-P-08 / SW-P-88 matrix control
    CLAUDE.md
    codec/                    stdlib-only wire codec (lift-ready)
    consumer/   provider/   wireshark/
    assets/                   SW-P-08 spec + Commie + TS SW-P-08 emulator
  tsl/                        placeholder for future TSL UMD plugin
    assets/                   TSL UMD spec
  transport/                  UDP + TCP + AN2 framer + JSONL capture
  export/                     json/yaml/csv exporter + importer
  scenario/                   scenario-driven test runner
  storage/                    file-backed persistence (planned)

tests/                        unit / integration / smoke / fixtures
docs/                         cross-cutting only:
                                ARCHITECTURE.md · CONNECTOR.md · VISION.md
                                wireshark.md · protocols/schema.md
                                protocols/elements/ · examples/ · deployment/
                                references/ · links/ · fixtures-products.md
```

Per-protocol `internal/<proto>/CLAUDE.md` is authoritative for that
protocol's wire format, command catalogue, and quirks. Don't duplicate that
content here.

---

## Plugin tiers

Both compile-time, Tier-1 registries:

- `internal/protocol/` — consumer plugins. One `init()` per protocol calls
  `protocol.Register(&Factory{})`.
- `internal/provider/` — provider plugins. Same pattern, different registry.

`cmd/dhs/main.go` blank-imports each consumer + provider package to trigger
registration.

## IProtocol

```go
type Protocol interface {
    Connect(ctx context.Context, ip string, port int) error
    Disconnect() error
    GetDeviceInfo(ctx context.Context) (DeviceInfo, error)
    GetSlotInfo(ctx context.Context, slot int) (SlotInfo, error)
    Walk(ctx context.Context, slot int) ([]Object, error)
    GetValue(ctx context.Context, req ValueRequest) (Value, error)
    SetValue(ctx context.Context, req ValueRequest, val Value) (Value, error)
    Subscribe(req ValueRequest, fn EventFunc) error
    Unsubscribe(req ValueRequest) error
}
```

All CLI commands, API handlers, and the device registry talk to `Protocol`
only. Never import `internal/<proto>/consumer` or `internal/<proto>/provider`
from outside their own package — only `init()` registration and `cmd/dhs/`
may do so.

---

## Compliance pattern

Every protocol plugin carries a `compliance.Profile`. When a device emits a
spec deviation, the plugin absorbs it (keeps running) and fires a
`compliance.Event`. Consumers of the library see the event count + summary;
the library never silently works around a deviation.

See `internal/protocol/compliance/` and each protocol's
`compliance_events.go`.

---

## Scale targets (broadcast industry baseline)

Size every data structure and algorithm for these minimums. They drive
codec choices (extended form always available), tree shape (sparse
maps, never dense arrays), and the roadmap to dhs-srv (multi-matrix
sharding, not single-process state).

| Target | Minimum |
|---|---:|
| Crosspoints per matrix | **65 535 × 65 535** (≈ 4.3 B) |
| Matrices per plant | **20 – 100** simultaneously |
| Levels per matrix | 4 – 16 typical (SW-P-08 ext up to 256) |

Consequences:

- Codec 16-bit wire fields are first-class, not an edge case. Auto-escalate
  to extended form (Probel `needsExtended()` pattern) everywhere.
- Tally dumps at 65 535 entries stream-process — decoder emits events as
  dests are decoded, never buffers the whole dump.
- Provider trees use `map[(matrix, level, dst)] → src`, not `[matrix][level][]src`.
- Benchmarks include a worst-case 65535² tally-dump decode per protocol.

See [project_scale_requirements] in memory.

---

## Performance + metrics (every protocol)

Every protocol's transport / session layer MUST expose live metrics on
its connector:

- **Frames**: rx/sec, tx/sec, rx total, tx total
- **Bytes**: rx/sec, tx/sec, rx total, tx total
- **Latency**: rx→tx handler turnaround p50/p95/p99 (µs)
- **Errors**: NAK count, frame-decode errors, reconnect count
- **Memory**: bytes attributable to connector buffers + tree
- **CPU %**: share of process CPU in this connector's goroutines
- **Uptime**: last-frame timestamp per direction

Neutral `ConnectorMetrics` struct defined in `internal/protocol/` (for
consumer) and `internal/provider/` (for provider); each plugin exposes
`Metrics() ConnectorMetrics` on its session. Use `atomic.Uint64` for
counters (no mutex on the hot path); HDR/log-linear histogram for
latency. Do not pull in Prometheus client-libs until dhs-srv wires a
scrape endpoint.

Surfaces:

1. Printed on session close in debug mode.
2. Emitted as a `protocol.Event` tick every ~10 s in watch mode.
3. Reachable via HTTP from dhs-srv when that lands.

See [feedback_transport_metrics] and [project_connector_metrics_v2]
in memory.

### Metrics surface on the producer (landed 2026-04-22)

`internal/metrics/` package provides:
- `Connector` — per-session counters: rx/tx frames + bytes, per-cmd
  `[256]atomic.Uint64` hit counts (rx and tx split), latency histogram
  (7 log-linear µs buckets), errors (decode, NAK, timeout, reconnect),
  Task-Manager fields (CPU%, treeBytes, poolBytes, inflightBytes,
  diskBytes).
- `Process` — runtime.MemStats + NumGoroutine + GC snapshot with a
  periodic sampler.
- `PromRegistry` — wires `prometheus/client_golang` GoCollector +
  ProcessCollector + custom `dhs_connector_*` / `dhs_process_*`
  collectors.
- `WriteCSV` / `WriteMarkdown` — snapshot renderers for the CLI
  export subcommand.

Producer wiring:
```
dhs producer <proto> serve --tree ... --port N \
    --metrics-addr :9100 \
    --log-format json
```
- `/metrics` serves Prom text exposition + OpenMetrics.
- `/snapshot.json` serves Snapshot + ProcessSnapshot JSON.
- `--log-format json` emits `slog.NewJSONHandler` output for
  Loki/Promtail.

Consumer CLI:
```
dhs metrics show                  # live Task-Manager view (MD)
dhs metrics export --format csv   # dump snapshot to CSV
dhs metrics export --format md --file report.md
```

Every protocol plugin satisfies the optional `metricsExposer`
interface (`Metrics() *metrics.Connector`) to participate in
`--metrics-addr`. Probel is wired today; ACP1 / ACP2 / Ember+
roll in D8 of the v2 chain.

Full Grafana + Prometheus + Loki stack under
`docs/deployment/grafana/`: docker-compose, alert rules YAML,
pre-provisioned dashboard JSON.

---

## Architecture principles

Rules every plugin + transport layer follows. Violations require
explicit sign-off.

1. **Encapsulation.** Plugin exposes `Protocol` / `Provider` interface +
   Factory + Metrics accessor. Everything else is package-private.
2. **Dependency injection.** Connectors take transport, logger, tree,
   clock as constructor parameters — no globals, no singletons outside
   the compile-time registries. Tests substitute in-memory impls
   without mocking libs.
3. **Separation of concerns.** `codec/` (bytes), `consumer/` (outbound
   session + API), `provider/` (inbound server + tree), `compliance/`
   (spec-deviation absorption), `wireshark/` (Lua only). No cross-imports
   between consumer and provider.
4. **Library independence.** `internal/<proto>/codec/` is stdlib-only
   and lift-to-own-repo ready. Codec never imports `acp/*`.
5. **No hidden state.** No package-level mutable vars outside the
   compile-time registries; cross-cutting concerns thread through
   constructors or `context.Context`.

See [feedback_architecture_principles] in memory.

---

## Wireshark dissectors (every protocol, no exceptions)

**Rule.** Every plugin MUST ship a Wireshark dissector at
`internal/<proto>/wireshark/dhs_<proto>.lua` that **fully decodes
every transport and every wire version the plugin implements**. Never
delegate to Wireshark's built-in dissector for a protocol that also
ships one of ours — consistency across protocols matters more than
avoiding duplication with upstream. Users need to see the same
`Protocol | Info` shape in Wireshark regardless of which dhs protocol
they're looking at.

**Naming convention (required to avoid clashes with Wireshark
built-ins).** All three names use a `dhs_` prefix:

| Item | Pattern | Examples |
|---|---|---|
| Lua filename | `dhs_<proto>.lua` | `dhs_osc.lua`, `dhs_acpv1.lua`, `dhs_acpv2.lua`, `dhs_emberplus.lua`, `dhs_probel_sw08p.lua`, `dhs_probel_sw02p.lua`, `dhs_tsl.lua` |
| Proto name | `Proto("dhs_<proto>", ...)` | `dhs_osc`, `dhs_acpv1`, `dhs_emberplus` |
| Field abbrev | `dhs_<proto>.<field>` | `dhs_osc.address`, `dhs_acpv1.mcode` |
| Protocol column | typically the human form (e.g. `"OSC/1.1"`) — set via `pinfo.cols.protocol` |

Wireshark built-ins own `osc.*`, `acp.*`, etc. — using a bare `<proto>.*`
field abbrev raises `bad argument #1 to '<ftype>' (A field of an
incompatible ftype with this abbrev already exists)` at load time.
The `dhs_` prefix avoids the clash globally.

Required coverage:

1. **Every transport we implement** — UDP, TCP (length-prefix), TCP
   (SLIP), S101, AN2, whatever the protocol uses. One dissector file,
   multiple registrations.
2. **Every wire version** — if the plugin registers `osc-v10` +
   `osc-v11` or `tsl-v31` + `tsl-v40` + `tsl-v50`, the single dissector
   decodes all of them and surfaces the version in the `Protocol`
   column.
3. **Every type tag / command byte / message kind** — the dissector
   knows the full command catalogue. An unknown byte / tag fires a
   Wireshark expert warning (`ef.ty = ftypes.EF_ERROR`), not a silent
   fallthrough.
4. **Per-command Info column detail** — not just the command name.
   Show the key arguments that identify the message: for Probel SW-P-08
   that's `matrix/level/dest/src`, for OSC that's `address | type-tag |
   arg-count`, for ACP2 that's `slot | type | pid | stat`. If you could
   tell two frames apart by eye in Wireshark, the Info column proves
   it.

The dissector is the **byte-exact reference** for the protocol. When
the spec, the device, and the codec disagree, the dissector breaks the
tie first — it's what you open before you open the Go code.

Install path (manual, one-time per dev machine):

| OS | Personal Lua plugin dir |
|---|---|
| Windows | `%APPDATA%\Wireshark\plugins\` |
| macOS | `~/.local/lib/wireshark/plugins/` |
| Linux | `~/.local/lib/wireshark/plugins/` |

Install + filter guide in [docs/wireshark.md](docs/wireshark.md). A
`make install-dissectors` target that copies every
`internal/*/wireshark/*.lua` into the right place is on the backlog.

See [feedback_wireshark_fully_implemented] in memory.

---

## Error hierarchy

```
ACPError (base)
├── TransportError
│   ├── ConnectionRefusedError
│   ├── ConnectionLostError
│   └── FrameDecodeError
├── ACP1Error
│   ├── ACP1TransportError      MCODE < 16
│   └── ACP1ObjectError         MCODE >= 16
├── ACP2Error
│   ├── InvalidObjectError      stat=1
│   ├── InvalidIndexError       stat=2
│   ├── InvalidPropertyError    stat=3
│   ├── AccessDeniedError       stat=4
│   └── InvalidValueError       stat=5
├── ValidationError             (client-side, pre-send)
└── ExportImportError
```

---

## Storage

No database. No Redis. Files only.

```
Linux:    ~/.local/share/dhs/
macOS:    ~/Library/Application Support/dhs/
Windows:  %APPDATA%\dhs\
Override: --data-dir flag or config.yaml
```

- `devices.yaml` → written only on add/remove
- `slot_{n}.yaml` → written only after a successful walk
- log entries → never written; in-memory circular buffer (1000 entries)

### Value freshness (cache invariants)

Property values ARE written to disk as a **stale cache** for fast startup.
Values on disk are NEVER trusted — they load as `stale` and must be
confirmed by a live source (announcement / get / walk) before being treated
as current.

States: `stale` → `live` → `updated`.

Startup priority: load stale → subscribe to what the view needs →
background walk fills the rest. If a walk result and an announcement race
for the same object, the announcement wins.

> **Performance at scale:** 100-1000 devices × 44k objects/slot needs a
> smart scheduling algorithm (staggered walks, per-device rate limits,
> subscription-first). Not implemented yet — revisit when the future
> `dhs-srv` handles multiple devices concurrently.

---

## Coding conventions

- Go 1.22+
- `context.Context` first param on every I/O function
- `log/slog` for operational output — never `fmt.Println`
- `errors.As` / `errors.Is` at call sites — never string-match errors
- `defer` always releases: mtids, connections, file handles
- No `panic` except truly unrecoverable init
- No global mutable state outside the device registry
- File writes: write to `.tmp` then `os.Rename` (atomic)
- Shared state: `sync.RWMutex` or channels

### File layout

- One primary type per file.
- Command files per-protocol follow `cmd_rxNNN_xxx.go` / `cmd_txNNN_xxx.go`
  where NNN is the decimal command byte, zero-padded to 3 digits.
- Codec packages under `internal/<proto>/codec/` are stdlib-only — they
  must not import `acp/*`. They should be lift-to-own-repo ready.

### Testing

- Unit tests: table-driven with expected byte sequences pulled from the
  authoritative spec, not from working code.
- Integration tests: `//go:build integration`, skip without env var:
  - ACP1: `ACP1_TEST_HOST`
  - ACP2: `ACP2_TEST_HOST`
  - Ember+: `EMBERPLUS_TEST_HOST`
- CI runs unit tests only — never integration against real or emulated
  devices.

### Naming

- Acronyms uppercase: `AN2`, `ACP1`, `ACP2`, `MAC`, `IP`, `UDP`, `TCP`.
- Error types: `FooError` suffix.
- Files: `snake_case.go`.

---

## Spec-strict, no-workaround posture

Spec deviations are surfaced as compliance events, never silently patched.
When the spec and a device disagree:

1. Read the spec. Byte-by-byte. Don't guess.
2. Read the Wireshark dissector (`internal/<proto>/wireshark/`) — it's the
   byte-exact reference.
3. If the device is wrong, absorb + fire event. Don't change the codec.
4. If the spec is ambiguous, ask; don't iterate through encodings.

**Exception — when every shipping controller contradicts the spec.** Very
rarely the spec describes a behaviour no production device actually
implements, and every controller instead depends on a different documented
clause. Criteria to invoke this exception — all must hold:

- At least two independent, in-the-field controllers (not lab emulators)
  verified live to contradict the spec on the same point.
- A separate spec clause justifies the alternative behaviour when read
  literally (the matrix still follows the spec, just a different clause
  than the one the controller community ignores).
- A compliance event NAMES the deviation so every occurrence is auditable.
- A unit test AND an integration test pin the behaviour against regression.

Worked example: SW-P-08 §3.2.30 tells listeners to track salvo-applied
tally via cmd 122 + cmd 123 and tells the matrix NOT to emit cmd 04 on
the salvo path. Neither Commie nor Lawo VSM implement that listener path;
both update tally exclusively from §3.2.3 cmd 04 broadcasts. Our provider
emits N × cmd 04 on salvo Set (§3.2.3 literal) and fires
`probel_salvo_emitted_connected` per slot. Documented in
`internal/probel-sw08p/CLAUDE.md` "Known deviations from spec" + memory
entry `feedback_probel_salvo_connected`.

See [feedback_no_workaround, feedback_spec_table_literal,
feedback_probel_salvo_connected] in memory.

---

## What NOT to do

- Never import a per-protocol package (`internal/<proto>/consumer` etc.)
  from outside its own tree except via `cmd/dhs/` blank imports.
- Never hardcode protocol names in generic code — use the registry.
- Never use fixed byte offsets for ACP2 properties — use pid/plen headers.
- Never call ACP2 pid=4 "event_delay" — it is "announce_delay".
- Never call ACP2 type=2 "event" — it is "announce".
- Never use ACP2 idx=0 to mean "first preset slot" — it is ACTIVE INDEX.
- Never write property values to disk as trusted state.
- Never add Redis, PostgreSQL, or any external data store.
- Never skip `AN2 EnableProtocolEvents` before expecting ACP2 announces.
- Never reuse a live mtid for a new ACP2 request.
- Never import `acp/*` from `internal/<proto>/codec/` packages.
- Never assume a matrix is small — every plugin must cope with 65535×65535
  per matrix and 100 matrices per fleet; use sparse maps, stream decoders,
  and extended wire forms unconditionally.
- Never silently skip latency / memory / throughput instrumentation in a
  connector; if the metric can't be measured today, log the gap rather
  than pretending it's zero.

---

## Adding a new protocol

1. `mkdir internal/<name>/{codec,consumer,provider,wireshark}` (codec
   optional if the wire codec is trivial).
2. Copy `internal/protocol/_template/` into `internal/<name>/consumer/`;
   implement `protocol.Protocol` + `Factory` with
   `func init() { protocol.Register(&Factory{}) }`.
3. Mirror for the provider under `internal/<name>/provider/`, registering
   with `provider.Register`.
4. Create `internal/<name>/CLAUDE.md` — wire format, command catalog,
   quirks, "what NOT to do".
5. Write the Wireshark dissector in `internal/<name>/wireshark/dhs_<name>.lua`
   — FULL implementation covering every transport + every wire version + every
   command / type tag, with a per-command Info column that uniquely identifies
   each frame. Never delegate to Wireshark's built-in, even if one exists.
   Use `Proto("dhs_<name>", ...)` and `dhs_<name>.<field>` abbrevs throughout
   to avoid namespace clashes with Wireshark built-ins. See
   "Wireshark dissectors" above.
6. Add `import _ "acp/internal/<name>/consumer"` and
   `import _ "acp/internal/<name>/provider"` to `cmd/dhs/main.go`.
7. Unit tests live inside the package (`internal/<name>/*/*_test.go`).
8. Done — `dhs consumer <name> <verb>` and `dhs producer <name> serve`
   pick it up automatically via the registries.
