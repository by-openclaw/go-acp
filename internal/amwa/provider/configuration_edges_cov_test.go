package provider

// The IS-14 edges: values the model cannot render, constraints that
// come from three different places, block members below the first
// level, and the restore scope rule.

import (
	"encoding/json"
	"errors"
	stdhttp "net/http"
	"strings"
	"testing"

	"dhs/internal/amwa/codec/is14"
	"dhs/internal/amwa/codec/ms05"
)

// refuseMarshal makes the model's renderer refuse, so the arms that
// answer a value nobody can render are reachable.
func refuseMarshal(t *testing.T) {
	t.Helper()
	prev := marshalJSON
	marshalJSON = func(any) ([]byte, error) { return nil, errors.New("refused") }
	t.Cleanup(func() { marshalJSON = prev })
}

// A value the model cannot render is a device error, not a null: a
// controller reading a null where a value belongs has been told the
// property is unset, which is a different fact about the device.
func TestValuesThatCannotBeRendered(t *testing.T) {
	s := configFixture(t)
	root := s.objectByOid(1)
	rx := s.objectByOid(5)

	refuseMarshal(t)

	if status, _ := propReq(t, s, stdhttp.MethodGet, []string{"1p6", "value"}, ""); status != 500 {
		t.Errorf("a property value = %d, want 500", status)
	}
	if status, _ := invokeNamed(t, s, root, "Get", `{"id":{"level":1,"index":6}}`); status != 500 {
		t.Errorf("the Get method = %d, want 500", status)
	}
	if status, _ := invokeNamed(t, s, root, "GetSequenceLength",
		`{"id":{"level":2,"index":2}}`); status != 500 {
		t.Errorf("a sequence accessor = %d, want 500", status)
	}
	if status, _ := invokeNamed(t, s, rx, "GetLostPacketCounters", `{}`); status != 500 {
		t.Errorf("a counter list = %d, want 500", status)
	}
}

// A sequence property holding something that is not a sequence is a
// device-model defect, and the accessor says so rather than reporting
// a length of zero — which a controller would read as an empty block.
func TestSequenceAccessorOnAValueThatIsNotASequence(t *testing.T) {
	s := configFixture(t)
	root := s.objectByOid(1)

	members := root.findProp("2p2")
	if members == nil {
		t.Fatal("every NcBlock carries members")
	}
	saved := members.value
	members.value = 42 // valid JSON, not a sequence
	t.Cleanup(func() { members.value = saved })

	if status, _ := invokeNamed(t, s, root, "GetSequenceLength",
		`{"id":{"level":2,"index":2}}`); status != 500 {
		t.Fatalf("= %d, want the device-model defect reported", status)
	}
}

// A constraint can come from three places, in order: the object's own
// runtime constraints, the property descriptor, and the datatype. The
// AMWA suite offers out-of-range values and expects whichever applies
// to refuse them.
func TestConstraintsComeFromThreePlaces(t *testing.T) {
	s := configFixture(t)
	gain := objectOfClass(t, s, vendorClassID)
	p := gain.findProp("4p2")
	if p == nil {
		t.Fatal("the gain worker carries gainDb")
	}

	// 1. The object's runtime constraints win while they are there.
	if c := effectiveConstraint(gain, p); c == nil {
		t.Fatal("the gain worker publishes runtime constraints")
	}

	// 2. Without them, the property descriptor's own constraints apply.
	runtime := gain.findProp("1p8")
	if runtime == nil {
		t.Fatal("every NcObject carries runtimePropertyConstraints")
	}
	saved := runtime.value
	runtime.value = nil
	t.Cleanup(func() { runtime.value = saved })

	descConstraint := &ms05.NcParameterConstraintsNumber{}
	savedDesc := p.desc.Constraints
	p.desc.Constraints = descConstraint
	if c := effectiveConstraint(gain, p); c != descConstraint {
		t.Errorf("the descriptor's constraints = %+v, want the ones it declares", c)
	}

	// 3. And with neither, the datatype's.
	p.desc.Constraints = nil
	t.Cleanup(func() { p.desc.Constraints = savedDesc })
	if c := effectiveConstraint(gain, p); c == nil {
		t.Error("the vendor datatype declares constraints of its own")
	}
}

// jsonKindFor knows the JSON shape of every primitive by name. A
// datatype that is a primitive under some other name has no shape this
// validator can check, and saying so is better than guessing one.
func TestJSONKindForAnUnnamedPrimitive(t *testing.T) {
	registerVendorModels() // the catalogue must exist first
	desc := "a vendor primitive"
	if err := ms05.RegisterDatatype(ms05.NcDatatypeDescriptor{
		NcDescriptor: ms05.NcDescriptor{Description: &desc},
		Name:         "DhsTestPrimitive",
		Type:         ms05.NcDatatypeTypePrimitive,
	}); err != nil {
		t.Fatal(err)
	}
	if got := jsonKindFor("DhsTestPrimitive"); got != "" {
		t.Fatalf("= %q, want no shape", got)
	}
}

// SetGainDb is a named write of one property, so an object that does
// not carry that property cannot answer it — PropertyNotImplemented,
// not a silent success.
func TestGainMethodOnAnObjectWithoutGain(t *testing.T) {
	s := configFixture(t)
	gain := objectOfClass(t, s, vendorClassID)
	root := s.objectByOid(1)

	md := methodNamed(t, gain, "SetGainDb")
	status, _, err := s.invoke(root, md, json.RawMessage(`{"gainDb":-6}`))
	if err != nil || status != 400 {
		t.Fatalf("= %d (%v), want 400", status, err)
	}
}

// A fault injection with a target the model has answers, because that
// is the seam the Ansible verify plays drive.
func TestFaultInjectionOnARealMonitor(t *testing.T) {
	s := configFixture(t)
	fault := objectOfClass(t, s, faultClassID)
	rx := s.objectByOid(5)

	// A monitor reports nothing until its IS-05 resource is active —
	// an inactive one has no state to be faulty about — so the
	// injection targets an activated one, as the verify play does.
	for id, key := range s.monitorByResource {
		if key == strings.Join(rx.path, ".") {
			s.SetMonitorActive(id, true)
		}
	}

	args, err := json.Marshal(map[string]any{
		"monitorRole": rx.role,
		"domain":      "linkStatus",
		"status":      3,
		"message":     "injected by the verify play",
	})
	if err != nil {
		t.Fatal(err)
	}
	if status, out := invokeNamed(t, s, fault, "InjectMonitorFault", string(args)); status != 200 {
		t.Fatalf("= %d (%+v), want the injection accepted", status, out)
	}
}

// Block members below the first level are listed only when the caller
// asked to recurse. A controller enumerating one block's children must
// not be handed its grandchildren as though they were.
func TestMembersStopAtOneLevelWithoutRecurse(t *testing.T) {
	s := configFixture(t)
	root := s.objectByOid(1)
	gain := objectOfClass(t, s, vendorClassID)

	// A grandchild of root, under the gain worker.
	child := mustObject(faultClassID, 99, append(append([]string{}, gain.path...), "Nested"),
		map[string]any{"userLabel": "nested", "enabled": true})
	key := "root.GainControl.Nested"
	s.mu.Lock()
	s.objects[key] = child
	s.order = append(s.order, key)
	s.mu.Unlock()
	t.Cleanup(func() {
		s.mu.Lock()
		delete(s.objects, key)
		s.order = s.order[:len(s.order)-1]
		s.mu.Unlock()
	})

	shallow := s.membersOf(root, false)
	deep := s.membersOf(root, true)
	for _, m := range shallow {
		if m.Role == "Nested" {
			t.Error("a grandchild was listed without recurse")
		}
	}
	found := false
	for _, m := range deep {
		if m.Role == "Nested" {
			found = true
		}
	}
	if !found {
		t.Error("recursing must reach the grandchild")
	}
}

// The restore scope is the intersection of what the dataset offers and
// what the target covers. An object in the model but outside the scope
// gets no validation entry at all — the suite counts exactly the
// in-scope paths.
func TestRestoreScopeIsAnIntersection(t *testing.T) {
	s := configFixture(t)
	gain := objectOfClass(t, s, vendorClassID)
	recurse := false

	out := s.restore(gain, &is14.BulkPropertiesSetArgs{
		Recurse: &recurse,
		DataSet: &is14.BulkPropertiesHolder{Values: []is14.ObjectPropertiesHolder{
			{Path: []string{"root"}}, // in the model, outside this target
			{Path: gain.path},
		}},
	}, false)

	for _, e := range out {
		if len(e.Path) == 1 && e.Path[0] == "root" {
			t.Errorf("an out-of-scope object was validated: %+v", e)
		}
	}
}

// A null offered for a property that cannot hold one is an ERROR
// notice, not silence: typeMismatch passes nil through by design, and
// without this the validation response reads as acceptance.
func TestRestoreNoticesANullOnANonNullableProperty(t *testing.T) {
	s := configFixture(t)
	gain := objectOfClass(t, s, vendorClassID)
	recurse := false

	out := s.restore(gain, &is14.BulkPropertiesSetArgs{
		Recurse: &recurse,
		DataSet: &is14.BulkPropertiesHolder{Values: []is14.ObjectPropertiesHolder{{
			Path:   gain.path,
			Values: []is14.PropertyHolder{{ID: ms05.NcPropertyId{Level: 4, Index: 2}, Value: nil}},
		}}},
	}, false)

	if len(out) != 1 {
		t.Fatalf("validations = %+v, want one for the target", out)
	}
	found := false
	for _, n := range out[0].Notices {
		if n.NoticeType == is14.NoticeError {
			found = true
		}
	}
	if !found {
		t.Errorf("notices = %+v, want the null refused", out[0].Notices)
	}
}
