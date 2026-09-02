package registry

import (
	"bytes"
	"encoding/json"
	"io"
	stdhttp "net/http"
	"testing"

	"dhs/internal/amwa/codec/is04"
)

// postSubscription POSTs a raw SubscriptionRequest and returns the
// status code + decoded body.
func postSubscription(t *testing.T, base string, req SubscriptionRequest) (int, []byte) {
	t.Helper()
	body, _ := json.Marshal(req)
	resp, err := stdhttp.Post(base+"/x-nmos/query/v1.3/subscriptions",
		"application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("POST /subscriptions: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	raw, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, raw
}

// TestSubscriptionAncestryPostValidation: the Query WS accepts the
// same query.ancestry_* control params as the REST listing, with the
// same rejection codes — 501 off sources/flows, 400 when malformed.
// Before this landed the params were silently stripped with the rest
// of query.*, so a subscriber got an unfiltered firehose while its
// subscription echoed the ancestry params back as if honored.
func TestSubscriptionAncestryPostValidation(t *testing.T) {
	store := NewStore()
	mgr := NewSubscriptionManager(nil, store, "127.0.0.1:0", "v1.3")
	base, stop := startRegistryHTTP(t, store, mgr)
	defer stop()
	a, _, _ := seedAncestryChain(t, store)

	cases := []struct {
		name   string
		path   string
		params map[string]string
		want   int
	}{
		{"valid children filter on /sources", "/sources",
			map[string]string{"query.ancestry_id": a, "query.ancestry_type": "children"}, 201},
		{"ancestry undefined for /senders", "/senders",
			map[string]string{"query.ancestry_id": a, "query.ancestry_type": "children"}, 501},
		{"id without type", "/sources",
			map[string]string{"query.ancestry_id": a}, 400},
		{"bad type", "/sources",
			map[string]string{"query.ancestry_id": a, "query.ancestry_type": "cousins"}, 400},
		{"bad generations", "/sources",
			map[string]string{"query.ancestry_id": a, "query.ancestry_type": "children",
				"query.ancestry_generations": "zero"}, 400},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			params := map[string]any{}
			for k, v := range tc.params {
				params[k] = v
			}
			code, body := postSubscription(t, "http://"+base, SubscriptionRequest{
				ResourcePath: tc.path, Params: params,
			})
			if code != tc.want {
				t.Fatalf("HTTP %d, want %d: %s", code, tc.want, body)
			}
		})
	}
}

// subForAncestry builds a bare subscription with an ancestry filter
// seeded from the store's snapshot — the exact state a WS-upgraded
// subscriber holds after the sync grains went out.
func subForAncestry(store *Store, rootID, typ string, gens int) *subscription {
	s := &subscription{
		ResourcePath: "/sources",
		ancestryID:   rootID,
		ancestryType: typ,
		ancestryGens: gens,
	}
	s.seedAncestry(store.SnapshotChanges("v1.3"))
	return s
}

func TestSubscriptionAncestrySyncMembership(t *testing.T) {
	store := NewStore()
	a, b, c := seedAncestryChain(t, store)
	sub := subForAncestry(store, a, ancestryChildren, 0)

	for id, want := range map[string]bool{a: false, b: true, c: true} {
		if got := sub.ancestryMember(id); got != want {
			t.Errorf("member(%s) = %v, want %v", id, got, want)
		}
	}

	// One generation only: C drops out.
	sub1 := subForAncestry(store, a, ancestryChildren, 1)
	if !sub1.ancestryMember(b) || sub1.ancestryMember(c) {
		t.Errorf("1-generation filter: member(B)=%v member(C)=%v, want true/false",
			sub1.ancestryMember(b), sub1.ancestryMember(c))
	}
}

// TestSubscriptionAncestryLiveProjection: live changes are clipped to
// the changed resource's membership transition, mirroring
// projectChange's enter/leave semantics.
func TestSubscriptionAncestryLiveProjection(t *testing.T) {
	store := NewStore()
	a, b, _ := seedAncestryChain(t, store)
	sub := subForAncestry(store, a, ancestryChildren, 0)

	body := func(id string, parents ...string) json.RawMessage {
		if parents == nil {
			parents = []string{}
		}
		raw, _ := json.Marshal(map[string]any{"id": id, "parents": parents})
		return raw
	}

	d := "9c000000-0000-4000-8000-00000000000d"

	t.Run("new descendant enters the set", func(t *testing.T) {
		got, ok := sub.ancestryProject(Change{
			Kind: ChangeCreated, ResourceType: is04.ResourceSource,
			ID: d, Post: body(d, b),
		})
		if !ok || got.Kind != ChangeCreated || len(got.Post) == 0 {
			t.Fatalf("ok=%v kind=%v post=%d bytes — want a created grain", ok, got.Kind, len(got.Post))
		}
	})

	t.Run("reparent away leaves the set as a delete", func(t *testing.T) {
		got, ok := sub.ancestryProject(Change{
			Kind: ChangeUpdated, ResourceType: is04.ResourceSource,
			ID: d, Pre: body(d, b), Post: body(d),
		})
		if !ok || got.Kind != ChangeDeleted || got.Post != nil {
			t.Fatalf("ok=%v kind=%v — want a pre-only deleted grain", ok, got.Kind)
		}
	})

	t.Run("outside on both sides emits nothing", func(t *testing.T) {
		e := "9c000000-0000-4000-8000-00000000000e"
		if _, ok := sub.ancestryProject(Change{
			Kind: ChangeCreated, ResourceType: is04.ResourceSource,
			ID: e, Post: body(e),
		}); ok {
			t.Fatal("orphan source must not pass a children-of-A filter")
		}
	})

	t.Run("root is not its own child", func(t *testing.T) {
		if _, ok := sub.ancestryProject(Change{
			Kind: ChangeUpdated, ResourceType: is04.ResourceSource,
			ID: a, Pre: body(a), Post: body(a),
		}); ok {
			t.Fatal("the ancestry root must not match its own filter")
		}
	})
}

// TestSubscriptionWithoutAncestryUnchanged pins the no-filter fast
// path: every change passes through untouched.
func TestSubscriptionWithoutAncestryUnchanged(t *testing.T) {
	sub := &subscription{ResourcePath: "/sources"}
	in := Change{Kind: ChangeUpdated, ResourceType: is04.ResourceSource,
		ID: "x", Pre: json.RawMessage(`{"id":"x"}`), Post: json.RawMessage(`{"id":"x"}`)}
	got, ok := sub.ancestryProject(in)
	if !ok || got.Kind != ChangeUpdated || len(got.Pre) == 0 || len(got.Post) == 0 {
		t.Fatalf("no-filter path altered the change: ok=%v kind=%v", ok, got.Kind)
	}
	if !sub.ancestryMember("anything") {
		t.Fatal("no-filter membership must be universal")
	}
}
