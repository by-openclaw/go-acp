package provider

// The IS-14 REST surface as a controller walks it, and what it answers
// when the controller asks for something that is not there or reaches
// for a verb the resource does not define. MS-05-02 gives every
// refusal a status code of its own, and the AMWA suite reads them.

import (
	"encoding/json"
	stdhttp "net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"dhs/internal/amwa/codec/ms05"
)

// configReq drives one request through the top-level dispatch, the way
// the mounted route does.
func configReq(t *testing.T, s *IS14ConfigurationServer, method, tail, body string) (int, any) {
	t.Helper()
	var r *stdhttp.Request
	url := "/x-nmos/configuration/v1.0/rolePaths/" + tail
	if body == "" {
		r = httptest.NewRequest(method, url, nil)
	} else {
		r = httptest.NewRequest(method, url, strings.NewReader(body))
	}
	status, out, err := s.dispatch(method, tail, r)
	if err != nil {
		t.Fatalf("dispatch %s %s: %v", method, tail, err)
	}
	return status, out
}

// An oid is an unsigned address in MS-05-02, so a negative one names
// nothing rather than indexing backwards through the model.
func TestObjectByOidRefusesANegativeAddress(t *testing.T) {
	s := configFixture(t)
	if obj := s.objectByOid(-1); obj != nil {
		t.Fatalf("objectByOid(-1) = %+v, want nothing", obj)
	}
}

// A model object whose class is not in the framework cannot be built,
// and building the device model out of one is a build defect rather
// than a runtime condition: the process stops instead of serving a
// model a controller would walk into a hole in.
func TestModelObjectsComeFromTheFramework(t *testing.T) {
	if _, err := newConfigObject(ms05.NcClassId{9, 9, 9}, 1, []string{"root"}, nil); err == nil {
		t.Fatal("a class the framework does not carry must be refused")
	}

	defer func() {
		r := recover()
		msg, _ := r.(string)
		if !strings.Contains(msg, "framework model load") {
			t.Fatalf("recovered %v, want the build defect named", r)
		}
	}()
	mustObject(ms05.NcClassId{9, 9, 9}, 1, []string{"root"}, nil)
}

// A pinned wire version serves that one alone: the AMWA suite runs a
// version at a time, and a server answering on every minor makes the
// round it is not testing look served.
func TestConfigurationServerHonoursAPinnedVersion(t *testing.T) {
	s := NewIS14ConfigurationServer(nil, audioBundle(), IS14ConfigurationConfig{APIVer: "v1.0"})
	if len(s.vers) != 1 || s.vers[0] != "v1.0" {
		t.Fatalf("versions = %v, want only the pinned one", s.vers)
	}
}

// Every level of the role-path tree is a listing that answers GET and
// nothing else. A controller that PUTs to an index has misread the
// API, and MS-05-02 has a status that says exactly that.
func TestRolePathTreeAnswersGETOnly(t *testing.T) {
	s := configFixture(t)

	for _, tail := range []string{"", "root", "root/descriptor", "root/properties", "root/methods"} {
		if status, body := configReq(t, s, stdhttp.MethodPut, tail, "{}"); status != 405 {
			t.Errorf("PUT %q = %d (%+v), want 405", tail, status, body)
		}
	}
}

// A role path nobody published is a 404 with BadOid, and so is a
// resource under one that the API does not define.
func TestRolePathRefusals(t *testing.T) {
	s := configFixture(t)

	if status, _ := configReq(t, s, stdhttp.MethodGet, "not-a-role-path", ""); status != 404 {
		t.Errorf("an unknown role path = %d, want 404", status)
	}
	if status, _ := configReq(t, s, stdhttp.MethodGet, "root/nonsense", ""); status != 404 {
		t.Errorf("an unknown sub-resource = %d, want 404", status)
	}
	if status, _ := configReq(t, s, stdhttp.MethodGet, "root/descriptor/deeper", ""); status != 404 {
		t.Errorf("a path below the descriptor = %d, want 404", status)
	}
}

// The properties subtree: an unknown property is
// PropertyNotImplemented, the index and descriptor answer GET only,
// and value answers GET and PUT and nothing else.
func TestPropertySubtreeRefusals(t *testing.T) {
	s := configFixture(t)
	obj := s.objectByOid(1)

	if status, _ := propReq(t, s, stdhttp.MethodGet, []string{"9p9"}, ""); status != 404 {
		t.Errorf("an unknown property = %d, want 404", status)
	}
	if status, _ := propReq(t, s, stdhttp.MethodPut, []string{"1p6"}, "{}"); status != 405 {
		t.Errorf("PUT on a property index = %d, want 405", status)
	}
	if status, _ := propReq(t, s, stdhttp.MethodPut, []string{"1p6", "descriptor"}, "{}"); status != 405 {
		t.Errorf("PUT on a descriptor = %d, want 405", status)
	}
	if status, _ := propReq(t, s, stdhttp.MethodDelete, []string{"1p6", "value"}, ""); status != 405 {
		t.Errorf("DELETE on a value = %d, want 405", status)
	}
	if status, _ := propReq(t, s, stdhttp.MethodGet, []string{"1p6", "value", "deeper"}, ""); status != 404 {
		t.Errorf("a path below a value = %d, want 404", status)
	}
	_ = obj
}

// A PUT whose body is not a property-value request is a bad command,
// not a write of whatever could be parsed out of it.
func TestPropertyValuePutRefusesABodyItCannotDecode(t *testing.T) {
	s := configFixture(t)

	if status, _ := propReq(t, s, stdhttp.MethodPut, []string{"1p6", "value"}, `{`); status != 400 {
		t.Errorf("a body that is not JSON = %d, want 400", status)
	}
}

// MS-05-02 gives each refusal its own status: readonly, not nullable,
// wrong type. A controller reading the status knows whether to fix the
// value or stop trying.
func TestSetPropertyRefusals(t *testing.T) {
	s := configFixture(t)
	obj := s.objectByOid(1)

	readonly := obj.findProp("1p1") // classId — readonly on every object
	if readonly == nil {
		t.Fatal("every NcObject carries classId")
	}
	if st, err := s.setProperty(obj, readonly, json.RawMessage(`[1,1]`)); err == nil ||
		st != ms05.NcMethodStatusReadonly {
		t.Errorf("writing a readonly property = %v (%v)", st, err)
	}

	// The vendor gain worker carries the one writable, non-nullable,
	// constrained property in this model — gainDb — which is what the
	// remaining three refusals need.
	gain := objectOfClass(t, s, vendorClassID)
	gainDb := gain.findProp("4p2")
	if gainDb == nil {
		t.Fatal("the gain worker carries gainDb")
	}
	if st, err := s.setProperty(gain, gainDb, json.RawMessage(`null`)); err == nil ||
		st != ms05.NcMethodStatusParameterError {
		t.Errorf("a null into a non-nullable property = %v (%v)", st, err)
	}
	if st, err := s.setProperty(gain, gainDb, json.RawMessage(`"loud"`)); err == nil ||
		st != ms05.NcMethodStatusParameterError {
		t.Errorf("a string into a number = %v (%v)", st, err)
	}
	if st, err := s.setProperty(gain, gainDb, json.RawMessage(`{`)); err == nil ||
		st != ms05.NcMethodStatusBadCommandFormat {
		t.Errorf("a value that is not JSON = %v (%v)", st, err)
	}
	// And the runtime constraint the class publishes is enforced, not
	// decoration: out of range is a ParameterError, not a write.
	if st, err := s.setProperty(gain, gainDb, json.RawMessage(`9999`)); err == nil ||
		st != ms05.NcMethodStatusParameterError {
		t.Errorf("a value outside the constraint = %v (%v)", st, err)
	}
}

// The methods subtree: the listing answers GET, a method answers
// PATCH, and anything else is a refusal that names the verb.
func TestMethodSubtreeRefusals(t *testing.T) {
	s := configFixture(t)
	obj := s.objectByOid(1)

	call := func(method string, rest []string, body string) (int, any) {
		t.Helper()
		var r *stdhttp.Request
		url := "/x-nmos/configuration/v1.0/rolePaths/root/methods"
		if body == "" {
			r = httptest.NewRequest(method, url, nil)
		} else {
			r = httptest.NewRequest(method, url, strings.NewReader(body))
		}
		status, out, err := s.dispatchMethods(method, obj, rest, r)
		if err != nil {
			t.Fatalf("dispatchMethods: %v", err)
		}
		return status, out
	}

	if status, _ := call(stdhttp.MethodGet, nil, ""); status != 200 {
		t.Errorf("the method listing = %d", status)
	}
	if status, _ := call(stdhttp.MethodPut, nil, "{}"); status != 405 {
		t.Errorf("PUT on the method listing = %d, want 405", status)
	}
	if status, _ := call(stdhttp.MethodGet, []string{"1m1", "deeper"}, ""); status != 404 {
		t.Errorf("a path below a method = %d, want 404", status)
	}
	if status, _ := call(stdhttp.MethodPatch, []string{"9m9"}, "{}"); status != 404 {
		t.Errorf("an unknown method = %d, want 404", status)
	}
	if status, _ := call(stdhttp.MethodGet, []string{"1m1"}, ""); status != 405 {
		t.Errorf("GET on a method = %d, want 405 — methods are invoked with PATCH", status)
	}
	if status, _ := call(stdhttp.MethodPatch, []string{"1m1"}, `{`); status != 400 {
		t.Errorf("a PATCH body that is not JSON = %d, want 400", status)
	}
}

// bulkProperties answers GET (backup), PUT (restore) and PATCH
// (validate) — and nothing else, because a DELETE there would be a
// controller asking to erase a device model.
func TestBulkPropertiesVerbs(t *testing.T) {
	s := configFixture(t)

	if status, _ := configReq(t, s, stdhttp.MethodDelete, "root/bulkProperties", ""); status != 405 {
		t.Errorf("DELETE on bulkProperties = %d, want 405", status)
	}
	if status, _ := configReq(t, s, stdhttp.MethodPut, "root/bulkProperties", `{`); status != 400 {
		t.Errorf("a restore body that is not JSON = %d, want 400", status)
	}
	// The two query flags are read from the request, not the path, and
	// a backup that ignores them would hand a controller the whole
	// model when it asked for one object.
	r := httptest.NewRequest(stdhttp.MethodGet,
		"/x-nmos/configuration/v1.0/rolePaths/root/bulkProperties?recurse=false&includeDescriptors=false", nil)
	status, body, err := s.dispatch(stdhttp.MethodGet, "root/bulkProperties", r)
	if err != nil || status != 200 {
		t.Fatalf("a backup = %d (%+v) %v", status, body, err)
	}
}
