package codec

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

// The requests, byte for byte what CCM 0v1 §13.3.1 and §13.3.3 print.
func TestSubscriptionRequestsAreTheOnesTheDocumentPrints(t *testing.T) {
	cases := []struct {
		name string
		got  []byte
		want string
	}{
		{"create (§13.3.1)", CreateSubscription(1, "/io/ip/receivers/audio/550e8400-e29b-41d4-a716-446655440000"),
			`{"type":"CreateSubscription","id":1,"payload":{"relativeUrl":"/io/ip/receivers/audio/550e8400-e29b-41d4-a716-446655440000"}}`},
		{"wildcard (§13.3.5)", CreateSubscription(7, "/io/ip/receivers/audio/*"),
			`{"type":"CreateSubscription","id":7,"payload":{"relativeUrl":"/io/ip/receivers/audio/*"}}`},
		{"delete (§13.3.3)", DeleteSubscription(2, "a09a3171-de22-4d50-9dc2-661f1b80656a"),
			`{"type":"DeleteSubscription","id":2,"payload":{"subscriptionId":"a09a3171-de22-4d50-9dc2-661f1b80656a"}}`},
	}
	for _, c := range cases {
		if string(c.got) != c.want {
			t.Errorf("%s:\n got %s\nwant %s", c.name, c.got, c.want)
		}
	}
}

func TestParseMessageReadsEveryMessageOfSection13(t *testing.T) {
	// §13.3.2
	m, err := ParseMessage([]byte(`{"type":"CreateSubscriptionResponse","id":1,"payload":{"status":200,"message":"Subscription OK","subscriptionId":"a09a3171-de22-4d50-9dc2-661f1b80656a"}}`))
	if err != nil || m.Type != MsgCreateSubscriptionResponse || m.ID != 1 || m.Status != StatusOK ||
		m.Text != "Subscription OK" || m.SubscriptionID != "a09a3171-de22-4d50-9dc2-661f1b80656a" {
		t.Errorf("create response = %+v, %v", m, err)
	}
	// §13.3.4
	m, err = ParseMessage([]byte(`{"type":"DeleteSubscriptionResponse","id":2,"payload":{"status":200,"message":"DeleteSubscription OK"}}`))
	if err != nil || m.Type != MsgDeleteSubscriptionResponse || m.Text != "DeleteSubscription OK" {
		t.Errorf("delete response = %+v, %v", m, err)
	}
	// §13.4
	m, err = ParseMessage([]byte(`{"type":"Events","id":578,"payload":[
		{"documentRoot":"/io/ip/receivers/audio/550e8400-e29b-41d4-a716-446655440000","patch":[{"op":"replace","path":"/enable","value":true}]},
		{"documentRoot":"/io/madi/234a6764-3484-f875-f87f-948578557873","patch":[{"op":"replace","path":"/enable","value":true}]}]}`))
	want := []DocumentPatch{
		{"/io/ip/receivers/audio/550e8400-e29b-41d4-a716-446655440000", []PatchOp{{OpReplace, "/enable", true}}},
		{"/io/madi/234a6764-3484-f875-f87f-948578557873", []PatchOp{{OpReplace, "/enable", true}}},
	}
	if err != nil || m.Type != MsgEvents || m.ID != 578 || !reflect.DeepEqual(m.Events, want) {
		t.Errorf("events = %+v, %v", m, err)
	}
	// What SHUFFLE 6.0.0 sends: the singular type, the same payload.
	m, err = ParseMessage([]byte(`{"type":"Event","id":1,"payload":[{"documentRoot":"/self","patch":[{"op":"replace","path":"","value":{"a":1}}]}]}`))
	if err != nil || m.Type != MsgEvent || len(m.Events) != 1 || m.Events[0].Patch[0].Path != "" {
		t.Errorf("event = %+v, %v", m, err)
	}
	// A type the document does not define is handed back, not refused.
	m, err = ParseMessage([]byte(`{"type":"Hello","id":3,"payload":"x"}`))
	if err != nil || m.Type != "Hello" || m.ID != 3 || m.Events != nil {
		t.Errorf("unknown = %+v, %v", m, err)
	}
}

func TestParseMessageRefusesWhatItCannotRead(t *testing.T) {
	for _, frame := range []string{
		`not json`,
		`{"type":"CreateSubscriptionResponse","id":1,"payload":[1]}`,
		`{"type":"Events","id":1,"payload":{"documentRoot":"/x"}}`,
	} {
		if _, err := ParseMessage([]byte(frame)); err == nil {
			t.Errorf("%s: no error", frame)
		}
	}
}

// RFC 6901 §5: the pointers it evaluates against its example document.
func TestPointerSplitsAsRFC6901Does(t *testing.T) {
	cases := map[string][]string{
		"":       nil,
		"/foo":   {"foo"},
		"/foo/0": {"foo", "0"},
		"/":      {""},
		"/a~1b":  {"a/b"},
		"/m~0n":  {"m~n"},
		"/~01":   {"~1"},
		"/ ":     {" "},
	}
	for in, want := range cases {
		got, err := Pointer(in)
		if err != nil || !reflect.DeepEqual(got, want) {
			t.Errorf("Pointer(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	if _, err := Pointer("foo"); err == nil {
		t.Error("a pointer that does not start with / was accepted")
	}
}

// RFC 6902 Appendix A, for the three operations §13.4.1 uses.
func TestApplyPatchFollowsRFC6902(t *testing.T) {
	cases := []struct {
		name, doc string
		op        PatchOp
		want      string
	}{
		{"A.1 add an object member", `{"foo":"bar"}`, PatchOp{OpAdd, "/baz", "qux"}, `{"baz":"qux","foo":"bar"}`},
		{"A.2 add an array element", `{"foo":["bar","baz"]}`, PatchOp{OpAdd, "/foo/1", "qux"}, `{"foo":["bar","qux","baz"]}`},
		{"A.3 remove an object member", `{"baz":"qux","foo":"bar"}`, PatchOp{OpRemove, "/baz", nil}, `{"foo":"bar"}`},
		{"A.4 remove an array element", `{"foo":["bar","qux","baz"]}`, PatchOp{OpRemove, "/foo/1", nil}, `{"foo":["bar","baz"]}`},
		{"A.5 replace a value", `{"baz":"qux","foo":"bar"}`, PatchOp{OpReplace, "/baz", "boo"}, `{"baz":"boo","foo":"bar"}`},
		{"A.10 add a nested member", `{"foo":"bar"}`, PatchOp{OpAdd, "/child", map[string]any{"grandchild": map[string]any{}}}, `{"child":{"grandchild":{}},"foo":"bar"}`},
		{"A.16 add an array value at the end", `{"foo":["bar"]}`, PatchOp{OpAdd, "/foo/-", []any{"abc", "def"}}, `{"foo":["bar",["abc","def"]]}`},
		{"add at the index one past the end", `{"foo":["bar"]}`, PatchOp{OpAdd, "/foo/1", "baz"}, `{"foo":["bar","baz"]}`},
		{"add replaces a member that exists (§4.1)", `{"foo":"bar"}`, PatchOp{OpAdd, "/foo", "baz"}, `{"foo":"baz"}`},
		{"replace inside an array element", `{"legs":[{"ip":"1.1.1.1"},{"ip":"2.2.2.2"}]}`, PatchOp{OpReplace, "/legs/1/ip", "3.3.3.3"}, `{"legs":[{"ip":"1.1.1.1"},{"ip":"3.3.3.3"}]}`},
		{"replace an array element", `["a","b"]`, PatchOp{OpReplace, "/0", "c"}, `["c","b"]`},
		{"replace the whole document (§13.3.6)", `null`, PatchOp{OpReplace, "", map[string]any{"enable": true}}, `{"enable":true}`},
		{"remove the whole document", `{"a":1}`, PatchOp{OpRemove, "", nil}, `null`},
	}
	for _, c := range cases {
		var doc any
		if err := json.Unmarshal([]byte(c.doc), &doc); err != nil {
			t.Fatal(err)
		}
		got, err := ApplyPatch(doc, c.op)
		if err != nil {
			t.Errorf("%s: %v", c.name, err)
			continue
		}
		b, _ := json.Marshal(got)
		if string(b) != c.want {
			t.Errorf("%s:\n got %s\nwant %s", c.name, b, c.want)
		}
	}
}

func TestApplyPatchRefusesWhatDoesNotFit(t *testing.T) {
	cases := []struct {
		name, doc string
		op        PatchOp
		why       string
	}{
		{"an operation §13.4.1 does not use", `{"a":1}`, PatchOp{"move", "/a", nil}, "not one of"},
		{"a pointer that is not one", `{"a":1}`, PatchOp{OpReplace, "a", 1}, "does not start with /"},
		{"A.9 replace a member that is not there", `{"foo":"bar"}`, PatchOp{OpReplace, "/baz", 1}, `no member "baz"`},
		{"remove a member that is not there", `{"foo":"bar"}`, PatchOp{OpRemove, "/baz", nil}, `no member "baz"`},
		{"A.12 add under a parent that is not there", `{"foo":"bar"}`, PatchOp{OpAdd, "/baz/bat", "qux"}, `no member "baz"`},
		{"below a member that fails", `{"a":{"b":1}}`, PatchOp{OpReplace, "/a/c", 1}, `no member "c"`},
		{"an index past the end", `{"foo":["bar"]}`, PatchOp{OpReplace, "/foo/3", 1}, "no element"},
		{"an index that is not a number", `{"foo":["bar"]}`, PatchOp{OpRemove, "/foo/x", nil}, "no element"},
		{"add beyond one past the end", `{"foo":["bar"]}`, PatchOp{OpAdd, "/foo/5", 1}, "no element"},
		{"below an element that fails", `{"foo":[{"a":1}]}`, PatchOp{OpReplace, "/foo/0/b", 1}, `no member "b"`},
		{"inside a scalar", `{"foo":"bar"}`, PatchOp{OpAdd, "/foo/x", 1}, "inside a value"},
	}
	for _, c := range cases {
		var doc any
		if err := json.Unmarshal([]byte(c.doc), &doc); err != nil {
			t.Fatal(err)
		}
		got, err := ApplyPatch(doc, c.op)
		if err == nil || !strings.Contains(err.Error(), c.why) {
			t.Errorf("%s: err = %v, want one naming %q", c.name, err, c.why)
		}
		if b, _ := json.Marshal(got); string(b) != c.doc {
			t.Errorf("%s: the document changed to %s", c.name, b)
		}
	}
}
