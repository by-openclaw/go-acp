package registry

// Cross-layer pagination walk — the REAL registry HTTP face walked by
// the REAL Query client.
//
// Why this test exists: the client's pagination unit test mocked the
// registry, the registry's paging tests mocked no client, and the two
// mocks agreed with each other while both disagreed with the wire —
// a controller walk against the live 211-sender plant returned 100
// (2026-08-29). IS-04 §6.1.6 Link rel="next" points at NEWER data, so
// a whole-collection walk must anchor at paging.since=0:0 and ascend;
// this test pins the two halves against each other so neither can
// drift alone again.

import (
	"context"
	"fmt"
	"testing"

	"dhs/internal/amwa/codec/is04"
	v13 "dhs/internal/amwa/codec/is04/v13"
	"dhs/internal/amwa/session/query"
)

func TestQueryClientWalksWholeCollectionAcrossPages(t *testing.T) {
	const total = 250 // > 2 spec-default pages of 100

	store := NewStore()
	for i := 0; i < total; i++ {
		n := validNode(fmt.Sprintf("f47ac10b-58cc-4372-a567-0e02b2c3%04d", i))
		if err := store.PutNode(n); err != nil {
			t.Fatalf("put node %d: %v", i, err)
		}
	}

	addr, stop := startRegistryHTTP(t, store, nil)
	defer stop()

	c, err := query.NewClient("http://"+addr, v13.New())
	if err != nil {
		t.Fatal(err)
	}
	nodes, err := c.ListNodes(context.Background(), nil)
	if err != nil {
		t.Fatalf("walk: %v", err)
	}
	if len(nodes) != total {
		t.Fatalf("walk returned %d of %d nodes — pagination chain broken between real registry and real client", len(nodes), total)
	}
	seen := map[string]bool{}
	for _, n := range nodes {
		if seen[n.ID] {
			t.Fatalf("duplicate node %s in walk", n.ID)
		}
		seen[n.ID] = true
	}

	// And the head page really is limited — the walk didn't succeed by
	// the registry ignoring its own paging.
	res := store.ListPaged(is04.ResourceNode, PageOptions{})
	if got := len(res.Items.([]is04.Node)); got != DefaultPageLimit {
		t.Fatalf("head page = %d items, want the default limit %d", got, DefaultPageLimit)
	}
}
