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
	"fmt"
	"sort"
	"strings"
	"time"

	"dhs/internal/amwa/codec/bcp"
	"dhs/internal/amwa/codec/registers"
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
				spec.SeverityWarn, fmt.Sprintf("caps.constraint_sets[%d]: empty", i)))
			continue
		}
		// Every x-nmos cap parameter URN must be in the AMWA
		// capabilities register (#851). A urn:x-nmos:cap:* not in any
		// register is a vendor that invented a capability — a
		// controller filtering against it silently drops the receiver.
		// Vendor URNs outside the urn:x-nmos: namespace are permitted
		// (BCP-004-01 §3) and not flagged.
		for _, urn := range sortedParamURNs(set) {
			if urn == "" || !strings.HasPrefix(urn, "urn:x-nmos:cap:") {
				continue
			}
			if !registers.Known(urn) {
				out = append(out, event("bcp_004_01_unknown_cap_urn", spec.SeverityWarn,
					fmt.Sprintf("caps.constraint_sets[%d]: %q is not in the AMWA capabilities register", i, urn)))
			}
		}
	}
	return out
}

// sortedParamURNs returns a constraint set's parameter keys (its URNs)
// in a stable order — meta keys and cap URNs alike; the caller filters.
func sortedParamURNs(set map[string]any) []string {
	keys := make([]string, 0, len(set))
	for k := range set {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func event(code string, sev spec.Severity, detail string) spec.ComplianceEvent {
	return spec.ComplianceEvent{
		SpecID: SpecID, APIVer: APIVer, SpecPatch: SpecPatch,
		Code: code, Severity: sev, Detail: detail, At: time.Now(),
	}
}

func init() { bcp.Register(Validator{}) }
