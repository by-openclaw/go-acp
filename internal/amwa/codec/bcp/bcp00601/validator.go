// Package bcp00601 implements the AMWA BCP-006-01 v1.0.0 NMOS With
// JPEG XS validator.
//
// Spec: https://specs.amwa.tv/bcp-006-01/
//
// Constraint: a JPEG XS Flow MUST advertise media_type
// "video/jxsv". Sender / Receiver carrying that Flow inherit the
// transport-params constraint set defined in BCP-006-01 §6.
package bcp00601

import (
	"encoding/json"
	"time"

	"acp/internal/amwa/codec/bcp"
	"acp/internal/amwa/codec/spec"
)

const (
	SpecID         = "bcp-006-01"
	APIVer         = "v1.0"
	SpecPatch      = "v1.0.0"
	JPEGXSMediaType = "video/jxsv"
)

// Validator implements [bcp.Validator] for IS-04 Flow.
type Validator struct{}

// New returns a Validator.
func New() Validator { return Validator{} }

func (Validator) SpecID() string    { return SpecID }
func (Validator) APIVer() string    { return APIVer }
func (Validator) SpecPatch() string { return SpecPatch }

func (Validator) HostKind() bcp.Kind { return bcp.KindFlow }

// Validate inspects a Flow body. Returns events when a Flow that
// claims JPEG XS via format URN advertises a wrong media_type, or
// when a flow with media_type=video/jxsv does not declare the
// expected JPEG XS format URN. Either case is a BCP-006-01 deviation.
func (Validator) Validate(payload []byte) []spec.ComplianceEvent {
	var body struct {
		Format    string `json:"format"`
		MediaType string `json:"media_type"`
	}
	if err := json.Unmarshal(payload, &body); err != nil {
		return []spec.ComplianceEvent{event("bcp_006_01_decode_error", spec.SeverityError, err.Error())}
	}
	if body.MediaType == JPEGXSMediaType && body.Format != "urn:x-nmos:format:video" {
		return []spec.ComplianceEvent{event(
			"bcp_006_01_format_mismatch",
			spec.SeverityWarn,
			"flow.media_type="+JPEGXSMediaType+" but flow.format="+body.Format)}
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
