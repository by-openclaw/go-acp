package is04

import (
	"encoding/json"
	"fmt"

	"dhs/internal/amwa/codec/spec"
)

// Node is the IS-04 v1.3 Node resource — the device itself.
//
// JSON Schema: https://specs.amwa.tv/is-04/releases/v1.3.3/APIs/schemas/with-refs/node.html
type Node struct {
	ResourceCore

	Href       string         `json:"href"`
	Hostname   string         `json:"hostname,omitempty"`
	Caps       map[string]any `json:"caps"`
	API        NodeAPI        `json:"api"`
	Services   []NodeService  `json:"services"`
	Clocks     []NodeClock    `json:"clocks"`
	Interfaces []NodeIface    `json:"interfaces"`
}

// NodeAPI is the `api` sub-object on Node — versions + endpoints.
type NodeAPI struct {
	Versions  []string       `json:"versions"`
	Endpoints []NodeEndpoint `json:"endpoints"`
}

// NodeEndpoint is one entry in NodeAPI.Endpoints. `authorization` is
// required by IS-04 v1.3 endpoint schema, so we always emit it
// (omitempty would drop the field on `false`, breaking round-trip
// equality the AMWA test_31 SYNC grain check enforces).
type NodeEndpoint struct {
	Host          string `json:"host"`
	Port          int    `json:"port"`
	Protocol      string `json:"protocol"`
	Authorization bool   `json:"authorization"`
}

// NodeService is one entry in Node.Services — a non-NMOS service
// running on the Node addressed by URN type. `authorization` is a
// required field per IS-04 v1.3 services schema; same rationale as
// NodeEndpoint above.
type NodeService struct {
	Href          string `json:"href"`
	Type          string `json:"type"`
	Authorization bool   `json:"authorization"`
}

// NodeClock is one entry in Node.Clocks. Polymorphic per `clock_*.json`
// — `name` + `ref_type` are the discriminator. We hold the raw map and
// validate the bare minimum (name + ref_type ∈ {"internal","ptp"}).
type NodeClock struct {
	Name    string `json:"name"`
	RefType string `json:"ref_type"`

	// PTP-specific fields (only populated when RefType == "ptp"):
	Traceable bool   `json:"traceable,omitempty"`
	Version   string `json:"version,omitempty"`
	GMID      string `json:"gmid,omitempty"`
	Locked    bool   `json:"locked,omitempty"`
}

// NodeIface is one entry in Node.Interfaces.
type NodeIface struct {
	ChassisID             *string                `json:"chassis_id"`
	PortID                string                 `json:"port_id"`
	Name                  string                 `json:"name"`
	AttachedNetworkDevice *AttachedNetworkDevice `json:"attached_network_device,omitempty"`
}

// AttachedNetworkDevice mirrors interfaces[].attached_network_device.
type AttachedNetworkDevice struct {
	ChassisID string `json:"chassis_id"`
	PortID    string `json:"port_id"`
}

// Validate enforces every required-field + pattern rule from
// node.json. Optional fields with empty/zero values are skipped.
//
// The `api` sub-object (versions + endpoints) is only required from
// IS-04 v1.1 onward — v1.0 nodes carry only the top-level `href`.
// We therefore validate the api block when it is supplied, but we
// don't require it. Per-version registry handlers enforce stricter
// presence at the URL boundary.
func (n *Node) Validate() error {
	errs := validateCore(&n.ResourceCore, "node")

	if n.Href == "" {
		errs = append(errs, "node.href: required")
	}
	if n.Caps == nil {
		errs = append(errs, "node.caps: required (may be empty object)")
	}

	// `api` is OPTIONAL in canonical Validate — IS-04 v1.0.3 Node
	// schema has no `api` property at all (added in v1.1). When
	// present we validate per-element shape; when absent we don't
	// reject. Strict per-version presence requirements live in
	// `internal/amwa/registry/store.go validateRegistrationPresenceVersioned`.
	if len(n.API.Versions) > 0 || len(n.API.Endpoints) > 0 {
		for i, v := range n.API.Versions {
			if !IsValidAPIVersion(v) {
				errs = append(errs, fmt.Sprintf("node.api.versions[%d] %q: must match `vMAJOR.MINOR`", i, v))
			}
		}
		for i, e := range n.API.Endpoints {
			if e.Host == "" {
				errs = append(errs, fmt.Sprintf("node.api.endpoints[%d].host: required", i))
			}
			if e.Port < 1 || e.Port > 65535 {
				errs = append(errs, fmt.Sprintf("node.api.endpoints[%d].port=%d: out of [1..65535]", i, e.Port))
			}
			if !IsValidHTTPProtocol(e.Protocol) {
				errs = append(errs, fmt.Sprintf("node.api.endpoints[%d].protocol %q: must be \"http\" or \"https\"", i, e.Protocol))
			}
		}
	}

	// services existed since v1.0 — present-but-array validations only.
	for i, s := range n.Services {
		if s.Href == "" {
			errs = append(errs, fmt.Sprintf("node.services[%d].href: required", i))
		}
		if s.Type == "" {
			errs = append(errs, fmt.Sprintf("node.services[%d].type: required (URN)", i))
		}
	}

	// clocks landed in IS-04 v1.1, interfaces in v1.2 — both are
	// optional from this validator's point of view (a v1.0 fixture
	// has neither). When the caller does ship them we still enforce
	// per-element shape.
	for i, c := range n.Clocks {
		if c.Name == "" {
			errs = append(errs, fmt.Sprintf("node.clocks[%d].name: required", i))
		}
		switch c.RefType {
		case "internal", "ptp":
		default:
			errs = append(errs, fmt.Sprintf("node.clocks[%d].ref_type %q: must be \"internal\" or \"ptp\"", i, c.RefType))
		}
	}

	for i, iface := range n.Interfaces {
		if iface.Name == "" {
			errs = append(errs, fmt.Sprintf("node.interfaces[%d].name: required", i))
		}
		if iface.PortID == "" || !IsValidMAC(iface.PortID) {
			errs = append(errs, fmt.Sprintf("node.interfaces[%d].port_id %q: must match MAC pattern xx-xx-xx-xx-xx-xx", i, iface.PortID))
		}
		// chassis_id may be null OR a MAC OR a freeform string per spec; only fully-empty (non-null) is wrong.
		if iface.ChassisID != nil && *iface.ChassisID == "" {
			errs = append(errs, fmt.Sprintf("node.interfaces[%d].chassis_id: empty string disallowed (use null for unknown)", i))
		}
	}

	return joinErrs("is04 node validation failed", errs)
}

// EncodeNode marshals the Node with validation.
func (n *Node) Encode() ([]byte, error) {
	if err := n.Validate(); err != nil {
		return nil, err
	}
	return json.MarshalIndent(n, "", "  ")
}

// DecodeNode parses + validates a Node payload.
func DecodeNode(raw []byte) (*Node, error) {
	return DecodeNodeReporting(raw, APIVersion, nil)
}

// DecodeNodeReporting parses a Node payload and validates it against the
// canonical rules, which track the latest IS-04 minor.
//
// A per-minor codec wants [ParseNode] instead: a v1.0 payload judged by
// v1.3 rules is failed for missing fields that minor never had.
func DecodeNodeReporting(raw []byte, apiVer string, rep spec.Reporter) (*Node, error) {
	n, err := ParseNode(raw, apiVer, rep)
	if err != nil {
		return nil, err
	}
	if err := n.Validate(); err != nil {
		return nil, err
	}
	return n, nil
}

// ParseNode decodes a Node served on an apiVer tree WITHOUT applying any
// minor's validation rules. Two classes of deviation are absorbed and
// reported rather than raised: a field IS-04 defines nowhere (see
// absorb.go) and a field it did not define until after apiVer (see
// [Since]). The caller then validates against the minor it asked for.
func ParseNode(raw []byte, apiVer string, rep spec.Reporter) (*Node, error) {
	var n Node
	if err := decodeAbsorbing(raw, &n, "node", apiVer, rep); err != nil {
		return nil, err
	}
	return &n, nil
}
