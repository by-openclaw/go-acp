package schemas

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"dhs/internal/amwa/codec/jsonschema"
)

// validNode is a minimal v1.3 Node that AMWA's own schema accepts.
// Every negative case below is this fixture with one thing broken, so
// a failure names the break rather than the fixture.
const validNode = `{
  "id": "f47ac10b-58cc-4372-a567-0e02b2c3d479",
  "version": "1700000000:0",
  "label": "dhs lab Node",
  "description": "",
  "tags": {},
  "href": "http://dhs.local:8080/",
  "hostname": "dhs.local",
  "caps": {},
  "api": {
    "versions": ["v1.3"],
    "endpoints": [{"host": "dhs.local", "port": 8080, "protocol": "http"}]
  },
  "services": [],
  "clocks": [],
  "interfaces": [
    {"name": "eth0", "chassis_id": "00-11-22-33-44-55", "port_id": "00-11-22-33-44-66"}
  ]
}`

// TestEverySchemaParsesAndResolves walks every shipped file at every
// minor. A broken $ref or an unparseable schema would otherwise only
// surface the first time a device sent that resource shape.
func TestEverySchemaParsesAndResolves(t *testing.T) {
	for _, ver := range Minors() {
		names, err := Names(ver)
		if err != nil {
			t.Fatalf("%s: %v", ver, err)
		}
		if len(names) == 0 {
			t.Fatalf("%s: no schemas shipped", ver)
		}
		c, err := For(ver)
		if err != nil {
			t.Fatalf("%s: %v", ver, err)
		}
		for _, name := range names {
			// Validating `null` exercises parse + every $ref on the
			// path without asserting anything about the instance.
			err := c.Validate(name, []byte(`null`))
			var ve *jsonschema.ValidationError
			if err != nil && !errors.As(err, &ve) {
				t.Errorf("%s/%s: schema itself is broken: %v", ver, name, err)
				continue
			}
			if ve != nil {
				for _, p := range ve.Problems {
					if strings.Contains(p.Detail, "does not resolve") ||
						strings.Contains(p.Detail, "does not compile") ||
						strings.Contains(p.Detail, jsonschema.ErrUnknownKeyword.Error()) {
						t.Errorf("%s/%s: %s", ver, name, p)
					}
				}
			}
		}
		t.Logf("%s: %d schemas parse, all $refs resolve", ver, len(names))
	}
}

// TestNoUnimplementedKeyword is the guard that keeps this validator
// honest. If AMWA ships a schema using a keyword we do not enforce,
// documents would silently go unchecked — so the validator reports it
// and this test fails rather than letting coverage rot quietly.
func TestNoUnimplementedKeyword(t *testing.T) {
	for _, ver := range Minors() {
		names, _ := Names(ver)
		c, _ := For(ver)
		for _, name := range names {
			for _, probe := range []string{`{}`, `[]`, `""`, `0`, `true`, `null`} {
				err := c.Validate(name, []byte(probe))
				var ve *jsonschema.ValidationError
				if !errors.As(err, &ve) {
					continue
				}
				for _, p := range ve.Problems {
					if strings.Contains(p.Detail, jsonschema.ErrUnknownKeyword.Error()) {
						t.Errorf("%s/%s on %s: %s", ver, name, probe, p)
					}
				}
			}
		}
	}
}

func TestValidNodeAccepted(t *testing.T) {
	if err := Validate("v1.3", "node", []byte(validNode)); err != nil {
		t.Fatalf("AMWA's own schema must accept this Node: %v", err)
	}
}

// TestRejects is the important half: a validator that accepts
// everything is worthless. Each case breaks exactly one rule that
// AMWA's schema states, and names the keyword we expect to catch it.
func TestRejects(t *testing.T) {
	cases := []struct {
		name    string
		mutate  func(m map[string]any)
		keyword string
	}{
		{"missing required id", func(m map[string]any) { delete(m, "id") }, "required"},
		{"missing required version", func(m map[string]any) { delete(m, "version") }, "required"},
		{"id is not a UUID", func(m map[string]any) { m["id"] = "not-a-uuid" }, "pattern"},
		{"version is not <secs>:<nanos>", func(m map[string]any) { m["version"] = "yesterday" }, "pattern"},
		{"label is not a string", func(m map[string]any) { m["label"] = 42 }, "type"},
		{"href is not a URI", func(m map[string]any) { m["href"] = "not a uri" }, "format"},
		{"api.versions is not an array", func(m map[string]any) {
			m["api"] = map[string]any{"versions": "v1.3", "endpoints": []any{}}
		}, "type"},
		{"endpoint protocol outside enum", func(m map[string]any) {
			m["api"] = map[string]any{
				"versions": []any{"v1.3"},
				"endpoints": []any{map[string]any{
					"host": "h", "port": 80, "protocol": "gopher"}},
			}
		}, "enum"},
		// chassis_id is anyOf [MAC pattern, "^.+$", null] — so "ZZ" is
		// LEGAL and only the empty string is not. Worth pinning: our
		// hand-written validator would have rejected "ZZ". The schema
		// is the authority, not our reading of it.
		{"interface chassis_id empty", func(m map[string]any) {
			m["interfaces"] = []any{map[string]any{
				"name": "eth0", "chassis_id": "", "port_id": "00-11-22-33-44-66"}}
		}, "anyOf"},
		{"interface missing port_id", func(m map[string]any) {
			m["interfaces"] = []any{map[string]any{
				"name": "eth0", "chassis_id": "00-11-22-33-44-55"}}
		}, "required"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var m map[string]any
			if err := json.Unmarshal([]byte(validNode), &m); err != nil {
				t.Fatalf("fixture: %v", err)
			}
			tc.mutate(m)
			raw, _ := json.Marshal(m)

			err := Validate("v1.3", "node", raw)
			if err == nil {
				t.Fatalf("must be rejected, was accepted: %s", raw)
			}
			var ve *jsonschema.ValidationError
			if !errors.As(err, &ve) {
				t.Fatalf("want a ValidationError, got %v", err)
			}
			for _, p := range ve.Problems {
				if p.Keyword == tc.keyword {
					return
				}
			}
			t.Fatalf("expected keyword %q to catch this, got %v", tc.keyword, ve.Problems)
		})
	}
}

// TestVersionsAreIsolated: a v1.0 validator can only see v1.0 schemas.
// This is what stops a rule leaking across minors — the failure that
// had us checking a v1.0 Flow for `frame_width`, which v1.0 does not
// define.
func TestVersionsAreIsolated(t *testing.T) {
	v10, _ := Names("v1.0")
	v13, _ := Names("v1.3")
	have10 := map[string]bool{}
	for _, n := range v10 {
		have10[n] = true
	}
	var onlyLater []string
	for _, n := range v13 {
		if !have10[n] {
			onlyLater = append(onlyLater, n)
		}
	}
	if len(onlyLater) == 0 {
		t.Fatal("expected v1.3 to ship schemas v1.0 does not have")
	}
	c10, _ := For("v1.0")
	for _, n := range onlyLater {
		err := c10.Validate(n, []byte(`{}`))
		if err == nil {
			t.Errorf("the v1.0 validator loaded %q, which belongs to a later minor", n)
			continue
		}
		var ve *jsonschema.ValidationError
		if errors.As(err, &ve) {
			t.Errorf("the v1.0 validator resolved %q instead of refusing it", n)
		}
	}
	t.Logf("v1.0 correctly cannot load %d later-minor schemas", len(onlyLater))
}

// TestNeuronV10DeviceWithControls: the regression that started all of
// this. A real EVS Neuron serves `controls` on its v1.0 Device tree.
// Our hand-written v1.0 codec rejected it. AMWA's own v1.0 schema does
// not — which means the device was right and we were wrong.
func TestNeuronV10DeviceWithControls(t *testing.T) {
	raw := []byte(`{
	  "id": "11111111-1111-4111-8111-111111111111",
	  "version": "1700000000:0",
	  "label": "NEURON",
	  "type": "urn:x-nmos:device:generic",
	  "node_id": "22222222-2222-4222-8222-222222222222",
	  "senders": [], "receivers": [],
	  "controls": [
	    {"type": "urn:x-nmos:control:sr-ctrl/v1.0", "href": "http://10.6.255.102:3000/"}
	  ]
	}`)
	if err := Validate("v1.0", "device", raw); err != nil {
		t.Fatalf("AMWA's v1.0 schema accepts this Device; we must too: %v", err)
	}
}
