// Package bcp00802 implements the AMWA BCP-008-02 v1.0.0 NMOS
// Sender Status Monitoring feature set — mirror of BCP-008-01 on
// the Sender side.
package bcp00802

import (
	"encoding/json"
	"time"

	"dhs/internal/amwa/codec/bcp"
	"dhs/internal/amwa/codec/ms05"
	"dhs/internal/amwa/codec/spec"
)

const (
	SpecID    = "bcp-008-02"
	APIVer    = "v1.0"
	SpecPatch = "v1.0.0"
)

// NcSenderMonitorClassID is the canonical classId for the
// NcSenderMonitor feature-set class.
//
// Inheritance: NcObject(1) -> NcWorker(1.2) -> NcStatusMonitor(1.2.2)
//             -> NcSenderMonitor(1.2.2.2).
var NcSenderMonitorClassID = ms05.NcClassId{1, 2, 2, 2}

// Validator implements [bcp.Validator] for an MS-05-02 class
// fingerprint payload.
type Validator struct{}

// New returns a Validator.
func New() Validator { return Validator{} }

func (Validator) SpecID() string    { return SpecID }
func (Validator) APIVer() string    { return APIVer }
func (Validator) SpecPatch() string { return SpecPatch }

func (Validator) HostKind() bcp.Kind { return bcp.KindMS05Class }

func (Validator) Validate(payload []byte) []spec.ComplianceEvent {
	var body struct {
		ClassID ms05.NcClassId `json:"classId"`
		Name    string         `json:"name"`
	}
	if err := json.Unmarshal(payload, &body); err != nil {
		return []spec.ComplianceEvent{event("bcp_008_02_decode_error", spec.SeverityError, err.Error())}
	}
	if body.Name != "NcSenderMonitor" {
		return nil
	}
	if !classIDMatches(body.ClassID, NcSenderMonitorClassID) {
		return []spec.ComplianceEvent{event(
			"bcp_008_02_class_id_mismatch",
			spec.SeverityError,
			"NcSenderMonitor classId mismatch")}
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
