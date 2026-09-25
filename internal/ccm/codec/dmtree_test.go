package codec

import (
	"encoding/json"
	"testing"
)

// DMTree is pure data — which is exactly why it is worth testing
// directly rather than only through the walk that fills it: a device
// model that reorders itself between two captures makes every firmware
// diff unreadable, and nothing about that failure looks like a bug in
// the walk.

func TestDMTreeHoldsWhatTheWalkPutsIn(t *testing.T) {
	tree := NewDMTree("https://bridge.invalid/api/v1")
	if tree.Base != "https://bridge.invalid/api/v1" {
		t.Errorf("base = %q", tree.Base)
	}
	if tree.Len() != 0 || len(tree.Branches) != 0 {
		t.Errorf("a new tree is empty: %d resources, %d branches", tree.Len(), len(tree.Branches))
	}

	tree.AddResource("/self", json.RawMessage(`{"app":"BRIDGE"}`))
	tree.AddResource("/io/sdi", json.RawMessage(`[{"uuid":"sdi-7"}]`))
	tree.AddBranch("/io")

	if tree.Len() != 2 {
		t.Errorf("Len = %d, want 2 — branches are not resources", tree.Len())
	}
	if len(tree.Branches) != 1 || tree.Branches[0] != "/io" {
		t.Errorf("branches = %v", tree.Branches)
	}
	if string(tree.Resources["/self"]) != `{"app":"BRIDGE"}` {
		t.Errorf("body = %s", tree.Resources["/self"])
	}
}

func TestDMTreeKeepsItsOwnCopyOfEveryBody(t *testing.T) {
	// The walk hands it the read buffer. A tree that kept the caller's
	// slice would have every resource change under it the moment that
	// buffer was reused for the next GET — and the corruption would look
	// like a device that answers one endpoint with another's payload.
	tree := NewDMTree("base")
	body := []byte(`{"locked":true}`)
	tree.AddResource("/misc/reference", body)

	copy(body, []byte(`{"locked":FALS`))
	if got := string(tree.Resources["/misc/reference"]); got != `{"locked":true}` {
		t.Errorf("the tree aliased the caller's buffer: %s", got)
	}
}

func TestSortedPathsIsStableAcrossCaptures(t *testing.T) {
	// Two captures of one device must diff cleanly, and Go's map order
	// is deliberately random — so the order has to come from here.
	paths := []string{"/processing/video", "/self", "/io/sdi", "/io/ip/senders"}
	want := []string{"/io/ip/senders", "/io/sdi", "/processing/video", "/self"}

	for run := 0; run < 5; run++ {
		tree := NewDMTree("base")
		for _, p := range paths {
			tree.AddResource(p, json.RawMessage(`{}`))
		}
		got := tree.SortedPaths()
		if len(got) != len(want) {
			t.Fatalf("paths = %v", got)
		}
		for i := range want {
			if got[i] != want[i] {
				t.Fatalf("run %d: paths = %v, want %v", run, got, want)
			}
		}
	}

	if got := NewDMTree("base").SortedPaths(); len(got) != 0 {
		t.Errorf("an empty tree has no paths: %v", got)
	}
}

func TestNodeKindReadsAsTheWordAnOperatorUses(t *testing.T) {
	// It ends up in log lines and deviation text.
	if got := NodeBranch.String(); got != "branch" {
		t.Errorf("NodeBranch = %q", got)
	}
	if got := NodeResource.String(); got != "resource" {
		t.Errorf("NodeResource = %q", got)
	}
	// Anything that is not a branch reads as a resource rather than as
	// a number nobody can act on.
	if got := NodeKind(42).String(); got != "resource" {
		t.Errorf("NodeKind(42) = %q", got)
	}
}

func TestClassifyRefusesAMalformedArray(t *testing.T) {
	// An array of strings is a branch and the walk recurses into every
	// name, so a body it cannot read is an error rather than a guess —
	// guessing would have the walk fetch paths the device never named.
	if _, _, err := ClassifyBody([]byte(`["ok","\q"]`)); err == nil {
		t.Error("an array with an invalid escape must be an error")
	}
	// And a well-formed array of strings still classifies as a branch,
	// which is the case the check above must not have broken.
	kind, names, err := ClassifyBody([]byte(`["self","io","processing"]`))
	if err != nil || kind != NodeBranch {
		t.Fatalf("kind = %v, err = %v", kind, err)
	}
	if len(names) != 3 || names[0] != "self" || names[2] != "processing" {
		t.Errorf("children = %v", names)
	}
}
