package provider

// DhsGainControl: the constraints surface is DECLARED (catalogue) and
// ENFORCED (writes + restore) — the pair the AMWA MS-05/IS-14 suites
// verify (test_ms05_22..26, IS-14 test_27).

import (
	"encoding/json"
	"strings"
	"testing"

	"dhs/internal/amwa/codec/is12"
	"dhs/internal/amwa/codec/ms05"
)

func TestVendorGainCatalogue(t *testing.T) {
	registerVendorModels()

	cls, ok := ms05.StandardClass(vendorClassID)
	if !ok {
		t.Fatal("DhsGainControl is not in the class catalogue")
	}
	if cls.Name != vendorClassName || len(cls.Properties) != 2 || len(cls.Methods) != 1 {
		t.Errorf("class descriptor = %+v", cls)
	}
	if _, ok := ms05.StandardDatatype(vendorDatatypeName); !ok {
		t.Fatal("DhsGainDb is not in the datatype catalogue")
	}
	// The flattened descriptor must inherit NcWorker + NcObject.
	flat, ok := ms05.FlattenedClass(vendorClassID)
	if !ok {
		t.Fatal("FlattenedClass(vendor)")
	}
	names := map[string]bool{}
	for _, p := range flat.Properties {
		names[p.Name] = true
	}
	for _, want := range []string{"channelLabel", "gainDb", "enabled", "oid", "runtimePropertyConstraints"} {
		if !names[want] {
			t.Errorf("flattened class is missing inherited/own property %q", want)
		}
	}
}

// TestVendorGainHierarchyConsistent guards the declared hierarchy the
// AMWA suite checks (ms05_26): each level defines every bound the one
// below defines and is at least as restrictive.
func TestVendorGainHierarchyConsistent(t *testing.T) {
	dt := vendorGainDatatype().Constraints.(*ms05.NcParameterConstraintsNumber)
	var prop *ms05.NcParameterConstraintsNumber
	for _, p := range vendorGainClass().Properties {
		if p.Name == "gainDb" {
			prop = p.Constraints.(*ms05.NcParameterConstraintsNumber)
		}
	}
	var run *ms05.NcPropertyConstraintsNumber
	for _, c := range vendorRuntimeConstraints() {
		if n, ok := c.(*ms05.NcPropertyConstraintsNumber); ok {
			run = n
		}
	}
	if prop == nil || run == nil {
		t.Fatal("gainDb constraint levels missing")
	}
	num := func(v any) float64 { f, _ := v.(float64); return f }
	if num(prop.Minimum) < num(dt.Minimum) || num(prop.Maximum) > num(dt.Maximum) || num(prop.Step) < num(dt.Step) {
		t.Errorf("property constraints (%v..%v/%v) loosen the datatype's (%v..%v/%v)",
			prop.Minimum, prop.Maximum, prop.Step, dt.Minimum, dt.Maximum, dt.Step)
	}
	if num(run.Minimum) < num(prop.Minimum) || num(run.Maximum) > num(prop.Maximum) || num(run.Step) < num(prop.Step) {
		t.Errorf("runtime constraints (%v..%v/%v) loosen the property's (%v..%v/%v)",
			run.Minimum, run.Maximum, run.Step, prop.Minimum, prop.Maximum, prop.Step)
	}
}

func TestVendorGainWriteEnforcement(t *testing.T) {
	addr := serveConfigNode(t)
	val := "http://" + addr + "/x-nmos/configuration/v1.0/rolePaths/root.GainControl/properties/4p2/value/"
	lbl := "http://" + addr + "/x-nmos/configuration/v1.0/rolePaths/root.GainControl/properties/4p1/value/"

	cases := []struct {
		name, url, body string
		want            int
	}{
		{"gain above runtime max", val, `{"value": 7.0}`, 400},
		{"gain below runtime min", val, `{"value": -70}`, 400},
		{"gain off the step grid", val, `{"value": 0.75}`, 400},
		{"gain in range", val, `{"value": 5.5}`, 200},
		{"label above runtime maxCharacters", lbl, `{"value": "labels-longer-than-16"}`, 400},
		{"label breaking the pattern", lbl, `{"value": "bad*chars"}`, 400},
		{"label in range", lbl, `{"value": "Gain-2"}`, 200},
	}
	for _, tc := range cases {
		st, raw := doJSON(t, "PUT", tc.url, tc.body)
		if st != tc.want {
			t.Errorf("%s: PUT = %d %s, want %d", tc.name, st, raw, tc.want)
		}
	}

	// The accepted values stuck; the rejected ones did not.
	st, raw := doJSON(t, "GET", val, "")
	if st != 200 || !strings.Contains(string(raw), "5.5") {
		t.Errorf("gainDb after writes = %d %s, want 5.5", st, raw)
	}
	st, raw = doJSON(t, "GET", lbl, "")
	if st != 200 || !strings.Contains(string(raw), "Gain-2") {
		t.Errorf("channelLabel after writes = %d %s, want Gain-2", st, raw)
	}
}

func TestVendorGainRestoreValidateNotices(t *testing.T) {
	addr := serveConfigNode(t)
	bp := "http://" + addr + "/x-nmos/configuration/v1.0/rolePaths/root.GainControl/bulkProperties/"

	// PATCH = validate only: an out-of-constraint value must come back
	// as an Error notice, and must NOT be applied.
	dataSet := `{"arguments":{"dataSet":{"validationFingerprint":null,"values":[{"path":["root","GainControl"],"dependencyPaths":[],"allowedMembersClasses":[],"values":[{"id":{"level":4,"index":2},"descriptor":null,"value":100}]}]},"recurse":false,"restoreMode":1}}`
	st, raw := doJSON(t, "PATCH", bp, dataSet)
	if st != 200 {
		t.Fatalf("validate = %d %s", st, raw)
	}
	body := string(raw)
	// NoticeType Error is the numeric 400 on the wire.
	if !strings.Contains(body, "maximum") || !strings.Contains(body, `"noticeType":400`) {
		t.Errorf("validate response carries no constraint Error notice: %s", body)
	}

	st, raw = doJSON(t, "GET", "http://"+addr+"/x-nmos/configuration/v1.0/rolePaths/root.GainControl/properties/4p2/value/", "")
	if st != 200 || strings.Contains(string(raw), "100") {
		t.Errorf("validate-only round applied the value: %d %s", st, raw)
	}
}

func TestNCPSetGainDb(t *testing.T) {
	addr := serveNCPNode(t)

	// Resolve the worker's oid (bundle-dependent) via IS-14.
	st, raw := mxlGet(t, "http://"+addr+"/x-nmos/configuration/v1.0/rolePaths/root.GainControl/properties/1p2/value")
	if st != 200 {
		t.Fatalf("oid GET = %d %s", st, raw)
	}
	var oidResp struct {
		Value uint32 `json:"value"`
	}
	if err := json.Unmarshal(raw, &oidResp); err != nil || oidResp.Value == 0 {
		t.Fatalf("oid decode: %v (%s)", err, raw)
	}

	ws := ncpDial(t, addr)

	// Violating SetGainDb answers a per-command error.
	resp := ncpRoundTrip(t, ws, is12.CommandMessage{Commands: []is12.Command{{
		Handle: 21, OID: int(oidResp.Value), MethodID: is12.MethodID{Level: 4, Index: 1},
		Arguments: json.RawMessage(`{"gainDb": 100}`),
	}}})
	cr, ok := resp.(is12.CommandResponseMessage)
	if !ok {
		t.Fatalf("response type %T", resp)
	}
	if cr.Responses[0].Result.Status == 200 {
		t.Errorf("violating SetGainDb succeeded: %+v", cr.Responses[0].Result)
	}

	// In-range SetGainDb applies and reads back through 4p2.
	resp = ncpRoundTrip(t, ws, is12.CommandMessage{Commands: []is12.Command{{
		Handle: 22, OID: int(oidResp.Value), MethodID: is12.MethodID{Level: 4, Index: 1},
		Arguments: json.RawMessage(`{"gainDb": 3.5}`),
	}}})
	cr = resp.(is12.CommandResponseMessage)
	if cr.Responses[0].Result.Status != 200 {
		t.Fatalf("SetGainDb = %+v", cr.Responses[0].Result)
	}
	resp = ncpRoundTrip(t, ws, is12.CommandMessage{Commands: []is12.Command{{
		Handle: 23, OID: int(oidResp.Value), MethodID: is12.MethodID{Level: 1, Index: 1},
		Arguments: json.RawMessage(`{"id":{"level":4,"index":2}}`),
	}}})
	cr = resp.(is12.CommandResponseMessage)
	if cr.Responses[0].Result.Status != 200 || string(cr.Responses[0].Result.Value) != "3.5" {
		t.Errorf("gainDb after SetGainDb = %+v", cr.Responses[0].Result)
	}
}
