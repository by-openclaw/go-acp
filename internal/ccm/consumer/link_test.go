package consumer

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"dhs/internal/ccm/codec"
	"dhs/internal/clock"
	dhsc "dhs/internal/consumer"
	"dhs/internal/plugin"
)

// A crosspoint that names a resource is a link a UI can follow; one
// that names a label is a lookup table somebody maintains by hand. The
// two products lay their matrices out differently, so the discovery of
// WHICH endpoints describe a matrix is tested against both layouts.

// bridgeLayout is how the video bridge declares a matrix: the states
// sit beside the info.
const bridgeLayout = `openapi: '3.1.2'
paths:
  /v1/self:
    get:
      operationId: 'GetSelf'
  /v1/matrix/video/path/info:
    get:
      operationId: 'GetInfo'
  /v1/matrix/video/path/main:
    get:
      operationId: 'GetMain'
    put:
      operationId: 'SetMain'
  /v1/matrix/video/path/backup:
    get:
      operationId: 'GetBackup'
  /v1/matrix/video/output/info:
    get:
      operationId: 'GetOutInfo'
  /v1/processing/video/channels:
    get:
      operationId: 'GetChannels'
  /v1/io/ip/receivers/video:
    get:
      operationId: 'GetReceivers'
`

// shufflerLayout is how the audio shuffler declares one: the states
// sit a level BELOW the info.
const shufflerLayout = `openapi: 3.1.1
paths:
  /self:
    get:
      operationId: GetSelf
  /matrices/audio/info:
    get:
      operationId: GetAudioMatrixInfo
  /matrices/audio/state/main:
    get:
      operationId: GetMain
  /matrices/audio/state/backup:
    get:
      operationId: GetBackup
  /matrices/audio/state/active:
    get:
      operationId: GetActive
`

func specFrom(t *testing.T, doc string) *codec.Spec {
	t.Helper()
	s, err := codec.ParseSpec([]byte(doc))
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestAMatrixIsFoundInEitherLayout(t *testing.T) {
	bridge := specFrom(t, bridgeLayout)
	infos := matrixInfoPaths(bridge)
	if len(infos) != 2 {
		t.Fatalf("info endpoints = %v", infos)
	}
	states := matrixStatePaths(bridge, "/matrix/video/path/info")
	if len(states) != 2 {
		t.Fatalf("states beside the info = %v", states)
	}
	for _, s := range states {
		if strings.HasSuffix(s, "/info") {
			t.Errorf("%s is another matrix's description, not a crosspoint map", s)
		}
	}

	shuf := specFrom(t, shufflerLayout)
	if got := matrixInfoPaths(shuf); len(got) != 1 || got[0] != "/matrices/audio/info" {
		t.Fatalf("shuffler info = %v", got)
	}
	// A level deeper, and all three states found.
	got := matrixStatePaths(shuf, "/matrices/audio/info")
	if len(got) != 3 {
		t.Fatalf("shuffler states = %v", got)
	}

	// An info at the root has no parent to search.
	if s := matrixStatePaths(bridge, "/info"); s != nil {
		t.Errorf("states for a root info = %v", s)
	}
}

// matrixNeuron serves one linkable matrix, the bridge way.
func matrixNeuron(t *testing.T) *httptest.Server {
	t.Helper()
	routes := map[string]string{
		"/self":         `{"app":{"productName":"BRIDGE","productVersion":"7.0.3"}}`,
		"/docs/api.yml": bridgeLayout,
		"/matrix/video/path/info": `{"description":"Video path matrix",
		  "destinations":[{"children":[{"id":"ch-a"},{"id":"ch-b"}],
		    "path":"/api/v1/processing/video/channels","template":"CH{idx}",
		    "type":"Video processing channel"}],
		  "sources":[{"children":[{"id":"rx-0"},{"id":"rx-1"}],
		    "path":"/api/v1/io/ip/receivers/video","template":"IP{idx}","type":"IP"}]}`,
		"/matrix/video/path/main":   `{"CH00":"IP01","CH01":"IP00"}`,
		"/matrix/video/path/backup": `{"CH00":"IP00","CH01":"IP00"}`,
		// Declared but not served: the link pass must survive it.
		"/matrix/video/output/info":  ``,
		"/processing/video/channels": `[{"uuid":"ch-a","name":"A"},{"uuid":"ch-b","name":"B"}]`,
		"/io/ip/receivers/video":     `[{"uuid":"rx-0"},{"uuid":"rx-1"}]`,
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		body, ok := routes[r.URL.Path]
		if !ok || body == "" {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		_, _ = w.Write([]byte(body))
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func TestCrosspointsCarryTheResourceTheyAddress(t *testing.T) {
	srv := matrixNeuron(t)
	restore := dialClient
	dialClient = func(string) *Client { return testClient(srv) }
	t.Cleanup(func() { dialClient = restore })

	f := &Factory{}
	p, _ := f.New(plugin.Deps{Logger: quiet(), Clock: clock.System()}).(*Plugin)
	if err := p.Connect(context.Background(), "127.0.0.1", 0); err != nil {
		t.Fatalf("connect: %v", err)
	}
	objs, err := p.Walk(context.Background(), 0)
	if err != nil {
		t.Fatalf("Walk: %v", err)
	}

	byPath := map[string]dhsc.Object{}
	for _, o := range objs {
		byPath[strings.Join(o.Path, ".")] = o
	}

	o, ok := byPath["matrix.video.path.main.CH00"]
	if !ok {
		t.Fatalf("the crosspoint is missing: %v", keysWithPrefix(byPath, "matrix"))
	}
	if o.Value.Str != "IP01" {
		t.Errorf("value = %q — the device's own answer must survive the linking", o.Value.Str)
	}
	if got := o.Meta[MetaTarget]; got != "/api/v1/processing/video/channels/ch-a" {
		t.Errorf("%s = %v", MetaTarget, got)
	}
	if got := o.Meta[MetaSource]; got != "/api/v1/io/ip/receivers/video/rx-1" {
		t.Errorf("%s = %v", MetaSource, got)
	}
	if got := o.Meta[MetaTargetType]; got != "Video processing channel" {
		t.Errorf("%s = %v", MetaTargetType, got)
	}
	if got := o.Meta[MetaSourceType]; got != "IP" {
		t.Errorf("%s = %v", MetaSourceType, got)
	}
	// A whole-member crosspoint carries no channel.
	if _, has := o.Meta[MetaTargetSub]; has {
		t.Errorf("%s set on a matrix that routes whole members", MetaTargetSub)
	}

	// Every state map is linked, not only the first.
	if b, ok := byPath["matrix.video.path.backup.CH01"]; !ok || b.Meta[MetaSource] == nil {
		t.Errorf("backup crosspoint = %+v", b.Meta)
	}
	// An object that is not a crosspoint is left alone.
	if n, ok := byPath["processing.video.channels.ch-a.name"]; ok && n.Meta[MetaTarget] != nil {
		t.Errorf("a plain object was annotated: %v", n.Meta)
	}
}

// shufflerSpec is the audio shuffler's layout: no /v1, matrices
// spelled the long way, states a level below the info, and the
// per-channel resource the bridge does not have.
const shufflerSpec = `openapi: 3.1.1
paths:
  /self:
    get:
      operationId: GetSelf
  /matrices/audio/info:
    get:
      operationId: GetInfo
  /matrices/audio/state/main:
    get:
      operationId: GetMain
    put:
      operationId: SetMain
  /io/ip/receivers/audio:
    get:
      operationId: GetReceivers
  /io/ip/receivers/audio/{uuid}:
    get:
      operationId: GetReceiver
  /io/ip/receivers/audio/{uuid}/channels/{channelUuid}:
    get:
      operationId: GetReceiverChannel
    put:
      operationId: SetReceiverChannel
  /io/ip/senders/audio:
    get:
      operationId: GetSenders
  /io/ip/senders/audio/{uuid}:
    get:
      operationId: GetSender
`

// shufflerNeuron serves the shuffler's shape: several providers per
// axis, every crosspoint key a CHANNEL uuid, and the ownership of
// those channels stated only by the members themselves.
func shufflerNeuron(t *testing.T) *httptest.Server {
	t.Helper()
	routes := map[string]string{
		"/self":         `{"app":{"productName":"SHUFFLE","productVersion":"2.0.0"}}`,
		"/docs/api.yml": shufflerSpec,
		"/matrices/audio/info": `{
		  "destinations":[{"path":"/io/ip/senders/audio","slots":"channels"},
		                  {"path":"/processing/audio/analyser/channels"}],
		  "sources":[{"path":"/io/ip/receivers/audio","slots":"channels"},
		             {"path":"/io/madi/inputs","slots":"channels"}]}`,
		"/matrices/audio/state/main":             `{"s1":"c2","s2":"m0"}`,
		"/io/ip/receivers/audio":                 `[{"uuid":"rx1"}]`,
		"/io/ip/receivers/audio/rx1":             `{"uuid":"rx1","name":"RX 1","channels":["c1","c2"]}`,
		"/io/ip/receivers/audio/rx1/channels/c1": `{"uuid":"c1","gain":-3}`,
		"/io/ip/receivers/audio/rx1/channels/c2": `{"uuid":"c2","gain":0}`,
		"/io/ip/senders/audio":                   `[{"uuid":"sx1"}]`,
		"/io/ip/senders/audio/sx1":               `{"uuid":"sx1","channels":["s1","s2"]}`,
		// Declared on the matrix but not served by this build.
		"/io/madi/inputs": ``,
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		body, ok := routes[r.URL.Path]
		if !ok || body == "" {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		_, _ = w.Write([]byte(body))
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func TestAChannelCrosspointNamesTheChannelResource(t *testing.T) {
	srv := shufflerNeuron(t)
	restore := dialClient
	dialClient = func(string) *Client { return testClient(srv) }
	t.Cleanup(func() { dialClient = restore })

	f := &Factory{}
	p, _ := f.New(plugin.Deps{Logger: quiet(), Clock: clock.System()}).(*Plugin)
	if err := p.Connect(context.Background(), "127.0.0.1", 0); err != nil {
		t.Fatalf("connect: %v", err)
	}
	objs, err := p.Walk(context.Background(), 0)
	if err != nil {
		t.Fatalf("Walk: %v", err)
	}
	byPath := map[string]dhsc.Object{}
	for _, o := range objs {
		byPath[strings.Join(o.Path, ".")] = o
	}

	// The per-channel resource is two parameters deep. Before the walk
	// expanded both, it was requested with a literal `{channelUuid}`,
	// 404ed, and every channel's own state was missing from the model.
	if _, ok := byPath["io.ip.receivers.audio.rx1.channels.c2.gain"]; !ok {
		t.Errorf("the channel resource is missing: %v",
			keysWithPrefix(byPath, "io.ip.receivers.audio.rx1.channels"))
	}

	o, ok := byPath["matrices.audio.state.main.s1"]
	if !ok {
		t.Fatalf("crosspoint missing: %v", keysWithPrefix(byPath, "matrices"))
	}
	// Destination: channel s1 is channel 0 of sender sx1.
	if got := o.Meta[MetaTarget]; got != "/io/ip/senders/audio/sx1/channels/s1" {
		t.Errorf("%s = %v", MetaTarget, got)
	}
	if got := o.Meta[MetaTargetSub]; got != 0 {
		t.Errorf("%s = %v, want channel 0", MetaTargetSub, got)
	}
	// Source: channel c2 is channel 1 of receiver rx1 — and which
	// receiver owns it is something only the device could say.
	if got := o.Meta[MetaSource]; got != "/io/ip/receivers/audio/rx1/channels/c2" {
		t.Errorf("%s = %v", MetaSource, got)
	}
	if got := o.Meta[MetaSourceSub]; got != 1 {
		t.Errorf("%s = %v, want channel 1", MetaSourceSub, got)
	}

	// A crosspoint whose source lives on a provider this build does not
	// serve keeps its destination and reports no source, rather than
	// attaching the first provider that happened to be listed.
	s2, ok := byPath["matrices.audio.state.main.s2"]
	if !ok {
		t.Fatal("the second crosspoint is missing")
	}
	if s2.Meta[MetaTarget] == nil {
		t.Errorf("destination lost: %v", s2.Meta)
	}
	if got, has := s2.Meta[MetaSource]; has {
		t.Errorf("%s = %v — that channel belongs to nothing this device serves",
			MetaSource, got)
	}
}

func TestAPathTheDeviceWroteIsMadeRequestable(t *testing.T) {
	// The bridge writes its provider paths with the API prefix the
	// client is already based at; the shuffler writes them without.
	// Both have to become something this client can GET.
	srv := matrixNeuron(t)
	c := testClient(srv)
	cases := []struct{ declared, want string }{
		{"/api/v1/processing/video/channels", "/api/v1/processing/video/channels"},
		{"/io/ip/senders/audio", "/io/ip/senders/audio"},
		{"", ""},
	}
	for _, tc := range cases {
		if got := relativeTo(c, tc.declared); got != tc.want {
			t.Errorf("relativeTo(%q) = %q, want %q", tc.declared, got, tc.want)
		}
	}
	// With a based client, the prefix it is already carrying comes off.
	based := testClient(srv)
	based.base = srv.URL + "/api/v1"
	if got := relativeTo(based, "/api/v1/processing/video/channels"); got != "/processing/video/channels" {
		t.Errorf("based: %q", got)
	}
	if got := relativeTo(based, "/io/sdi"); got != "/io/sdi" {
		t.Errorf("a path that never had the prefix keeps its own shape: %q", got)
	}
}

func TestAChannelListIsReadWhicheverWayTheDevicePublishesIt(t *testing.T) {
	// A device may publish a member's channels as a JSON array or as an
	// object keyed by position. Both are the same list, and a matrix
	// that resolved under one and not the other is a connector that
	// works on the device it was written against — which is how a
	// 17 728-crosspoint matrix came back with 64 links.
	routes := map[string]string{
		"/self":         `{"app":{"productName":"SHUFFLE","productVersion":"2.0.0"}}`,
		"/docs/api.yml": shufflerSpec,
		"/matrices/audio/info": `{
		  "destinations":[{"path":"/io/ip/senders/audio","slots":"channels"}],
		  "sources":[{"path":"/io/ip/receivers/audio","slots":"channels"}]}`,
		"/matrices/audio/state/main": `{"s11":"c11"}`,
		"/io/ip/receivers/audio":     `[{"uuid":"rx1"}]`,
		// Twelve channels as an OBJECT keyed by position — and the
		// crosspoint uses channel 11, so a connector that sorted those
		// keys as text would put it in the wrong place even if it read
		// them at all.
		"/io/ip/receivers/audio/rx1": `{"uuid":"rx1","channels":{"0":"c0","1":"c1","2":"c2",
		  "3":"c3","4":"c4","5":"c5","6":"c6","7":"c7","8":"c8","9":"c9","10":"c10","11":"c11"}}`,
		"/io/ip/senders/audio":     `[{"uuid":"sx1"}]`,
		"/io/ip/senders/audio/sx1": `{"uuid":"sx1","channels":["s0","s1","s2","s3","s4","s5","s6","s7","s8","s9","s10","s11"]}`,
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		body, ok := routes[r.URL.Path]
		if !ok {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		_, _ = w.Write([]byte(body))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	restore := dialClient
	dialClient = func(string) *Client { return testClient(srv) }
	t.Cleanup(func() { dialClient = restore })

	f := &Factory{}
	p, _ := f.New(plugin.Deps{Logger: quiet(), Clock: clock.System()}).(*Plugin)
	if err := p.Connect(context.Background(), "127.0.0.1", 0); err != nil {
		t.Fatalf("connect: %v", err)
	}
	objs, err := p.Walk(context.Background(), 0)
	if err != nil {
		t.Fatalf("Walk: %v", err)
	}
	var xp *dhsc.Object
	for i := range objs {
		if strings.Join(objs[i].Path, ".") == "matrices.audio.state.main.s11" {
			xp = &objs[i]
		}
	}
	if xp == nil {
		t.Fatal("the crosspoint is missing")
	}
	// Channel 11 of each side — the twelfth, not the third.
	if got := xp.Meta[MetaTargetSub]; got != 11 {
		t.Errorf("%s = %v, want 11 (an array-published list)", MetaTargetSub, got)
	}
	if got := xp.Meta[MetaSourceSub]; got != 11 {
		t.Errorf("%s = %v, want 11 (an object-published list, in NUMERIC key order)", MetaSourceSub, got)
	}
	if got := xp.Meta[MetaSource]; got != "/io/ip/receivers/audio/rx1/channels/c11" {
		t.Errorf("%s = %v", MetaSource, got)
	}
}

func TestChannelsByMemberReadsThePositionAsANumber(t *testing.T) {
	objs := []dhsc.Object{
		strLeaf([]string{"io", "sd", "m1", "channels", "10"}, "c10"),
		strLeaf([]string{"io", "sd", "m1", "channels", "2"}, "c2"),
		strLeaf([]string{"io", "sd", "m1", "channels", "0"}, "c0"),
		// Not channels, or not a position, or empty: skipped.
		strLeaf([]string{"io", "sd", "m1", "name"}, "x"),
		strLeaf([]string{"io", "sd", "m1", "channels", "notanumber"}, "c"),
		strLeaf([]string{"io", "sd", "m1", "channels", "3"}, ""),
		strLeaf([]string{"channels", "0"}, "too-short"),
		{Path: []string{"io", "sd", "m1", "channels", "4"}, Kind: dhsc.KindInt,
			Value: dhsc.Value{Kind: dhsc.KindInt, Int: 7}},
	}
	got := channelsByMember(objs)
	want := []string{"c0", "c2", "c10"}
	ids := got["io/sd/m1"]
	if len(ids) != len(want) {
		t.Fatalf("channels = %v, want %v", ids, want)
	}
	for i := range want {
		if ids[i] != want[i] {
			t.Fatalf("channels = %v, want %v — 10 sorts after 2", ids, want)
		}
	}
	if len(got) != 1 {
		t.Errorf("members = %v", got)
	}
}

func strLeaf(path []string, v string) dhsc.Object {
	return dhsc.Object{Path: path, Kind: dhsc.KindString,
		Value: dhsc.Value{Kind: dhsc.KindString, Str: v}}
}

func TestIndexAxisAsksTheDeviceOnlyForWhatItMustAsk(t *testing.T) {
	srv, hits := countingNeuron(map[string]string{
		"/whole":       `[{"uuid":"w1"},{"uuid":"w2"}]`,
		"/chan":        `[{"uuid":"m1"}]`,
		"/chan/m1":     `{"uuid":"m1","channels":["c1","c2"]}`,
		"/cached":      `[{"uuid":"k1"}]`,
		"/listedwhole": `[{"uuid":"l1","channels":["x1"]}]`,
		"/empty":       `[]`,
	})
	defer srv.Close()
	p := testPlugin(t, srv)
	c := testClient(srv)
	plan := newWalkPlan()
	// A member the walk already read is not read again.
	plan.bodies["/cached/k1"] = []byte(`{"uuid":"k1","channels":["y1","y2"]}`)

	idx := p.indexAxis(context.Background(), c, plan, nil, []codec.MatrixProvider{
		// Positional: the info body already answers, so nothing is read.
		{Path: "/positional", Template: "CH{idx}"},
		// A provider with no path at all.
		{Path: ""},
		// Members routed whole: the key is the member.
		{Path: "/whole", Type: "IP"},
		// Members routed per channel, read from the member.
		{Path: "/chan", Slots: "channels", Type: "Delay"},
		// Ditto, but the walk already has the body.
		{Path: "/cached", Slots: "channels"},
		// Ditto, but the collection listing carried the whole member.
		{Path: "/listedwhole", Slots: "channels"},
		// A provider this build does not serve.
		{Path: "/missing", Slots: "channels"},
		// A provider that lists nothing.
		{Path: "/empty", Slots: "channels"},
	})

	for _, key := range []string{"w1", "w2", "c1", "c2", "y1", "y2", "x1"} {
		if !idx[key].Resolved {
			t.Errorf("%s did not resolve: %+v", key, idx[key])
		}
	}
	if got := idx["w1"]; got.Path != "/whole/w1" || got.Sub != -1 || got.Type != "IP" {
		t.Errorf("a whole member = %+v", got)
	}
	if got := idx["c2"]; got.Path != "/chan/m1/channels/c2" || got.Sub != 1 || got.ID != "m1" {
		t.Errorf("a channel = %+v", got)
	}
	if hits("/positional") != 0 {
		t.Error("a positional provider needs no read")
	}
	if hits("/cached/k1") != 0 {
		t.Error("a member the walk already read must not be read again")
	}
	if hits("/listedwhole/l1") != 0 {
		t.Error("a member its collection carried whole must not be read again")
	}

	// An axis with nothing to index says so, rather than handing back
	// an empty map that looks like an answer.
	if got := p.indexAxis(context.Background(), c, newWalkPlan(), nil,
		[]codec.MatrixProvider{{Path: "/positional", Template: "CH{idx}"}}); got != nil {
		t.Errorf("index = %v, want nil", got)
	}
}

func TestChannelsOfReadsEitherShape(t *testing.T) {
	cases := []struct {
		name string
		body string
		want []string
	}{
		{"a list of uuids", `{"channels":["a","b"]}`, []string{"a", "b"}},
		{"objects carrying uuids", `{"channels":[{"uuid":"a"},{"uuid":"b"}]}`, []string{"a", "b"}},
		{"a channel this connector cannot name keeps its position",
			`{"channels":["a",{"no":"uuid"},"c"]}`, []string{"a", "", "c"}},
		{"no channels", `{"uuid":"x"}`, nil},
		{"not json", `nonsense`, nil},
	}
	for _, c := range cases {
		got := channelsOf([]byte(c.body))
		if len(got) != len(c.want) {
			t.Errorf("%s: channelsOf = %v, want %v", c.name, got, c.want)
			continue
		}
		for i := range got {
			if got[i] != c.want[i] {
				t.Errorf("%s: channelsOf = %v, want %v", c.name, got, c.want)
				break
			}
		}
	}
}

func TestAnnotateRecordsOnlyWhatResolved(t *testing.T) {
	// A source no provider accounts for leaves the source side blank
	// rather than inventing one — and the destination is still linked,
	// because half an answer beats none.
	o := dhsc.Object{Path: []string{"matrix", "x", "main", "CH00"}}
	annotate(&o, codec.Crosspoint{
		Destination: codec.Endpoint{Key: "CH00", ID: "a", Path: "/p/a", Sub: 3, Type: "Bank", Resolved: true},
		Source:      codec.Endpoint{Key: "NOPE", Resolved: false},
	})
	if o.Meta[MetaTarget] != "/p/a" || o.Meta[MetaTargetSub] != 3 || o.Meta[MetaTargetType] != "Bank" {
		t.Errorf("meta = %v", o.Meta)
	}
	if _, has := o.Meta[MetaSource]; has {
		t.Errorf("an unresolved source must not be recorded: %v", o.Meta)
	}
}

func TestChannelsOfHandlesEveryShapeOrSaysNo(t *testing.T) {
	// The fallback for a member the walk did not read. It has to cope
	// with both publishing shapes and refuse the rest, because a wrong
	// answer here is a matrix that resolves to the wrong channel.
	cases := []struct {
		name string
		body string
		want []string
	}{
		{"an array", `{"channels":["a","b"]}`, []string{"a", "b"}},
		{"an object keyed by position", `{"channels":{"1":"b","0":"a","10":"k"}}`,
			[]string{"a", "b", "k"}},
		{"objects in an array", `{"channels":[{"uuid":"a"},{"uuid":"b"}]}`, []string{"a", "b"}},
		{"objects in a keyed map", `{"channels":{"0":{"uuid":"a"}}}`, []string{"a"}},
		{"an unnameable channel keeps its place", `{"channels":["a",{"no":"id"}]}`,
			[]string{"a", ""}},
		{"no channels field", `{"uuid":"x"}`, nil},
		{"an empty field", `{"channels":null}`, nil},
		{"keys that are not positions", `{"channels":{"left":"a"}}`, nil},
		{"channels that are a number", `{"channels":8}`, nil},
		{"not json", `nonsense`, nil},
	}
	for _, c := range cases {
		got := channelsOf([]byte(c.body))
		if len(got) != len(c.want) {
			t.Errorf("%s: channelsOf = %v, want %v", c.name, got, c.want)
			continue
		}
		for i := range c.want {
			if got[i] != c.want[i] {
				t.Errorf("%s: channelsOf = %v, want %v", c.name, got, c.want)
				break
			}
		}
	}
}

func TestRelativeToSurvivesABaseItCannotParse(t *testing.T) {
	c := &Client{base: "://not a url"}
	if got := relativeTo(c, "/io/sdi"); got != "/io/sdi" {
		t.Errorf("relativeTo = %q — an unparseable base leaves the path alone", got)
	}
}
