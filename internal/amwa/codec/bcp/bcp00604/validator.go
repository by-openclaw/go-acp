// Package bcp00604 implements the AMWA BCP-006-04 v1.0.0 NMOS
// Support for MPEG Transport Streams validator.
//
// Spec: https://specs.amwa.tv/bcp-006-04/
//
// Constraint: an MPEG-TS Flow MUST advertise media_type
// "video/MP2T" and format URN
// "urn:x-nmos:format:mux".
package bcp00604

import (
	"encoding/json"
	"time"

	"dhs/internal/amwa/codec/bcp"
	"dhs/internal/amwa/codec/spec"
)

const (
	SpecID          = "bcp-006-04"
	APIVer          = "v1.0"
	SpecPatch       = "v1.0.0"
	MPEGTSMediaType = "video/MP2T"
	MuxFormatURN    = "urn:x-nmos:format:mux"
)

// Validator implements [bcp.Validator] for IS-04 Flow.
type Validator struct{}

// New returns a Validator.
func New() Validator { return Validator{} }

func (Validator) SpecID() string    { return SpecID }
func (Validator) APIVer() string    { return APIVer }
func (Validator) SpecPatch() string { return SpecPatch }

func (Validator) HostKind() bcp.Kind { return bcp.KindFlow }

// Validate inspects a Flow body. Returns an event when a Flow
// advertises media_type=video/MP2T but format URN is not the mux
// URN — a BCP-006-04 deviation.
func (Validator) Validate(payload []byte) []spec.ComplianceEvent {
	var body struct {
		Format    string `json:"format"`
		MediaType string `json:"media_type"`
	}
	if err := json.Unmarshal(payload, &body); err != nil {
		return []spec.ComplianceEvent{event("bcp_006_04_decode_error", spec.SeverityError, err.Error())}
	}
	if body.MediaType == MPEGTSMediaType && body.Format != MuxFormatURN {
		return []spec.ComplianceEvent{event(
			"bcp_006_04_format_mismatch",
			spec.SeverityWarn,
			"flow.media_type="+MPEGTSMediaType+" but flow.format="+body.Format)}
	}
	return nil
}

func event(code string, sev spec.Severity, detail string) spec.ComplianceEvent {
	return spec.ComplianceEvent{
		SpecID: SpecID, APIVer: APIVer, SpecPatch: SpecPatch,
		Code: code, Severity: sev, Detail: detail, At: time.Now(),
	}
}

func init() { bcp.Register(Validator{}) }
