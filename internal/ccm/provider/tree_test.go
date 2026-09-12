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

// --- §11 writes ---

func fieldsOf(t *testing.T, doc string) map[string]json.RawMessage {
	t.Helper()
	var m map[string]json.RawMessage
	if err := json.Unmarshal([]byte(doc), &m); err != nil {
		t.Fatalf("bad test fields %s: %v", doc, err)
	}
	return m
}

func resourceOf(t *testing.T, tr *Tree, path string) map[string]any {
	t.Helper()
	body, kind := tr.Get(path)
	if kind != KindResource {
		t.Fatalf("%q kind = %v, want KindResource", path, kind)
	}
	var m map[string]any
	if err := json.Unmarshal(body, &m); err != nil {
		t.Fatalf("%q not an object: %v", path, err)
	}
	return m
}

// PATCH merges: only the named fields change, everything else is kept —
// §11.2's reason for existing (multiple clients, no clobbering).
func TestUpdatePatchMergesOnlyNamedFields(t *testing.T) {
	tr := mustTree(t)
	if err := tr.Update("io/ip/senders/tx-1", fieldsOf(t, `{"name":"CAM 1B","enable":true}`), false); err != nil {
		t.Fatalf("Update: %v", err)
	}
	got := resourceOf(t, tr, "io/ip/senders/tx-1")
	if got["name"] != "CAM 1B" || got["enable"] != true || got["uuid"] != "tx-1" {
		t.Errorf("patched = %v, want name+enable changed, uuid kept", got)
	}
}

// PUT replaces the mutable fields wholesale (§11.1: all mutable fields), so a
// field the body omits is gone — but the identity keys survive.
func TestUpdatePutReplacesMutableFields(t *testing.T) {
	tr := mustTree(t)
	if err := tr.Update("io/ip/senders/tx-1", fieldsOf(t, `{"enable":false}`), true); err != nil {
		t.Fatalf("Update: %v", err)
	}
	got := resourceOf(t, tr, "io/ip/senders/tx-1")
	if _, still := got["name"]; still {
		t.Errorf("PUT kept a field the body omitted: %v", got)
	}
	if got["enable"] != false || got["uuid"] != "tx-1" {
		t.Errorf("put = %v, want enable=false and uuid kept", got)
	}
}

// §14.1: uuid/id are device-assigned and immutable. A body that tries to
// re-key the resource is ignored for those fields, on PATCH and PUT alike.
func TestUpdateNeverOverwritesImmutableIdentity(t *testing.T) {
	tr := mustTree(t)
	_ = tr.Update("io/ip/senders/tx-1", fieldsOf(t, `{"uuid":"evil","id":"evil","name":"ok"}`), false)
	got := resourceOf(t, tr, "io/ip/senders/tx-1")
	if got["uuid"] != "tx-1" || got["id"] != nil || got["name"] != "ok" {
		t.Errorf("PATCH let identity change: %v", got)
	}
	_ = tr.Update("io/ip/senders/tx-1", fieldsOf(t, `{"uuid":"evil","x":1}`), true)
	got = resourceOf(t, tr, "io/ip/senders/tx-1")
	if got["uuid"] != "tx-1" || got["x"] != float64(1) {
		t.Errorf("PUT let identity change: %v", got)
	}
}

// Only resources are writable: an unknown path and an interior node are both
// ErrNotFound (a node has no body to mutate).
func TestUpdateUnknownAndNodeAreNotFound(t *testing.T) {
	tr := mustTree(t)
	for _, p := range []string{"nope", "io/ip", ""} {
		if err := tr.Update(p, fieldsOf(t, `{"a":1}`), false); err != ErrNotFound {
			t.Errorf("Update(%q) = %v, want ErrNotFound", p, err)
		}
	}
}

// A stored body that is not an object — a collection array, or a null — is
// not a mutable resource (§11.1 puts writes on individual resources).
func TestUpdateNonObjectIsNotMutable(t *testing.T) {
	tr, err := LoadTree([]byte(`{"io/ip/senders/video":[{"uuid":"a"}],"misc/flag":null}`))
	if err != nil {
		t.Fatalf("LoadTree: %v", err)
	}
	for _, p := range []string{"io/ip/senders/video", "misc/flag"} {
		if err := tr.Update(p, fieldsOf(t, `{"a":1}`), false); err != ErrNotMutable {
			t.Errorf("Update(%q) = %v, want ErrNotMutable", p, err)
		}
	}
}

// A controller PATCHes while another GETs; the tree must serve both without
// a data race (the -race CI run is the real check — this gives it the work).
func TestConcurrentGetAndUpdate(t *testing.T) {
	tr := mustTree(t)
	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := 0; i < 200; i++ {
			_ = tr.Update("io/ip/senders/tx-1", fieldsOf(t, `{"enable":true}`), i%2 == 0)
		}
	}()
	for i := 0; i < 200; i++ {
		tr.Get("io/ip/senders/tx-1")
		tr.Get("io/ip/senders")
		_ = tr.Len()
		_ = tr.Nodes()
	}
	<-done
}
