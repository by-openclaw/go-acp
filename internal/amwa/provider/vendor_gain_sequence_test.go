package provider

// The DhsGainControl writable sequences: the AMWA suite's sequence
// rounds (MS-05-01 test_ms05_30 constrained sequences, _31 enum
// sequences, _32 struct sequences, _37/_38/_39 SetSequenceItem /
// AddSequenceItem / RemoveSequenceItem) replayed step for step over
// both APIs that carry them — IS-14 methods and IS-12 commands — and
// the item-level checks the rounds rely on.

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"

	"dhs/internal/amwa/codec/is12"
	"dhs/internal/amwa/codec/ms05"
)

func gainObject(t *testing.T, s *IS14ConfigurationServer) *configObject {
	t.Helper()
	obj, ok := s.objects["root.GainControl"]
	if !ok {
		t.Fatal("the model carries root.GainControl")
	}
	return obj
}

// seqArgs renders the IS-14 arguments object of a sequence method.
func seqArgs(t *testing.T, id ms05.NcPropertyId, index *int, value any) string {
	t.Helper()
	args := map[string]any{"id": map[string]any{"level": id.Level, "index": id.Index}}
	if index != nil {
		args["index"] = *index
	}
	if value != nil {
		args["value"] = value
	}
	return string(mustJSON(t, args))
}

// decodedValue is what the tool compares: the JSON form of a result.
func decodedValue(t *testing.T, body any) any {
	t.Helper()
	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	var out struct {
		Value any `json:"value"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatal(err)
	}
	return out.Value
}

func intp(i int) *int { return &i }

// test_ms05_37/38/39 over IS-14, on each of the three sequences: add a
// copy of the first item (the returned index is the old length), read
// it back, overwrite it with a copy of the last item, remove it.
func TestVendorGainSequenceMethodsThroughIS14(t *testing.T) {
	s := configFixture(t)
	gain := gainObject(t, s)

	for _, name := range []string{"channelLabels", "channelModes", "presets"} {
		p := findPropByName(gain, name)
		if p == nil {
			t.Fatalf("no %s on the gain worker", name)
		}
		original, _ := asSequence(p.value)
		if len(original) == 0 {
			t.Fatalf("%s is seeded empty — the suite needs an item to copy", name)
		}
		n := len(original)

		st, out := invokeNamed(t, s, gain, "AddSequenceItem", seqArgs(t, p.desc.ID, nil, original[0]))
		if st != 200 || decodedValue(t, out) != float64(n) {
			t.Fatalf("%s: AddSequenceItem = %d %+v, want index %d", name, st, out, n)
		}
		st, out = invokeNamed(t, s, gain, "GetSequenceLength", seqArgs(t, p.desc.ID, nil, nil))
		if st != 200 || decodedValue(t, out) != float64(n+1) {
			t.Errorf("%s: length after add = %d %+v, want %d", name, st, out, n+1)
		}
		st, out = invokeNamed(t, s, gain, "GetSequenceItem", seqArgs(t, p.desc.ID, intp(n), nil))
		if st != 200 || !reflect.DeepEqual(decodedValue(t, out), original[0]) {
			t.Errorf("%s: added item reads back as %d %+v, want %v", name, st, out, original[0])
		}

		st, out = invokeNamed(t, s, gain, "SetSequenceItem", seqArgs(t, p.desc.ID, intp(n), original[n-1]))
		if st != 200 {
			t.Fatalf("%s: SetSequenceItem = %d %+v", name, st, out)
		}
		st, out = invokeNamed(t, s, gain, "GetSequenceItem", seqArgs(t, p.desc.ID, intp(n), nil))
		if st != 200 || !reflect.DeepEqual(decodedValue(t, out), original[n-1]) {
			t.Errorf("%s: set item reads back as %d %+v, want %v", name, st, out, original[n-1])
		}

		st, out = invokeNamed(t, s, gain, "RemoveSequenceItem", seqArgs(t, p.desc.ID, intp(n), nil))
		if st != 200 {
			t.Fatalf("%s: RemoveSequenceItem = %d %+v", name, st, out)
		}
		st, out = invokeNamed(t, s, gain, "GetSequenceLength", seqArgs(t, p.desc.ID, nil, nil))
		if st != 200 || decodedValue(t, out) != float64(n) {
			t.Errorf("%s: length after remove = %d %+v, want %d", name, st, out, n)
		}
		if now, _ := asSequence(p.value); !reflect.DeepEqual(now, original) {
			t.Errorf("%s after the round = %v, want the original %v", name, now, original)
		}
	}
}

// The refusals of the writers: a readonly sequence (every standard
// one), a missing index, an index past the end, a null item.
func TestVendorGainSequenceWriterRefusalsThroughIS14(t *testing.T) {
	s := configFixture(t)
	gain := gainObject(t, s)
	root := s.objectByOid(1)
	labels := findPropByName(gain, "channelLabels").desc.ID
	members := ms05.NcPropertyId{Level: 2, Index: 2}

	cases := []struct {
		name, method, args string
		obj                *configObject
		want               ms05.NcMethodStatus
	}{
		{"add to a readonly sequence", "AddSequenceItem", seqArgs(t, members, nil, map[string]any{}), root, ms05.NcMethodStatusReadonly},
		{"set in a readonly sequence", "SetSequenceItem", seqArgs(t, members, intp(0), map[string]any{}), root, ms05.NcMethodStatusReadonly},
		{"remove from a readonly sequence", "RemoveSequenceItem", seqArgs(t, members, intp(0), nil), root, ms05.NcMethodStatusReadonly},
		{"set without an index", "SetSequenceItem", seqArgs(t, labels, nil, "x"), gain, ms05.NcMethodStatusParameterError},
		{"set without a value", "SetSequenceItem", seqArgs(t, labels, intp(0), nil), gain, ms05.NcMethodStatusParameterError},
		{"add without a value", "AddSequenceItem", seqArgs(t, labels, nil, nil), gain, ms05.NcMethodStatusParameterError},
		{"remove without an index", "RemoveSequenceItem", seqArgs(t, labels, nil, nil), gain, ms05.NcMethodStatusParameterError},
		{"set past the end", "SetSequenceItem", seqArgs(t, labels, intp(99), "x"), gain, ms05.NcMethodStatusIndexOutOfBounds},
		{"remove past the end", "RemoveSequenceItem", seqArgs(t, labels, intp(99), nil), gain, ms05.NcMethodStatusIndexOutOfBounds},
		{"add a null item", "AddSequenceItem", `{"id":{"level":4,"index":4},"value":null}`, gain, ms05.NcMethodStatusParameterError},
		{"add an item of the wrong kind", "AddSequenceItem", seqArgs(t, labels, nil, 7), gain, ms05.NcMethodStatusParameterError},
	}
	for _, tc := range cases {
		st, out := invokeNamed(t, s, tc.obj, tc.method, tc.args)
		e, ok := out.(ms05.NcMethodResultError)
		if st != 400 || !ok || e.Status != tc.want {
			t.Errorf("%s: = %d %+v, want status %d", tc.name, st, out, tc.want)
		}
	}
	if now, _ := asSequence(findPropByName(gain, "channelLabels").value); !reflect.DeepEqual(now, []any{"L", "R"}) {
		t.Errorf("channelLabels after the refusals = %v", now)
	}
}

// test_ms05_30: the constraint on channelLabels governs each item on
// every write path — AddSequenceItem, SetSequenceItem and a
// whole-sequence Set — with the tool's own valid and violating values.
func TestVendorGainConstrainedSequenceEnforced(t *testing.T) {
	addr := serveConfigNode(t)
	base := "http://" + addr + "/x-nmos/configuration/v1.0/rolePaths/root.GainControl/"
	method := func(id int) string { return base + "methods/1m" + string(rune('0'+id)) + "/" }
	value := base + "properties/4p4/value/"
	add := func(v string) int {
		st, _ := doJSON(t, "PATCH", method(5), `{"arguments":{"id":{"level":4,"index":4},"value":"`+v+`"}}`)
		return st
	}
	setLast := func(v string) int {
		st, raw := doJSON(t, "GET", value, "")
		if st != 200 {
			t.Fatalf("GET channelLabels = %d %s", st, raw)
		}
		var cur struct {
			Value []string `json:"value"`
		}
		if err := json.Unmarshal(raw, &cur); err != nil {
			t.Fatal(err)
		}
		st, _ = doJSON(t, "PATCH", method(4), `{"arguments":{"id":{"level":4,"index":4},"index":`+
			string(mustJSON(t, len(cur.Value)-1))+`,"value":"`+v+`"}}`)
		return st
	}

	// Valid: the suite's add-then-set-last pair.
	if st := add("Aux 1"); st != 200 {
		t.Errorf("add a valid label = %d", st)
	}
	if st := setLast("Aux_2"); st != 200 {
		t.Errorf("set a valid label = %d", st)
	}
	// Violating: the suite's negative pattern examples, then a value
	// above maxCharacters.
	for _, bad := range []string{"!$%^&*()+_:;/", "*********", "seventeen-chars-X"} {
		if st := add(bad); st != 400 {
			t.Errorf("add %q = %d, want the constraint refusal", bad, st)
		}
		if st := setLast(bad); st != 400 {
			t.Errorf("set %q = %d, want the constraint refusal", bad, st)
		}
	}
	// A whole-sequence Set checks every item and applies none on a
	// refusal; the suite's restore of the original value goes through.
	if st, raw := doJSON(t, "PUT", value, `{"value":["Ok","bad*"]}`); st != 400 || !strings.Contains(string(raw), "item 1") {
		t.Errorf("Set with one bad item = %d %s", st, raw)
	}
	if st, raw := doJSON(t, "GET", value, ""); st != 200 || !strings.Contains(string(raw), "Aux_2") {
		t.Errorf("a refused Set changed the sequence: %d %s", st, raw)
	}
	if st, raw := doJSON(t, "PUT", value, `{"value":["L","R"]}`); st != 200 {
		t.Errorf("restoring the original = %d %s", st, raw)
	}
	if st, raw := doJSON(t, "GET", value, ""); st != 200 || !strings.Contains(string(raw), `["L","R"]`) {
		t.Errorf("after restore = %d %s", st, raw)
	}
}

// test_ms05_31 / _32: the enum and struct sequences accept their
// reversed value and the original back, and refuse what their
// datatypes exclude — a number outside the enum, a struct with a
// field missing, mistyped, out of its constraint, or undeclared.
func TestVendorGainEnumAndStructSequencesValidated(t *testing.T) {
	s := configFixture(t)
	gain := gainObject(t, s)
	modes := findPropByName(gain, "channelModes")
	presets := findPropByName(gain, "presets")

	set := func(p *configProperty, v string) (ms05.NcMethodStatus, error) {
		return s.setProperty(gain, p, json.RawMessage(v))
	}
	if st, err := set(modes, `[2,1]`); err != nil {
		t.Errorf("reversed enum sequence refused: %d %v", st, err)
	}
	if st, err := set(presets, `[{"name":"Dim","gainDb":-20},{"name":"Unity","gainDb":0}]`); err != nil {
		t.Errorf("reversed struct sequence refused: %d %v", st, err)
	}
	refused := []struct {
		name string
		p    *configProperty
		v    string
		want string
	}{
		{"enum value outside the items", modes, `[0,5]`, "not a member of enum"},
		{"enum item of the wrong kind", modes, `["Normal"]`, "number expected"},
		{"struct missing a required field", presets, `[{"name":"Solo"}]`, "gainDb of DhsGainPreset is required"},
		{"struct field of the wrong kind", presets, `[{"name":"Solo","gainDb":"loud"}]`, "number expected"},
		{"struct field outside its datatype range", presets, `[{"name":"Hot","gainDb":99}]`, "above the constraint maximum"},
		{"struct field breaking its own constraint", presets, `[{"name":"bad*name","gainDb":0}]`, "does not match the constraint pattern"},
		{"struct with an undeclared field", presets, `[{"name":"X","gainDb":0,"extra":1}]`, "has no field extra"},
		{"struct item that is not an object", presets, `[42]`, "object expected"},
		{"a null item", presets, `[null]`, "is null"},
	}
	for _, tc := range refused {
		st, err := set(tc.p, tc.v)
		if err == nil || st != ms05.NcMethodStatusParameterError || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: = %d %v, want ParameterError mentioning %q", tc.name, st, err, tc.want)
		}
	}
	// The same checks guard the item writers.
	if _, st, err := s.sequenceAdd(gain, modes, json.RawMessage(`7`)); err == nil || st != ms05.NcMethodStatusParameterError {
		t.Errorf("AddSequenceItem with a non-member = %d %v", st, err)
	}
	if st, err := s.sequenceSet(gain, presets, 0, json.RawMessage(`{"name":"X","gainDb":-200}`)); err == nil || st != ms05.NcMethodStatusParameterError {
		t.Errorf("SetSequenceItem below the datatype minimum = %d %v", st, err)
	}
	// Restore (the suite always does).
	if _, err := set(modes, `[0,0]`); err != nil {
		t.Error(err)
	}
	if _, err := set(presets, `[{"name":"Unity","gainDb":0},{"name":"Dim","gainDb":-20}]`); err != nil {
		t.Error(err)
	}
}

// A backup of the gain worker restores: the per-item constraint check
// is what keeps a constrained sequence out of the Error notices, and
// what puts a violating item in.
func TestVendorGainSequenceRestoreValidation(t *testing.T) {
	addr := serveConfigNode(t)
	bp := "http://" + addr + "/x-nmos/configuration/v1.0/rolePaths/root.GainControl/bulkProperties/"
	dataSet := func(labels string) string {
		return `{"arguments":{"dataSet":{"validationFingerprint":null,"values":[{"path":["root","GainControl"],"dependencyPaths":[],"allowedMembersClasses":[],"values":[{"id":{"level":4,"index":4},"descriptor":null,"value":` + labels + `}]}]},"recurse":false,"restoreMode":1}}`
	}
	st, raw := doJSON(t, "PATCH", bp, dataSet(`["Left","Right"]`))
	if st != 200 || strings.Contains(string(raw), `"noticeType":400`) {
		t.Errorf("validating an in-constraint sequence = %d %s", st, raw)
	}
	st, raw = doJSON(t, "PATCH", bp, dataSet(`["Left","a-label-past-sixteen"]`))
	if st != 200 || !strings.Contains(string(raw), `"noticeType":400`) || !strings.Contains(string(raw), "item 1") {
		t.Errorf("validating a violating item = %d %s, want an Error notice naming it", st, raw)
	}
}

// test_ms05_37/38/39 over IS-12, and the notifications the three
// writers owe a subscriber: changeType SequenceItemAdded / Changed /
// Removed with the item index (MS-05-02 NcPropertyChangedEventData),
// the item as the value — none for a removal.
func TestNCPSequenceWritesNotifyWithChangeType(t *testing.T) {
	addr := serveNCPNode(t)
	st, raw := mxlGet(t, "http://"+addr+"/x-nmos/configuration/v1.0/rolePaths/root.GainControl/properties/1p2/value")
	if st != 200 {
		t.Fatalf("oid GET = %d %s", st, raw)
	}
	var oidResp struct {
		Value int `json:"value"`
	}
	if err := json.Unmarshal(raw, &oidResp); err != nil || oidResp.Value == 0 {
		t.Fatalf("oid decode: %v (%s)", err, raw)
	}
	oid := oidResp.Value
	ws := ncpDial(t, addr)
	if resp, ok := ncpRoundTrip(t, ws, is12.SubscriptionMessage{Subscriptions: []int{oid}}).(is12.SubscriptionResponseMessage); !ok || len(resp.Subscriptions) != 1 {
		t.Fatalf("subscription response = %#v", resp)
	}

	// One command, then its response AND its notification, in either
	// order.
	roundTrip := func(handle, method int, args string) (is12.MethodResult, is12.PropertyChangedEventData) {
		t.Helper()
		frame, _ := is12.Encode(is12.CommandMessage{Commands: []is12.Command{{
			Handle: handle, OID: oid, MethodID: is12.MethodID{Level: 1, Index: method},
			Arguments: json.RawMessage(args),
		}}})
		if err := ws.SendText(frame); err != nil {
			t.Fatal(err)
		}
		var result is12.MethodResult
		var event is12.PropertyChangedEventData
		for i := 0; i < 2; i++ {
			_ = ws.SetReadDeadline(time.Now().Add(3 * time.Second))
			in, err := ws.ReadText()
			if err != nil {
				t.Fatalf("read %d after method %d: %v", i, method, err)
			}
			m, err := is12.Decode(in)
			if err != nil {
				t.Fatal(err)
			}
			switch v := m.(type) {
			case is12.CommandResponseMessage:
				result = v.Responses[0].Result
			case is12.NotificationMessage:
				event = v.Notifications[0].EventData
			}
		}
		return result, event
	}

	r, ev := roundTrip(31, 5, `{"id":{"level":4,"index":4},"value":"Aux"}`) // AddSequenceItem (1m5)
	if r.Status != 200 || string(r.Value) != "2" {
		t.Fatalf("AddSequenceItem = %+v, want index 2", r)
	}
	if ev.ChangeType != int(ms05.NcPropertyChangeTypeSequenceItemAdded) || ev.SequenceItemIndex == nil || *ev.SequenceItemIndex != 2 || string(ev.Value) != `"Aux"` {
		t.Errorf("add notification = %+v", ev)
	}
	r, ev = roundTrip(32, 4, `{"id":{"level":4,"index":4},"index":2,"value":"Aux-2"}`) // SetSequenceItem (1m4)
	if r.Status != 200 {
		t.Fatalf("SetSequenceItem = %+v", r)
	}
	if ev.ChangeType != int(ms05.NcPropertyChangeTypeSequenceItemChanged) || ev.SequenceItemIndex == nil || *ev.SequenceItemIndex != 2 || string(ev.Value) != `"Aux-2"` {
		t.Errorf("set notification = %+v", ev)
	}
	r, ev = roundTrip(33, 6, `{"id":{"level":4,"index":4},"index":2}`) // RemoveSequenceItem (1m6)
	if r.Status != 200 {
		t.Fatalf("RemoveSequenceItem = %+v", r)
	}
	if ev.ChangeType != int(ms05.NcPropertyChangeTypeSequenceItemRemoved) || ev.SequenceItemIndex == nil || *ev.SequenceItemIndex != 2 || string(ev.Value) != "null" {
		t.Errorf("remove notification = %+v", ev)
	}
	// GetSequenceLength (1m7): a reader, so a response and nothing else.
	cr, ok := ncpRoundTrip(t, ws, is12.CommandMessage{Commands: []is12.Command{{
		Handle: 34, OID: oid, MethodID: is12.MethodID{Level: 1, Index: 7},
		Arguments: json.RawMessage(`{"id":{"level":4,"index":4}}`),
	}}}).(is12.CommandResponseMessage)
	if !ok || cr.Responses[0].Result.Status != 200 || string(cr.Responses[0].Result.Value) != "2" {
		t.Errorf("length after the round = %+v, want 2", cr)
	}
}

// The IS-12 writers' refusals, through the command path: index
// missing, past the end, a readonly sequence, a constraint violation.
func TestNCPSequenceWriterRefusals(t *testing.T) {
	s := ncpFixture(t)
	gain := gainObject(t, s.config)
	oid := int(gain.oid)
	labels := map[string]int{"level": 4, "index": 4}
	members := map[string]int{"level": 2, "index": 2}

	cases := []struct {
		name   string
		oid    int
		method int
		args   map[string]any
		want   ms05.NcMethodStatus
	}{
		{"set without an index", oid, 4, map[string]any{"id": labels, "value": "x"}, ms05.NcMethodStatusParameterError},
		{"remove without an index", oid, 6, map[string]any{"id": labels}, ms05.NcMethodStatusParameterError},
		{"set past the end", oid, 4, map[string]any{"id": labels, "index": 9, "value": "x"}, ms05.NcMethodStatusIndexOutOfBounds},
		{"remove past the end", oid, 6, map[string]any{"id": labels, "index": 9}, ms05.NcMethodStatusIndexOutOfBounds},
		{"add to a readonly sequence", 1, 5, map[string]any{"id": members, "value": map[string]any{}}, ms05.NcMethodStatusReadonly},
		{"remove from a readonly sequence", 1, 6, map[string]any{"id": members, "index": 0}, ms05.NcMethodStatusReadonly},
		{"add a violating item", oid, 5, map[string]any{"id": labels, "value": "far-too-long-for-sixteen"}, ms05.NcMethodStatusParameterError},
		{"set a violating item", oid, 4, map[string]any{"id": labels, "index": 0, "value": "no*stars"}, ms05.NcMethodStatusParameterError},
	}
	for _, tc := range cases {
		if r := ncpCall(t, s, tc.oid, 1, tc.method, tc.args); r.Status != int(tc.want) {
			t.Errorf("%s: = %d (%s), want %d", tc.name, r.Status, r.ErrorMessage, tc.want)
		}
	}
	if now, _ := asSequence(findPropByName(gain, "channelLabels").value); !reflect.DeepEqual(now, []any{"L", "R"}) {
		t.Errorf("channelLabels after the refusals = %v", now)
	}
}

// structMismatch on the shapes the vendor struct does not have: nullable
// fields left out, sequence-typed fields (absent, scalar, holding a null,
// holding a struct that is itself short), a field with no datatype, and
// a struct the catalogue does not know.
func TestStructMismatchShapes(t *testing.T) {
	registerVendorModels()
	untyped := "DhsTestUntypedField"
	if err := ms05.RegisterDatatype(ms05.NcDatatypeDescriptor{
		Name: untyped, Type: ms05.NcDatatypeTypeStruct,
		Fields: []ms05.NcFieldDescriptor{{Name: "anything"}},
	}); err != nil {
		t.Fatal(err)
	}
	cls := func(extra string) map[string]any {
		var m map[string]any
		if err := json.Unmarshal([]byte(`{"classId":[1,2,0,1],"name":"X","properties":[],"methods":[],"events":[]`+extra+`}`), &m); err != nil {
			t.Fatal(err)
		}
		return m
	}
	cases := []struct {
		name, typeName string
		obj            map[string]any
		want           string
	}{
		{"nullable fields left out", "NcClassDescriptor", cls(""), ""},
		{"nullable field null", "NcClassDescriptor", cls(`,"fixedRole":null`), ""},
		{"sequence field absent", "NcClassDescriptor", map[string]any{"classId": []any{1.0}, "name": "X"}, "field properties of NcClassDescriptor is required"},
		{"sequence field not an array", "NcClassDescriptor", cls(`,"properties":5`), "field properties of NcClassDescriptor must be an array"},
		{"sequence field holding a null", "NcClassDescriptor", cls(`,"methods":[null]`), "field methods of NcClassDescriptor: item 0 is null"},
		{"sequence field holding a short struct", "NcClassDescriptor", cls(`,"events":[{}]`), "field events of NcClassDescriptor: field id of NcEventDescriptor is required"},
		{"field with no datatype", untyped, map[string]any{"anything": 1.0}, ""},
		{"unknown struct", "NoSuchStruct", map[string]any{"x": 1.0}, ""},
	}
	for _, tc := range cases {
		if got := structMismatch(tc.typeName, tc.obj); got != tc.want {
			t.Errorf("%s: = %q, want %q", tc.name, got, tc.want)
		}
	}
}

// sequenceItem's own guards: a readonly sequence and a missing value,
// before anything is decoded.
func TestSequenceItemGuards(t *testing.T) {
	s := configFixture(t)
	root := s.objectByOid(1)
	if _, st, err := s.sequenceItem(root, root.findProp("2p2"), json.RawMessage(`{}`)); err == nil || st != ms05.NcMethodStatusReadonly {
		t.Errorf("an item for a readonly sequence = %d %v", st, err)
	}
	gain := gainObject(t, s)
	if _, st, err := s.sequenceItem(gain, findPropByName(gain, "channelLabels"), nil); err == nil || st != ms05.NcMethodStatusParameterError {
		t.Errorf("no value = %d %v", st, err)
	}
	if _, st, err := s.sequenceItem(gain, findPropByName(gain, "channelLabels"), json.RawMessage(`{`)); err == nil || st != ms05.NcMethodStatusBadCommandFormat {
		t.Errorf("malformed value = %d %v", st, err)
	}
}

// invokeSequence is routed by name; a name outside the five is the
// dispatcher's defect, answered as MethodNotImplemented rather than
// treated as one of them.
func TestInvokeSequenceUnknownName(t *testing.T) {
	s := configFixture(t)
	root := s.objectByOid(1)
	id := ms05.NcElementId{Level: 2, Index: 2}
	st, out, err := s.invokeSequence(root, "ShuffleSequence", methodArgs{ID: &id})
	e, ok := out.(ms05.NcMethodResultError)
	if err != nil || st != 400 || !ok || e.Status != ms05.NcMethodStatusMethodNotImplemented {
		t.Errorf("= %d %+v %v", st, out, err)
	}
}
