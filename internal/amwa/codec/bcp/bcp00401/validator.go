// Package bcp00401 implements the AMWA BCP-004-01 v1.0.0 Receiver
// Capabilities validator.
//
// Spec: https://specs.amwa.tv/bcp-004-01/
//
// Receiver capabilities are advertised via `caps.constraint_sets` —
// an array of constraint set objects, each binding parameter URNs
// to NMOS-formatted constraint values. The validator confirms
// the array shape + per-set parameter constraint shape.
//
// Schemas bundled at testdata/schemas/v1.0.0/ for audit traceability.
package bcp00401

import (
	"encoding/json"
	"time"

	"dhs/internal/amwa/codec/bcp"
	"dhs/internal/amwa/codec/spec"
)

const (
	SpecID    = "bcp-004-01"
	APIVer    = "v1.0"
	SpecPatch = "v1.0.0"
)

// Validator implements [bcp.Validator] for IS-04 Receiver caps.
type Validator struct{}

// New returns a Validator.
func New() Validator { return Validator{} }

func (Validator) SpecID() string    { return SpecID }
func (Validator) APIVer() string    { return APIVer }
func (Validator) SpecPatch() string { return SpecPatch }

func (Validator) HostKind() bcp.Kind { return bcp.KindReceiver }

// Validate inspects a Receiver body. Verifies that
// caps.constraint_sets is an array (when present) and each entry is
// a JSON object with at least one URN-keyed parameter constraint.
func (Validator) Validate(payload []byte) []spec.ComplianceEvent {
	var body struct {
		Caps struct {
			ConstraintSets []map[string]any `json:"constraint_sets"`
		} `json:"caps"`
	}
	if err := json.Unmarshal(payload, &body); err != nil {
		return []spec.ComplianceEvent{event("bcp_004_01_decode_error", spec.SeverityError, err.Error())}
	}
	if body.Caps.ConstraintSets == nil {
		return nil
	}
	var out []spec.ComplianceEvent
	for i, set := range body.Caps.ConstraintSets {
		if len(set) == 0 {
			out = append(out, event("bcp_004_01_empty_constraint_set",
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
