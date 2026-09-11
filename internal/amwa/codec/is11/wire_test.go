package is11_test

// The strict-decoder arms and the encode-side validation refusals:
// a wire body that IS-11's schemas reject must fail here with an error
// naming the surface, and an in-memory value that violates an enum
// must never reach the wire.

import (
	"strings"
	"testing"

	"dhs/internal/amwa/codec/is11"
)

const (
	goodInput  = `{"id":"i","version":"1:0","label":"","description":"","tags":{},"base_edid_support":false,"connected":true,"edid_support":false,"status":{"state":"signal_present"},"device_id":"d"}`
	goodOutput = `{"id":"o","version":"1:0","label":"","description":"","tags":{},"connected":true,"edid_support":false,"status":{"state":"default_signal"},"device_id":"d"}`
	goodActive = `{"constraint_sets":[{"urn:x-nmos:cap:format:media_type":{"enum":["video/raw"]}}]}`
)

func TestDecodeRejectsMalformedWire(t *testing.T) {
	cases := []struct {
		name    string
		raw     string
		wantErr string
	}{
		{"input unknown member", `{"id":"i","status":{"state":"no_signal"},"bogus":1}`, "decode input"},
		{"input trailing JSON", goodInput + ` {}`, "trailing JSON"},
		{"input not JSON", `{`, "decode input"},
		{"input bad state", `{"id":"i","status":{"state":"default_signal"}}`, "input status state"},
		{"output unknown member", `{"id":"o","status":{"state":"no_signal"},"bogus":1}`, "decode output"},
		{"output trailing JSON", goodOutput + ` []`, "trailing JSON"},
		{"output bad state", `{"id":"o","status":{"state":"awaiting_signal"}}`, "output status state"},
		{"active unknown member", `{"constraint_sets":[],"bogus":1}`, "decode active constraints"},
		{"active trailing JSON", goodActive + ` 1`, "trailing JSON"},
		{"active missing array", `{}`, "constraint_sets is required"},
		{"active non-cap key", `{"constraint_sets":[{"urn:x-vendor:thing":1}]}`, "not a urn:x-nmos:cap: URN"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var err error
			switch {
			case strings.HasPrefix(tc.name, "input"):
				_, err = is11.DecodeInput([]byte(tc.raw))
			case strings.HasPrefix(tc.name, "output"):
				_, err = is11.DecodeOutput([]byte(tc.raw))
			default:
				_, err = is11.DecodeActiveConstraints([]byte(tc.raw))
			}
			if err == nil {
				t.Fatalf("decode accepted %s", tc.raw)
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("error = %q, want it to mention %q", err, tc.wantErr)
			}
		})
	}
}

func TestEncodeRefusesInvalidValues(t *testing.T) {
	if _, err := is11.EncodeInput(is11.Input{Status: is11.Status{State: "compliant_stream"}}); err == nil {
		t.Error("EncodeInput must refuse a receiver state on an input")
	}
	if _, err := is11.EncodeOutput(is11.Output{Status: is11.Status{State: "constrained"}}); err == nil {
		t.Error("EncodeOutput must refuse a sender state on an output")
	}
	if _, err := is11.EncodeActiveConstraints(is11.ActiveConstraints{}); err == nil {
		t.Error("EncodeActiveConstraints must refuse a nil constraint_sets")
	}
	if _, err := is11.EncodeActiveConstraints(is11.ActiveConstraints{ConstraintSets: []is11.ConstraintSet{{}}}); err == nil {
		t.Error("EncodeActiveConstraints must refuse an empty constraint set")
	}
}

func TestDecodeAcceptsCanonicalWire(t *testing.T) {
	in, err := is11.DecodeInput([]byte(goodInput))
	if err != nil || in.Status.State != is11.InputSignalPresent {
		t.Fatalf("DecodeInput = %+v, %v", in, err)
	}
	out, err := is11.DecodeOutput([]byte(goodOutput))
	if err != nil || out.Status.State != is11.OutputDefaultSignal {
		t.Fatalf("DecodeOutput = %+v, %v", out, err)
	}
	a, err := is11.DecodeActiveConstraints([]byte(goodActive))
	if err != nil || len(a.ConstraintSets) != 1 {
		t.Fatalf("DecodeActiveConstraints = %+v, %v", a, err)
	}
}

func TestVersionSelection(t *testing.T) {
	if got := is11.AllCodecs(); len(got) != 1 || got[0].APIVer() != "v1.0" {
		t.Fatalf("AllCodecs = %v, want the single v1.0 codec", got)
	}
	c, err := is11.SelectHighest([]string{"v0.9", "v1.0"})
	if err != nil || c.APIVer() != "v1.0" {
		t.Fatalf("SelectHighest = %v, %v", c, err)
	}
	if _, err := is11.SelectHighest([]string{"v2.0"}); err == nil {
		t.Error("a peer with no common version must be an error, never a downgrade")
	}
}

// Registering a codec that claims another spec is an init-time bug;
// the panic is the only place to catch it before the wrong codec
// starts answering IS-11 traffic.
func TestRegisterRejectsForeignSpecID(t *testing.T) {
	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("Register must panic on a foreign SpecID")
		}
		if msg, _ := r.(string); !strings.Contains(msg, "is-04") {
			t.Fatalf("panic = %v, want it to name the foreign SpecID", r)
		}
	}()
	is11.Register(foreignCodec{})
}

type foreignCodec struct{ is11.Codec }

func (foreignCodec) SpecID() string    { return "is-04" }
func (foreignCodec) APIVer() string    { return "v1.0" }
func (foreignCodec) SpecPatch() string { return "v1.0.0" }
