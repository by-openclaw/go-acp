package provider

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"dhs/internal/amwa/codec/is12"
	"dhs/internal/amwa/codec/ms05"
)

// ncpFixture is an IS-12 control server over the audio model. Commands
// are driven straight through runCommand: the WebSocket carries them, but
// what the tests are about is the MS-05 method semantics.
func ncpFixture(t *testing.T) *IS12NCPServer {
	t.Helper()
	cfg := NewIS14ConfigurationServer(nil, audioBundle(), IS14ConfigurationConfig{})
	return NewIS12NCPServer(nil, cfg)
}

func ncpCall(t *testing.T, s *IS12NCPServer, oid, level, index int, args any) is12.MethodResult {
	t.Helper()
	var raw json.RawMessage
	if args != nil {
		b, err := json.Marshal(args)
		if err != nil {
			t.Fatal(err)
		}
		raw = b
	}
	return s.runCommand(is12.Command{
		Handle: 1, OID: oid,
		MethodID:  is12.MethodID{Level: level, Index: index},
		Arguments: raw,
	})
}

func mustJSON(t *testing.T, v any) json.RawMessage {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func mustOK(t *testing.T, r is12.MethodResult, what string) json.RawMessage {
	t.Helper()
	if r.Status != int(ms05.NcMethodStatusOk) {
		t.Fatalf("%s: status %d (%s)", what, r.Status, r.ErrorMessage)
	}
	return r.Value
}

// A command for an object that does not exist is refused by oid, before
// any method dispatch.
func TestNCPUnknownOID(t *testing.T) {
	s := ncpFixture(t)
	r := ncpCall(t, s, 9999, 1, 1, map[string]any{"id": map[string]int{"level": 1, "index": 6}})
	if r.Status != int(ms05.NcMethodStatusBadOid) || !strings.Contains(r.ErrorMessage, "9999") {
		t.Errorf("unknown oid = %d %q", r.Status, r.ErrorMessage)
	}
}

// FindMembersByPath walks the role path below root (the leading "root"
// segment is the block itself, not part of the path).
func TestNCPFindMembersByPath(t *testing.T) {
	s := ncpFixture(t)
	found := mustOK(t, ncpCall(t, s, 1, 2, 2, map[string]any{"path": []string{"ClassManager"}}), "FindMembersByPath")
	var members []ms05.NcBlockMemberDescriptor
	if err := json.Unmarshal(found, &members); err != nil {
		t.Fatal(err)
	}
	if len(members) != 1 || members[0].Role != "ClassManager" {
		t.Fatalf("FindMembersByPath = %+v, want exactly the class manager", members)
	}

	// A path nothing matches is an empty answer, not an error.
	empty := mustOK(t, ncpCall(t, s, 1, 2, 2, map[string]any{"path": []string{"NoSuchRole"}}), "unmatched path")
	_ = json.Unmarshal(empty, &members)
	if len(members) != 0 {
		t.Errorf("unmatched path returned %+v", members)
	}

	// An empty path, and arguments that are not an object, are parameter
	// errors.
	if r := ncpCall(t, s, 1, 2, 2, map[string]any{"path": []string{}}); r.Status != int(ms05.NcMethodStatusParameterError) {
		t.Errorf("empty path = %d", r.Status)
	}
	if r := s.runCommand(is12.Command{OID: 1, MethodID: is12.MethodID{Level: 2, Index: 2}, Arguments: json.RawMessage(`[]`)}); r.Status != int(ms05.NcMethodStatusParameterError) {
		t.Errorf("malformed arguments = %d", r.Status)
	}
}

// FindMembersByRole is a substring match, case-SENSITIVE unless the
// caller says otherwise, and exact when matchWholeString is set.
func TestNCPFindMembersByRole(t *testing.T) {
	s := ncpFixture(t)
	count := func(args map[string]any) int {
		t.Helper()
		var members []ms05.NcBlockMemberDescriptor
		_ = json.Unmarshal(mustOK(t, ncpCall(t, s, 1, 2, 3, args), "FindMembersByRole"), &members)
		return len(members)
	}
	if n := count(map[string]any{"role": "Manager"}); n < 2 {
		t.Errorf("substring found %d, want the managers", n)
	}
	if n := count(map[string]any{"role": "manager"}); n != 0 {
		t.Errorf("the default is case-sensitive, found %d for a lower-case role", n)
	}
	insensitive := false
	if n := count(map[string]any{"role": "manager", "caseSensitive": &insensitive}); n < 2 {
		t.Errorf("case-insensitive substring found %d, want the managers", n)
	}
	if n := count(map[string]any{"role": "ClassManager", "matchWholeString": true}); n != 1 {
		t.Errorf("whole-string match found %d, want one", n)
	}
	if n := count(map[string]any{"role": "Manager", "matchWholeString": true}); n != 0 {
		t.Errorf("whole-string match on a substring found %d, want none", n)
	}
	if r := ncpCall(t, s, 1, 2, 3, map[string]any{"role": ""}); r.Status != int(ms05.NcMethodStatusParameterError) {
		t.Errorf("empty role = %d", r.Status)
	}
	if r := s.runCommand(is12.Command{OID: 1, MethodID: is12.MethodID{Level: 2, Index: 3}, Arguments: json.RawMessage(`[]`)}); r.Status != int(ms05.NcMethodStatusParameterError) {
		t.Errorf("malformed arguments = %d", r.Status)
	}
}

// FindMembersByClassId matches the exact class, or every class derived
// from it when asked.
func TestNCPFindMembersByClass(t *testing.T) {
	s := ncpFixture(t)
	count := func(args map[string]any) int {
		t.Helper()
		var members []ms05.NcBlockMemberDescriptor
		_ = json.Unmarshal(mustOK(t, ncpCall(t, s, 1, 2, 4, args), "FindMembersByClassId"), &members)
		return len(members)
	}
	if n := count(map[string]any{"classId": ms05.NcClassId{1, 3, 2}}); n != 1 {
		t.Errorf("exact class match found %d, want the class manager", n)
	}
	// Every manager derives from NcManager (1.3).
	derived := count(map[string]any{"classId": ms05.NcClassId{1, 3}, "includeDerived": true})
	exact := count(map[string]any{"classId": ms05.NcClassId{1, 3}})
	if derived <= exact {
		t.Errorf("derived match found %d, exact %d; derived must find more", derived, exact)
	}
	if r := s.runCommand(is12.Command{OID: 1, MethodID: is12.MethodID{Level: 2, Index: 4}, Arguments: json.RawMessage(`[]`)}); r.Status != int(ms05.NcMethodStatusParameterError) {
		t.Errorf("malformed arguments = %d", r.Status)
	}
}

// GetDatatype answers from the flattened model by default and from the
// standard one when inheritance is switched off; an unknown name is a
// parameter error.
func TestNCPGetDatatype(t *testing.T) {
	s := ncpFixture(t)
	// The Class Manager owns GetDatatype (3m2 on NcClassManager).
	raw := mustOK(t, ncpCall(t, s, 3, 3, 2, map[string]any{"name": "NcBlockMemberDescriptor"}), "GetDatatype")
	if !strings.Contains(string(raw), "NcBlockMemberDescriptor") {
		t.Errorf("GetDatatype = %s", raw)
	}
	noInherit := false
	if r := ncpCall(t, s, 3, 3, 2, map[string]any{"name": "NcBlockMemberDescriptor", "includeInherited": &noInherit}); r.Status != int(ms05.NcMethodStatusOk) {
		t.Errorf("standard datatype lookup = %d (%s)", r.Status, r.ErrorMessage)
	}
	if r := ncpCall(t, s, 3, 3, 2, map[string]any{"name": "NoSuchType"}); r.Status != int(ms05.NcMethodStatusParameterError) {
		t.Errorf("unknown datatype = %d", r.Status)
	}
	if r := s.runCommand(is12.Command{OID: 3, MethodID: is12.MethodID{Level: 3, Index: 2}, Arguments: json.RawMessage(`[]`)}); r.Status != int(ms05.NcMethodStatusParameterError) {
		t.Errorf("malformed arguments = %d", r.Status)
	}
}

// The sequence methods address a property's items: length, get, set, add
// and remove, each refusing an index outside the sequence and a property
// that is not one.
func TestNCPSequenceMethods(t *testing.T) {
	s := ncpFixture(t)
	// NcClassManager.controlClasses (3p1) is a sequence.
	const classMgr = 3
	seqID := map[string]int{"level": 3, "index": 1}

	lengthOf := func() int {
		t.Helper()
		var n int
		_ = json.Unmarshal(mustOK(t, ncpCall(t, s, classMgr, 1, 7, map[string]any{"id": seqID}), "GetSequenceLength"), &n)
		return n
	}
	before := lengthOf()
	if before == 0 {
		t.Fatal("the class manager must publish its control classes")
	}

	idx := 0
	item := mustOK(t, ncpCall(t, s, classMgr, 1, 3, map[string]any{"id": seqID, "index": &idx}), "GetSequenceItem")
	if len(item) == 0 {
		t.Error("GetSequenceItem returned nothing")
	}
	if r := ncpCall(t, s, classMgr, 1, 3, map[string]any{"id": seqID, "index": before + 5}); r.Status == int(ms05.NcMethodStatusOk) {
		t.Error("an index past the end must be refused")
	}
	if r := ncpCall(t, s, classMgr, 1, 3, map[string]any{"id": seqID}); r.Status == int(ms05.NcMethodStatusOk) {
		t.Error("GetSequenceItem without an index must be refused")
	}

	// controlClasses is read-only, as MS-05 declares it: every mutation is
	// refused, and the sequence is untouched.
	for _, m := range []struct {
		name  string
		index int
		args  map[string]any
	}{
		{"SetSequenceItem", 4, map[string]any{"id": seqID, "index": &idx, "value": json.RawMessage(`{}`)}},
		{"AddSequenceItem", 5, map[string]any{"id": seqID, "value": json.RawMessage(`{}`)}},
		{"RemoveSequenceItem", 6, map[string]any{"id": seqID, "index": &idx}},
	} {
		if r := ncpCall(t, s, classMgr, 1, m.index, m.args); r.Status != int(ms05.NcMethodStatusReadonly) {
			t.Errorf("%s on a read-only sequence = %d (%s)", m.name, r.Status, r.ErrorMessage)
		}
	}
	if n := lengthOf(); n != before {
		t.Errorf("a refused mutation changed the sequence (%d, was %d)", n, before)
	}

	// The same property made writable: the three mutations then apply, and
	// each still refuses an index outside the sequence and a value that is
	// not JSON.
	obj := s.config.objectByOid(classMgr)
	s.config.mu.Lock()
	obj.findProp("3p1").desc.IsReadOnly = false
	s.config.mu.Unlock()

	if r := ncpCall(t, s, classMgr, 1, 5, map[string]any{"id": seqID, "value": json.RawMessage(`{}`)}); r.Status != int(ms05.NcMethodStatusOk) {
		t.Errorf("AddSequenceItem = %d (%s)", r.Status, r.ErrorMessage)
	}
	if n := lengthOf(); n != before+1 {
		t.Errorf("length after add = %d, want %d", n, before+1)
	}
	last := before
	if r := ncpCall(t, s, classMgr, 1, 4, map[string]any{"id": seqID, "index": &last, "value": json.RawMessage(`{}`)}); r.Status != int(ms05.NcMethodStatusOk) {
		t.Errorf("SetSequenceItem = %d (%s)", r.Status, r.ErrorMessage)
	}
	if r := ncpCall(t, s, classMgr, 1, 6, map[string]any{"id": seqID, "index": &last}); r.Status != int(ms05.NcMethodStatusOk) {
		t.Errorf("RemoveSequenceItem = %d (%s)", r.Status, r.ErrorMessage)
	}
	if n := lengthOf(); n != before {
		t.Errorf("length after remove = %d, want the original %d", n, before)
	}
	past := before + 5
	for _, m := range []struct {
		name  string
		index int
		args  map[string]any
	}{
		{"SetSequenceItem", 4, map[string]any{"id": seqID, "index": &past, "value": json.RawMessage(`{}`)}},
		{"RemoveSequenceItem", 6, map[string]any{"id": seqID, "index": &past}},
	} {
		if r := ncpCall(t, s, classMgr, 1, m.index, m.args); r.Status != int(ms05.NcMethodStatusIndexOutOfBounds) {
			t.Errorf("%s past the end = %d (%s)", m.name, r.Status, r.ErrorMessage)
		}
	}
	for _, m := range []struct {
		name  string
		index int
		args  map[string]any
	}{
		{"SetSequenceItem", 4, map[string]any{"id": seqID, "index": &idx}},
		{"AddSequenceItem", 5, map[string]any{"id": seqID}},
	} {
		if r := ncpCall(t, s, classMgr, 1, m.index, m.args); r.Status != int(ms05.NcMethodStatusParameterError) {
			t.Errorf("%s with no value = %d (%s)", m.name, r.Status, r.ErrorMessage)
		}
	}
	if r := s.methodSequence(obj, mustJSON(t, map[string]any{"id": seqID}), "explode"); r.Status != int(ms05.NcMethodStatusMethodNotImplemented) {
		t.Errorf("an unknown sequence op = %d", r.Status)
	}

	// A property that is not a sequence, and one that does not exist.
	if r := ncpCall(t, s, 1, 1, 7, map[string]any{"id": map[string]int{"level": 1, "index": 6}}); r.Status != int(ms05.NcMethodStatusInvalidRequest) {
		t.Errorf("non-sequence property = %d (%s)", r.Status, r.ErrorMessage)
	}
	if r := ncpCall(t, s, 1, 1, 7, map[string]any{"id": map[string]int{"level": 9, "index": 9}}); r.Status != int(ms05.NcMethodStatusPropertyNotImplemented) {
		t.Errorf("unknown property = %d", r.Status)
	}
	if r := s.runCommand(is12.Command{OID: 1, MethodID: is12.MethodID{Level: 1, Index: 7}, Arguments: json.RawMessage(`[]`)}); r.Status != int(ms05.NcMethodStatusParameterError) {
		t.Errorf("malformed arguments = %d", r.Status)
	}
}

// asSequence accepts any slice, not just []any — the model stores typed
// slices — and rejects everything else.
func TestAsSequence(t *testing.T) {
	if _, ok := asSequence(nil); ok {
		t.Error("nil is not a sequence")
	}
	if _, ok := asSequence("text"); ok {
		t.Error("a string is not a sequence")
	}
	got, ok := asSequence([]any{1, "two"})
	if !ok || len(got) != 2 {
		t.Errorf("[]any = %v, %v", got, ok)
	}
	typed, ok := asSequence([]string{"a", "b", "c"})
	if !ok || len(typed) != 3 || typed[2] != "c" {
		t.Errorf("typed slice = %v, %v", typed, ok)
	}
}

// The WebSocket idle timeout is settable before serving.
func TestNCPSetWSIdleTimeout(t *testing.T) {
	s := ncpFixture(t)
	s.SetWSIdleTimeout(90 * time.Second)
	s.mu.Lock()
	got := s.wsIdle
	s.mu.Unlock()
	if got != 90*time.Second {
		t.Errorf("wsIdle = %v, want 90s", got)
	}
}
