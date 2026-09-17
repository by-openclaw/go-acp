package provider

// The IS-14 method surface: the vendor workers a plant operator drives
// through Ansible, the BCP-008 monitor counters, the sequence
// accessors, and the bulk restore's validation notices.

import (
	stdhttp "net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/iotest"

	"dhs/internal/amwa/codec/is14"
	"dhs/internal/amwa/codec/ms05"
)

// A body the transport cuts short is a bad command, not a partial
// write: the Node must not act on the half of a request that arrived.
func TestBodiesThatCannotBeReadAreRefused(t *testing.T) {
	s := configFixture(t)
	obj := s.objectByOid(1)

	broken := func(url string) *stdhttp.Request {
		r := httptest.NewRequest(stdhttp.MethodPut, url, iotest.ErrReader(errTest("connection reset")))
		return r
	}

	status, _, err := s.dispatchProperties(stdhttp.MethodPut, obj, []string{"1p6", "value"},
		broken("/x-nmos/configuration/v1.0/rolePaths/root/properties/1p6/value"))
	if err != nil || status != 400 {
		t.Errorf("a truncated property write = %d (%v), want 400", status, err)
	}

	r := httptest.NewRequest(stdhttp.MethodPatch,
		"/x-nmos/configuration/v1.0/rolePaths/root/methods/1m1",
		iotest.ErrReader(errTest("connection reset")))
	status, _, err = s.dispatchMethods(stdhttp.MethodPatch, obj, []string{"1m1"}, r)
	if err != nil || status != 400 {
		t.Errorf("a truncated method invocation = %d (%v), want 400", status, err)
	}

	r = httptest.NewRequest(stdhttp.MethodPut,
		"/x-nmos/configuration/v1.0/rolePaths/root/bulkProperties",
		iotest.ErrReader(errTest("connection reset")))
	status, _, err = s.dispatchBulk(stdhttp.MethodPut, obj, r)
	if err != nil || status != 400 {
		t.Errorf("a truncated restore = %d (%v), want 400", status, err)
	}
}

// A method invocation whose arguments are not JSON is refused. The
// AMWA suite sends these deliberately.
func TestMethodArgumentsThatWillNotDecode(t *testing.T) {
	s := configFixture(t)
	obj := s.objectByOid(1)

	r := httptest.NewRequest(stdhttp.MethodPatch,
		"/x-nmos/configuration/v1.0/rolePaths/root/methods/1m1",
		strings.NewReader(`not json at all`))
	status, _, err := s.dispatchMethods(stdhttp.MethodPatch, obj, []string{"1m1"}, r)
	if err != nil || status != 400 {
		t.Fatalf("= %d (%v), want 400", status, err)
	}
}

// A property whose declared datatype is not in the framework cannot
// have a descriptor served for it. That is a device-model defect, and
// DeviceError is the status MS-05-02 gives it — not a 200 with
// nothing in it.
func TestPropertyDescriptorWithoutADatatype(t *testing.T) {
	s := configFixture(t)
	obj := s.objectByOid(1)

	p := obj.findProp("1p6")
	if p == nil {
		t.Fatal("every NcObject carries userLabel")
	}
	bogus := "NcNotInTheFramework"
	restore := p.desc.TypeName
	p.desc.TypeName = &bogus
	t.Cleanup(func() { p.desc.TypeName = restore })

	if status, _ := propReq(t, s, stdhttp.MethodGet, []string{"1p6", "descriptor"}, ""); status != 500 {
		t.Fatalf("= %d, want the device-model defect reported", status)
	}
}

// Get on a property the object does not have is
// PropertyNotImplemented, and Get on one it does answers with the
// value.
func TestGetMethodOnAnUnknownProperty(t *testing.T) {
	s := configFixture(t)
	obj := s.objectByOid(1)

	if status, _ := invokeNamed(t, s, obj, "Get", `{"id":{"level":9,"index":9}}`); status != 400 {
		t.Errorf("Get on an unknown property = %d, want 400", status)
	}
	if status, _ := invokeNamed(t, s, obj, "Get", `{"id":{"level":1,"index":6}}`); status != 200 {
		t.Errorf("Get on userLabel = %d, want 200", status)
	}
}

// The vendor gain worker is the Ansible seam into audio gain, and it
// goes through the same setProperty gate as a REST write — constraint
// included.
func TestGainControlMethod(t *testing.T) {
	s := configFixture(t)
	gain := objectOfClass(t, s, vendorClassID)

	if status, _ := invokeNamed(t, s, gain, "SetGainDb", `{}`); status != 400 {
		t.Errorf("SetGain with no argument = %d, want 400", status)
	}
	if status, _ := invokeNamed(t, s, gain, "SetGainDb", `{"gainDb":9999}`); status != 400 {
		t.Errorf("SetGain outside the constraint = %d, want 400", status)
	}
	if status, _ := invokeNamed(t, s, gain, "SetGainDb", `{"gainDb":-6}`); status != 200 {
		t.Errorf("SetGain in range = %d, want 200", status)
	}
	if got := gain.findProp("4p2").value; got != -6.0 {
		t.Errorf("gainDb = %v, want the written value", got)
	}
}

// The fault-injection worker is how the Ansible verify plays drive
// BCP-008 states with plain HTTP. Arguments it cannot use are refused
// rather than silently doing nothing.
func TestFaultControlMethods(t *testing.T) {
	s := configFixture(t)
	fault := objectOfClass(t, s, faultClassID)

	if status, _ := invokeNamed(t, s, fault, "InjectMonitorFault", `{}`); status != 400 {
		t.Errorf("a fault injection with no target = %d, want 400", status)
	}
}

// BCP-008 counters read back per monitor, and a monitor nobody has
// counted for answers with an empty list rather than null — a
// controller charting them should not have to special-case the first
// reading.
func TestMonitorCounterMethods(t *testing.T) {
	s := configFixture(t)
	rx := s.objectByOid(5) // the receiver monitor
	tx := s.objectByOid(6) // the sender monitor

	for _, tc := range []struct {
		obj  *configObject
		name string
	}{
		{rx, "GetLostPacketCounters"},
		{rx, "GetLatePacketCounters"},
		{tx, "GetTransmissionErrorCounters"},
	} {
		name := tc.name
		status, out := invokeNamed(t, s, tc.obj, name, `{}`)
		if status != 200 {
			t.Fatalf("%s = %d", name, status)
		}
		res, ok := out.(ms05.NcMethodResultPropertyValue)
		if !ok {
			t.Fatalf("%s returned %T", name, out)
		}
		if string(res.Value) != "[]" {
			t.Errorf("%s = %s, want an empty list", name, res.Value)
		}
	}

	if status, _ := invokeNamed(t, s, rx, "ResetCountersAndMessages", `{}`); status != 200 {
		t.Errorf("a counter reset = %d", status)
	}
}

// The sequence accessors: length, one item, and the refusals for an
// index nobody has and for the writes this model does not allow —
// every sequence property here is readonly.
func TestSequenceMethods(t *testing.T) {
	s := configFixture(t)
	root := s.objectByOid(1)

	// 2p2 is NcBlock.members — a sequence.
	length, out := invokeNamed(t, s, root, "GetSequenceLength", `{"id":{"level":2,"index":2}}`)
	if length != 200 {
		t.Fatalf("GetSequenceLength = %d (%+v)", length, out)
	}
	if status, _ := invokeNamed(t, s, root, "GetSequenceItem", `{"id":{"level":2,"index":2}}`); status != 400 {
		t.Errorf("GetSequenceItem with no index = %d, want 400", status)
	}
	if status, _ := invokeNamed(t, s, root, "GetSequenceItem",
		`{"id":{"level":2,"index":2},"index":9999}`); status != 400 {
		t.Errorf("GetSequenceItem out of bounds = %d, want 400", status)
	}
	if status, _ := invokeNamed(t, s, root, "GetSequenceItem",
		`{"id":{"level":2,"index":2},"index":0}`); status != 200 {
		t.Errorf("GetSequenceItem in bounds = %d, want 200", status)
	}
	if status, _ := invokeNamed(t, s, root, "SetSequenceItem",
		`{"id":{"level":2,"index":2},"index":0,"value":{}}`); status != 400 {
		t.Errorf("writing a readonly sequence = %d, want 400", status)
	}
	if status, _ := invokeNamed(t, s, root, "GetSequenceLength",
		`{"id":{"level":1,"index":6}}`); status != 400 {
		t.Errorf("a sequence accessor on a scalar = %d, want 400", status)
	}
}

// Members of a block: the immediate ones by default, the whole subtree
// when the caller asks to recurse.
func TestGetMemberDescriptors(t *testing.T) {
	s := configFixture(t)
	root := s.objectByOid(1)

	_, shallow := invokeNamed(t, s, root, "GetMemberDescriptors", `{"recurse":false}`)
	_, deep := invokeNamed(t, s, root, "GetMemberDescriptors", `{"recurse":true}`)

	a, ok1 := shallow.(ms05.NcMethodResultBlockMemberDescriptors)
	b, ok2 := deep.(ms05.NcMethodResultBlockMemberDescriptors)
	if !ok1 || !ok2 {
		t.Fatalf("results were %T and %T", shallow, deep)
	}
	if len(a.Value) == 0 {
		t.Error("root has members")
	}
	if len(b.Value) < len(a.Value) {
		t.Errorf("recursing found fewer members (%d) than not (%d)", len(b.Value), len(a.Value))
	}
}

// The restore validation is where a controller learns what a backup
// would do before it does it: a role path the model does not have, a
// property that is not there, one that is readonly, a null into a
// non-nullable property, and a value the datatype cannot hold. Each
// gets its own notice, and the error ones fail the object.
func TestRestoreValidationNotices(t *testing.T) {
	s := configFixture(t)
	root := s.objectByOid(1)

	recurse := true
	body := &is14.BulkPropertiesSetArgs{
		Recurse: &recurse,
		DataSet: &is14.BulkPropertiesHolder{Values: []is14.ObjectPropertiesHolder{
			{
				Path: []string{"root", "NotAnObject"},
			},
			{
				Path: []string{"root"},
				Values: []is14.PropertyHolder{
					{ID: ms05.NcPropertyId{Level: 9, Index: 9}},                     // not there
					{ID: ms05.NcPropertyId{Level: 1, Index: 1}},                     // readonly
					{ID: ms05.NcPropertyId{Level: 2, Index: 1}, Value: nil},         // not nullable
					{ID: ms05.NcPropertyId{Level: 1, Index: 6}, Value: []any{1, 2}}, // wrong type
				},
			},
		}},
	}
	out := s.restore(root, body, false)

	byPath := map[string]is14.ObjectPropertiesSetValidation{}
	for _, e := range out {
		byPath[strings.Join(e.Path, ".")] = e
	}
	missing, ok := byPath["root.NotAnObject"]
	if !ok || missing.Status != is14.RestoreValidationNotFound {
		t.Errorf("a role path the model does not have = %+v", missing)
	}
	present, ok := byPath["root"]
	if !ok {
		t.Fatalf("root was not validated: %+v", out)
	}
	if len(present.Notices) < 4 {
		t.Errorf("notices = %+v, want one per offered property", present.Notices)
	}
}
