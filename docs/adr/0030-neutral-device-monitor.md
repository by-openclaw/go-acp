# ADR-0030 — Neutral device monitor: actor-per-device, event-driven, per-request interval scheduler

Status: proposed

This ADR is a living document. Add new facts in the Revisions trailer.

## Context

Realtime device monitoring at plant scale is a first-class need: the
operator wants live status (frequency, level, C/N, lock, alarms) for a
fleet of devices, updated continuously, while manual control still
works and the devices themselves are not overloaded.

The reference implementation the operator brought is a 1997-era IRD
manager. Its shape:

- **One thread for the whole fleet.** A single loop walked every device
  in turn, every X seconds, collecting main status.
- **Blocking serial operations.** Each `set` blocked for its confirm,
  then wrote the database, before the loop moved on. A manual operation
  waited on the same poll thread.
- **Direct database writes** as the state store, no event stream.
- **Control reassertion** by re-forcing the device's control mode to
  serial each pass, so a front-panel operator could not silently steal
  control from the manager.

That design was correct for its constraints and encodes hard-won
knowledge, serialize the wire, confirm before you trust, reassert
control, do not charge the device. But one-thread-for-all does not
meet this repo's scale targets (20–100 devices per plant, and the
broadcast baseline in `CLAUDE.md`): one slow or dead device stalls the
whole fleet, status latency is the sum of every device's poll time, and
there is no push path to a UI or to `dhs-srv`.

Multiple connectors need the same capability. SNMP needs interval
polling because the protocol has no "any OID changed" notification
(only vendor-defined traps fire, and only on transition). Ember+,
ACP2, and RollCall also benefit from a uniform scheduled-read plus
change-emit layer. The scheduler must therefore be **neutral**, not
SNMP-specific.

## Decision

Build a neutral device monitor in `internal/consumer/`, driving the
`Protocol` interface only. It keeps the 1997 design's four hard-won
rules and modernises the mechanism. Each rule below is binding.

### 1. Actor per device, not one thread for all

Every device runs its own supervised worker: a goroutine with a bounded
inbound mailbox channel. A slow or dead device blocks only its own
worker, never the fleet. Workers are `context.Context`-cancellable and
supervised for restart, consistent with ADR-0009. This generalises the
RollCall held-connection session (#1101): one connection held per
device, reused, not reopened per operation.

### 2. Serial on the wire, commands over queries

Inside a worker there is one command queue and one held connection. Both
scheduled reads and operator actions enqueue onto it; the worker
executes one at a time, so a manual `set` never races a poll. Operator
commands take priority over scheduled polls so a human is never starved
behind the loop. This is the modern form of "manual operation waits the
poll thread", the wait is a queue hand-off, not a blocked thread.

### 3. Per-request interval scheduler, declarative

A poll profile, keyed to the card model like everything in ADR-0022,
lists each read by its neutral address (label or path, which each
protocol resolves to its own wire form) with a per-request `interval`
and an `on_change` flag. Interval is a field on the entry, never encoded
in a filename. The scheduler:

- **Buckets by distinct interval** into timers, so N reads at the same
  cadence cost one ticker, not N.
- **Jitters deterministically** with `hash(address) mod interval`, so
  same-cadence reads spread across the window instead of firing on one
  tick edge (no thundering herd). Same seed every run.
- **Enqueues, never executes.** The scheduler only submits read
  commands to the device worker's queue at the due time.

Config duplicates (the same address twice, or a subtree that swallows a
listed scalar) are rejected at load with a named error, fail fast.

### 4. Confirmed writes, stale-until-confirmed

A write command runs `SetValue`, then reads the value back with
`GetValue` to confirm, then writes the cache atomically (`.tmp` +
`os.Rename`), marks the value fresh, and emits a change event. The
`SetValue` echo alone is never trusted. This is the operator's "each set
I get a confirm then update the cache" and it is already the repo rule:
values load stale and a live read confirms them.

### 5. Event-driven fan-out, push not poll

Change detection diffs each read against last-known state and emits a
delta only when a value moved. Deltas go to an event bus; subscribers
(`Subscribe`, a future `dhs-srv` WebSocket, the Prometheus surface)
consume them. Downstream consumers run **zero timers**, they subscribe.

The bus is **in-process Go channels plus a subscriber registry**, not an
external broker. Each subscriber gets its own bounded channel with the
same latest-wins backpressure as the poll queue; this is exactly what
`Protocol.Subscribe(req, fn)` already promises. No NATS, no Redis, no
message broker, consistent with the repo's stdlib-and-no-external-store
posture (ADR-0005, ADR-0006). **WebSocket is not the bus**: it is the
outward transport at the `dhs-srv` boundary, where that separate binary
subscribes to the in-process bus and re-emits deltas to the `dhs-ui`
browser. A cross-node broker (NATS or similar) is reconsidered only if
`dhs-srv` ever shards across processes, and only as its own ADR-0005
dependency decision, never inside a single connector.
The polling burden lives once, here, and is invisible to everything that
reads from us. For a poll-only protocol like SNMP this means the gateway
turns a dumb device into a change-driven source; the consumer cannot
tell it apart from Ember+ or RollCall, which push natively.

### 6. Let the device breathe, keep status realtime

Two mechanisms bound load without going stale:

- **Per-device rate budget.** A token bucket caps PDUs/requests per
  second and in-flight count, independent of how many reads the profile
  lists. If the profile asks for more than the budget, the loader warns
  and the scheduler slows the least-volatile reads first.
- **Latest-wins coalescing.** When a device falls behind, superseded
  read ticks are dropped, not queued, so the worker always fetches the
  freshest value rather than draining a backlog.
- **Adaptive backoff.** On timeout or drop, that read's cadence backs
  off exponentially and fires a compliance event, then recovers when
  answers return. Fragile agents (the Ericsson IRDs drop requests under
  load, RFC 1157 §4.1) are never hammered.

### 7. Reassert control

A per-device guard hook can assert the control mode the manager wants
(remote/serial) each pass and re-force it on detected drift, the modern
form of "reforce into serial" so a front-panel operator cannot silently
take over. For the IRD this writes `controlMode` over SNMP.

### 8. Observability and testability first

Every worker exposes the neutral `ConnectorMetrics` (frames/bytes,
latency p50/p95/p99, errors, uptime) per device; logging is `slog`. The
monitor takes transport, clock, and logger by injection (no globals), so
tests drive it with an in-memory `Protocol` and a fake clock, the
existing `base_clock_test` pattern.

### 9. Relationship to the producer

The scheduler, confirmed-write, and control reassertion are
**consumer-side**, they are things done to a real device. A pure
emulator producer (manifest- or scenario-driven `serve`) needs none of
them, its tree has no upstream to poll.

The **event bus and change-detection are neutral primitives both roles
share**:

- A **gateway or federation producer** (ADR-0024 mirror frame / virtual
  frame, and the any-to-any gateway vision) keeps its served tree live
  by *subscribing* to the monitor's deltas rather than running its own
  loop. The consumer polls the upstream and emits; the producer
  re-serves downstream in whatever protocol the controller speaks. The
  bus is the translation seam.
- A producer already owes its connected controllers change
  notifications (Ember+ parameter-change, ACP2 announce, TSL push, our
  own SNMP traps). That is the same change-then-fan-out primitive
  pointed outward, so it is built once in neutral code and reused in
  both directions.
- Timers in a producer are housekeeping only (keepalive, metric-snapshot
  cadence), never device polling.

Placement therefore: scheduler + confirmed-write on the consumer path;
bus + change-detection neutral and shared; the gateway producer is a
subscriber.

## Consequences

- Every connector gets scheduled polling, change-emit, confirmed writes,
  and load discipline for free by implementing `Protocol`. No per-protocol
  poll loops.
- SNMP is the first user, and the natural home for its ADR-0025 DONE
  poller deliverable (epic #1096). RollCall's session work already proved
  the held-connection half.
- The per-device worker shards cleanly to multiple processes for
  `dhs-srv`, matching the repo's "shard, not single-process" scale
  posture.
- New surface to own: a bounded event bus, the poll-profile schema and
  loader, and the supervisor wiring. All stdlib.
- The 1997 knowledge is preserved rule-for-rule; only the mechanism
  (thread-per-fleet → actor-per-device, DB-write → event-sourced cache,
  poll-pull → subscribe-push) is modernised.

## Revisions

- 2026-09-19 — Initial proposal. Captures the operator's IRD-manager
  design (serial confirm-then-persist, control reassertion, do-not-charge)
  and modernises it to an actor-per-device, event-driven, neutral
  scheduler. Drafted during the SNMP live-trap session.
