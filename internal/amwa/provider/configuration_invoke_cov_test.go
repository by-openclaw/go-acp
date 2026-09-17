package provider

import (
	"encoding/json"
	"strings"
	"testing"

	"dhs/internal/amwa/codec/ms05"
)

// configFixture is an IS-14 configuration face over the audio model —
// the same device model the IS-12 tests drive, reached through the
// REST invoke path instead of the WebSocket one.
func configFixture(t *testing.T) *IS14ConfigurationServer {
	t.Helper()
	return NewIS14ConfigurationServer(nil, audioBundle(), IS14ConfigurationConfig{})
}

// methodNamed finds a method descriptor by name on an object's class,
// walking the flattened class so inherited methods are reachable.
func methodNamed(t *testing.T, obj *configObject, name string) *ms05.NcMethodDescriptor {
	t.Helper()
	for i := range obj.class.Methods {
		if obj.class.Methods[i].Name == name {
			return &obj.class.Methods[i]
		}
	}
	t.Fatalf("class %v has no method %q", obj.classID, obj.classID)
	return nil
}

// objectOfClass finds the first object in the model whose class is the
// given one.
func objectOfClass(t *testing.T, s *IS14ConfigurationServer, id ms05.NcClassId) *configObject {
	t.Helper()
	for oid := 1; oid < 200; oid++ {
		if obj := s.objectByOid(oid); obj != nil && classKey(obj.classID) == classKey(id) {
			return obj
		}
	}
	t.Fatalf("no object of class %v in the model", id)
	return nil
}

// Get and Set over the IS-14 invoke path address a property by id, and
// every way of naming no property is refused with the MS-05 status
// that says which mistake was made.
func TestInvokeGetAndSet(t *testing.T) {
	s := configFixture(t)
	root := s.objectByOid(1)
	if root == nil {
		t.Fatal("the model must have a root block")
	}
	get := methodNamed(t, root, "Get")
	set := methodNamed(t, root, "Set")

	// 1p6 is userLabel on every NcObject.
	args := func(v any) json.RawMessage {
		raw, err := json.Marshal(v)
		if err != nil {
			t.Fatal(err)
		}
		return raw
	}
	userLabel := map[string]any{"id": map[string]int{"level": 1, "index": 6}}

	status, body, err := s.invoke(root, get, args(userLabel))
	if err != nil || status != 200 {
		t.Fatalf("Get = %d, %+v, %v", status, body, err)
	}
	if _, ok := body.(ms05.NcMethodResultPropertyValue); !ok {
		t.Errorf("Get body = %T, want a property value", body)
	}

	setArgs := map[string]any{
		"id":    map[string]int{"level": 1, "index": 6},
		"value": "renamed",
	}
	status, body, err = s.invoke(root, set, args(setArgs))
	if err != nil || status != 200 {
		t.Fatalf("Set = %d, %+v, %v", status, body, err)
	}
	_, body, _ = s.invoke(root, get, args(userLabel))
	raw := body.(ms05.NcMethodResultPropertyValue).Value
	if string(raw) != `"renamed"` {
		t.Errorf("read back %s, want the value just written", raw)
	}

	// A write of the wrong kind is refused by the datatype gate.
	status, _, _ = s.invoke(root, set, args(map[string]any{
		"id": map[string]int{"level": 1, "index": 6}, "value": 42,
	}))
	if status != 400 {
		t.Errorf("a wrong-kind write = %d, want 400", status)
	}

	// A Set with no value at all writes null, which the property
	// either accepts or refuses — either way it is answered, not
	// dropped.
	if status, _, err := s.invoke(root, set, args(map[string]any{
		"id": map[string]int{"level": 1, "index": 6},
	})); err != nil || (status != 200 && status != 400) {
		t.Errorf("a null write = %d, %v", status, err)
	}

	for name, tc := range map[string]struct {
		md   *ms05.NcMethodDescriptor
		args json.RawMessage
		want ms05.NcMethodStatus
	}{
		"Get with no id": {get, args(map[string]any{}), ms05.NcMethodStatusParameterError},
		"Get of a property the object does not have": {
			get, args(map[string]any{"id": map[string]int{"level": 9, "index": 9}}),
			ms05.NcMethodStatusPropertyNotImplemented,
		},
		"Set with no id": {set, args(map[string]any{"value": "x"}), ms05.NcMethodStatusParameterError},
		"Set of a property the object does not have": {
			set, args(map[string]any{"id": map[string]int{"level": 9, "index": 9}, "value": "x"}),
			ms05.NcMethodStatusPropertyNotImplemented,
		},
	} {
		t.Run(name, func(t *testing.T) {
			status, body, err := s.invoke(root, tc.md, tc.args)
			if err != nil || status != 400 {
				t.Fatalf("= %d, %+v, %v", status, body, err)
			}
			e, ok := body.(ms05.NcMethodResultError)
			if !ok || e.Status != tc.want {
				t.Errorf("body = %+v, want status %d", body, tc.want)
			}
		})
	}

	// Arguments that are not a JSON object cannot name anything.
	if status, _, _ := s.invoke(root, get, json.RawMessage(`"nope"`)); status != 400 {
		t.Errorf("unreadable arguments = %d, want 400", status)
	}

	// A method the invoke table does not implement says so rather than
	// answering as if it had run.
	unknown := &ms05.NcMethodDescriptor{Name: "NoSuchMethod"}
	if status, _, _ := s.invoke(root, unknown, args(map[string]any{})); status == 200 {
		t.Error("an unimplemented method must not answer OK")
	}
}

// SetGainDb is a named write of the vendor gain property, and it goes
// through the same constraint gate as a plain Set — a controller
// cannot use the convenience method to bypass the range.
func TestInvokeSetGainDb(t *testing.T) {
	s := configFixture(t)
	obj := objectOfClass(t, s, vendorClassID)
	md := methodNamed(t, obj, "SetGainDb")

	status, _, err := s.invoke(obj, md, json.RawMessage(`{"gainDb":-6}`))
	if err != nil || status != 200 {
		t.Fatalf("SetGainDb = %d, %v", status, err)
	}
	p := obj.findProp("4p2")
	if p == nil {
		t.Fatal("the gain control must carry gainDb (4p2)")
	}

	for name, raw := range map[string]string{
		"no gainDb argument":             `{}`,
		"arguments that are not JSON":    `{`,
		"a value outside the constraint": `{"gainDb":1000}`,
	} {
		t.Run(name, func(t *testing.T) {
			if status, _, _ := s.invoke(obj, md, json.RawMessage(raw)); status != 400 {
				t.Errorf("= %d, want 400", status)
			}
		})
	}
}

// The BCP-008 fault-injection methods are invocable over IS-14 REST so
// an Ansible verify play can reach them with plain HTTP; a malformed
// argument is refused rather than half-applied.
func TestInvokeFaultMethods(t *testing.T) {
	s := configFixture(t)
	obj := objectOfClass(t, s, faultClassID)

	inject := methodNamed(t, obj, "InjectMonitorFault")
	if status, _, _ := s.invoke(obj, inject, json.RawMessage(`{`)); status != 400 {
		t.Error("unreadable fault arguments must be refused")
	}

	clear := methodNamed(t, obj, "ClearMonitorFault")
	status, _, err := s.invoke(obj, clear, json.RawMessage(`{"oid":1}`))
	if err != nil {
		t.Fatalf("ClearMonitorFault: %v", err)
	}
	if status != 200 && status != 400 {
		t.Errorf("ClearMonitorFault = %d", status)
	}
}

// A sequence property is read and written item-wise; every refusal
// names the MS-05 status a controller acts on.
func TestInvokeSequenceRefusals(t *testing.T) {
	s := configFixture(t)
	root := s.objectByOid(1)
	length := methodNamed(t, root, "GetSequenceLength")

	// 2p2 on NcBlock is `members`, a sequence (2p1 is `enabled`).
	status, body, err := s.invoke(root, length, json.RawMessage(`{"id":{"level":2,"index":2}}`))
	if err != nil {
		t.Fatalf("GetSequenceLength: %v", err)
	}
	if status != 200 {
		t.Errorf("GetSequenceLength = %d (%+v)", status, body)
	}

	for name, raw := range map[string]string{
		"no id":                             `{}`,
		"a property that is not there":      `{"id":{"level":9,"index":9}}`,
		"a property that is not a sequence": `{"id":{"level":1,"index":6}}`,
	} {
		t.Run(name, func(t *testing.T) {
			if status, _, _ := s.invoke(root, length, json.RawMessage(raw)); status == 200 {
				t.Error("was accepted")
			}
		})
	}

	// An index past the end of the sequence is a range error, not a
	// panic.
	item := methodNamed(t, root, "GetSequenceItem")
	if status, body, _ := s.invoke(root, item, json.RawMessage(`{"id":{"level":2,"index":2},"index":9999}`)); status == 200 {
		t.Errorf("an index past the end = %d (%+v)", status, body)
	}
}

// ms05Err renders the status and message a controller reads; the
// helper is what every refusal above goes through.
func TestMS05ErrShape(t *testing.T) {
	status, body, err := ms05Err(400, ms05.NcMethodStatusParameterError, "id argument required")
	if err != nil || status != 400 {
		t.Fatalf("= %d, %v", status, err)
	}
	e, ok := body.(ms05.NcMethodResultError)
	if !ok || e.Status != ms05.NcMethodStatusParameterError ||
		!strings.Contains(e.ErrorMessage, "id argument") {
		t.Errorf("body = %+v", body)
	}
}
