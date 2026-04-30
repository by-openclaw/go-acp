package is04

import (
	"bytes"
	"encoding/json"
	"fmt"
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
	Versions  []string      `json:"versions"`
	Endpoints []NodeEndpoint `json:"endpoints"`
}

// NodeEndpoint is one entry in NodeAPI.Endpoints.
type NodeEndpoint struct {
	Host          string `json:"host"`
	Port          int    `json:"port"`
	Protocol      string `json:"protocol"`
	Authorization bool   `json:"authorization,omitempty"`
}

// NodeService is one entry in Node.Services — a non-NMOS service
// running on the Node addressed by URN type.
type NodeService struct {
	Href          string `json:"href"`
	Type          string `json:"type"`
	Authorization bool   `json:"authorization,omitempty"`
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
	ChassisID            *string                 `json:"chassis_id"`
	PortID               string                  `json:"port_id"`
	Name                 string                  `json:"name"`
	AttachedNetworkDevice *AttachedNetworkDevice `json:"attached_network_device,omitempty"`
}

// AttachedNetworkDevice mirrors interfaces[].attached_network_device.
type AttachedNetworkDevice struct {
	ChassisID string `json:"chassis_id"`
	PortID    string `json:"port_id"`
}

// Validate enforces every required-field + pattern rule from
// node.json. Optional fields with empty/zero values are skipped.
func (n *Node) Validate() error {
	errs := validateCore(&n.ResourceCore, "node")

	if n.Href == "" {
		errs = append(errs, "node.href: required")
	}
	if n.Caps == nil {
		errs = append(errs, "node.caps: required (may be empty object)")
	}

	if len(n.API.Versions) == 0 {
		errs = append(errs, "node.api.versions: required (>=1 entry)")
	}
	for i, v := range n.API.Versions {
		if !IsValidAPIVersion(v) {
			errs = append(errs, fmt.Sprintf("node.api.versions[%d] %q: must match `vMAJOR.MINOR`", i, v))
		}
	}
	if len(n.API.Endpoints) == 0 {
		errs = append(errs, "node.api.endpoints: required (>=1 entry)")
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

	if n.Services == nil {
		errs = append(errs, "node.services: required (may be empty array)")
	}
	for i, s := range n.Services {
		if s.Href == "" {
			errs = append(errs, fmt.Sprintf("node.services[%d].href: required", i))
		}
		if s.Type == "" {
			errs = append(errs, fmt.Sprintf("node.services[%d].type: required (URN)", i))
		}
	}

	if n.Clocks == nil {
		errs = append(errs, "node.clocks: required (may be empty array)")
	}
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

	if n.Interfaces == nil {
		errs = append(errs, "node.interfaces: required (may be empty array)")
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
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	var n Node
	if err := d.Decode(&n); err != nil {
		return nil, fmt.Errorf("is04: decode node: %w", err)
	}
	if d.More() {
		return nil, fmt.Errorf("is04: decode node: trailing JSON content")
	}
	if err := n.Validate(); err != nil {
		return nil, err
	}
	return &n, nil
}
