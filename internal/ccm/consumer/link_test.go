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
