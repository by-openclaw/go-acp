// Package manifest parses the .cache/manifest/<device>.json file per
// ADR-0022 §"Manifest shape". The manifest wires Device → Frame →
// Slot → DM-by-ref. DMs are stored under .cache/dm/<proto>/<Model@SwRev>.json.
//
// The package is loader-only — it produces typed structs that callers
// (producer, consumer) translate into protocol-specific tree shapes.
package manifest

// Manifest is the top-level shape parsed from .cache/manifest/<device>.json.
type Manifest struct {
	Device Device  `json:"device"`
	Frames []Frame `json:"frames"`
}

// Device identifies one physical unit + its network endpoints.
type Device struct {
	Name     string `json:"name"`
	Protocol string `json:"protocol"`
	// IP is the device's own identity IP — the manifest FILE KEY per
	// ADR-0028 (unique + static in the plant; rename-proof where
	// names are not). Distinct from Endpoints (reachability): a
	// device behind a control plane (cerebrum-nb) is reached via the
	// server endpoint but keyed by its own IP. Empty IP falls back to
	// the slugified Name (the ADR's IP-less edge case — callers warn).
	IP string `json:"ip,omitempty"`
	// FQDN is optional metadata only (ADR-0028: the devops direction,
	// recorded now so a later DNS migration is a re-key script, not a
	// schema change). NEVER a key.
	FQDN      string     `json:"fqdn,omitempty"`
	Endpoints []Endpoint `json:"endpoints"`
}

// Endpoint is one IP/port/transport tuple. A device with two endpoints
// is the redundant-controller case (ADR-0023) and MUST be tcp on every
// endpoint: UDP announces are subnet broadcast (single NIC / single
// link) and cannot serve a second controller, so udp is only valid as
// the sole endpoint. Enforced in (*Manifest).validate.
type Endpoint struct {
	IP        string `json:"ip"`
	Port      int    `json:"port"`
	Transport string `json:"transport"` // tcp | udp (udp ⇒ single endpoint only)
}

// Frame is one chassis instance under the Device.
type Frame struct {
	Name  string `json:"name"`
	Slots []Slot `json:"slots"`
}

// Slot binds an opaque per-protocol address to a DM reference.
// Addr keys are plugin-defined (e.g. acp1/acp2: `{"slot":N}`,
// emberplus: `{"oid":"1.4.2"}`, probel: `{"matrix":M,"level":L}`).
// DM is the `Model@SwRev` reference resolved against
// `.cache/dm/<proto>/<DM>.json`.
type Slot struct {
	Addr map[string]any `json:"addr"`
	DM   string         `json:"dm"`
	// Protos optionally overrides the AN2 GetSlotInfo payload-protocol
	// advertisement for this slot (acp2 emulation fidelity). The real
	// EVS Neuron advertises [2,3,4] on slot 0 and [2,3] on slot 1, and
	// Cerebrum's Neuron driver refuses to proceed past a [2]-only reply
	// (wire-proven 2026-08-20) — the fixture must be able to state what
	// the emulated hardware states. Empty = the provider's default.
	Protos []uint8 `json:"protos,omitempty"`
}

// SlotProtos collects the per-slot proto advertisements declared in the
// manifest, keyed by the numeric slot index from Addr. Slots without a
// protos list (or without a numeric slot addr) are absent from the map.
func (m *Manifest) SlotProtos() map[uint8][]uint8 {
	out := map[uint8][]uint8{}
	for _, fr := range m.Frames {
		for _, sl := range fr.Slots {
			if len(sl.Protos) == 0 {
				continue
			}
			n, ok := slotIndex(sl.Addr)
			if !ok || n < 0 || n > 254 {
				continue
			}
			out[uint8(n)] = sl.Protos
		}
	}
	return out
}
