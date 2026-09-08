package ccm

import (
	"encoding/json"
	"testing"
)

// A small but real-shaped CCM capture: identity at /self, two io/ip senders,
// one receiver, and a processing resource — the same self/io/processing shape
// a live BRIDGE walk returns.
const sampleTree = `{
  "self": {"productName":"BRIDGE","productVersion":"7.0.2","modelVersion":3},
  "io/ip/senders/tx-1": {"uuid":"tx-1","name":"CAM 1"},
  "io/ip/senders/tx-2": {"uuid":"tx-2","name":"CAM 2"},
  "io/ip/receivers/rx-1": {"uuid":"rx-1","name":"MV IN"},
  "processing/mixer": {"name":"mixer","enabled":true}
}`

func mustTree(t *testing.T) *Tree {
	t.Helper()
	tr, err := LoadTree([]byte(sampleTree))
	if err != nil {
		t.Fatalf("LoadTree: %v", err)
	}
	return tr
}

// A resource path returns its captured body verbatim — the receiver is
// entitled to the exact bytes, so the provider must not re-shape them.
func TestGetResourceReturnsStoredBody(t *testing.T) {
	tr := mustTree(t)
	body, kind := tr.Get("/self")
	if kind != KindResource {
		t.Fatalf("kind = %v, want KindResource", kind)
	}
	var got map[string]any
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatalf("body is not the stored JSON: %v", err)
	}
	if got["productName"] != "BRIDGE" || got["productVersion"] != "7.0.2" {
		t.Errorf("self body = %v, want the captured identity", got)
	}
}

// A node path returns the sorted array of its immediate child names — the
// self-describing contract a CCM controller recurses on. Sorted so two
// exports of the same device diff line-for-line.
func TestGetNodeListsSortedChildren(t *testing.T) {
	tr := mustTree(t)
	cases := map[string][]string{
		"":              {"io", "processing", "self"},
		"io":            {"ip"},
		"io/ip":         {"receivers", "senders"},
		"io/ip/senders": {"tx-1", "tx-2"},
	}
	for path, want := range cases {
		body, kind := tr.Get(path)
		if kind != KindNode {
			t.Fatalf("%q kind = %v, want KindNode", path, kind)
		}
		var got []string
		if err := json.Unmarshal(body, &got); err != nil {
			t.Fatalf("%q body not a string array: %v", path, err)
		}
		if len(got) != len(want) {
			t.Fatalf("%q children = %v, want %v", path, got, want)
		}
		for i := range want {
			if got[i] != want[i] {
				t.Fatalf("%q children = %v, want %v (sorted)", path, got, want)
			}
		}
	}
}

// The root lists top-level nodes whether or not the caller wrote the slash.
func TestGetRootIsSlashInsensitive(t *testing.T) {
	tr := mustTree(t)
	a, ka := tr.Get("")
	b, kb := tr.Get("/")
	if ka != KindNode || kb != KindNode || string(a) != string(b) {
		t.Errorf("root differs by slash: %q(%v) vs %q(%v)", a, ka, b, kb)
	}
}

// An unknown path is a 404, not an empty node or a guess.
func TestGetAbsentPath(t *testing.T) {
	tr := mustTree(t)
	if body, kind := tr.Get("io/ip/senders/tx-9"); kind != KindAbsent || body != nil {
		t.Errorf("absent path = %q,%v, want nil,KindAbsent", body, kind)
	}
	if _, kind := tr.Get("does/not/exist"); kind != KindAbsent {
		t.Errorf("unknown branch kind = %v, want KindAbsent", kind)
	}
}

// A path recorded as a resource is served as the resource even if it also has
// children — a captured leaf is always returned verbatim.
func TestResourceWinsOverNode(t *testing.T) {
	tr, err := LoadTree([]byte(`{"a":{"leaf":true},"a/b":{"child":true}}`))
	if err != nil {
		t.Fatalf("LoadTree: %v", err)
	}
	body, kind := tr.Get("a")
	if kind != KindResource {
		t.Fatalf("kind = %v, want KindResource (leaf wins)", kind)
	}
	if string(body) != `{"leaf":true}` {
		t.Errorf("body = %s, want the stored leaf", body)
	}
}

func TestLenAndNodes(t *testing.T) {
	tr := mustTree(t)
	if tr.Len() != 5 {
		t.Errorf("Len = %d, want 5 resources", tr.Len())
	}
	// Interior nodes: "", io, io/ip, io/ip/senders, io/ip/receivers, processing.
	if tr.Nodes() != 6 {
		t.Errorf("Nodes = %d, want 6", tr.Nodes())
	}
}

func TestLoadTreeErrors(t *testing.T) {
	cases := map[string]string{
		"not json":                        `{`,
		"not an object":                   `["a","b"]`,
		"empty object":                    `{}`,
		"empty path":                      `{"":{"x":1}}`,
		"unknown-field top-level is fine": `{"self":{"anything":1}}`,
	}
	for name, doc := range cases {
		_, err := LoadTree([]byte(doc))
		wantErr := name != "unknown-field top-level is fine"
		if wantErr && err == nil {
			t.Errorf("%s: want an error", name)
		}
		if !wantErr && err != nil {
			t.Errorf("%s: unexpected error %v", name, err)
		}
	}
}
