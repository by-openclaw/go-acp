package registry

// A registry hands back what was registered (#1338), and shows a
// resource registered at a later minor on its earlier endpoints with the
// keys that minor does not know removed (#1337 — IS-04 "Upgrade Path").

import (
	"encoding/json"
	stdhttp "net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"dhs/internal/amwa/codec/is04"
	httpsession "dhs/internal/amwa/session/http"
)

// withKeys is a typed fixture as a Node would send it: marshalled, then
// given keys the types do not model, or given other values.
func withKeys(t *testing.T, v any, set map[string]any) json.RawMessage {
	t.Helper()
	var doc map[string]any
	if err := json.Unmarshal(mustJSONBytes(t, v), &doc); err != nil {
		t.Fatal(err)
	}
	for k, val := range set {
		doc[k] = val
	}
	return mustJSONBytes(t, doc)
}

func register(t *testing.T, s *Store, typ is04.ResourceType, doc json.RawMessage, apiVer string) {
	t.Helper()
	if err := s.IngestRegistrationVersioned(&is04.RegistrationRequest{Type: typ, Data: doc}, apiVer); err != nil {
		t.Fatalf("register %s at %s: %v", typ, apiVer, err)
	}
}

func jsonEqual(a, b []byte) bool {
	var x, y any
	return json.Unmarshal(a, &x) == nil && json.Unmarshal(b, &y) == nil && reflect.DeepEqual(x, y)
}

// queryAt serves the store's Query API at v1.2 and v1.3.
func queryAt(t *testing.T, store *Store) string {
	t.Helper()
	srv := httpsession.NewServer(nil)
	for _, ver := range []string{"v1.2", "v1.3"} {
		installQueryRoutes(srv, store, nil, "/x-nmos/query/"+ver, ver)
		installRegistrationRoutes(srv, store, "/x-nmos/registration/"+ver, ver)
	}
	ts := httptest.NewServer(srv.MuxHandler())
	t.Cleanup(ts.Close)
	return ts.URL + "/x-nmos/query/"
}

// nmosCppNode is a Node as nmos-cpp registers its registry's own: a
// service with no `authorization` key — and, here, a vendor's key.
func nmosCppNode(t *testing.T, id string) json.RawMessage {
	return withKeys(t, validNode(id), map[string]any{
		"services": []any{map[string]any{"href": "http://10.6.250.104:8110/x-dns-sd/v1.0", "type": "urn:x-dns-sd/v1.0"}},
		"vendor_x": map[string]any{"keep": 1},
	})
}

func TestTheQueryAPIServesTheDocumentThatWasRegistered(t *testing.T) {
	store := NewStore()
	doc := nmosCppNode(t, fxNode)
	register(t, store, is04.ResourceNode, doc, "v1.3")
	// A second Node that came in as a typed value has no document.
	const typedOnly = "33333333-3333-4333-8333-333333333333"
	if err := store.PutNode(validNode(typedOnly)); err != nil {
		t.Fatal(err)
	}
	base := queryAt(t, store)

	// Its own minor: the document, key for key — no `authorization`
	// invented on the service, the vendor's key still there.
	status, body := get(t, base+"v1.3/nodes/"+fxNode)
	if status != stdhttp.StatusOK || !jsonEqual(body, doc) {
		t.Fatalf("GET at v1.3 = %d %s\nwant the registered document %s", status, body, doc)
	}
	if got := store.Document(is04.ResourceNode, fxNode, "v1.3"); !jsonEqual(got, doc) {
		t.Errorf("Document at v1.3 = %s", got)
	}

	// The Registration API reads it back the same.
	registration := strings.Replace(base, "/query/", "/registration/", 1)
	if status, body = get(t, registration+"v1.3/resource/nodes/"+fxNode); status != stdhttp.StatusOK || !jsonEqual(body, doc) {
		t.Errorf("the Registration API read back %d %s", status, body)
	}
	if status, body = get(t, registration+"v1.3/resource/nodes/"+typedOnly); status != stdhttp.StatusOK || !strings.Contains(string(body), typedOnly) {
		t.Errorf("the Registration API read a typed-only node back as %d %s", status, body)
	}

	// The list carries it the same way, beside the typed-only Node,
	// which is encoded from its typed value as it always was.
	_, body = get(t, base+"v1.3/nodes")
	var listed []json.RawMessage
	if err := json.Unmarshal(body, &listed); err != nil || len(listed) != 2 {
		t.Fatalf("list at v1.3 = %s (%v)", body, err)
	}
	found := false
	for _, item := range listed {
		found = found || jsonEqual(item, doc)
	}
	if !found {
		t.Errorf("the list at v1.3 does not carry the registered document: %s", body)
	}
	if status, body = get(t, base+"v1.3/nodes/"+typedOnly); status != stdhttp.StatusOK || !strings.Contains(string(body), `"id"`) {
		t.Errorf("a typed-only node at v1.3 = %d %s", status, body)
	}
	if store.Document(is04.ResourceNode, typedOnly, "v1.3") != nil {
		t.Error("a typed-only node has a document on file")
	}

	// An earlier minor: shown (IS-04 Upgrade Path), with what v1.3
	// introduced removed and the vendor's key left alone.
	want := translateDown(is04.ResourceNode, doc, "v1.3", "v1.2")
	status, body = get(t, base+"v1.2/nodes/"+fxNode)
	if status != stdhttp.StatusOK || !jsonEqual(body, want) {
		t.Fatalf("GET at v1.2 = %d %s\nwant the translation %s", status, body, want)
	}
	if strings.Contains(string(body), "attached_network_device") || !strings.Contains(string(body), "vendor_x") {
		t.Errorf("the v1.2 view kept a v1.3 key or lost the vendor's: %s", body)
	}
	_, body = get(t, base+"v1.2/nodes")
	if err := json.Unmarshal(body, &listed); err != nil || len(listed) != 2 {
		t.Fatalf("list at v1.2 = %s (%v)", body, err)
	}

	// The SYNC snapshot of a subscription is the same documents.
	for ver, wantDoc := range map[string]json.RawMessage{"v1.3": doc, "v1.2": want} {
		sync := store.SnapshotChangesFor(ver, is04.ResourceNode)
		var mine json.RawMessage
		for _, c := range sync {
			if c.ID == fxNode {
				mine = c.Post
			}
		}
		if len(sync) != 2 || !jsonEqual(mine, wantDoc) {
			t.Errorf("snapshot at %s = %s, want %s", ver, mine, wantDoc)
		}
	}
}

// A page that is not a slice, and a page of resources with nothing on
// file, go out as they came.
func TestDocumentsOfLeavesWhatHasNoDocument(t *testing.T) {
	store := populated(t)
	if got := store.documentsOf(is04.ResourceNode, 5, "v1.3"); got != 5 {
		t.Errorf("a page that is not a slice became %v", got)
	}
	page := []is04.Node{validNode(fxNode)}
	if got, ok := store.documentsOf(is04.ResourceNode, page, "v1.3").([]is04.Node); !ok || len(got) != 1 {
		t.Errorf("a typed page with nothing on file became %T", store.documentsOf(is04.ResourceNode, page, "v1.3"))
	}
}

// changes records what the store announces.
func changes(store *Store) *[]Change {
	var seen []Change
	store.AddListener(func(c Change) { seen = append(seen, c) })
	return &seen
}

// Registering the same document again announces nothing — in another
// order of keys too. A document that differs only in a key the types do
// not model is an update all the same; a refused one changes nothing;
// a deleted resource leaves the file.
func TestARegistrationIsJudgedByItsDocument(t *testing.T) {
	store := NewStore()
	seen := changes(store)
	first := withKeys(t, validNode(fxNode), map[string]any{"vendor_x": 1})
	register(t, store, is04.ResourceNode, first, "v1.3")
	if len(*seen) != 1 || (*seen)[0].Kind != ChangeCreated || !jsonEqual((*seen)[0].RawPost, first) || (*seen)[0].RawPre != nil {
		t.Fatalf("the first registration announced %+v", *seen)
	}

	register(t, store, is04.ResourceNode, first, "v1.3")
	var reordered map[string]any
	_ = json.Unmarshal(first, &reordered)
	spaced, _ := json.MarshalIndent(reordered, "", "  ")
	register(t, store, is04.ResourceNode, spaced, "v1.3")
	// The same content with its keys in another order.
	other := `{"vendor_x":1,` + strings.TrimPrefix(string(withKeys(t, validNode(fxNode), nil)), "{")
	register(t, store, is04.ResourceNode, json.RawMessage(other), "v1.3")
	if len(*seen) != 1 {
		t.Fatalf("the same document registered again announced %d change(s)", len(*seen)-1)
	}

	second := withKeys(t, validNode(fxNode), map[string]any{"vendor_x": 2})
	register(t, store, is04.ResourceNode, second, "v1.3")
	if len(*seen) != 2 {
		t.Fatalf("a document that differs in an unmodelled key announced %d change(s), want 1", len(*seen)-1)
	}
	if c := (*seen)[1]; c.Kind != ChangeUpdated || !jsonEqual(c.RawPre, first) || !jsonEqual(c.RawPost, second) {
		t.Errorf("the update = %+v", c)
	}
	if got := store.Document(is04.ResourceNode, fxNode, "v1.3"); !jsonEqual(got, second) {
		t.Errorf("the document on file = %s", got)
	}

	// Refused: nothing is staged, nothing changes.
	bad := withKeys(t, validNode(fxNode), map[string]any{"href": 5})
	if err := store.IngestRegistrationVersioned(&is04.RegistrationRequest{Type: is04.ResourceNode, Data: bad}, "v1.3"); err == nil {
		t.Fatal("a node whose href is a number was registered")
	}
	store.mu.RLock()
	staged := len(store.arriving[is04.ResourceNode])
	store.mu.RUnlock()
	if staged != 0 || !jsonEqual(store.Document(is04.ResourceNode, fxNode, "v1.3"), second) {
		t.Errorf("a refused registration left %d staged document(s), or changed the file", staged)
	}
	// A body that is not JSON, or has no id to file it under, is not staged.
	store.documentArrives(is04.ResourceNode, "", second)
	store.documentArrives(is04.ResourceNode, fxNode, json.RawMessage(`{`))
	store.documentSettled(is04.ResourceNode, fxNode)
	if len(*seen) != 2 {
		t.Errorf("an unusable body announced something")
	}

	store.DeleteNode(fxNode)
	last := (*seen)[len(*seen)-1]
	if last.Kind != ChangeDeleted || !jsonEqual(last.RawPre, second) || store.Document(is04.ResourceNode, fxNode, "v1.3") != nil {
		t.Errorf("the delete = %+v, document still on file: %t", last, store.Document(is04.ResourceNode, fxNode, "v1.3") != nil)
	}
}

// Every resource type is judged the same way.
func TestAnUnmodelledKeyIsAnUpdateForEveryResourceType(t *testing.T) {
	store := NewStore()
	fixtures := []struct {
		t is04.ResourceType
		v any
	}{
		{is04.ResourceNode, validNode(fxNode)},
		{is04.ResourceDevice, validDevice(fxDevice, fxNode)},
		{is04.ResourceSource, validSource(fxSource, fxDevice)},
		{is04.ResourceFlow, validFlow(fxFlow, fxSource, fxDevice)},
		{is04.ResourceSender, validSender(fxSender, fxDevice)},
		{is04.ResourceReceiver, validReceiver(fxReceiver, fxDevice)},
	}
	for _, f := range fixtures {
		register(t, store, f.t, withKeys(t, f.v, map[string]any{"vendor_x": 1}), "v1.3")
	}
	seen := changes(store)
	for i, f := range fixtures {
		register(t, store, f.t, withKeys(t, f.v, map[string]any{"vendor_x": 2}), "v1.3")
		if len(*seen) != i+1 || (*seen)[i].Kind != ChangeUpdated || (*seen)[i].ResourceType != f.t || !strings.Contains(string((*seen)[i].RawPost), `"vendor_x":2`) {
			t.Fatalf("%s: announced %+v", f.t, (*seen)[len(*seen)-1])
		}
	}
}

// A typed Put replaces a registered resource: the typed value is then
// the truth and the document leaves the file. A resource that came in
// typed and is then registered with the same content gets its document
// on file without a change being announced.
func TestATypedPutAndADocumentTakeTurns(t *testing.T) {
	store := NewStore()
	doc := withKeys(t, validNode(fxNode), map[string]any{"vendor_x": 1})
	register(t, store, is04.ResourceNode, doc, "v1.3")
	seen := changes(store)

	renamed := validNode(fxNode)
	renamed.Label = "renamed"
	if err := store.PutNode(renamed); err != nil {
		t.Fatal(err)
	}
	if c := (*seen)[0]; c.Kind != ChangeUpdated || !jsonEqual(c.RawPre, doc) || c.RawPost != nil {
		t.Errorf("the typed update = %+v", c)
	}
	if store.Document(is04.ResourceNode, fxNode, "v1.3") != nil {
		t.Error("the document outlived a typed Put")
	}

	same := mustJSONBytes(t, renamed)
	register(t, store, is04.ResourceNode, same, "v1.3")
	if len(*seen) != 1 {
		t.Errorf("registering what the store already holds announced a change: %+v", (*seen)[1:])
	}
	if got := store.Document(is04.ResourceNode, fxNode, "v1.3"); !jsonEqual(got, same) {
		t.Errorf("the document on file = %s", got)
	}
}

// A subscriber is sent the registered documents, as its minor shows
// them; a body the projection removed stays removed; a change with no
// document goes out as the codec encodes it.
func TestWireChangeSendsTheRegisteredDocument(t *testing.T) {
	doc := nmosCppNode(t, fxNode)
	typed := mustJSONBytes(t, validNode(fxNode))
	c := Change{Kind: ChangeUpdated, ResourceType: is04.ResourceNode, ID: fxNode, APIVer: "v1.3",
		Pre: typed, Post: typed, RawPre: doc, RawPost: doc}

	if out := wireChange(c, "v1.3"); !jsonEqual(out.Pre, doc) || !jsonEqual(out.Post, doc) {
		t.Errorf("at its own minor: pre %s post %s", out.Pre, out.Post)
	}
	want := translateDown(is04.ResourceNode, doc, "v1.3", "v1.2")
	if out := wireChange(c, "v1.2"); !jsonEqual(out.Post, want) {
		t.Errorf("at an earlier minor: post %s, want %s", out.Post, want)
	}

	removed := c
	removed.Pre = nil // the projection turned it into an addition for this subscriber
	if out := wireChange(removed, "v1.3"); out.Pre != nil || !jsonEqual(out.Post, doc) {
		t.Errorf("a removed body came back: pre %s", out.Pre)
	}

	plain := Change{Kind: ChangeCreated, ResourceType: is04.ResourceNode, ID: fxNode, APIVer: "v1.3", Post: typed}
	if out, enc := wireChange(plain, "v1.3"), reencodeChange(plain, "v1.3"); !jsonEqual(out.Post, enc.Post) {
		t.Errorf("a change with no document: %s, want the codec's %s", out.Post, enc.Post)
	}
}

func TestSameDocument(t *testing.T) {
	for _, tc := range []struct {
		a, b string
		want bool
	}{
		{`{"a":1,"b":2}`, `{"a":1,"b":2}`, true},
		{`{"a":1,"b":2}`, `{"b":2,"a":1}`, true},
		{`{"a":1}`, `{"a":2}`, false},
		{`{"a":1}`, `{`, false},
	} {
		if got := sameDocument(json.RawMessage(tc.a), json.RawMessage(tc.b)); got != tc.want {
			t.Errorf("sameDocument(%s, %s) = %t", tc.a, tc.b, got)
		}
	}
}
