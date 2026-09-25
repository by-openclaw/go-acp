package consumer

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	dhsc "dhs/internal/consumer"
)

// The parts of a write that decide whether a device ends up with what
// was asked for: which field is changed, what type it is written as,
// and what happens when the path does not describe anything.

func TestSetFieldChangesOnlyWhatItNames(t *testing.T) {
	doc := func() any {
		var v any
		_ = json.Unmarshal([]byte(`{
		  "ptp":{"domain":77,"priority1":248},
		  "legs":[{"uuid":"a","port":1},{"uuid":"b","port":2}],
		  "names":["x","y"],
		  "flat":1
		}`), &v)
		return v
	}

	cases := []struct {
		name  string
		field []string
		val   dhsc.Value
		want  string // a substring the re-encoded document must contain
	}{
		{"a nested field", []string{"ptp", "domain"},
			dhsc.Value{Kind: dhsc.KindInt, Int: 3}, `"domain":3`},
		{"an array member by uuid", []string{"legs", "b", "port"},
			dhsc.Value{Kind: dhsc.KindInt, Int: 9}, `"port":9`},
		{"an array member by index", []string{"legs", "0", "port"},
			dhsc.Value{Kind: dhsc.KindInt, Int: 8}, `"port":8`},
		{"an array element itself", []string{"names", "1"},
			dhsc.Value{Kind: dhsc.KindString, Str: "z"}, `["x","z"]`},
		{"a bool", []string{"flat"},
			dhsc.Value{Kind: dhsc.KindBool, Bool: true}, `"flat":true`},
		{"a float", []string{"flat"},
			dhsc.Value{Kind: dhsc.KindFloat, Float: 1.5}, `"flat":1.5`},
		{"a string", []string{"flat"},
			dhsc.Value{Kind: dhsc.KindString, Str: "s"}, `"flat":"s"`},
	}
	for _, c := range cases {
		d := doc()
		if err := setField(d, c.field, c.val); err != nil {
			t.Errorf("%s: %v", c.name, err)
			continue
		}
		b, _ := json.Marshal(d)
		if !strings.Contains(string(b), c.want) {
			t.Errorf("%s: %s does not contain %s", c.name, b, c.want)
		}
	}

	// And the refusals — each one a way a write could otherwise go
	// somewhere the operator did not mean.
	bad := []struct {
		name  string
		field []string
	}{
		{"no field at all", nil},
		{"a field that is not there", []string{"ptp", "nope"}},
		{"through a field that is not there", []string{"nope", "domain"}},
		{"through a scalar", []string{"flat", "deeper"}},
		{"an array index past the end", []string{"names", "9"}},
		{"an array index that is not a number", []string{"names", "zz"}},
		{"a uuid no element has", []string{"legs", "zz", "port"}},
		{"a scalar addressed as an array", []string{"flat", "0", "x"}},
	}
	for _, c := range bad {
		if err := setField(doc(), c.field, dhsc.Value{Kind: dhsc.KindInt, Int: 1}); err == nil {
			t.Errorf("%s: must be refused", c.name)
		}
	}
}

func TestAWriteTheDeviceRefusesIsReportedAsTheDeviceSaidIt(t *testing.T) {
	cases := []struct {
		name, body string
		status     int
		want       string
	}{
		{"the device explains itself", `{"message":"matrix is locked"}`, 409, "matrix is locked"},
		{"another spelling", `{"error":"read only"}`, 403, "read only"},
		{"a third", `{"detail":"busy"}`, 503, "busy"},
		{"no explanation", `{}`, 500, "answered 500"},
		{"not even JSON", `nope`, 500, "answered 500"},
	}
	for _, c := range cases {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(c.status)
			_, _ = w.Write([]byte(c.body))
		}))
		err := testClient(srv).put(context.Background(), "/x", map[string]any{"a": 1})
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: err = %v, want it to say %q", c.name, err, c.want)
		}
		srv.Close()
	}

	// A device that is not there at all.
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	c := testClient(srv)
	srv.Close()
	if err := c.put(context.Background(), "/x", nil); err == nil {
		t.Error("an unreachable device must fail")
	}
}

func TestWritingSomethingThatIsNotJSONIsRefused(t *testing.T) {
	// The resource has to be read back and re-encoded, so a body that
	// is not JSON cannot be written to safely — better to say so than
	// to PUT a guess over it.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/self":
			_, _ = w.Write([]byte(`{"app":{"productName":"X","productVersion":"1"}}`))
		case "/docs/api.yml":
			_, _ = w.Write([]byte("openapi: 3.1.1\npaths:\n  /thing:\n    get:\n      operationId: G\n    put:\n      operationId: S\n"))
		case "/thing":
			_, _ = w.Write([]byte("not json"))
		default:
			http.Error(w, "no", http.StatusNotFound)
		}
	}))
	defer srv.Close()
	restore := dialClient
	dialClient = func(string) *Client { return testClient(srv) }
	t.Cleanup(func() { dialClient = restore })

	p := testPluginConnected(t, srv)
	_, err := p.SetValue(context.Background(), dhsc.ValueRequest{Path: "thing.field"},
		dhsc.Value{Kind: dhsc.KindInt, Int: 1})
	if err == nil || !strings.Contains(err.Error(), "not JSON") {
		t.Errorf("err = %v", err)
	}

	// And a resource that cannot be read at all is not written either.
	_, err = p.SetValue(context.Background(), dhsc.ValueRequest{Path: "gone.field"},
		dhsc.Value{Kind: dhsc.KindInt, Int: 1})
	if err == nil {
		t.Error("a resource this device does not serve must fail")
	}
}

// testPluginConnected builds a plugin already pointed at a fake.
func testPluginConnected(t *testing.T, srv *httptest.Server) *Plugin {
	t.Helper()
	p := testPlugin(t, srv)
	if err := p.Connect(context.Background(), "127.0.0.1", 0); err != nil {
		t.Fatalf("connect: %v", err)
	}
	return p
}
