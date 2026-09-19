# monitor — the neutral device monitor

Written for: engineers integrating the monitor into a connector or `dhs-srv`.

The monitor is the protocol-agnostic polling, change-detection, and
confirmed-write layer defined in
[ADR-0030](../../../docs/adr/0030-neutral-device-monitor.md). It drives
the `consumer.Protocol` interface only, so every connector — SNMP,
Ember+, ACP2, RollCall — reuses one implementation instead of writing
its own poll loop.

It replaces the classic single-thread poll loop with an
**actor-per-device, event-driven** design:

- one goroutine and one held connection per device (a slow or dead
  device stalls only itself);
- scheduled reads and operator writes share the device's queue, writes
  first, so a human is never starved behind the poll;
- a single next-due scheduler with per-request intervals, deterministic
  jitter, and coalescing;
- confirmed writes (set, then read back before the value is trusted);
- an in-process channel bus with latest-wins backpressure — downstream
  subscribes and runs zero timers.

Stdlib only. No broker: WebSocket and NATS live outside, at the
`dhs-srv` boundary.

---

## Library usage

### 1. Build a monitor

```go
import (
    "dhs/internal/consumer/monitor"
    "dhs/internal/clock"
)

m := monitor.New(
    monitor.WithLogger(logger),   // optional; defaults to a discard logger
    monitor.WithClock(clock.System()), // optional; tests pass clock.NewFake
)
defer m.Stop()
```

### 2. Load a poll profile

A profile lists what to poll and how often. It is JSON (the repo imports
config as JSON — see the profile format below).

```go
prof, err := monitor.Load(profileBytes)
if err != nil {
    return err // duplicate address, missing address, or non-positive interval
}
```

### 3. Add a device

`Proto` is any `consumer.Protocol` — an SNMP session, an Ember+ session,
etc. It must already be connected (the monitor does not dial for you in
this increment; it reuses the connection you give it).

```go
err := m.Add(ctx, monitor.Device{
    Name:    "ird-111",
    Proto:   snmpSession,      // implements consumer.Protocol
    Profile: prof,
    MinGap:  20 * time.Millisecond, // pace the wire; 0 disables pacing
    AssertControl: func(ctx context.Context) error {
        // reclaim the device, e.g. re-force controlMode=snmp
        return snmpSession.SetControlRemote(ctx)
    },
})
```

### 4. Subscribe to changes

Downstream runs no timers. It receives an event only when a value
actually moved (or on every confirmed write).

```go
id, events := m.Subscribe(256, nil) // buf, optional filter
defer m.Unsubscribe(id)

go func() {
    for ev := range events {
        // ev.Path / ev.Label identify the object
        // ev.Value is the decoded value
        // ev.Changes[0].Old/New show what moved (empty on first sight)
        // ev.Freshness == "live"
        log.Printf("%s = %v (%v)", ev.Path, ev.Value, ev.Changes)
    }
}()
```

Filter to one subtree:

```go
_, events := m.Subscribe(64, func(e consumer.Event) bool {
    return strings.HasPrefix(e.Path, "1.3.6.1.4.1.1773.1.1.10")
})
```

### 5. Write, confirmed

`Set` jumps ahead of scheduled reads, writes, reads back to confirm, and
returns the device-confirmed value. The raw set echo is never trusted.

```go
confirmed, err := m.Set(ctx, "ird-111",
    consumer.ValueRequest{Path: "1.3.6.1.4.1.1773.1.3.200.1.11.0"},
    consumer.Value{Kind: consumer.KindEnum, Enum: 4}, // controlMode = snmp
)
```

### 6. Reassert control, inspect, remove

```go
_ = m.AssertControl(ctx, "ird-111")  // queue the control-mode guard

s, ok := m.Stats("ird-111")          // reads, writes, changes, errors
_ = s
_ = ok

m.Remove("ird-111")                  // stop one device and wait for exit
m.Stop()                             // stop all devices
```

---

## Poll profile format (JSON)

```json
{
  "model": "RX1290@<swrev>",
  "defaults": { "interval": "30s", "on_change": true },
  "oids": [
    { "oid": "1.3.6.1.4.1.1773.1.1.10", "interval": "1s" },
    { "oid": "1.3.6.1.4.1.1773.1.3.200.1.11.0", "interval": "10s", "on_change": false },
    { "oid": "1.3.6.1.4.1.1773.1.1.1.7", "interval": "5m", "on_change": false }
  ]
}
```

| Field | Where | Meaning |
|---|---|---|
| `model` | top | Card model key (`Model@SwRev`), for reference. |
| `defaults.interval` | top | Interval for entries that omit their own. Duration string (`"30s"`, `"5m"`). |
| `defaults.on_change` | top | Default emit policy for entries that omit their own. |
| `oids[].oid` | entry | Address. For SNMP a dotted OID; feeds the neutral `Path`. |
| `oids[].path` / `label` / `slot` / `group` / `id` | entry | Neutral address fields for non-SNMP protocols. Set one. |
| `oids[].interval` | entry | Per-entry override of the default interval. |
| `oids[].on_change` | entry | `true` = emit only when the value moves; `false` = emit every poll. |

**Validation (fail-fast at `Load`):**

- every entry has an address (`oid`, `path`, `label`, or `group`);
- every entry's effective interval is positive (its own, or the default);
- no two entries address the same object (no silent last-wins).

**Scalar SNMP note.** SNMP scalars are addressed with a trailing `.0`
instance (e.g. `...1.11.0`). Table columns take their row index.

---

## Concepts

- **Interval + jitter.** Each address polls at its interval. The first
  fire is offset by a deterministic `hash(address) mod interval`, so
  fifty 1-second reads spread across the second instead of firing on one
  edge. Same seed every run.
- **Coalescing.** If a device falls behind, a duplicate read for an
  address already queued or in flight is skipped — the freshest read
  wins, no backlog.
- **Rate gap.** `MinGap` paces wire operations so a fragile agent is
  never charged. The device sees a steady trickle no matter how
  aggressive the profile looks.
- **Confirmed writes.** `Set` = write, then read back, then emit — the
  value is stale until a live read confirms it.
- **Change events.** `on_change` reads emit only on movement, with
  `ev.Changes` naming the old and new value. Writes always emit.
- **Bus backpressure.** Each subscriber has a bounded channel; on
  overflow the oldest event is dropped so the newest state gets through.
  `m.Drops()` counts sheds.
- **Control assert.** `AssertControl` runs your guard to reclaim a
  device a front-panel operator may have taken over.

---

## CLI

SNMP is the first CLI user. The command is `dhs monitor`.

```
dhs monitor validate --profile rx1290.json
        # load-and-validate a profile, report model + distinct intervals

dhs monitor watch <target> [snmp flags] --profile rx1290.json \
        [--filter PREFIX] [--min-gap DUR] [--duration DUR]
        # dial the agent, schedule the profile, and print a line only when
        # a value moves — MIB-resolved name, enum-named integers, and what
        # it changed from. Ends counters: reads, changes, errors.
```

The target host goes **last**, after the flags (the CLI stops flag
parsing at the first positional). Example against a live RX1290:

```
$ dhs monitor watch --version 1 --community public \
        --profile rx1290.json --duration 10s 10.6.255.111
watching 10.6.255.111 — 5 objects, 1 distinct intervals (Ctrl-C to stop)
08:22:34  controlMode.0        = snmp(4)
08:22:35  unitAlarmStatus.0    = major(5)
08:22:35  cfgAlarmStatus.0     = 0
stopped: 16 reads, 5 changes, 0 errors
```

`--proto` defaults to `snmp` (the only protocol wired today). Other
protocols reuse the same command once their `consumer.Protocol` is
adapted, as SNMP is in `internal/snmp/monitor`.

---

## Testing

The monitor injects its clock (`clock.Clock`), so tests advance time
instantly with `clock.NewFake` — no sleeps, no flake. See the package
tests for a fake `consumer.Protocol` and fake-clock patterns. Current
statement coverage: 97.6%.
