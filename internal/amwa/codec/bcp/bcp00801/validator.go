// Package bcp00801 implements the AMWA BCP-008-01 v1.0.0 NMOS
// Receiver Status Monitoring feature set.
//
// Spec: https://specs.amwa.tv/bcp-008-01/
//
// BCP-008-01 layers an MS-05-02 NcReceiverMonitor class onto each
// Receiver via the IS-12 control endpoint. The class identifier is
// fixed by the spec; the validator inspects an MS-05-02 class
// fingerprint payload and confirms the classId matches the
// NcReceiverMonitor lineage.
package bcp00801

import (
	"encoding/json"
	"time"

	"dhs/internal/amwa/codec/bcp"
	"dhs/internal/amwa/codec/ms05"
	"dhs/internal/amwa/codec/spec"
)

const (
	SpecID    = "bcp-008-01"
	APIVer    = "v1.0"
	SpecPatch = "v1.0.0"
)

// NcReceiverMonitorClassID is the canonical classId for the
// NcReceiverMonitor feature-set class — registered into the
// MS-05-02 framework registry by the BCP-008-01 spec text §4.
//
// Inheritance: NcObject(1) -> NcWorker(1.2) -> NcStatusMonitor(1.2.2)
//
//	-> NcReceiverMonitor(1.2.2.1).
var NcReceiverMonitorClassID = ms05.NcClassId{1, 2, 2, 1}

// Validator implements [bcp.Validator] for an MS-05-02 class
// fingerprint payload.
type Validator struct{}

// New returns a Validator.
func New() Validator { return Validator{} }

func (Validator) SpecID() string    { return SpecID }
func (Validator) APIVer() string    { return APIVer }
func (Validator) SpecPatch() string { return SpecPatch }

func (Validator) HostKind() bcp.Kind { return bcp.KindMS05Class }

// Validate inspects a class fingerprint payload — typically an
// NcClassDescriptor body — and confirms the classId aligns with the
// NcReceiverMonitor class lineage.
func (Validator) Validate(payload []byte) []spec.ComplianceEvent {
	var body struct {
		ClassID ms05.NcClassId `json:"classId"`
		Name    string         `json:"name"`
	}
	if err := json.Unmarshal(payload, &body); err != nil {
		return []spec.ComplianceEvent{event("bcp_008_01_decode_error", spec.SeverityError, err.Error())}
	}
	// Only validate class descriptors that claim NcReceiverMonitor.
	if body.Name != "NcReceiverMonitor" {
		return nil
	}
	if !classIDMatches(body.ClassID, NcReceiverMonitorClassID) {
		return []spec.ComplianceEvent{event(
			"bcp_008_01_class_id_mismatch",
			spec.SeverityError,
			"NcReceiverMonitor classId mismatch")}
	}
	return nil
}

func classIDMatches(got, want ms05.NcClassId) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}

func event(code string, sev spec.Severity, detail string) spec.ComplianceEvent {
	return spec.ComplianceEvent{
		SpecID: SpecID, APIVer: APIVer, SpecPatch: SpecPatch,
		Code: code, Severity: sev, Detail: detail, At: time.Now(),
	}
}

func init() { bcp.Register(Validator{}) }
