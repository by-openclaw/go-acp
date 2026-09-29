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
		  "flat":1,
		  "on":false,
		  "name":"n"
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
		{"a bool", []string{"on"},
			dhsc.Value{Kind: dhsc.KindBool, Bool: true}, `"on":true`},
		{"a bool from its text", []string{"on"},
			dhsc.Value{Kind: dhsc.KindString, Str: "1"}, `"on":true`},
		{"a bool off from its text", []string{"on"},
			dhsc.Value{Kind: dhsc.KindString, Str: "off"}, `"on":false`},
		{"a float", []string{"flat"},
			dhsc.Value{Kind: dhsc.KindFloat, Float: 1.5}, `"flat":1.5`},
		// The CLI hands over text; a number field still gets a number —
		// the Neuron refuses "20000" with "type must be number".
		{"a number from its text", []string{"flat"},
			dhsc.Value{Kind: dhsc.KindString, Str: "20000"}, `"flat":20000`},
		{"a fraction from its text", []string{"flat"},
			dhsc.Value{Kind: dhsc.KindString, Str: " 2.5 "}, `"flat":2.5`},
		{"a string", []string{"name"},
			dhsc.Value{Kind: dhsc.KindString, Str: "s"}, `"name":"s"`},
		{"an int into a string field", []string{"name"},
			dhsc.Value{Kind: dhsc.KindInt, Int: 7}, `"name":"7"`},
		{"a float into a string field", []string{"name"},
			dhsc.Value{Kind: dhsc.KindFloat, Float: 1.5}, `"name":"1.5"`},
		{"a bool into a string field", []string{"name"},
			dhsc.Value{Kind: dhsc.KindBool, Bool: true}, `"name":"true"`},
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

	// A value that is not what the field is, refused before the wire.
	wrong := []struct {
		name  string
		field []string
		val   dhsc.Value
	}{
		{"text that is no number", []string{"flat"}, dhsc.Value{Kind: dhsc.KindString, Str: "abc"}},
		{"text that is no boolean", []string{"on"}, dhsc.Value{Kind: dhsc.KindString, Str: "maybe"}},
		{"a whole object", []string{"ptp"}, dhsc.Value{Kind: dhsc.KindString, Str: "{}"}},
		{"a whole array element object", []string{"legs", "0"}, dhsc.Value{Kind: dhsc.KindString, Str: "{}"}},
	}
	for _, c := range wrong {
		if err := setField(doc(), c.field, c.val); err == nil {
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

func TestAWriteStopsWhenTheResourceCannotBeReadOrWritten(t *testing.T) {
	// Read-modify-write has two device calls and either can fail. The
	// operator has to be told which, because "could not read it" and
	// "it refused the write" are different problems.
	const spec = "openapi: 3.1.1\npaths:\n  /gone:\n    get:\n      operationId: G\n    put:\n      operationId: S\n  /ro:\n    get:\n      operationId: G2\n    put:\n      operationId: S2\n"
	var putFails bool
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/self":
			_, _ = w.Write([]byte(`{"app":{"productName":"X","productVersion":"1"}}`))
		case r.URL.Path == "/docs/api.yml":
			_, _ = w.Write([]byte(spec))
		case r.URL.Path == "/ro" && r.Method == http.MethodPut:
			putFails = true
			http.Error(w, "read only", http.StatusForbidden)
		case r.URL.Path == "/ro":
			_, _ = w.Write([]byte(`{"field":1}`))
		default:
			http.Error(w, "not found", http.StatusNotFound)
		}
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	restore := dialClient
	dialClient = func(string) *Client { return testClient(srv) }
	t.Cleanup(func() { dialClient = restore })

	p := testPluginConnected(t, srv)

	// The pre-read fails: the resource is declared but not served.
	_, err := p.SetValue(context.Background(), dhsc.ValueRequest{Path: "gone.field"},
		dhsc.Value{Kind: dhsc.KindInt, Int: 1})
	if err == nil || !strings.Contains(err.Error(), "before writing it") {
		t.Errorf("read failure = %v", err)
	}

	// The read works and the device refuses the write.
	_, err = p.SetValue(context.Background(), dhsc.ValueRequest{Path: "ro.field"},
		dhsc.Value{Kind: dhsc.KindInt, Int: 2})
	if err == nil || !strings.Contains(err.Error(), "403") {
		t.Errorf("write refusal = %v", err)
	}
	if !putFails {
		t.Error("the PUT must have been attempted")
	}
}

func TestReadingOneValueReportsADeviceThatWillNotServeIt(t *testing.T) {
	srv, _ := countingNeuron(map[string]string{
		"/self":         `{"app":{"productName":"X","productVersion":"1"}}`,
		"/docs/api.yml": "openapi: 3.1.1\npaths:\n  /gone:\n    get:\n      operationId: G\n",
	})
	defer srv.Close()
	restore := dialClient
	dialClient = func(string) *Client { return testClient(srv) }
	t.Cleanup(func() { dialClient = restore })

	p := testPluginConnected(t, srv)
	if _, err := p.GetValue(context.Background(), dhsc.ValueRequest{Path: "gone.field"}); err == nil {
		t.Error("a declared resource this build does not serve must be an error")
	}
}

func TestAWriteAnswersWithWhatTheDeviceHoldsAfterward(t *testing.T) {
	// The answer to a write is the device's read-back, not the request:
	// a device that clamps, rounds or ignores a value has to be seen
	// doing it. Three resources: one that clamps the port, one whose
	// read-back fails, one that drops the field it was just given.
	const spec = "openapi: 3.1.1\npaths:\n" +
		"  /clamp:\n    get:\n      operationId: G1\n    put:\n      operationId: S1\n" +
		"  /flaky:\n    get:\n      operationId: G2\n    put:\n      operationId: S2\n" +
		"  /drop:\n    get:\n      operationId: G3\n    put:\n      operationId: S3\n"
	var gotBody string
	flakyReads := 0
	dropped := false
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/self":
			_, _ = w.Write([]byte(`{"app":{"productName":"X","productVersion":"1"}}`))
		case r.URL.Path == "/docs/api.yml":
			_, _ = w.Write([]byte(spec))
		case r.URL.Path == "/clamp" && r.Method == http.MethodPut:
			b := make([]byte, 256)
			n, _ := r.Body.Read(b)
			gotBody = string(b[:n])
		case r.URL.Path == "/clamp":
			_, _ = w.Write([]byte(`{"port":65535}`))
		case r.URL.Path == "/flaky" && r.Method == http.MethodGet:
			flakyReads++
			if flakyReads > 1 {
				http.Error(w, "gone", http.StatusInternalServerError)
				return
			}
			_, _ = w.Write([]byte(`{"port":1}`))
		case r.URL.Path == "/drop" && r.Method == http.MethodPut:
			dropped = true
		case r.URL.Path == "/drop":
			if dropped {
				_, _ = w.Write([]byte(`{"other":1}`))
				return
			}
			_, _ = w.Write([]byte(`{"port":1}`))
		}
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	restore := dialClient
	dialClient = func(string) *Client { return testClient(srv) }
	t.Cleanup(func() { dialClient = restore })
	p := testPluginConnected(t, srv)

	// Text from the CLI goes out as a number, and the answer is the
	// device's clamped value, not the 70000 that was asked for.
	got, err := p.SetValue(context.Background(), dhsc.ValueRequest{Path: "clamp.port"},
		dhsc.Value{Kind: dhsc.KindString, Str: "70000"})
	if err != nil {
		t.Fatalf("clamp: %v", err)
	}
	if !strings.Contains(gotBody, `"port":70000`) {
		t.Errorf("PUT body %s must carry the port as a number", gotBody)
	}
	if got.Kind != dhsc.KindInt || got.Int != 65535 {
		t.Errorf("answer = %+v, want the device's 65535", got)
	}

	// A write whose read-back fails says so.
	if _, err := p.SetValue(context.Background(), dhsc.ValueRequest{Path: "flaky.port"},
		dhsc.Value{Kind: dhsc.KindInt, Int: 2}); err == nil || !strings.Contains(err.Error(), "read-back failed") {
		t.Errorf("flaky read-back = %v", err)
	}

	// A field that vanished after the write is not reported as written.
	if _, err := p.SetValue(context.Background(), dhsc.ValueRequest{Path: "drop.port"},
		dhsc.Value{Kind: dhsc.KindInt, Int: 2}); err == nil || !strings.Contains(err.Error(), "gone on read-back") {
		t.Errorf("dropped field = %v", err)
	}

	// A value of the wrong type never reaches the device.
	if _, err := p.SetValue(context.Background(), dhsc.ValueRequest{Path: "clamp.port"},
		dhsc.Value{Kind: dhsc.KindString, Str: "abc"}); err == nil || !strings.Contains(err.Error(), "not a number") {
		t.Errorf("wrong type = %v", err)
	}
}
