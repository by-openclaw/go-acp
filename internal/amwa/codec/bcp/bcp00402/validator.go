// Package bcp00402 implements the AMWA BCP-004-02 v1.0.0 Sender
// Capabilities validator.
//
// Spec: https://specs.amwa.tv/bcp-004-02/
//
// Mirror of BCP-004-01 but on Sender bodies — Sender caps are
// advertised under `caps.constraint_sets` and follow the same
// shape rules.
package bcp00402

import (
	"encoding/json"
	"time"

	"dhs/internal/amwa/codec/bcp"
	"dhs/internal/amwa/codec/spec"
)

const (
	SpecID    = "bcp-004-02"
	APIVer    = "v1.0"
	SpecPatch = "v1.0.0"
)

// Validator implements [bcp.Validator] for IS-04 Sender caps.
type Validator struct{}

// New returns a Validator.
func New() Validator { return Validator{} }

func (Validator) SpecID() string    { return SpecID }
func (Validator) APIVer() string    { return APIVer }
func (Validator) SpecPatch() string { return SpecPatch }

func (Validator) HostKind() bcp.Kind { return bcp.KindSender }

// Validate mirrors BCP-004-01 logic on a Sender body.
func (Validator) Validate(payload []byte) []spec.ComplianceEvent {
	var body struct {
		Caps struct {
			ConstraintSets []map[string]any `json:"constraint_sets"`
		} `json:"caps"`
	}
	if err := json.Unmarshal(payload, &body); err != nil {
		return []spec.ComplianceEvent{event("bcp_004_02_decode_error", spec.SeverityError, err.Error())}
	}
	if body.Caps.ConstraintSets == nil {
		return nil
	}
	var out []spec.ComplianceEvent
	for i, set := range body.Caps.ConstraintSets {
		if len(set) == 0 {
			out = append(out, event("bcp_004_02_empty_constraint_set",
				spec.SeverityWarn, "caps.constraint_sets["+itoa(i)+"]: empty"))
		}
	}
	return out
}

func event(code string, sev spec.Severity, detail string) spec.ComplianceEvent {
	return spec.ComplianceEvent{
		SpecID: SpecID, APIVer: APIVer, SpecPatch: SpecPatch,
		Code: code, Severity: sev, Detail: detail, At: time.Now(),
	}
}

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	var b [20]byte
	pos := len(b)
	for i > 0 {
		pos--
		b[pos] = byte('0' + i%10)
		i /= 10
	}
	return string(b[pos:])
}

func init() { bcp.Register(Validator{}) }
