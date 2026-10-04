package connection

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"dhs/internal/amwa/codec/is05"
)

// --- test payloads (valid IS-05, built through the codec so they cannot
// drift from what the decoder accepts) ---

func validSenderBody(t *testing.T) []byte {
	t.Helper()
	b, err := is05.EncodeStagedSender(is05.StagedSender{
		MasterEnableField: is05.MasterEnableField{MasterEnable: true},
		Activation:        is05.Activation{Mode: is05.ActivationModeImmediate},
		TransportParams:   []is05.TransportParams{{}},
	})
	if err != nil {
		t.Fatalf("encode sender: %v", err)
	}
	return b
}

func validReceiverBody(t *testing.T) []byte {
	t.Helper()
	b, err := is05.EncodeStagedReceiver(is05.StagedReceiver{
		MasterEnableField: is05.MasterEnableField{MasterEnable: true},
		Activation:        is05.Activation{Mode: is05.ActivationModeImmediate},
		TransportParams:   []is05.TransportParams{{}},
	})
	if err != nil {
		t.Fatalf("encode receiver: %v", err)
	}
	return b
}

// --- NewClient ---

func TestNewClient(t *testing.T) {
	cases := []struct {
		name, href string
		wantVer    string
		wantErr    bool
	}{
		{"valid", "http://10.6.255.102:3000/x-nmos/connection/v1.1", "v1.1", false},
		{"trailing slash trimmed", "http://h:3000/x-nmos/connection/v1.0/", "v1.0", false},
		{"parse error", "http://a\x7fb/x-nmos/connection/v1.1", "", true},
		{"not absolute", "/x-nmos/connection/v1.1", "", true},
		{"no version segment", "http://h:3000/x-nmos/connection/foo", "", true},
		{"empty path", "http://h:3000", "", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c, err := NewClient(tc.href)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("NewClient(%q) = nil error, want error", tc.href)
				}
				return
			}
			if err != nil {
				t.Fatalf("NewClient(%q): %v", tc.href, err)
			}
			if c.APIVer != tc.wantVer {
				t.Errorf("APIVer = %q, want %q", c.APIVer, tc.wantVer)
			}
			if strings.HasSuffix(c.Base, "/") {
				t.Errorf("Base %q keeps a trailing slash", c.Base)
			}
			if c.HTTP == nil {
				t.Error("HTTP client is nil")
			}
		})
	}
}

// --- read methods over a live test server ---

func newTestClient(t *testing.T, h http.HandlerFunc) *Client {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	return &Client{HTTP: srv.Client(), Base: srv.URL, APIVer: "v1.1"}
}

func TestReadMethodsDecodeState(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/receivers/rx1/staged"),
			strings.HasSuffix(r.URL.Path, "/receivers/rx1/active"):
			_, _ = w.Write(validReceiverBody(t))
		case strings.HasSuffix(r.URL.Path, "/senders/tx1/active"):
			_, _ = w.Write(validSenderBody(t))
		default:
			http.Error(w, "unexpected "+r.URL.Path, http.StatusNotFound)
		}
	})
	ctx := context.Background()

	if got, err := c.StagedReceiver(ctx, "rx1"); err != nil || got == nil || !got.MasterEnable {
		t.Fatalf("StagedReceiver = %+v, %v", got, err)
	}
	if got, err := c.ActiveReceiver(ctx, "rx1"); err != nil || got == nil {
		t.Fatalf("ActiveReceiver = %+v, %v", got, err)
	}
	if got, err := c.ActiveSender(ctx, "tx1"); err != nil || got == nil {
		t.Fatalf("ActiveSender = %+v, %v", got, err)
	}
}

// A non-2xx must surface the Device's own body — the reason a route was
// refused — not just the status code.
func TestReadSurfacesDeviceErrorBody(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "transport_params[0].destination_ip is not routable", http.StatusBadRequest)
	})
	ctx := context.Background()
	_, err := c.StagedReceiver(ctx, "rx1")
	if err == nil {
		t.Fatal("want an error for HTTP 400")
	}
	if !strings.Contains(err.Error(), "not routable") {
		t.Errorf("error dropped the device message: %v", err)
	}
	// The other read methods propagate the same transport error.
	if _, err := c.ActiveReceiver(ctx, "rx1"); err == nil {
		t.Error("ActiveReceiver must surface the HTTP error")
	}
	if _, err := c.ActiveSender(ctx, "tx1"); err == nil {
		t.Error("ActiveSender must surface the HTTP error")
	}
}

// A 200 with a body the codec rejects propagates the decode error.
func TestReadPropagatesDecodeError(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{not json`))
	})
	if _, err := c.ActiveSender(context.Background(), "tx1"); err == nil {
		t.Fatal("a malformed body must fail decode")
	}
}

// --- TransportFile ---

func TestTransportFile(t *testing.T) {
	const sdp = "v=0\r\no=- 0 0 IN IP4 127.0.0.1\r\n"
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/senders/tx1/transportfile") {
			_, _ = io.WriteString(w, sdp)
			return
		}
		http.Error(w, "bad sender", http.StatusNotFound)
	})
	got, err := c.TransportFile(context.Background(), "tx1")
	if err != nil {
		t.Fatalf("TransportFile: %v", err)
	}
	if got != sdp {
		t.Errorf("SDP = %q, want it returned verbatim", got)
	}
	// A missing sender is an error carrying the body.
	if _, err := c.TransportFile(context.Background(), "nope"); err == nil {
		t.Error("want an error for a 404 transportfile")
	}
}

func TestTransportFileNetworkError(t *testing.T) {
	c := &Client{HTTP: http.DefaultClient, Base: deadBase(t), APIVer: "v1.1"}
	if _, err := c.TransportFile(context.Background(), "tx1"); err == nil {
		t.Fatal("want a network error against a closed server")
	}
}

func TestTransportFileRequestBuildError(t *testing.T) {
	c := &Client{HTTP: http.DefaultClient, Base: "http://h/\x7f", APIVer: "v1.1"}
	if _, err := c.TransportFile(context.Background(), "tx1"); err == nil {
		t.Fatal("a control char in the URL must fail request construction")
	}
}

// --- PATCH ---

func TestPatchSendsMergeBodyAndDecodes(t *testing.T) {
	var gotBody string
	var gotMethod string
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		b, _ := io.ReadAll(r.Body)
		gotBody = string(b)
		if strings.Contains(r.URL.Path, "/receivers/") {
			_, _ = w.Write(validReceiverBody(t))
		} else {
			_, _ = w.Write(validSenderBody(t))
		}
	})
	ctx := context.Background()

	rx, err := c.PatchReceiver(ctx, "rx1", map[string]any{"master_enable": true})
	if err != nil || rx == nil {
		t.Fatalf("PatchReceiver = %+v, %v", rx, err)
	}
	if gotMethod != http.MethodPatch {
		t.Errorf("method = %s, want PATCH", gotMethod)
	}
	if !strings.Contains(gotBody, "master_enable") {
		t.Errorf("patch body did not carry the merge field: %q", gotBody)
	}

	tx, err := c.PatchSender(ctx, "tx1", map[string]any{"master_enable": false})
	if err != nil || tx == nil {
		t.Fatalf("PatchSender = %+v, %v", tx, err)
	}
}

// An unmarshalable patch value fails before any request is sent.
func TestPatchMarshalError(t *testing.T) {
	c := &Client{HTTP: http.DefaultClient, Base: "http://h/x-nmos/connection/v1.1", APIVer: "v1.1"}
	bad := map[string]any{"x": make(chan int)}
	if _, err := c.PatchReceiver(context.Background(), "rx1", bad); err == nil {
		t.Fatal("an unmarshalable patch body must fail")
	}
}

// --- Bulk ---

func TestBulk(t *testing.T) {
	yes := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
	if !yes.Bulk(context.Background()) {
		t.Error("Bulk must report true on a 200")
	}
	no := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) { http.Error(w, "no", http.StatusNotFound) })
	if no.Bulk(context.Background()) {
		t.Error("Bulk must report false when the endpoint 404s")
	}
}

// --- low-level get/patch error arms ---

// A network failure (server closed) surfaces from do().
func TestGetNetworkError(t *testing.T) {
	c := &Client{HTTP: http.DefaultClient, Base: deadBase(t), APIVer: "v1.1"}
	if _, err := c.StagedReceiver(context.Background(), "rx1"); err == nil {
		t.Fatal("want a network error against a closed server")
	}
}

// A control char in the base URL fails request construction in get and patch.
func TestRequestBuildErrors(t *testing.T) {
	c := &Client{HTTP: http.DefaultClient, Base: "http://h/\x7f", APIVer: "v1.1"}
	if _, err := c.StagedReceiver(context.Background(), "rx1"); err == nil {
		t.Error("get: a control char must fail request construction")
	}
	if _, err := c.PatchSender(context.Background(), "tx1", map[string]any{"a": 1}); err == nil {
		t.Error("patch: a control char must fail request construction")
	}
}

// deadBase starts a server, captures its URL, then closes it — a base that
// resolves but refuses connections, so Do returns a transport error.
func deadBase(t *testing.T) string {
	t.Helper()
	srv := httptest.NewServer(http.NotFoundHandler())
	url := srv.URL
	srv.Close()
	return url
}

// --- bulk requests ---

const (
	bulkRxA = "2c47bf5e-1b2c-4abc-9def-deadbeef0103"
	bulkRxB = "2c47bf5e-1b2c-4abc-9def-deadbeef0006"
)

// One POST carries every entry to bulk/<kind>, and the per-id verdicts
// come back as the Device gave them — a partial success included.
func TestBulkPostsOnceAndReadsEveryVerdict(t *testing.T) {
	for _, kind := range []string{"receivers", "senders"} {
		var gotPath, gotMethod, gotCT, gotBody string
		c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
			gotPath, gotMethod, gotCT = r.URL.Path, r.Method, r.Header.Get("Content-Type")
			b, _ := io.ReadAll(r.Body)
			gotBody = string(b)
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `[{"id":"`+bulkRxA+`","code":200},{"id":"`+bulkRxB+`","code":400,"error":"bad sdp","debug":null}]`)
		})
		items := []is05.BulkItem{
			{ID: bulkRxA, Params: map[string]any{"master_enable": true}},
			{ID: bulkRxB, Params: map[string]any{"master_enable": false}},
		}
		call := c.BulkReceivers
		if kind == "senders" {
			call = c.BulkSenders
		}
		results, deviations, err := call(context.Background(), items)
		if err != nil || len(deviations) != 0 {
			t.Fatalf("%s: err=%v deviations=%v", kind, err, deviations)
		}
		if gotMethod != http.MethodPost || !strings.HasSuffix(gotPath, "/bulk/"+kind) || gotCT != "application/json" {
			t.Errorf("%s: request = %s %s (%s)", kind, gotMethod, gotPath, gotCT)
		}
		if !strings.Contains(gotBody, `"id":"`+bulkRxA+`"`) || !strings.Contains(gotBody, `"params":{"master_enable":false}`) {
			t.Errorf("%s: body = %s", kind, gotBody)
		}
		if len(results) != 2 || !results[0].OK() || results[1].OK() || results[1].Error != "bad sdp" {
			t.Errorf("%s: results = %+v", kind, results)
		}
	}
}

// What can go wrong around the request: a body the codec refuses is
// never sent, a Device that refuses the whole request is an error with
// its message, a response outside the schema is reported beside the
// verdicts, and the transport and request-construction failures
// surface.
func TestBulkRefusalsAndDeviations(t *testing.T) {
	ctx := context.Background()
	sent := 0
	c := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		sent++
		http.Error(w, `{"code":400,"error":"Invalid bulk body"}`, http.StatusBadRequest)
	})
	if _, _, err := c.BulkReceivers(ctx, nil); err == nil || sent != 0 {
		t.Errorf("an empty request: err=%v, sent=%d — it must not reach the Device", err, sent)
	}
	item := []is05.BulkItem{{ID: bulkRxA, Params: map[string]any{}}}
	if _, _, err := c.BulkReceivers(ctx, item); err == nil || !strings.Contains(err.Error(), "Invalid bulk body") {
		t.Errorf("a refused request: err=%v, want the Device's message", err)
	}

	odd := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, `[{"id":"`+bulkRxA+`","code":302}]`)
	})
	results, deviations, err := odd.BulkReceivers(ctx, item)
	if err != nil || len(deviations) != 1 || len(results) != 1 || results[0].Code != 302 {
		t.Errorf("an out-of-schema response: results=%+v deviations=%v err=%v", results, deviations, err)
	}

	notBulk := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) { _, _ = io.WriteString(w, `{"ok":true}`) })
	if _, _, err := notBulk.BulkSenders(ctx, item); err == nil {
		t.Error("a response that is not an array must be an error")
	}

	dead := &Client{HTTP: http.DefaultClient, Base: deadBase(t), APIVer: "v1.1"}
	if _, _, err := dead.BulkReceivers(ctx, item); err == nil {
		t.Error("want a network error against a closed server")
	}
	bad := &Client{HTTP: http.DefaultClient, Base: "http://h/\x7f", APIVer: "v1.1"}
	if _, _, err := bad.BulkReceivers(ctx, item); err == nil {
		t.Error("a control char must fail request construction")
	}
}
