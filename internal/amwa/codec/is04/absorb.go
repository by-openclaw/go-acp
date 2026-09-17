// Decoding a resource from a REAL device, which is not the same thing
// as decoding one from the spec.
//
// Every decoder here used to call DisallowUnknownFields and return an
// error the moment a peer sent a field we did not model. That is the
// right rule for a PRODUCER validating its own output and the wrong
// one for a CONSUMER reading somebody else's device: a vendor
// extension is not a corrupt payload, and a controller that stops dead
// on one cannot read the plant it was bought to control.
//
// It also contradicts the repo's own compliance rule (root CLAUDE.md,
// "Compliance pattern"): the plugin ABSORBS a deviation, keeps
// running, and fires a compliance.Event. Never silently — the event is
// what makes the deviation auditable — but never fatally either.
//
// Measured against an EVS Neuron (BRIDGE 6.7.4) on 2026-08-27: strict
// decoding failed 144 of 176 Flows on an undeclared `caps`, all 176
// Senders at v1.1, and the Node's own `self` on an undeclared
// `ip_addr`. None of those payloads is unreadable; we were refusing to
// read them.

package is04

import (
	"bytes"
	"encoding/json"
	"fmt"
	"reflect"
	"sort"
	"strings"
	"time"

	"dhs/internal/amwa/codec/spec"
)

// UnknownFieldCode is the compliance code raised when a peer sends a
// field the modelled resource does not carry.
const UnknownFieldCode = "nmos_is04_unknown_field"

// decodeAbsorbing unmarshals raw into dst, absorbing fields dst does
// not model and reporting each one.
//
// Returns an error ONLY for input that is not decodable at all —
// malformed JSON, a type mismatch, trailing content. An unknown field
// is a deviation, not a failure.
func decodeAbsorbing(raw []byte, dst any, kind, apiVer string, r spec.Reporter) error {
	// The lenient decode is the one whose result we keep. Unknown
	// fields are simply not written to dst.
	d := json.NewDecoder(bytes.NewReader(raw))
	if err := d.Decode(dst); err != nil {
		return fmt.Errorf("is04: decode %s: %w", kind, err)
	}
	if d.More() {
		return fmt.Errorf("is04: decode %s: trailing JSON content", kind)
	}
	if r == nil {
		return nil
	}
	for _, name := range unknownFields(raw, dst) {
		r.Report(spec.ComplianceEvent{
			SpecID:   "IS-04",
			APIVer:   apiVer,
			Code:     UnknownFieldCode,
			Severity: spec.SeverityInfo,
			Detail: fmt.Sprintf(
				"%s carries %q, which IS-04 %s does not define; absorbed and ignored",
				kind, name, apiVer),
			Resource: kind,
			At:       time.Now(),
		})
	}
	return nil
}

// unknownFields lists the top-level JSON keys that dst has no field
// for, sorted so the report is stable.
//
// Top level only, deliberately. A nested extension is carried inside a
// field we already model and does not change how the resource is read;
// walking the whole tree would turn one honest signal into noise
// proportional to the payload.
func unknownFields(raw []byte, dst any) []string {
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(raw, &obj); err != nil {
		return nil
	}
	known := jsonFieldNames(reflect.TypeOf(dst))
	var out []string
	for k := range obj {
		if !known[k] {
			out = append(out, k)
		}
	}
	sort.Strings(out)
	return out
}

// jsonFieldNames collects every json tag name a struct accepts,
// following embedded structs the way encoding/json does.
func jsonFieldNames(t reflect.Type) map[string]bool {
	out := map[string]bool{}
	collectJSONNames(t, out)
	return out
}

func collectJSONNames(t reflect.Type, out map[string]bool) {
	for t != nil && t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	if t == nil || t.Kind() != reflect.Struct {
		return
	}
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		tag := f.Tag.Get("json")
		if tag == "-" {
			continue
		}
		name, _, _ := strings.Cut(tag, ",")
		if f.Anonymous && name == "" {
			// Embedded: its fields are promoted to this level.
			collectJSONNames(f.Type, out)
			continue
		}
		if name == "" {
			name = f.Name
		}
		out[name] = true
	}
}
