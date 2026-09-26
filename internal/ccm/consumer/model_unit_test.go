package consumer

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"dhs/internal/ccm/codec"
	"dhs/internal/clock"
	dhsc "dhs/internal/consumer"
	"dhs/internal/plugin"
)

// countingNeuron serves a fixed set of routes and counts what was
// asked for, so a test can assert round trips NOT taken as well as
// answers returned.
func countingNeuron(routes map[string]string) (*httptest.Server, func(prefix string) int) {
	var (
		mu   sync.Mutex
		seen []string
	)
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		seen = append(seen, r.URL.Path)
		mu.Unlock()
		body, ok := routes[r.URL.Path]
		if !ok {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		_, _ = w.Write([]byte(body))
	})
	srv := httptest.NewServer(mux)
	return srv, func(prefix string) int {
		mu.Lock()
		defer mu.Unlock()
		n := 0
		for _, p := range seen {
			if strings.HasPrefix(p, prefix) {
				n++
			}
		}
		return n
	}
}

func testPlugin(t *testing.T, srv *httptest.Server) *Plugin {
	t.Helper()
	f := &Factory{}
	p, ok := f.New(plugin.Deps{Logger: quiet(), Clock: clock.System()}).(*Plugin)
	if !ok {
		t.Fatal("the factory did not build a ccm plugin")
	}
	return p
}

// The pure half of building a model: what a JSON body becomes, and
// what it must never become.

func TestFlattenGivesOneObjectPerLeaf(t *testing.T) {
	body := []byte(`{"ptp":{"domain":77,"locked":true,"offset":-0.5},"name":"ref"}`)
	objs := flatten("/misc/reference", body, true)

	got := map[string]dhsc.Value{}
	for _, o := range objs {
		got[strings.Join(o.Path, ".")] = o.Value
		if o.Access&accessWrite == 0 {
			t.Errorf("%v should be writable", o.Path)
		}
	}
	if len(got) != 4 {
		t.Fatalf("objects = %v", got)
	}
	if v := got["misc.reference.ptp.domain"]; v.Kind != dhsc.KindInt || v.Int != 77 {
		t.Errorf("domain = %+v — a whole number is an integer, not a float", v)
	}
	if v := got["misc.reference.ptp.locked"]; v.Kind != dhsc.KindBool || !v.Bool {
		t.Errorf("locked = %+v", v)
	}
	if v := got["misc.reference.ptp.offset"]; v.Kind != dhsc.KindFloat || v.Float != -0.5 {
		t.Errorf("offset = %+v", v)
	}
	if v := got["misc.reference.name"]; v.Kind != dhsc.KindString || v.Str != "ref" {
		t.Errorf("name = %+v", v)
	}
}

func TestAnArrayElementIsAddressedByItsUUIDWhenItHasOne(t *testing.T) {
	// An index moves when the device reorders; a uuid does not. An
	// operator's saved path, an alarm rule and a DM diff all break on
	// the first reorder if the index is the address.
	body := []byte(`[{"uuid":"s-2","name":"b"},{"uuid":"s-1","name":"a"}]`)
	objs := flatten("/io/ip/senders/video", body, false)

	paths := map[string]bool{}
	for _, o := range objs {
		paths[strings.Join(o.Path, ".")] = true
		if o.Access&accessWrite != 0 {
			t.Errorf("%v must be read-only", o.Path)
		}
	}
	for _, want := range []string{"io.ip.senders.video.s-1.name", "io.ip.senders.video.s-2.name"} {
		if !paths[want] {
			t.Errorf("missing %s (got %v)", want, paths)
		}
	}

	// Without a uuid there is nothing better than the index.
	idx := flatten("/misc/list", []byte(`["a","b"]`), false)
	if len(idx) != 2 || strings.Join(idx[0].Path, ".") != "misc.list.0" {
		t.Errorf("indexed = %v", idx)
	}
}

func TestANonJSONBodyIsStillARealResource(t *testing.T) {
	// The device answers /docs/api.yml with YAML, and a firmware could
	// answer anything with anything. Dropping it would hide a resource
	// that exists.
	objs := flatten("/misc/raw", []byte("not json at all"), false)
	if len(objs) != 1 || objs[0].Value.Str != "not json at all" {
		t.Errorf("objects = %+v", objs)
	}
}

func TestANullFieldIsRecordedRatherThanDropped(t *testing.T) {
	// A field the device declares and has no value for is information:
	// its absence should be visible, not inferred from a gap.
	objs := flatten("/misc/x", []byte(`{"unset":null}`), false)
	if len(objs) != 1 || objs[0].Value.Kind != dhsc.KindString || objs[0].Value.Str != "" {
		t.Errorf("objects = %+v", objs)
	}
}

func TestSummariseKeepsAModelReadable(t *testing.T) {
	// The real device puts a JPEG in every video channel.
	long := strings.Repeat("a", maxValueLen+10)
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"short text is itself", "Main", "Main"},
		{"binary is described", "\xff\xd8\xff\xe0JFIF\x00", "<binary, 9 bytes>"},
		{"a control byte is binary", "ok\x00then", "<binary, 7 bytes>"},
		{"long text is cut, with its length", long, long[:maxValueLen] + "… <266 bytes total>"},
		{"tabs and newlines are text", "a\tb\nc", "a\tb\nc"},
	}
	for _, c := range cases {
		if got := summarise(c.in); got != c.want {
			t.Errorf("%s: summarise(%q) = %q, want %q", c.name, c.in, got, c.want)
		}
	}
	// And the length survives even when the bytes do not.
	o := leaf([]string{"x", "thumbnail"}, "\xff\xd8binary", false)
	if o.MaxLen != 8 {
		t.Errorf("MaxLen = %d, want the real length", o.MaxLen)
	}
}

func TestParseListingReadsWhicheverShapeACollectionTakes(t *testing.T) {
	cases := []struct {
		name string
		body string
		want []string
	}{
		{"objects with uuids", `[{"uuid":"a"},{"uuid":"b"}]`, []string{"a", "b"}},
		{"objects with numeric ids", `[{"id":0},{"id":7}]`, []string{"0", "7"}},
		{"a list of names", `["main","backup"]`, []string{"main", "backup"}},
		{"a list of channel uuids", `["ch-a","ch-b"]`, []string{"ch-a", "ch-b"}},
		{"an element with neither", `[{"name":"x"},{"uuid":"a"}]`, []string{"a"}},
		{"empty", `[]`, nil},
		{"not a collection", `{"a":1}`, nil},
		{"not json", `nonsense`, nil},
	}
	for _, c := range cases {
		got := parseListing([]byte(c.body))
		if len(got.ids) != len(c.want) {
			t.Errorf("%s: ids = %v, want %v", c.name, got.ids, c.want)
			continue
		}
		for i := range got.ids {
			if got.ids[i] != c.want[i] {
				t.Errorf("%s: ids = %v, want %v", c.name, got.ids, c.want)
				break
			}
		}
	}

	// An object element is kept whole, because the collection may
	// already be the answer the member read would return. A named
	// element is an id with no body — there is nothing to keep.
	l := parseListing([]byte(`[{"uuid":"a","gain":-3},{"uuid":"b","gain":0}]`))
	if string(l.members["a"]) != `{"uuid":"a","gain":-3}` {
		t.Errorf("member body = %q", l.members["a"])
	}
	if got := parseListing([]byte(`["ch-a"]`)); len(got.members) != 0 {
		t.Errorf("a list of names has no member bodies: %v", got.members)
	}
}

func TestAListingStandsInForAMemberOnlyOnceProven(t *testing.T) {
	// The shortcut is worth 63 round trips on a 64-member collection,
	// and worth nothing if it is wrong — so it is decided by reading
	// one member, not by trusting the shape.
	cases := []struct {
		name          string
		member        string // what the member resource answers
		wantCollected bool   // whether the listing is then trusted
	}{
		{"the listing is the whole member", `{"uuid":"a","gain":-3}`, true},
		{"the listing was a summary", `{"uuid":"a","gain":-3,"status":"ok"}`, false},
	}
	for _, c := range cases {
		srv, hits := countingNeuron(map[string]string{
			"/things":   `[{"uuid":"a","gain":-3},{"uuid":"b","gain":-3}]`,
			"/things/a": c.member,
			"/things/b": c.member,
		})
		defer srv.Close()

		p := testPlugin(t, srv)
		plan := newWalkPlan()
		paths := p.expand(context.Background(), testClient(srv), plan, "/things/{uuid}")
		if len(paths) != 2 {
			t.Fatalf("%s: expanded to %v", c.name, paths)
		}
		for _, path := range paths {
			if _, err := p.read(context.Background(), testClient(srv), plan, path); err != nil {
				t.Fatalf("%s: read %s: %v", c.name, path, err)
			}
		}
		if got := plan.trusted["/things"]; got != c.wantCollected {
			t.Errorf("%s: trusted = %v", c.name, got)
		}
		// Either way the first member is read; the second is read only
		// when the listing could not be trusted.
		want := 1
		if !c.wantCollected {
			want = 2
		}
		if got := hits("/things/"); got != want {
			t.Errorf("%s: member reads = %d, want %d", c.name, got, want)
		}
	}
}

func TestExpandReachesAParameterBelowAParameter(t *testing.T) {
	// The shuffler routes audio per channel, so the resource that
	// carries a channel's state is two parameters deep. Expanding one
	// of them would send `{channelUuid}` to the device as a literal.
	srv, _ := countingNeuron(map[string]string{
		"/rx":                `[{"uuid":"r1"},{"uuid":"r2"}]`,
		"/rx/r1/channels":    `["c1","c2"]`,
		"/rx/r2/channels":    `[]`,
		"/rx/r1/channels/c1": `{"gain":-3}`,
		"/rx/r1/channels/c2": `{"gain":0}`,
	})
	defer srv.Close()

	p := testPlugin(t, srv)
	plan := newWalkPlan()
	got := p.expand(context.Background(), testClient(srv), plan, "/rx/{uuid}/channels/{channelUuid}")
	want := []string{"/rx/r1/channels/c1", "/rx/r1/channels/c2"}
	if len(got) != len(want) {
		t.Fatalf("expanded to %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("expanded to %v, want %v", got, want)
		}
	}
	// No placeholder ever reaches the device.
	for _, path := range got {
		if strings.Contains(path, "{") {
			t.Errorf("a placeholder survived: %s", path)
		}
	}
	// Both the outer and the inner collection are read for ids only,
	// so neither body is flattened into the model twice.
	if !plan.collections["/rx"] || !plan.collections["/rx/r1/channels"] {
		t.Errorf("collections = %v", plan.collections)
	}
}

func TestSameJSONComparesValuesNotBytes(t *testing.T) {
	cases := []struct {
		name string
		a, b string
		want bool
	}{
		{"key order does not decide it", `{"a":1,"b":2}`, `{"b":2,"a":1}`, true},
		{"whitespace does not either", `{"a":1}`, "{\n  \"a\": 1\n}", true},
		{"a missing field does", `{"a":1}`, `{"a":1,"b":2}`, false},
		{"two identical non-JSON bodies", "not json", "not json", true},
		{"two different non-JSON bodies", "not json", "also not", false},
	}
	for _, c := range cases {
		if got := sameJSON([]byte(c.a), []byte(c.b)); got != c.want {
			t.Errorf("%s: sameJSON(%q,%q) = %v", c.name, c.a, c.b, got)
		}
	}
}

func TestACollectionCanBeAFieldOfItsParent(t *testing.T) {
	// The shuffler declares per-channel resources but serves no
	// `.../channels` listing: the ids are a field of the member. Every
	// other shape must still say "no", or a walk would invent members.
	srv, _ := countingNeuron(map[string]string{
		"/rx/m1":     `{"uuid":"m1","channels":["c1"],"name":"x","empty":[]}`,
		"/rx/m2":     `["not","an","object"]`,
		"/served":    `[{"uuid":"s1"}]`,
		"/served/s1": `{"uuid":"s1"}`,
	})
	defer srv.Close()
	p := testPlugin(t, srv)
	c := testClient(srv)

	cases := []struct {
		name string
		coll string
		want []string
	}{
		{"a list field is the collection", "/rx/m1/channels", []string{"c1"}},
		{"a field that is not a list is not", "/rx/m1/name", nil},
		{"an empty list is not a collection", "/rx/m1/empty", nil},
		{"a field the parent does not have", "/rx/m1/nothing", nil},
		{"a parent that is not an object", "/rx/m2/channels", nil},
		{"a parent that is not served", "/nope/m1/channels", nil},
		{"nothing above the root", "/channels", nil},
	}
	for _, tc := range cases {
		got, ok := p.listingFromParent(context.Background(), c, newWalkPlan(), tc.coll)
		if (len(tc.want) > 0) != ok {
			t.Errorf("%s: ok = %v", tc.name, ok)
			continue
		}
		for i := range tc.want {
			if got.ids[i] != tc.want[i] {
				t.Errorf("%s: ids = %v, want %v", tc.name, got.ids, tc.want)
			}
		}
	}

	// A collection the device DOES serve is read from the device, and
	// the parent is never consulted.
	if l := p.listing(context.Background(), c, newWalkPlan(), "/served"); len(l.ids) != 1 {
		t.Errorf("served collection = %v", l.ids)
	}
}

func TestExpansionStopsRatherThanCrawling(t *testing.T) {
	// A spec that nests deeper than a walk expands leaves those paths
	// out instead of sending a placeholder or recursing forever.
	srv, _ := countingNeuron(map[string]string{
		"/a":             `["x"]`,
		"/a/x/b":         `["y"]`,
		"/a/x/b/y/c":     `["z"]`,
		"/a/x/b/y/c/z/d": `["w"]`,
	})
	defer srv.Close()

	p := testPlugin(t, srv)
	plan := newWalkPlan()
	got := p.expand(context.Background(), testClient(srv), plan, "/a/{i}/b/{j}/c/{k}/d/{l}")
	for _, path := range got {
		if strings.Contains(path, "{") {
			t.Errorf("a placeholder survived the cap: %s", path)
		}
	}
}

func TestDedupeRemovesTheRepeatsASpecCreates(t *testing.T) {
	// `/processing/video/channels/{id}` and `…/{uuid}` are one
	// resource under two parameter names, and both expand to the same
	// concrete paths.
	in := []string{"/a", "/a", "/b", "/c", "/c", "/c"}
	got := dedupe(in)
	if len(got) != 3 || got[0] != "/a" || got[1] != "/b" || got[2] != "/c" {
		t.Errorf("dedupe = %v", got)
	}
	if got := dedupe([]string{"/only"}); len(got) != 1 {
		t.Errorf("dedupe(one) = %v", got)
	}
	if got := dedupe(nil); got != nil {
		t.Errorf("dedupe(nil) = %v", got)
	}
}

func TestSegmentsSplitsAPathTheWayAnOperatorReadsIt(t *testing.T) {
	got := segments("/io/ip/senders/video")
	if len(got) != 4 || got[0] != "io" || got[3] != "video" {
		t.Errorf("segments = %v", got)
	}
}

func TestASpecWithNoReadablePathIsRefusedBeforeAnyRead(t *testing.T) {
	// A device whose document declares only parameterised paths gives a
	// walk nothing to stand on: there is no collection to expand from.
	// Saying so beats returning an empty model that looks like a device
	// with nothing in it.
	srv, _ := countingNeuron(map[string]string{})
	defer srv.Close()
	p := testPlugin(t, srv)

	spec, err := codec.ParseSpec([]byte("openapi: 3.1.1\npaths:\n  /{uuid}:\n    get:\n      operationId: G\n"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := p.resourcePaths(context.Background(), testClient(srv), spec); err == nil {
		t.Fatal("a spec with no static path must be refused")
	} else if !strings.Contains(err.Error(), "no readable path") {
		t.Errorf("err = %v", err)
	}

	// And the API root is skipped rather than read as a resource — it
	// lists node names that are already paths.
	spec2, err := codec.ParseSpec([]byte("openapi: 3.1.1\npaths:\n  /:\n    get:\n      operationId: Root\n  /self:\n    get:\n      operationId: S\n"))
	if err != nil {
		t.Fatal(err)
	}
	plan, err := p.resourcePaths(context.Background(), testClient(srv), spec2)
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range plan.paths {
		if path == "/" || path == "" {
			t.Errorf("the API root must not be a resource: %v", plan.paths)
		}
	}
}

func TestWalkReportsWhyItCouldNotPlanAtAll(t *testing.T) {
	// The planning failure has to reach the caller, not be swallowed
	// into an empty walk.
	srv, _ := countingNeuron(map[string]string{
		"/self":         `{"app":{"productName":"X","productVersion":"1"}}`,
		"/docs/api.yml": "openapi: 3.1.1\npaths:\n  /{uuid}:\n    get:\n      operationId: G\n",
	})
	defer srv.Close()
	restore := dialClient
	dialClient = func(string) *Client { return testClient(srv) }
	t.Cleanup(func() { dialClient = restore })

	p := testPlugin(t, srv)
	if err := p.Connect(context.Background(), "127.0.0.1", 0); err != nil {
		t.Fatalf("connect: %v", err)
	}
	if _, err := p.Walk(context.Background(), 0); err == nil {
		t.Error("Walk must report a plan it could not build")
	}
}

func TestAMemberThatVanishesBetweenListingAndReadIsReported(t *testing.T) {
	// The collection named it, the member 404s. read() returns the
	// error so the walk can record a declared resource this build does
	// not serve, rather than inventing an empty one.
	srv, _ := countingNeuron(map[string]string{
		"/things": `[{"uuid":"gone","name":"x"}]`,
	})
	defer srv.Close()
	p := testPlugin(t, srv)
	plan := newWalkPlan()
	paths := p.expand(context.Background(), testClient(srv), plan, "/things/{uuid}")
	if len(paths) != 1 {
		t.Fatalf("expanded to %v", paths)
	}
	if _, err := p.read(context.Background(), testClient(srv), plan, paths[0]); err == nil {
		t.Error("a member the device does not serve must be an error")
	}
}

func TestAnElementWithNoIdentifierIsSkipped(t *testing.T) {
	// A collection element carrying neither uuid nor id cannot be
	// addressed, so it is not turned into a member path.
	l := parseListing([]byte(`[{"name":"nameless"},{"uuid":"real"},12]`))
	if len(l.ids) != 1 || l.ids[0] != "real" {
		t.Errorf("ids = %v — only the addressable element counts", l.ids)
	}
}

func TestAValueKindTheModelHasNoCaseForBecomesItsText(t *testing.T) {
	// JSON carries objects and arrays into leaf position only when a
	// device nests something the flattener stopped at. Recording the
	// text beats dropping the object.
	o := leaf([]string{"x"}, map[string]any{"a": 1}, false)
	if o.Kind != dhsc.KindString || o.Value.Str == "" {
		t.Errorf("leaf = %+v", o)
	}
}

func TestTheOperatorsOwnSpecPathAndBaseAreUsedVerbatim(t *testing.T) {
	// --api-spec names ONE document: the probe ladder is not run, so a
	// device that would have answered a different spelling is not
	// silently preferred over what the operator asked for.
	srv, hits := countingNeuron(map[string]string{
		"/self":             `{"app":{"productName":"X","productVersion":"1"}}`,
		"/docs/api.yml":     "openapi: 3.1.1\npaths:\n  /a:\n    get:\n      operationId: A\n",
		"/docs/openapi.yml": "openapi: 3.1.1\npaths:\n  /b:\n    get:\n      operationId: B\n",
	})
	defer srv.Close()
	c := testClient(srv)
	c.specPath = "/docs/openapi.yml"
	doc, from, err := c.FetchSpec(context.Background())
	if err != nil {
		t.Fatalf("FetchSpec: %v", err)
	}
	if from != "/docs/openapi.yml" || !strings.Contains(string(doc), "/b:") {
		t.Errorf("read %q", from)
	}
	if hits("/docs/api.yml") != 0 {
		t.Error("a named spec must not fall back to the other spelling")
	}
}

func TestABaseThatIsOnlyPunctuationIsNoBase(t *testing.T) {
	cases := map[string]string{
		"/":     "",
		"  /  ": "",
		"":      "",
		"api":   "/api",
		"/api/": "/api",
	}
	for in, want := range cases {
		if got := normalizeAPIBase(in); got != want {
			t.Errorf("normalizeAPIBase(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestConnectReportsADeviceThatAnswersNothing(t *testing.T) {
	// Resolve probes the bases; a host that serves neither leaves the
	// connector unconnected with the reason, rather than half-open.
	srv, _ := countingNeuron(map[string]string{})
	defer srv.Close()
	restore := dialClient
	dialClient = func(string) *Client {
		c := testClient(srv)
		c.resolved = false
		return c
	}
	t.Cleanup(func() { dialClient = restore })

	p := testPlugin(t, srv)
	if err := p.Connect(context.Background(), "127.0.0.1", 0); err == nil {
		t.Error("a host that answers no API base must fail to connect")
	}
}

func TestAPathDeeperThanTheCapIsDroppedNotSent(t *testing.T) {
	// Four parameters against a cap of three. The device serves every
	// level, so the walk really does run out of budget rather than out
	// of ids — and what is left still carrying a placeholder is dropped
	// instead of being sent to the device as a literal.
	srv, _ := countingNeuron(map[string]string{
		"/a":             `["x"]`,
		"/a/x/b":         `["y"]`,
		"/a/x/b/y/c":     `["z"]`,
		"/a/x/b/y/c/z/d": `["w"]`,
	})
	defer srv.Close()

	p := testPlugin(t, srv)
	got := p.expand(context.Background(), testClient(srv), newWalkPlan(),
		"/a/{i}/b/{j}/c/{k}/d/{l}")
	if len(got) != 0 {
		t.Errorf("expanded to %v — nothing may survive a cap it did not fit in", got)
	}
}

func TestWatchingReportsAWalkItCouldNotDo(t *testing.T) {
	// A poll profile needs the model. When the walk that would build it
	// fails, the watch says why instead of polling nothing.
	srv, _ := countingNeuron(map[string]string{
		"/self":         `{"app":{"productName":"X","productVersion":"1"}}`,
		"/docs/api.yml": "openapi: 3.1.1\npaths:\n  /{uuid}:\n    get:\n      operationId: G\n",
	})
	defer srv.Close()
	restore := dialClient
	dialClient = func(string) *Client { return testClient(srv) }
	t.Cleanup(func() { dialClient = restore })

	p := testPluginConnected(t, srv)
	if _, err := p.pollProfileFor(context.Background(), dhsc.ValueRequest{Path: "anything"}); err == nil {
		t.Error("a profile built on a failed walk must report the failure")
	}
}

func TestACrosspointWhoseObjectIsNotInTheModelIsSkipped(t *testing.T) {
	// The state map is itself a collection — the spec declares members
	// under it — so the walk reads it for ids and puts no objects in
	// the model. Its crosspoints then have nothing to annotate, and the
	// link pass must step over them rather than index off the end.
	const spec = `openapi: 3.1.1
paths:
  /self:
    get:
      operationId: S
  /m/info:
    get:
      operationId: I
  /m/main:
    get:
      operationId: M
  /m/main/{id}:
    get:
      operationId: MM
`
	srv, _ := countingNeuron(map[string]string{
		"/self":         `{"app":{"productName":"X","productVersion":"1"}}`,
		"/docs/api.yml": spec,
		"/m/info": `{"destinations":[{"path":"/d","template":"CH{idx}","children":[{"id":"d0"}]}],
		             "sources":[{"path":"/s","template":"IP{idx}","children":[{"id":"s0"}]}]}`,
		"/m/main": `{"CH00":"IP00"}`,
	})
	defer srv.Close()
	restore := dialClient
	dialClient = func(string) *Client { return testClient(srv) }
	t.Cleanup(func() { dialClient = restore })

	p := testPluginConnected(t, srv)
	objs, err := p.Walk(context.Background(), 0)
	if err != nil {
		t.Fatalf("Walk: %v", err)
	}
	for _, o := range objs {
		if o.Meta[MetaTarget] != nil {
			t.Errorf("nothing may be annotated: %v", o.Path)
		}
	}
}

func TestAPathThatUsesExactlyTheCapStillExpands(t *testing.T) {
	// Three parameters against a cap of three: the loop runs out of
	// budget on the same pass that completes the path, so the result
	// leaves by the cap's exit rather than the early one. It is a
	// complete path and must survive.
	srv, _ := countingNeuron(map[string]string{
		"/a":         `["x"]`,
		"/a/x/b":     `["y"]`,
		"/a/x/b/y/c": `["z"]`,
	})
	defer srv.Close()

	p := testPlugin(t, srv)
	got := p.expand(context.Background(), testClient(srv), newWalkPlan(), "/a/{i}/b/{j}/c/{k}")
	if len(got) != 1 || got[0] != "/a/x/b/y/c/z" {
		t.Fatalf("expanded to %v, want the one complete path", got)
	}
}

func TestTheNeutralVerbsReadTheirSettingsFromTheEnvironment(t *testing.T) {
	// info, tree, get, watch and alarm carry no CCM flags, so the base,
	// the spec and TLS verification reach the connector through the
	// environment — the same channel every other per-connector setting
	// uses. Every other test replaces dialClient, so this is the only
	// place the real one runs.
	t.Setenv("CCM_API_BASE", "  /api  ")
	t.Setenv("CCM_API_SPEC", " /docs/openapi.yml ")
	t.Setenv("CCM_VERIFY_TLS", "1")

	c := dialClient("10.0.0.1")
	if c == nil {
		t.Fatal("dialClient must build a client")
	}
	if !strings.HasSuffix(c.base, "/api") {
		t.Errorf("base = %q — the surrounding spaces are not part of it", c.base)
	}
	if c.specPath != "/docs/openapi.yml" {
		t.Errorf("specPath = %q", c.specPath)
	}

	// Unset means find the base, and do not verify.
	t.Setenv("CCM_API_BASE", "")
	t.Setenv("CCM_API_SPEC", "")
	t.Setenv("CCM_VERIFY_TLS", "")
	if c := dialClient("10.0.0.1"); c.specPath != "" || c.resolved {
		t.Errorf("an unset environment leaves the base to be probed: %+v", c)
	}
}
