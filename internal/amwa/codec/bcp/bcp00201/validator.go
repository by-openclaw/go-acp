// Package bcp00201 implements the AMWA BCP-002-01 v1.0.0 Natural
// Grouping validator.
//
// Spec: https://specs.amwa.tv/bcp-002-01/
// Tag URN: urn:x-nmos:tag:grouping/v1.0
//
// The BCP requires Node implementations to surface Natural Group
// membership via the `grouping` tag on Source / Sender / Receiver /
// Flow JSON. Tag value is a JSON array of strings, each formatted
// per the NMOS Parameter Registers grouphint entry:
//
//	[<scope>:]<group_name>:<role>[:<role_aux>]
//
// `scope` is `node` or `device`; `group_name` and `role` are
// non-empty UTF-8 strings.
package bcp00201

import (
	"encoding/json"
	"regexp"
	"time"

	"dhs/internal/amwa/codec/bcp"
	"dhs/internal/amwa/codec/spec"
)

const (
	SpecID    = "bcp-002-01"
	APIVer    = "v1.0"
	SpecPatch = "v1.0.0"
	TagURN    = "urn:x-nmos:tag:grouping/v1.0"
)

// grouphintPattern matches `[<scope>:]<group>:<role>[:<aux>]`.
// Empty scope (omit + colon) is allowed.
var grouphintPattern = regexp.MustCompile(
	`^(?:(node|device):)?[^:]+:[^:]+(?::[^:]+)?$`,
)

// Validator implements [bcp.Validator].
type Validator struct{}

// New returns a Validator.
func New() Validator { return Validator{} }

func (Validator) SpecID() string    { return SpecID }
func (Validator) APIVer() string    { return APIVer }
func (Validator) SpecPatch() string { return SpecPatch }

// HostKind reports KindSender — Senders are the most common host,
// but the same tag may live on Source / Receiver / Flow. Callers
// fan resources of any KindFlow / KindSource / KindReceiver through
// this validator too via [bcp.Run] when the resource carries tags.
func (Validator) HostKind() bcp.Kind { return bcp.KindSender }

// Validate inspects an IS-04 resource body. Returns events for
// invalid grouphint entries; empty when the tag is absent (BCP-002-01
// is opt-in — its absence is not a deviation).
func (Validator) Validate(payload []byte) []spec.ComplianceEvent {
	var body struct {
		Tags map[string][]string `json:"tags"`
	}
	if err := json.Unmarshal(payload, &body); err != nil {
		return []spec.ComplianceEvent{newEvent(
			"bcp_002_01_decode_error", spec.SeverityError, err.Error())}
	}
	values, ok := body.Tags[TagURN]
	if !ok {
		return nil
	}
	var out []spec.ComplianceEvent
	for i, v := range values {
		if !grouphintPattern.MatchString(v) {
			out = append(out, newEvent(
				"bcp_002_01_grouphint_malformed",
				spec.SeverityWarn,
				"tags["+TagURN+"]["+itoa(i)+"]="+v))
		}
	}
	return out
}

func newEvent(code string, sev spec.Severity, detail string) spec.ComplianceEvent {
	return spec.ComplianceEvent{
		SpecID:    SpecID,
		APIVer:    APIVer,
		SpecPatch: SpecPatch,
		Code:      code,
		Severity:  sev,
		Detail:    detail,
		At:        time.Now(),
	}
}

func itoa(i int) string {
	const digits = "0123456789"
	if i == 0 {
		return "0"
	}
	var b [20]byte
	pos := len(b)
	for i > 0 {
		pos--
		b[pos] = digits[i%10]
		i /= 10
	}
	return string(b[pos:])
}

func init() { bcp.Register(Validator{}) }
