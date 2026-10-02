package consumer

import (
	"context"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	dhsc "dhs/internal/consumer"
	"dhs/internal/plugin"
)

// rootless is a device shaped like SHUFFLE 6.0.0: no document at the
// API base and none at a folder, a collection that lists its members,
// and an api.yml that declares one resource the box does not serve.
func rootless(t *testing.T, spec string) *httptest.Server {
	t.Helper()
	docs := map[string]string{
		"/self":   `{"productName":"SHUFFLE","productVersion":"6.0.0"}`,
		"/coll":   `["a"]`,
		"/coll/a": `{"x":1}`,
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/docs/api.yml" && spec != "" {
			_, _ = w.Write([]byte(spec))
			return
		}
		body, ok := docs[r.URL.Path]
		if !ok {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return srv
}

const rootlessSpec = "openapi: 3.1.1\npaths:\n  /self:\n    get:\n      operationId: A\n  /coll:\n    get:\n      operationId: B\n  /coll/{uuid}:\n    get:\n      operationId: C\n  /gone:\n    get:\n      operationId: D\n"

func TestADeviceWithNoAPIRootIsReadFromItsDeclaredPaths(t *testing.T) {
	srv := rootless(t, rootlessSpec)
	c := testClient(srv)

	// The tree walk has nothing to start from (CCM §5.1).
	if _, _, err := c.WalkTree(context.Background()); err == nil || !strings.Contains(err.Error(), "read API root") {
		t.Fatalf("tree walk on a rootless device: %v", err)
	}
	tree, deviations, err := c.WalkResources(context.Background(), plugin.Deps{Logger: quiet()})
	if err != nil {
		t.Fatal(err)
	}
	if got := tree.SortedPaths(); !reflect.DeepEqual(got, []string{"/coll/a", "/self"}) {
		t.Errorf("resources = %q", got)
	}
	if string(tree.Resources["/coll/a"]) != `{"x":1}` {
		t.Errorf("a resource is stored as the device served it: %s", tree.Resources["/coll/a"])
	}
	if !reflect.DeepEqual(tree.Branches, []string{"/coll"}) {
		t.Errorf("branches = %q", tree.Branches)
	}
	// Declared and not served: said, not dropped.
	if len(deviations) != 1 || !strings.HasPrefix(deviations[0], "/gone: ") {
		t.Errorf("deviations = %q", deviations)
	}
}

func TestTheResourceWalkSaysWhyItCannotRun(t *testing.T) {
	ctx := context.Background()
	// No api.yml served.
	if _, _, err := testClient(rootless(t, "")).WalkResources(ctx, plugin.Deps{Logger: quiet()}); err == nil {
		t.Error("no spec, no error")
	}
	// An api.yml that is not one.
	if _, _, err := testClient(rootless(t, "paths: [")).WalkResources(ctx, plugin.Deps{Logger: quiet()}); err == nil {
		t.Error("an unreadable spec, no error")
	}
	// An api.yml that declares nothing readable.
	const writeOnly = "openapi: 3.1.1\npaths:\n  /reset:\n    post:\n      operationId: R\n"
	if _, _, err := testClient(rootless(t, writeOnly)).WalkResources(ctx, plugin.Deps{Logger: quiet()}); err == nil || !strings.Contains(err.Error(), "no readable path") {
		t.Errorf("nothing readable: %v", err)
	}
	// No session.
	p := testPlugin(t, rootless(t, rootlessSpec))
	if _, _, err := p.WalkResources(ctx); err != errNotConnected {
		t.Errorf("not connected: %v", err)
	}
}

func TestIdentityIsReadWhereverTheDevicePutsIt(t *testing.T) {
	// As §15 has it.
	srv := rootless(t, rootlessSpec)
	restore := dialClient
	dialClient = func(string) *Client { return testClient(srv) }
	t.Cleanup(func() { dialClient = restore })
	p := testPluginConnected(t, srv)
	if id, err := p.IdentityProbe(context.Background(), 0); err != nil || id != "SHUFFLE@6.0.0" {
		t.Errorf("identity = %q, %v", id, err)
	}
	if got := p.ComplianceProfile().Snapshot()[SelfUnderApp]; got != 0 {
		t.Errorf("%s noted on a device that follows §15", SelfUnderApp)
	}
	_ = p.Disconnect()

	// Under `app`, as BRIDGE and CONVERT have it: read, and counted.
	d := newTypedDevice(t)
	q := testPluginConnected(t, d.srv)
	if id, err := q.IdentityProbe(context.Background(), 0); err != nil || id != "X@1" {
		t.Errorf("identity = %q, %v", id, err)
	}
	if got := q.ComplianceProfile().Snapshot()[SelfUnderApp]; got != 1 {
		t.Errorf("%s noted %d time(s), want 1", SelfUnderApp, got)
	}
	_ = q.Disconnect()
}

func TestACachedModelStandsInForAWalk(t *testing.T) {
	d := newTypedDevice(t)
	p := testPluginConnected(t, d.srv)
	t.Cleanup(func() { _ = p.Disconnect() })
	cached := []dhsc.Object{
		{Path: []string{"thing", "level"}, Label: "level"},
		{Path: []string{"thing", "mode"}, Label: "mode"},
	}
	// A bridge is one box: there is no slot 3 to seed.
	p.SeedTreeFromCachedObjects(3, cached)
	p.mu.Lock()
	empty := p.tree == nil
	p.mu.Unlock()
	if !empty {
		t.Fatal("a slot this device does not have was seeded")
	}

	p.SeedTreeFromCachedObjects(0, cached)
	before := len(d.requests())
	// A polled watch plans from the model: with one seeded, it reads
	// nothing from the device to make the plan.
	prof, err := p.pollProfileFor(context.Background(), dhsc.ValueRequest{Path: "thing"})
	if err != nil || len(prof.Entries) != 2 {
		t.Fatalf("profile = %+v, %v", prof, err)
	}
	if after := len(d.requests()); after != before {
		t.Errorf("%d request(s) to the device with a model already in hand", after-before)
	}
	p.mu.Lock()
	_, known := p.byPath["thing.mode"]
	p.mu.Unlock()
	if !known {
		t.Error("the seeded model is not addressable by path")
	}
}
