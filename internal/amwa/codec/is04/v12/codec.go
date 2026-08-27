// Package v12 is the AMWA NMOS IS-04 v1.2.2 wire codec.
//
// SELF-CONTAINED BY DESIGN. Everything v1.2-specific lives in this
// file: its drop table, its strip helper, its identity. Nothing is
// shared with the other minors, and the duplication between them is
// deliberate — a change to v1.2 must be incapable of altering how any
// other version behaves.
//
// It holds NO validation rules. Every rule comes from AMWA's own
// v1.2.2 JSON Schemas, shipped verbatim in is04/schemas/ and applied by
// [schemas.Validate]. Hand-written rules are how this codec drifted:
// a non-empty check on label/description that no schema states, a
// v1.0 Flow failed for `frame_width` which v1.0 does not define, a
// v1.0 Device refused for `controls` which the v1.0 schema permits.
//
// The two directions have opposite postures, and that is the point:
//
//	Encode  marshal -> drop what v1.2 lacks -> schema check is FATAL
//	Decode  parse tolerantly -> schema deviations become EVENTS
//
// We must not emit a payload AMWA would reject. But refusing to READ
// one costs the operator the whole resource and tells them nothing
// actionable — so a peer's deviation is absorbed and reported, per
// the repo-wide compliance posture in the root CLAUDE.md.
package v12

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"dhs/internal/amwa/codec/is04"
	"dhs/internal/amwa/codec/is04/schemas"
	"dhs/internal/amwa/codec/jsonschema"
	"dhs/internal/amwa/codec/spec"
)

// APIVer is the wire URL minor this codec serves.
const APIVer = "v1.2"

// SpecPatch is the AMWA spec revision it strictly complies with —
// the latest patch within v1.2, and the schema set in is04/schemas/v1.2.2.
const SpecPatch = "v1.2.2"

// drop names the properties a v1.2 payload MUST NOT carry, because
// IS-04 did not define them until a later minor.
//
// Paths are dot-separated; a segment ending in "[]" applies to every
// element of that array. This table is v1.2's alone — the other
// minors keep their own, even where the entries coincide.
var drop = map[string][]string{
	"node":     {"interfaces[].attached_network_device", "services[].authorization", "api.endpoints[].authorization"},
	"device":   {"controls[].authorization"},
	"source":   nil,
	"flow":     nil,
	"sender":   nil,
	"receiver": {"caps.constraint_sets", "caps.version"},
}

// Codec implements [is04.Codec] for IS-04 wire minor v1.2.
//
// Stateless and safe for concurrent use.
type Codec struct {
	// Reporter receives deviations found while DECODING a peer's
	// payload. Optional: the zero Codec absorbs silently.
	Reporter spec.Reporter
}

// New returns a Codec — equivalent to a zero-value Codec{}.
func New() Codec { return Codec{} }

// SpecID returns the AMWA NMOS catalogue slug for IS-04.
func (Codec) SpecID() string { return is04.SpecID }

// APIVer returns the wire URL minor — "v1.2".
func (Codec) APIVer() string { return APIVer }

// SpecPatch returns the latest patch release — "v1.2.2".
func (Codec) SpecPatch() string { return SpecPatch }

// EncodeNode renders a Node onto the v1.2 wire.
func (Codec) EncodeNode(n is04.Node) ([]byte, error) { return encode("node", n) }

// DecodeNode parses a Node served on a v1.2 tree.
func (c Codec) DecodeNode(raw []byte) (is04.Node, error) {
	n, err := is04.ParseNode(raw, APIVer, c.Reporter)
	if err != nil {
		return is04.Node{}, err
	}
	c.reportDeviations("node", raw)
	return *n, nil
}

// ValidateNode reports whether n can be served on the v1.2 wire.
func (Codec) ValidateNode(n is04.Node) error {
	_, err := encode("node", n)
	return err
}

// EncodeDevice renders a Device onto the v1.2 wire.
func (Codec) EncodeDevice(d is04.Device) ([]byte, error) { return encode("device", d) }

// DecodeDevice parses a Device served on a v1.2 tree.
func (c Codec) DecodeDevice(raw []byte) (is04.Device, error) {
	d, err := is04.ParseDevice(raw, APIVer, c.Reporter)
	if err != nil {
		return is04.Device{}, err
	}
	c.reportDeviations("device", raw)
	return *d, nil
}

// ValidateDevice reports whether d can be served on the v1.2 wire.
func (Codec) ValidateDevice(d is04.Device) error {
	_, err := encode("device", d)
	return err
}

// EncodeSource renders a Source onto the v1.2 wire.
func (Codec) EncodeSource(s is04.Source) ([]byte, error) { return encode("source", s) }

// DecodeSource parses a Source served on a v1.2 tree.
func (c Codec) DecodeSource(raw []byte) (is04.Source, error) {
	s, err := is04.ParseSource(raw, APIVer, c.Reporter)
	if err != nil {
		return is04.Source{}, err
	}
	c.reportDeviations("source", raw)
	return *s, nil
}

// ValidateSource reports whether s can be served on the v1.2 wire.
func (Codec) ValidateSource(s is04.Source) error {
	_, err := encode("source", s)
	return err
}

// EncodeFlow renders a Flow onto the v1.2 wire.
func (Codec) EncodeFlow(f is04.Flow) ([]byte, error) { return encode("flow", f) }

// DecodeFlow parses a Flow served on a v1.2 tree.
func (c Codec) DecodeFlow(raw []byte) (is04.Flow, error) {
	f, err := is04.ParseFlow(raw, APIVer, c.Reporter)
	if err != nil {
		return is04.Flow{}, err
	}
	c.reportDeviations("flow", raw)
	return *f, nil
}

// ValidateFlow reports whether f can be served on the v1.2 wire.
func (Codec) ValidateFlow(f is04.Flow) error {
	_, err := encode("flow", f)
	return err
}

// EncodeSender renders a Sender onto the v1.2 wire.
func (Codec) EncodeSender(s is04.Sender) ([]byte, error) { return encode("sender", s) }

// DecodeSender parses a Sender served on a v1.2 tree.
func (c Codec) DecodeSender(raw []byte) (is04.Sender, error) {
	s, err := is04.ParseSender(raw, APIVer, c.Reporter)
	if err != nil {
		return is04.Sender{}, err
	}
	c.reportDeviations("sender", raw)
	return *s, nil
}

// ValidateSender reports whether s can be served on the v1.2 wire.
func (Codec) ValidateSender(s is04.Sender) error {
	_, err := encode("sender", s)
	return err
}

// EncodeReceiver renders a Receiver onto the v1.2 wire.
func (Codec) EncodeReceiver(r is04.Receiver) ([]byte, error) { return encode("receiver", r) }

// DecodeReceiver parses a Receiver served on a v1.2 tree.
func (c Codec) DecodeReceiver(raw []byte) (is04.Receiver, error) {
	r, err := is04.ParseReceiver(raw, APIVer, c.Reporter)
	if err != nil {
		return is04.Receiver{}, err
	}
	c.reportDeviations("receiver", raw)
	return *r, nil
}

// ValidateReceiver reports whether r can be served on the v1.2 wire.
func (Codec) ValidateReceiver(r is04.Receiver) error {
	_, err := encode("receiver", r)
	return err
}

// encode is the one egress path: marshal, drop what v1.2 does not
// define, then check the result against AMWA's own v1.2.2 schema.
//
// The schema check is FATAL here. Emitting a payload AMWA would
// reject is our bug, and the AMWA test suite fails the Node for it.
func encode(kind string, x any) ([]byte, error) {
	raw, err := json.Marshal(x)
	if err != nil {
		return nil, fmt.Errorf("is04 %s: marshal %s: %w", APIVer, kind, err)
	}
	if raw, err = stripPaths(raw, drop[kind]); err != nil {
		return nil, err
	}
	if err := schemas.Validate(APIVer, kind, raw); err != nil {
		return nil, err
	}
	return json.MarshalIndent(json.RawMessage(raw), "", "  ")
}

// reportDeviations checks a peer's payload against AMWA's v1.2.2 schema
// and records every failure as a compliance event.
//
// Deliberately NOT fatal — see the package doc.
func (c Codec) reportDeviations(kind string, raw []byte) {
	if c.Reporter == nil {
		return
	}
	err := schemas.Validate(APIVer, kind, raw)
	if err == nil {
		return
	}
	var ve *jsonschema.ValidationError
	if !errors.As(err, &ve) {
		c.fire(kind, err.Error())
		return
	}
	for _, p := range ve.Problems {
		c.fire(kind, p.String())
	}
}

func (c Codec) fire(kind, detail string) {
	c.Reporter.Report(spec.ComplianceEvent{
		SpecID:    is04.SpecID,
		APIVer:    APIVer,
		SpecPatch: SpecPatch,
		Code:      "nmos_is04_schema_deviation",
		Severity:  spec.SeverityWarn,
		Detail:    fmt.Sprintf("peer %s does not match AMWA %s %s: %s", kind, "IS-04", SpecPatch, detail),
		Resource:  kind,
		At:        time.Now(),
	})
}

// stripPaths removes each dotted path from a JSON object. A segment
// ending in "[]" fans out across that array, so nested and top-level
// properties are the same code path.
//
// Private to v12 on purpose: the other minors carry their own copy.
func stripPaths(raw []byte, paths []string) ([]byte, error) {
	if len(paths) == 0 {
		return raw, nil
	}
	var doc any
	if err := json.Unmarshal(raw, &doc); err != nil {
		return nil, fmt.Errorf("is04 %s: strip: %w", APIVer, err)
	}
	for _, p := range paths {
		remove(doc, strings.Split(p, "."))
	}
	return json.Marshal(doc)
}

func remove(node any, segs []string) {
	obj, ok := node.(map[string]any)
	if !ok || len(segs) == 0 {
		return
	}
	if len(segs) == 1 {
		delete(obj, segs[0])
		return
	}
	key, fanOut := strings.CutSuffix(segs[0], "[]")
	child, present := obj[key]
	if !present {
		return
	}
	if !fanOut {
		remove(child, segs[1:])
		return
	}
	arr, ok := child.([]any)
	if !ok {
		return
	}
	for _, el := range arr {
		remove(el, segs[1:])
	}
}

func init() {
	is04.Register(Codec{})
}
