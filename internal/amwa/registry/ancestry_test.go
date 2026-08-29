package registry

import (
	"encoding/json"
	"io"
	stdhttp "net/http"
	"testing"

	"dhs/internal/amwa/codec/is04"
)

// seedAncestryChain registers device + three sources A -> B -> C
// (B's parents name A, C's parents name B).
func seedAncestryChain(t *testing.T, store *Store) (a, b, c string) {
	t.Helper()
	node := is04.Node{
		ResourceCore: is04.ResourceCore{ID: "9c000000-0000-4000-8000-000000000001",
			Version: "1:0", Label: "n", Tags: map[string][]string{}},
		Href: "http://n/",
		Caps: map[string]any{},
		API: is04.NodeAPI{Versions: []string{"v1.3"},
			Endpoints: []is04.NodeEndpoint{{Host: "n", Port: 80, Protocol: "http"}}},
		Interfaces: []is04.NodeIface{},
	}
	if err := store.PutNode(node); err != nil {
		t.Fatalf("put node: %v", err)
	}
	dev := is04.Device{
		ResourceCore: is04.ResourceCore{ID: "9c000000-0000-4000-8000-000000000002",
			Version: "1:0", Label: "d", Tags: map[string][]string{}},
		Type: "urn:x-nmos:device:generic", NodeID: node.ID,
		Senders: []string{}, Receivers: []string{},
	}
	if err := store.PutDevice(dev); err != nil {
		t.Fatalf("put device: %v", err)
	}
	mk := func(id string, parents ...string) is04.Source {
		if parents == nil {
			parents = []string{}
		}
		return is04.Source{
			ResourceCore: is04.ResourceCore{ID: id, Version: "1:0", Label: "s-" + id[len(id)-1:],
				Tags: map[string][]string{}},
			DeviceID: dev.ID, Format: "urn:x-nmos:format:video",
			Caps: map[string]any{}, Parents: parents,
		}
	}
	a = "9c000000-0000-4000-8000-00000000000a"
	b = "9c000000-0000-4000-8000-00000000000b"
	c = "9c000000-0000-4000-8000-00000000000c"
	for _, s := range []is04.Source{mk(a), mk(b, a), mk(c, b)} {
		if err := store.PutSource(s); err != nil {
			t.Fatalf("put source %s: %v", s.ID, err)
		}
	}
	return a, b, c
}

func TestAncestrySetTraversal(t *testing.T) {
	store := NewStore()
	a, b, c := seedAncestryChain(t, store)
	idx := store.ParentsIndex(is04.ResourceSource)

	cases := []struct {
		name string
		root string
		typ  string
		gens int
		want []string
	}{
		{"children of A = descendants", a, ancestryChildren, 0, []string{b, c}},
		{"children of A, 1 generation", a, ancestryChildren, 1, []string{b}},
		{"parents of C = ancestors", c, ancestryParents, 0, []string{b, a}},
		{"parents of C, 1 generation", c, ancestryParents, 1, []string{b}},
		{"children of C = none", c, ancestryChildren, 0, nil},
		{"random root = empty, not an error", "00000000-0000-4000-8000-0000000000ff", ancestryChildren, 0, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := ancestrySet(idx, tc.root, tc.typ, tc.gens)
			if len(got) != len(tc.want) {
				t.Fatalf("got %v, want %v", got, tc.want)
			}
			for _, id := range tc.want {
				if !got[id] {
					t.Fatalf("missing %s in %v", id, got)
				}
			}
		})
	}
}

// TestAncestryCycleDoesNotHang: parents chains cannot legally cycle,
// but a registry stores what peers sent it.
func TestAncestryCycleDoesNotHang(t *testing.T) {
	idx := map[string][]string{"x": {"y"}, "y": {"x"}}
	got := ancestrySet(idx, "x", ancestryParents, 0)
	if !got["y"] || got["x"] {
		t.Fatalf("cycle traversal wrong: %v", got)
	}
}

// TestAncestryHTTP walks the wire behaviour the AMWA suite checks plus
// the semantics it leaves to the spec.
func TestAncestryHTTP(t *testing.T) {
	store := NewStore()
	a, b, c := seedAncestryChain(t, store)
	base, stop := startRegistryHTTP(t, store, nil)
	defer stop()
	url := func(rest string) string { return "http://" + base + "/x-nmos/query/v1.3" + rest }

	get := func(t *testing.T, u string) (int, []byte) {
		t.Helper()
		resp, err := stdhttp.Get(u)
		if err != nil {
			t.Fatalf("GET %s: %v", u, err)
		}
		defer func() { _ = resp.Body.Close() }()
		body, _ := io.ReadAll(resp.Body)
		return resp.StatusCode, body
	}

	t.Run("random id answers 200 and empty (AMWA test_25)", func(t *testing.T) {
		code, body := get(t, url("/sources?query.ancestry_id=1b1b1b1b-0000-4000-8000-000000000000&query.ancestry_type=children"))
		if code != 200 {
			t.Fatalf("HTTP %d: %s", code, body)
		}
		var list []json.RawMessage
		if err := json.Unmarshal(body, &list); err != nil || len(list) != 0 {
			t.Fatalf("want empty list, got %s", body)
		}
	})

	t.Run("children of A", func(t *testing.T) {
		code, body := get(t, url("/sources?query.ancestry_id="+a+"&query.ancestry_type=children"))
		if code != 200 {
			t.Fatalf("HTTP %d: %s", code, body)
		}
		var list []map[string]any
		_ = json.Unmarshal(body, &list)
		if len(list) != 2 {
			t.Fatalf("want B and C, got %s", body)
		}
	})

	t.Run("parents of C capped to one generation", func(t *testing.T) {
		code, body := get(t, url("/sources?query.ancestry_id="+c+"&query.ancestry_type=parents&query.ancestry_generations=1"))
		if code != 200 {
			t.Fatalf("HTTP %d: %s", code, body)
		}
		var list []map[string]any
		_ = json.Unmarshal(body, &list)
		if len(list) != 1 || list[0]["id"] != b {
			t.Fatalf("want exactly B, got %s", body)
		}
	})

	t.Run("senders stay 501", func(t *testing.T) {
		code, _ := get(t, url("/senders?query.ancestry_id="+a+"&query.ancestry_type=children"))
		if code != 501 {
			t.Fatalf("HTTP %d, want 501 — ancestry is undefined for senders", code)
		}
	})

	t.Run("bad type is the client's error", func(t *testing.T) {
		code, _ := get(t, url("/sources?query.ancestry_id="+a+"&query.ancestry_type=cousins"))
		if code != 400 {
			t.Fatalf("HTTP %d, want 400", code)
		}
	})

	t.Run("bad generations is the client's error", func(t *testing.T) {
		code, _ := get(t, url("/sources?query.ancestry_id="+a+"&query.ancestry_type=children&query.ancestry_generations=zero"))
		if code != 400 {
			t.Fatalf("HTTP %d, want 400", code)
		}
	})
}
