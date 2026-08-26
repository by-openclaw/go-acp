package is04

import (
	"encoding/json"
	"strings"
	"testing"

	"dhs/internal/amwa/codec/spec"
)

func TestAtLeast(t *testing.T) {
	cases := []struct {
		have, want string
		got        bool
	}{
		{"v1.0", "v1.0", true},
		{"v1.0", "v1.1", false},
		{"v1.1", "v1.1", true},
		{"v1.2", "v1.1", true},
		{"v1.3", "v1.3", true},
		{"v1.2", "v1.3", false},
		{"v2.0", "v1.3", true},
		{"v1.10", "v1.9", true}, // minor is numeric, not lexical
		// An unparseable version sorts as older, so a typo in Since
		// strips the property rather than leaking it onto the wire.
		{"garbage", "v1.1", false},
	}
	for _, c := range cases {
		if got := atLeast(c.have, c.want); got != c.got {
			t.Errorf("atLeast(%q, %q) = %v, want %v", c.have, c.want, got, c.got)
		}
	}
}

// TestSinceRowsAreWellFormed guards the table itself: a typo in a
// version string silently changes what every minor strips.
func TestSinceRowsAreWellFormed(t *testing.T) {
	known := map[string]bool{"node": true, "device": true, "source": true,
		"flow": true, "sender": true, "receiver": true}
	for kind, rows := range Since {
		if !known[kind] {
			t.Errorf("Since has kind %q, which is not an IS-04 resource", kind)
		}
		seen := map[string]bool{}
		for _, f := range rows {
			if maj, min := parseMinor(f.Since); maj < 0 || min < 0 {
				t.Errorf("%s.%s: Since=%q does not parse as vMAJOR.MINOR", kind, f.Path, f.Since)
			}
			if f.Since == "v1.0" {
				t.Errorf("%s.%s: Since=v1.0 is a no-op — v1.0 is the floor", kind, f.Path)
			}
			if seen[f.Path] {
				t.Errorf("%s.%s: duplicate row", kind, f.Path)
			}
			seen[f.Path] = true
		}
	}
}

// TestLaterThanNarrowsWithMinor: the delta shrinks monotonically as
// the wire minor rises, and is empty at the latest.
func TestLaterThanNarrowsWithMinor(t *testing.T) {
	for kind := range Since {
		prev := len(LaterThan(kind, "v1.0"))
		for _, v := range []string{"v1.1", "v1.2", "v1.3"} {
			n := len(LaterThan(kind, v))
			if n > prev {
				t.Errorf("%s: LaterThan grew from %d to %d at %s", kind, prev, n, v)
			}
			prev = n
		}
		if n := len(LaterThan(kind, APIVersion)); n != 0 {
			t.Errorf("%s: %d rows are still later than the canonical minor %s",
				kind, n, APIVersion)
		}
	}
}

// TestStripLaterThanRemovesExactlyTheDelta walks every kind at every
// minor and asserts the wire carries nothing LaterThan names.
func TestStripLaterThanRemovesExactlyTheDelta(t *testing.T) {
	// One payload per kind carrying EVERY property in that kind's
	// table, so each minor has something to strip.
	for kind, rows := range Since {
		body := map[string]any{"id": "x"}
		for _, f := range rows {
			plant(body, strings.Split(f.Path, "."))
		}
		raw, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("%s: marshal fixture: %v", kind, err)
		}
		for _, ver := range []string{"v1.0", "v1.1", "v1.2", "v1.3"} {
			out, err := StripLaterThan(raw, kind, ver)
			if err != nil {
				t.Fatalf("%s %s: %v", kind, ver, err)
			}
			var doc any
			if err := json.Unmarshal(out, &doc); err != nil {
				t.Fatalf("%s %s: strip produced invalid JSON: %v", kind, ver, err)
			}
			for _, f := range LaterThan(kind, ver) {
				walk(doc, strings.Split(f.Path, "."), func(obj map[string]any, key string) {
					if _, present := obj[key]; present {
						t.Errorf("%s %s wire still carries %s (arrived %s)",
							kind, ver, f.Path, f.Since)
					}
				})
			}
			// and everything at-or-before this minor must survive
			for _, f := range rows {
				if !atLeast(ver, f.Since) {
					continue
				}
				found := false
				walk(doc, strings.Split(f.Path, "."), func(obj map[string]any, key string) {
					if _, present := obj[key]; present {
						found = true
					}
				})
				if !found {
					t.Errorf("%s %s: strip removed %s, which is legal from %s",
						kind, ver, f.Path, f.Since)
				}
			}
		}
	}
}

// TestAbsorbLaterThanReportsAndKeeps is the contract the real device
// forced: reading is tolerant. Nothing is rejected; every deviation
// is named.
func TestAbsorbLaterThanReportsAndKeeps(t *testing.T) {
	for kind, rows := range Since {
		body := map[string]any{"id": "x"}
		for _, f := range rows {
			plant(body, strings.Split(f.Path, "."))
		}
		raw, _ := json.Marshal(body)
		for _, ver := range []string{"v1.0", "v1.1", "v1.2"} {
			var rep spec.SliceReporter
			AbsorbLaterThan(raw, kind, ver, &rep)
			want := len(LaterThan(kind, ver))
			got := rep.Snapshot()
			if len(got) != want {
				t.Errorf("%s %s: reported %d deviations, want %d", kind, ver, len(got), want)
			}
			for _, e := range got {
				if e.Code != LaterMinorFieldCode {
					t.Errorf("%s %s: code = %q", kind, ver, e.Code)
				}
				if e.Severity != spec.SeverityWarn {
					t.Errorf("%s %s: severity = %v, want Warn", kind, ver, e.Severity)
				}
				if e.APIVer != ver || e.Resource != kind {
					t.Errorf("%s %s: event mis-scoped: %+v", kind, ver, e)
				}
			}
		}
	}
}

// TestAbsorbLaterThanNilReporterIsSafe: absorbing must never depend on
// a reporter being wired. The deviation goes unrecorded; the decode
// still proceeds.
func TestAbsorbLaterThanNilReporterIsSafe(t *testing.T) {
	AbsorbLaterThan([]byte(`{"controls":[]}`), "device", "v1.0", nil)
	AbsorbLaterThan([]byte(`not json`), "device", "v1.0", &spec.SliceReporter{})
}

// TestNeuronV10DeviceControlsAbsorbed pins the exact regression: a
// real EVS Neuron serves `controls` on its v1.0 Device tree. Refusing
// that lost the whole Device.
func TestNeuronV10DeviceControlsAbsorbed(t *testing.T) {
	raw := []byte(`{"id":"11111111-1111-4111-8111-111111111111",
	  "version":"1700000000:0","label":"NEURON","type":"urn:x-nmos:device:generic",
	  "node_id":"22222222-2222-4222-8222-222222222222","senders":[],"receivers":[],
	  "controls":[{"type":"urn:x-nmos:control:sr-ctrl/v1.0","href":"http://10.6.255.102:3000/"}]}`)
	var rep spec.SliceReporter
	d, err := ParseDevice(raw, "v1.0", &rep)
	if err != nil {
		t.Fatalf("the Neuron's v1.0 Device must parse: %v", err)
	}
	if d.Label != "NEURON" {
		t.Fatalf("label = %q", d.Label)
	}
	if len(d.Controls) != 1 {
		t.Fatalf("the absorbed `controls` must still reach the caller: %+v", d.Controls)
	}
	events := rep.Snapshot()
	if len(events) != 1 || !strings.Contains(events[0].Detail, "controls") {
		t.Fatalf("absorbing `controls` must be reported, got %+v", events)
	}
}

// plant creates the nested objects/arrays a Since path implies and
// sets a placeholder at the leaf.
func plant(obj map[string]any, segs []string) {
	if len(segs) == 1 {
		obj[segs[0]] = "planted"
		return
	}
	key, fanOut := strings.CutSuffix(segs[0], "[]")
	if fanOut {
		arr, _ := obj[key].([]any)
		if len(arr) == 0 {
			arr = []any{map[string]any{}}
			obj[key] = arr
		}
		for _, el := range arr {
			if em, ok := el.(map[string]any); ok {
				plant(em, segs[1:])
			}
		}
		return
	}
	child, ok := obj[key].(map[string]any)
	if !ok {
		child = map[string]any{}
		obj[key] = child
	}
	plant(child, segs[1:])
}
