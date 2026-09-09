package provider

import (
	"encoding/json"
	"strings"
	"testing"

	"dhs/internal/amwa/codec/is12"
	"dhs/internal/amwa/codec/ms05"
)

// oidOfClass finds the oid of the first object of a class in the
// server's model.
func oidOfClass(t *testing.T, s *IS12NCPServer, id ms05.NcClassId) int {
	t.Helper()
	for oid := 1; oid < 200; oid++ {
		if obj := s.config.objectByOid(oid); obj != nil &&
			classKey(obj.classID) == classKey(id) {
			return oid
		}
	}
	t.Fatalf("no object of class %v in the model", id)
	return 0
}

// Over IS-12, a property write goes through the same gate as the REST
// one: an argument the server cannot read, a property the object does
// not carry, and a value the property's constraints refuse are each
// answered with the MS-05 status that says which.
func TestNCPMethodSetRefusals(t *testing.T) {
	s := ncpFixture(t)

	// A write the model accepts.
	r := ncpCall(t, s, 1, 1, 2, map[string]any{
		"id": map[string]int{"level": 1, "index": 6}, "value": "renamed",
	})
	if r.Status != int(ms05.NcMethodStatusOk) {
		t.Fatalf("Set = %d (%s)", r.Status, r.ErrorMessage)
	}
	got := mustOK(t, ncpCall(t, s, 1, 1, 1, map[string]any{
		"id": map[string]int{"level": 1, "index": 6},
	}), "Get")
	if string(got) != `"renamed"` {
		t.Errorf("read back %s, want the value just written", got)
	}

	// Arguments the server cannot read at all.
	bad := s.runCommand(is12.Command{
		Handle: 1, OID: 1, MethodID: is12.MethodID{Level: 1, Index: 2},
		Arguments: json.RawMessage(`"not an object"`),
	})
	if bad.Status != int(ms05.NcMethodStatusParameterError) {
		t.Errorf("unreadable Set arguments = %d (%s)", bad.Status, bad.ErrorMessage)
	}
	bad = s.runCommand(is12.Command{
		Handle: 1, OID: 1, MethodID: is12.MethodID{Level: 1, Index: 1},
		Arguments: json.RawMessage(`"not an object"`),
	})
	if bad.Status != int(ms05.NcMethodStatusParameterError) {
		t.Errorf("unreadable Get arguments = %d (%s)", bad.Status, bad.ErrorMessage)
	}

	// A property the object does not carry.
	r = ncpCall(t, s, 1, 1, 2, map[string]any{
		"id": map[string]int{"level": 9, "index": 9}, "value": "x",
	})
	if r.Status != int(ms05.NcMethodStatusPropertyNotImplemented) {
		t.Errorf("Set of an absent property = %d (%s)", r.Status, r.ErrorMessage)
	}

	// A read-only property refuses the write rather than applying it.
	r = ncpCall(t, s, 1, 1, 2, map[string]any{
		"id": map[string]int{"level": 1, "index": 1}, "value": "x",
	})
	if r.Status == int(ms05.NcMethodStatusOk) {
		t.Error("classId is read-only and must refuse a write")
	}
}

// SetGainDb is the vendor convenience method; it writes through the
// same constraint gate, so it cannot be used to put the gain outside
// its declared range.
func TestNCPMethodSetGainDb(t *testing.T) {
	s := ncpFixture(t)
	oid := oidOfClass(t, s, vendorClassID)

	if r := ncpCall(t, s, oid, 4, 1, map[string]any{"gainDb": -6}); r.Status != int(ms05.NcMethodStatusOk) {
		t.Fatalf("SetGainDb = %d (%s)", r.Status, r.ErrorMessage)
	}

	for name, args := range map[string]json.RawMessage{
		"arguments the server cannot read": json.RawMessage(`"nope"`),
		"no gainDb argument":               json.RawMessage(`{}`),
		"a value outside the constraint":   json.RawMessage(`{"gainDb":1000}`),
	} {
		t.Run(name, func(t *testing.T) {
			r := s.runCommand(is12.Command{
				Handle: 1, OID: oid, MethodID: is12.MethodID{Level: 4, Index: 1}, Arguments: args,
			})
			if r.Status == int(ms05.NcMethodStatusOk) {
				t.Error("was accepted")
			}
		})
	}

	// The same 4m1 slot on an object of another class is not
	// SetGainDb — it must not be dispatched there.
	r := ncpCall(t, s, 1, 4, 1, map[string]any{"gainDb": -6})
	if r.Status == int(ms05.NcMethodStatusOk) {
		t.Error("4m1 on the root block is not SetGainDb")
	}
}

// The ClassManager answers with a class's own elements or its
// flattened ones, and says so when it carries no such class.
func TestNCPGetControlClass(t *testing.T) {
	s := ncpFixture(t)
	cm := oidOfClass(t, s, ms05.NcClassId{1, 3, 2})

	flat := mustOK(t, ncpCall(t, s, cm, 3, 1, map[string]any{
		"classId": ms05.NcClassId{1, 1},
	}), "GetControlClass")
	own := mustOK(t, ncpCall(t, s, cm, 3, 1, map[string]any{
		"classId": ms05.NcClassId{1, 1}, "includeInherited": false,
	}), "GetControlClass own")

	var flatDesc, ownDesc ms05.NcClassDescriptor
	if err := json.Unmarshal(flat, &flatDesc); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(own, &ownDesc); err != nil {
		t.Fatal(err)
	}
	if len(flatDesc.Properties) <= len(ownDesc.Properties) {
		t.Errorf("the flattened descriptor must carry more properties (%d vs %d)",
			len(flatDesc.Properties), len(ownDesc.Properties))
	}

	r := ncpCall(t, s, cm, 3, 1, map[string]any{"classId": ms05.NcClassId{9, 9, 9}})
	if r.Status != int(ms05.NcMethodStatusParameterError) ||
		!strings.Contains(r.ErrorMessage, "no class") {
		t.Errorf("a class the model does not carry = %d (%s)", r.Status, r.ErrorMessage)
	}

	bad := s.runCommand(is12.Command{
		Handle: 1, OID: cm, MethodID: is12.MethodID{Level: 3, Index: 1},
		Arguments: json.RawMessage(`"nope"`),
	})
	if bad.Status != int(ms05.NcMethodStatusParameterError) {
		t.Errorf("unreadable arguments = %d (%s)", bad.Status, bad.ErrorMessage)
	}
}

// A method id the model does not implement on that object is answered
// as unimplemented rather than silently succeeding.
func TestNCPUnimplementedMethod(t *testing.T) {
	s := ncpFixture(t)
	r := ncpCall(t, s, 1, 7, 7, map[string]any{})
	if r.Status != int(ms05.NcMethodStatusMethodNotImplemented) {
		t.Errorf("= %d (%s), want method-not-implemented", r.Status, r.ErrorMessage)
	}
}

// A value that cannot be marshalled is a device error, not a silent
// empty result — the helper every method's success path goes through.
func TestNCPOKValueRefusesAnUnmarshallableValue(t *testing.T) {
	r := ncpOKValue(make(chan int))
	if r.Status != int(ms05.NcMethodStatusDeviceError) {
		t.Errorf("= %d (%s), want a device error", r.Status, r.ErrorMessage)
	}
	if ok := ncpOK(); ok.Status != int(ms05.NcMethodStatusOk) || len(ok.Value) != 0 {
		t.Errorf("ncpOK = %+v", ok)
	}
}
