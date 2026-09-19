// Package monitor is the neutral device monitor from ADR-0030.
//
// It drives the consumer.Protocol interface only, so every connector —
// SNMP, Ember+, ACP2, RollCall — gets scheduled polling, change
// detection, confirmed writes and load discipline without a
// per-protocol loop. It never names a wire format.
//
// Shape (ADR-0030):
//
//   - One actor per device: a goroutine with a command queue and one
//     held connection. A slow or dead device stalls only itself.
//   - Serial on the wire: scheduled reads and operator writes share the
//     device's queue; writes take priority so a human is never starved.
//   - Per-request interval scheduler: each address carries its own
//     interval; a single next-due scheduler fires them, spread across
//     the window by a deterministic per-address jitter (no thundering
//     herd) and coalesced so a slow device never piles duplicates.
//   - Confirmed writes: SetValue then a read-back before the value is
//     trusted and cached, matching the repo's stale-until-confirmed rule.
//   - Event-driven fan-out: change detection emits a delta only when a
//     value moved, onto an in-process channel bus. Downstream runs zero
//     timers; it subscribes.
//
// Stdlib only (ADR-0006). The bus is Go channels, not a broker; WebSocket
// and NATS live outside, at the dhs-srv boundary.
package monitor
