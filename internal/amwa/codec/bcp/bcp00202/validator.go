// Package bcp00202 implements the AMWA BCP-002-02 v1.0.0 Asset
// Distinguishing Information validator.
//
// Spec: https://specs.amwa.tv/bcp-002-02/
// Tag URN: urn:x-nmos:tag:asset/v1.0
//
// The BCP requires Senders / Receivers to surface manufacturer +
// product + instance metadata via tag values shaped as:
//
//	"<key>=<value>"
//
// where `key` is one of `manufacturer`, `product`, `instance-id`,
// `function`, `name` and `value` is a non-empty UTF-8 string. The
// validator records an event for any malformed tag value.
package bcp00202

import (
	"encoding/json"
	"regexp"
	"time"

	"acp/internal/amwa/codec/bcp"
	"acp/internal/amwa/codec/spec"
)

const (
	SpecID    = "bcp-002-02"
	APIVer    = "v1.0"
	SpecPatch = "v1.0.0"
	TagURN    = "urn:x-nmos:tag:asset/v1.0"
)

// assetTagPattern matches `<key>=<value>` shape with a recognised
// key vocabulary. Keys are spec-defined; future extensions go into
// the NMOS Parameter Registers.
var assetTagPattern = regexp.MustCompile(
	`^(manufacturer|product|instance-id|function|name)=.+$`,
)

// Validator implements [bcp.Validator].
type Validator struct{}

// New returns a Validator.
func New() Validator { return Validator{} }

func (Validator) SpecID() string    { return SpecID }
func (Validator) APIVer() string    { return APIVer }
func (Validator) SpecPatch() string { return SpecPatch }

func (Validator) HostKind() bcp.Kind { return bcp.KindSender }

// Validate inspects an IS-04 resource body. Returns events for
// malformed asset tag values.
func (Validator) Validate(payload []byte) []spec.ComplianceEvent {
	var body struct {
		Tags map[string][]string `json:"tags"`
	}
	if err := json.Unmarshal(payload, &body); err != nil {
		return []spec.ComplianceEvent{event("bcp_002_02_decode_error", spec.SeverityError, err.Error())}
	}
	values, ok := body.Tags[TagURN]
	if !ok {
		return nil
	}
	var out []spec.ComplianceEvent
	for i, v := range values {
		if !assetTagPattern.MatchString(v) {
			out = append(out, event(
				"bcp_002_02_asset_malformed",
				spec.SeverityWarn,
				"tags["+TagURN+"]["+itoa(i)+"]="+v))
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
