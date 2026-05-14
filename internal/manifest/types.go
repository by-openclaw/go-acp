// Package manifest parses the .cache/manifest/<device>.json file per
// ADR-0022 §"Manifest shape". The manifest wires Device → Frame →
// Slot → DM-by-ref. DMs are stored under .cache/dm/<proto>/<Model@SwRev>.json.
//
// The package is loader-only — it produces typed structs that callers
// (producer, consumer) translate into protocol-specific tree shapes.
package manifest

// Manifest is the top-level shape parsed from .cache/manifest/<device>.json.
type Manifest struct {
	Device Device `json:"device"`
	Frames []Frame `json:"frames"`
}

// Device identifies one physical unit + its network endpoints.
type Device struct {
	Name      string     `json:"name"`
	Protocol  string     `json:"protocol"`
	Endpoints []Endpoint `json:"endpoints"`
}

// Endpoint is one IP/port/transport tuple. A device with two endpoints
// in the same Frame is the redundant-controller case (ADR-0023).
type Endpoint struct {
	IP        string `json:"ip"`
	Port      int    `json:"port"`
	Transport string `json:"transport"` // tcp | udp
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
}
