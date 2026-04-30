package is09

import (
	"bytes"
	"encoding/json"
	"fmt"
)

// APIVersion is the v1.0 wire version this codec implements.
const APIVersion = "v1.0"

// IndexResponse is the GET /x-nmos/system/v1.0/ payload — exactly one
// element per the RAML schema.
type IndexResponse []string

// IndexValue is the only valid IndexResponse content per spec
// (`schemas/with-refs/system-api-base.html`).
const IndexValue = "global/"

// Global is the IS-09 /global resource. Layout matches
// https://specs.amwa.tv/is-09/releases/v1.0.0/APIs/schemas/with-refs/global.html
// (which is allOf [resource_core.json, IS-09 globals]).
type Global struct {
	// resource_core.json — every field required.
	ID          string              `json:"id"`
	Version     string              `json:"version"`
	Label       string              `json:"label"`
	Description string              `json:"description"`
	Tags        map[string][]string `json:"tags"`

	// IS-09 globals — every leaf field except syslog/syslogv2 is required.
	IS04 IS04Config `json:"is04"`
	PTP  PTPConfig  `json:"ptp"`

	// Optional log-forwarding endpoints. Pointer so we can distinguish
	// "absent" from "present-with-zero-port".
	SyslogV2 *SyslogConfig `json:"syslogv2,omitempty"`
	Syslog   *SyslogConfig `json:"syslog,omitempty"`
}

// IS04Config carries the IS-04 heartbeat interval used by Nodes when
// re-registering.
type IS04Config struct {
	HeartbeatInterval int `json:"heartbeat_interval"`
}

// PTPConfig carries IEEE 1588-2008 (PTPv2) constants.
type PTPConfig struct {
	AnnounceReceiptTimeout int `json:"announce_receipt_timeout"`
	DomainNumber           int `json:"domain_number"`
}

// SyslogConfig is shared between syslogv2 (TLS, RFC 5424/5425) and
// syslog v1 (UDP, RFC 5424/5426). Both fields optional per spec.
type SyslogConfig struct {
	Hostname string `json:"hostname,omitempty"`
	Port     int    `json:"port,omitempty"`
}

// IndexBody returns the canonical /x-nmos/system/v1.0/ index payload.
func IndexBody() IndexResponse {
	return IndexResponse{IndexValue}
}

// Encode marshals g to spec-compliant JSON. The returned bytes are
// indented for human inspection; use EncodeCompact for wire emission.
func (g *Global) Encode() ([]byte, error) {
	if err := g.Validate(); err != nil {
		return nil, fmt.Errorf("is09: encode: %w", err)
	}
	return json.MarshalIndent(g, "", "  ")
}

// EncodeCompact marshals g without indentation (single-line). Used
// when emitting on the wire.
func (g *Global) EncodeCompact() ([]byte, error) {
	if err := g.Validate(); err != nil {
		return nil, fmt.Errorf("is09: encode: %w", err)
	}
	return json.Marshal(g)
}

// Decode parses raw JSON into a Global and validates it. Unknown keys
// (any field not in the spec schema) are rejected via
// json.Decoder.DisallowUnknownFields per the no-workaround rule —
// peers that emit extras must be flagged, not silently absorbed.
func Decode(raw []byte) (*Global, error) {
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	var g Global
	if err := d.Decode(&g); err != nil {
		return nil, fmt.Errorf("is09: decode: %w", err)
	}
	if d.More() {
		return nil, fmt.Errorf("is09: decode: trailing JSON content")
	}
	if err := g.Validate(); err != nil {
		return nil, fmt.Errorf("is09: decode: %w", err)
	}
	return &g, nil
}

// DecodeIndex parses the /x-nmos/system/v1.0/ index response and
// confirms the only allowed payload shape.
func DecodeIndex(raw []byte) (IndexResponse, error) {
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	var idx IndexResponse
	if err := d.Decode(&idx); err != nil {
		return nil, fmt.Errorf("is09: decode index: %w", err)
	}
	if len(idx) != 1 || idx[0] != IndexValue {
		return nil, fmt.Errorf("is09: decode index: expected exactly [%q], got %v", IndexValue, idx)
	}
	return idx, nil
}
