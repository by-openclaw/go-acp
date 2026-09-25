package consumer

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

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
