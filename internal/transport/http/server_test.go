package http

// Tests for the generic server. These moved here with the code they cover:
// routing, the trailing-slash rule, 404-vs-405, CORS, the panic barrier and
// the TLS listener are transport concerns, and their tests belong beside
// them rather than inside a protocol package.

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	stdhttp "net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func quietLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// do runs one request through the dispatcher without binding a port.
func do(s *Server, method, path string) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	s.MuxHandler().ServeHTTP(w, httptest.NewRequest(method, path, nil))
	return w
}

func okHandler(body any) HandlerFunc {
	return func(context.Context, *stdhttp.Request) (int, any, error) { return 0, body, nil }
}

// ---- routing ---------------------------------------------------------------

func TestHappyPathDefaultsTo200(t *testing.T) {
	s := NewServer(quietLogger())
	s.Handle(stdhttp.MethodGet, "/thing", okHandler(map[string]string{"id": "x"}))

	w := do(s, stdhttp.MethodGet, "/thing")
	if w.Code != stdhttp.StatusOK {
		t.Fatalf("status = %d", w.Code)
	}
	if ct := w.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("content-type = %q", ct)
	}
	var got map[string]string
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatalf("body: %v", err)
	}
	if got["id"] != "x" {
		t.Errorf("body = %v", got)
	}
}

func TestExplicitStatusIsHonoured(t *testing.T) {
	s := NewServer(quietLogger())
	s.Handle(stdhttp.MethodPost, "/thing", func(context.Context, *stdhttp.Request) (int, any, error) {
		return stdhttp.StatusCreated, map[string]string{}, nil
	})
	if w := do(s, stdhttp.MethodPost, "/thing"); w.Code != stdhttp.StatusCreated {
		t.Errorf("status = %d, want 201", w.Code)
	}
}

// A trailing slash is not a different resource: clients send both forms and
// answering 404 for the other spelling is defensible and useless.
func TestTrailingSlashIsTheSameResource(t *testing.T) {
	s := NewServer(quietLogger())
	s.Handle(stdhttp.MethodGet, "/coll/", okHandler([]string{}))
	if w := do(s, stdhttp.MethodGet, "/coll"); w.Code != stdhttp.StatusOK {
		t.Errorf("no-slash form: status = %d", w.Code)
	}

	s2 := NewServer(quietLogger())
	s2.Handle(stdhttp.MethodGet, "/coll", okHandler([]string{}))
	if w := do(s2, stdhttp.MethodGet, "/coll/"); w.Code != stdhttp.StatusOK {
		t.Errorf("slash form: status = %d", w.Code)
	}
}

// An exact match must beat the alternate spelling, so a route registered for
// one specific form still wins.
func TestExactMatchBeatsAlternateSpelling(t *testing.T) {
	s := NewServer(quietLogger())
	s.Handle(stdhttp.MethodGet, "/x", okHandler("no-slash"))
	s.Handle(stdhttp.MethodGet, "/x/", okHandler("slash"))
	w := do(s, stdhttp.MethodGet, "/x")
	if !strings.Contains(w.Body.String(), "no-slash") {
		t.Errorf("body = %q", w.Body.String())
	}
}

func TestPrefixRoutesLongestFirst(t *testing.T) {
	s := NewServer(quietLogger())
	s.HandlePrefix("/r/", stdhttp.MethodGet, okHandler("short"))
	s.HandlePrefix("/r/deep/", stdhttp.MethodGet, okHandler("long"))
	// Registered short-then-long; the longer prefix must still win.
	w := do(s, stdhttp.MethodGet, "/r/deep/thing")
	if !strings.Contains(w.Body.String(), "long") {
		t.Errorf("body = %q — longest prefix did not win", w.Body.String())
	}
	if w := do(s, stdhttp.MethodGet, "/r/other"); !strings.Contains(w.Body.String(), "short") {
		t.Errorf("body = %q", w.Body.String())
	}

	// And the other registration order: a SHORTER prefix added after a longer
	// one must sort behind it, not in front of it.
	s2 := NewServer(quietLogger())
	s2.HandlePrefix("/r/deep/", stdhttp.MethodGet, okHandler("long"))
	s2.HandlePrefix("/r/", stdhttp.MethodGet, okHandler("short"))
	if w := do(s2, stdhttp.MethodGet, "/r/deep/thing"); !strings.Contains(w.Body.String(), "long") {
		t.Errorf("reverse order: body = %q — longest prefix did not win", w.Body.String())
	}
}

func TestNotFound(t *testing.T) {
	s := NewServer(quietLogger())
	s.Handle(stdhttp.MethodGet, "/thing", okHandler(nil))
	w := do(s, stdhttp.MethodGet, "/nope")
	if w.Code != stdhttp.StatusNotFound {
		t.Errorf("status = %d", w.Code)
	}
}

// A wrong METHOD is 405, not 404 — answering 404 tells a client the endpoint
// does not exist at all.
func TestMethodNotAllowed(t *testing.T) {
	s := NewServer(quietLogger())
	s.Handle(stdhttp.MethodPost, "/thing", okHandler(nil))
	if w := do(s, stdhttp.MethodGet, "/thing"); w.Code != stdhttp.StatusMethodNotAllowed {
		t.Errorf("exact: status = %d, want 405", w.Code)
	}
	// Same across the other slash spelling.
	if w := do(s, stdhttp.MethodGet, "/thing/"); w.Code != stdhttp.StatusMethodNotAllowed {
		t.Errorf("alt spelling: status = %d, want 405", w.Code)
	}
}

func TestMethodNotAllowedOnPrefixRoute(t *testing.T) {
	s := NewServer(quietLogger())
	s.HandlePrefix("/health/", stdhttp.MethodPost, okHandler(nil))
	if w := do(s, stdhttp.MethodGet, "/health/node-1"); w.Code != stdhttp.StatusMethodNotAllowed {
		t.Errorf("status = %d, want 405", w.Code)
	}
}

func TestHandlerErrorBecomes500(t *testing.T) {
	s := NewServer(quietLogger())
	s.Handle(stdhttp.MethodGet, "/boom", func(context.Context, *stdhttp.Request) (int, any, error) {
		return 0, nil, errors.New("handler exploded")
	})
	w := do(s, stdhttp.MethodGet, "/boom")
	if w.Code != stdhttp.StatusInternalServerError {
		t.Fatalf("status = %d", w.Code)
	}
	if !strings.Contains(w.Body.String(), "handler exploded") {
		t.Errorf("the cause must reach the body: %q", w.Body.String())
	}
}

// A panicking handler must still produce a response, or the client sees a
// dropped connection and retries straight back into the panic.
func TestPanicBecomes500(t *testing.T) {
	s := NewServer(quietLogger())
	s.Handle(stdhttp.MethodGet, "/panic", func(context.Context, *stdhttp.Request) (int, any, error) {
		panic("boom")
	})
	w := do(s, stdhttp.MethodGet, "/panic")
	if w.Code != stdhttp.StatusInternalServerError {
		t.Errorf("status = %d", w.Code)
	}
	if !strings.Contains(w.Body.String(), "panic recovered") {
		t.Errorf("body = %q", w.Body.String())
	}
}

// The panic barrier must work without a logger too — a nil Logger is a
// legitimate zero value and must not turn a recovered panic into a new one.
func TestPanicWithNoLogger(t *testing.T) {
	s := NewServer(nil)
	s.Handle(stdhttp.MethodGet, "/panic", func(context.Context, *stdhttp.Request) (int, any, error) {
		panic("boom")
	})
	if w := do(s, stdhttp.MethodGet, "/panic"); w.Code != stdhttp.StatusInternalServerError {
		t.Errorf("status = %d", w.Code)
	}
}

func TestHandlerErrorWithNoLogger(t *testing.T) {
	s := NewServer(nil)
	s.Handle(stdhttp.MethodGet, "/boom", func(context.Context, *stdhttp.Request) (int, any, error) {
		return 0, nil, errors.New("nope")
	})
	if w := do(s, stdhttp.MethodGet, "/boom"); w.Code != stdhttp.StatusInternalServerError {
		t.Errorf("status = %d", w.Code)
	}
}

func TestDuplicateRoutePanics(t *testing.T) {
	s := NewServer(quietLogger())
	s.Handle(stdhttp.MethodGet, "/dup", okHandler(nil))
	defer func() {
		if recover() == nil {
			t.Error("a duplicate route is a wiring bug and must fail loudly")
		}
	}()
	s.Handle(stdhttp.MethodGet, "/dup", okHandler(nil))
}

func TestDuplicateRawRoutePanics(t *testing.T) {
	s := NewServer(quietLogger())
	s.HandleRaw("/ws", stdhttp.NotFoundHandler())
	defer func() {
		if recover() == nil {
			t.Error("a duplicate raw route must fail loudly")
		}
	}()
	s.HandleRaw("/ws", stdhttp.NotFoundHandler())
}

// ---- raw handlers ----------------------------------------------------------

func TestRawHandlerBypassesTheRouteTable(t *testing.T) {
	s := NewServer(quietLogger())
	s.HandleRaw("/ws", stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, _ *stdhttp.Request) {
		w.WriteHeader(stdhttp.StatusSwitchingProtocols)
	}))
	if w := do(s, stdhttp.MethodGet, "/ws"); w.Code != stdhttp.StatusSwitchingProtocols {
		t.Errorf("status = %d", w.Code)
	}
	// Same trailing-slash tolerance as the route table.
	if w := do(s, stdhttp.MethodGet, "/ws/"); w.Code != stdhttp.StatusSwitchingProtocols {
		t.Errorf("alt spelling: status = %d", w.Code)
	}
}

// ---- gate ------------------------------------------------------------------

func TestGateCanDeny(t *testing.T) {
	s := NewServer(quietLogger())
	s.Handle(stdhttp.MethodGet, "/thing", okHandler(nil))
	s.Gate = GateFunc(func(*stdhttp.Request) (int, map[string]string, any, *stdhttp.Request, bool) {
		return stdhttp.StatusUnauthorized,
			map[string]string{"WWW-Authenticate": "Bearer realm=test"},
			map[string]string{"error": "no token"}, nil, false
	})
	w := do(s, stdhttp.MethodGet, "/thing")
	if w.Code != stdhttp.StatusUnauthorized {
		t.Fatalf("status = %d", w.Code)
	}
	if got := w.Header().Get("WWW-Authenticate"); got != "Bearer realm=test" {
		t.Errorf("gate headers lost: %q", got)
	}
}

// The gate must run before RAW handlers too, or a WebSocket upgrade escapes
// the policy that covers every plain route.
func TestGateCoversRawHandlers(t *testing.T) {
	s := NewServer(quietLogger())
	reached := false
	s.HandleRaw("/ws", stdhttp.HandlerFunc(func(stdhttp.ResponseWriter, *stdhttp.Request) { reached = true }))
	s.Gate = GateFunc(func(*stdhttp.Request) (int, map[string]string, any, *stdhttp.Request, bool) {
		return stdhttp.StatusUnauthorized, nil, nil, nil, false
	})
	if w := do(s, stdhttp.MethodGet, "/ws"); w.Code != stdhttp.StatusUnauthorized {
		t.Errorf("status = %d", w.Code)
	}
	if reached {
		t.Error("an upgrade must not run when the gate denied the request")
	}
}

// A gate may hand back a modified request so it can stamp context values the
// server knows nothing about.
func TestGateCanReplaceTheRequest(t *testing.T) {
	type key struct{}
	s := NewServer(quietLogger())
	var seen any
	s.Handle(stdhttp.MethodGet, "/thing", func(ctx context.Context, _ *stdhttp.Request) (int, any, error) {
		seen = ctx.Value(key{})
		return 0, nil, nil
	})
	s.Gate = GateFunc(func(r *stdhttp.Request) (int, map[string]string, any, *stdhttp.Request, bool) {
		return 0, nil, nil, r.WithContext(context.WithValue(r.Context(), key{}, "client-1")), true
	})
	do(s, stdhttp.MethodGet, "/thing")
	if seen != "client-1" {
		t.Errorf("context value = %v, want the gate's", seen)
	}
}

func TestGatePassesWithoutReplacingTheRequest(t *testing.T) {
	s := NewServer(quietLogger())
	s.Handle(stdhttp.MethodGet, "/thing", okHandler(nil))
	s.Gate = GateFunc(func(*stdhttp.Request) (int, map[string]string, any, *stdhttp.Request, bool) {
		return 0, nil, nil, nil, true
	})
	if w := do(s, stdhttp.MethodGet, "/thing"); w.Code != stdhttp.StatusOK {
		t.Errorf("status = %d", w.Code)
	}
}

// Preflights are credential-less by browser design; gating them fails every
// cross-origin call.
func TestPreflightBypassesTheGate(t *testing.T) {
	s := NewServer(quietLogger())
	s.Handle(stdhttp.MethodGet, "/thing", okHandler(nil))
	s.Gate = GateFunc(func(*stdhttp.Request) (int, map[string]string, any, *stdhttp.Request, bool) {
		return stdhttp.StatusUnauthorized, nil, nil, nil, false
	})
	if w := do(s, stdhttp.MethodOptions, "/thing"); w.Code != stdhttp.StatusOK {
		t.Errorf("status = %d, want the preflight through", w.Code)
	}
}

// ---- CORS + preflight ------------------------------------------------------

// The zero value emits nothing: a server that is not browser-facing should
// not be handing out cross-origin permission by default.
func TestZeroCORSEmitsNoHeaders(t *testing.T) {
	s := NewServer(quietLogger())
	s.Handle(stdhttp.MethodGet, "/thing", okHandler(nil))
	w := do(s, stdhttp.MethodGet, "/thing")
	for _, h := range []string{"Access-Control-Allow-Origin", "Access-Control-Allow-Headers", "Access-Control-Max-Age"} {
		if got := w.Header().Get(h); got != "" {
			t.Errorf("%s = %q, want nothing", h, got)
		}
	}
}

func TestConfiguredCORSAppliesToSuccessAndError(t *testing.T) {
	s := NewServer(quietLogger())
	s.CORS = CORS{AllowOrigin: "*", AllowHeaders: "Content-Type", MaxAge: "3600"}
	s.Handle(stdhttp.MethodGet, "/thing", okHandler(nil))
	for _, path := range []string{"/thing", "/missing"} {
		w := do(s, stdhttp.MethodGet, path)
		if got := w.Header().Get("Access-Control-Allow-Origin"); got != "*" {
			t.Errorf("%s: allow-origin = %q", path, got)
		}
		if got := w.Header().Get("Access-Control-Max-Age"); got != "3600" {
			t.Errorf("%s: max-age = %q", path, got)
		}
	}
}

func TestPreflightAdvertisesRealMethods(t *testing.T) {
	s := NewServer(quietLogger())
	s.CORS = CORS{AllowOrigin: "*"}
	s.Handle(stdhttp.MethodGet, "/thing", okHandler(nil))
	s.Handle(stdhttp.MethodPatch, "/thing", okHandler(nil))
	// Registered on the OTHER spelling: a preflight for /coll must still
	// advertise the POST at /coll/, or the browser refuses a request that
	// would have worked.
	s.Handle(stdhttp.MethodPost, "/coll/", okHandler(nil))
	s.HandlePrefix("/thing", stdhttp.MethodDelete, okHandler(nil))

	w := do(s, stdhttp.MethodOptions, "/thing")
	if w.Code != stdhttp.StatusOK {
		t.Fatalf("status = %d, want 200 (not 204)", w.Code)
	}
	if w.Body.String() != "{}" {
		t.Errorf("body = %q, want an empty JSON object", w.Body.String())
	}
	got := w.Header().Get("Access-Control-Allow-Methods")
	if got != "OPTIONS, GET, PATCH, DELETE" {
		t.Errorf("methods = %q — want the fixed OPTIONS,GET,HEAD,POST,PUT,PATCH,DELETE order", got)
	}
	if w.Header().Get("Allow") != got {
		t.Errorf("Allow must mirror Access-Control-Allow-Methods")
	}

	if m := do(s, stdhttp.MethodOptions, "/coll").Header().Get("Allow"); !strings.Contains(m, "POST") {
		t.Errorf("alt spelling: Allow = %q, want the POST registered at /coll/", m)
	}
}

// An unknown path still gets a preflight answer — only OPTIONS itself.
func TestPreflightOnUnknownPath(t *testing.T) {
	s := NewServer(quietLogger())
	s.CORS = CORS{AllowOrigin: "*"}
	w := do(s, stdhttp.MethodOptions, "/nowhere")
	if w.Code != stdhttp.StatusOK {
		t.Fatalf("status = %d", w.Code)
	}
	if got := w.Header().Get("Allow"); got != "OPTIONS" {
		t.Errorf("Allow = %q, want OPTIONS alone", got)
	}
}

// ---- bodies ----------------------------------------------------------------

func TestRawBodyIsEmittedVerbatim(t *testing.T) {
	s := NewServer(quietLogger())
	s.Handle(stdhttp.MethodGet, "/sdp", okHandler(&RawBody{
		ContentType: "application/sdp", Body: []byte("v=0\r\n"),
	}))
	w := do(s, stdhttp.MethodGet, "/sdp")
	if ct := w.Header().Get("Content-Type"); ct != "application/sdp" {
		t.Errorf("content-type = %q", ct)
	}
	if w.Body.String() != "v=0\r\n" {
		t.Errorf("body = %q — must not be JSON-encoded", w.Body.String())
	}
}

func TestRawBodyWithoutContentType(t *testing.T) {
	s := NewServer(quietLogger())
	s.Handle(stdhttp.MethodGet, "/blob", okHandler(&RawBody{Body: []byte{0x01}}))
	if ct := do(s, stdhttp.MethodGet, "/blob").Header().Get("Content-Type"); ct != "application/octet-stream" {
		t.Errorf("content-type = %q", ct)
	}
}

func TestWithHeaders(t *testing.T) {
	s := NewServer(quietLogger())
	s.Handle(stdhttp.MethodPost, "/res", func(context.Context, *stdhttp.Request) (int, any, error) {
		return stdhttp.StatusCreated, &WithHeaders{
			Body:    map[string]string{"id": "n1"},
			Headers: map[string]string{"Location": "/res/n1"},
		}, nil
	})
	w := do(s, stdhttp.MethodPost, "/res")
	if w.Header().Get("Location") != "/res/n1" {
		t.Errorf("Location = %q", w.Header().Get("Location"))
	}
	if !strings.Contains(w.Body.String(), "n1") {
		t.Errorf("body = %q", w.Body.String())
	}
}

// WithHeaders wrapping RawBody is the combination IS-04 needs: a transportfile
// with a custom header.
func TestWithHeadersWrappingRawBody(t *testing.T) {
	s := NewServer(quietLogger())
	s.Handle(stdhttp.MethodGet, "/sdp", okHandler(&WithHeaders{
		Body:    &RawBody{ContentType: "application/sdp", Body: []byte("v=0")},
		Headers: map[string]string{"X-Test": "1"},
	}))
	w := do(s, stdhttp.MethodGet, "/sdp")
	if w.Header().Get("X-Test") != "1" || w.Header().Get("Content-Type") != "application/sdp" {
		t.Errorf("headers = %v", w.Header())
	}
	if w.Body.String() != "v=0" {
		t.Errorf("body = %q", w.Body.String())
	}
}

// The status line is already flushed when encoding fails, so this cannot
// become a 500 — logging is the only honest thing left.
func TestEncodeFailureAfterHeadersIsLogged(t *testing.T) {
	var logged strings.Builder
	s := NewServer(slog.New(slog.NewTextHandler(&logged, nil)))
	s.Handle(stdhttp.MethodGet, "/bad", okHandler(make(chan int)))
	w := do(s, stdhttp.MethodGet, "/bad")
	if w.Code != stdhttp.StatusOK {
		t.Errorf("status = %d — headers were already sent", w.Code)
	}
	if !strings.Contains(logged.String(), "encode failed") {
		t.Errorf("the failure must be logged: %q", logged.String())
	}
}

func TestEncodeFailureWithNoLogger(t *testing.T) {
	s := NewServer(nil)
	s.Handle(stdhttp.MethodGet, "/bad", okHandler(make(chan int)))
	if w := do(s, stdhttp.MethodGet, "/bad"); w.Code != stdhttp.StatusOK {
		t.Errorf("status = %d", w.Code)
	}
}

// ---- error encoder ---------------------------------------------------------

func TestDefaultErrorShape(t *testing.T) {
	s := NewServer(quietLogger())
	w := do(s, stdhttp.MethodGet, "/missing")
	var got defaultError
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatalf("body: %v", err)
	}
	if got.Code != stdhttp.StatusNotFound || got.Error != "Not Found" || got.Debug != "/missing" {
		t.Errorf("body = %+v", got)
	}
}

// A protocol that wants RFC 7807, or IS-04's envelope, injects its own shape
// and the server never invents one.
func TestInjectedErrorShape(t *testing.T) {
	s := NewServer(quietLogger())
	s.Errors = func(status int, errStr, debug string) any {
		return map[string]any{"type": "about:blank", "status": status, "title": errStr, "detail": debug}
	}
	w := do(s, stdhttp.MethodGet, "/missing")
	var got map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatalf("body: %v", err)
	}
	if got["title"] != "Not Found" || got["type"] != "about:blank" {
		t.Errorf("body = %v", got)
	}
}

// ---- TLS + Serve -----------------------------------------------------------

// A TLS server declares it only speaks TLS.
func TestHSTSOnTLSRequests(t *testing.T) {
	s := NewServer(quietLogger())
	s.Handle(stdhttp.MethodGet, "/thing", okHandler(nil))
	r := httptest.NewRequest(stdhttp.MethodGet, "/thing", nil)
	r.TLS = &tls.ConnectionState{}
	w := httptest.NewRecorder()
	s.MuxHandler().ServeHTTP(w, r)
	if got := w.Header().Get("Strict-Transport-Security"); got != "max-age=31536000" {
		t.Errorf("HSTS = %q", got)
	}
	// And absent on a plain request.
	if got := do(s, stdhttp.MethodGet, "/thing").Header().Get("Strict-Transport-Security"); got != "" {
		t.Errorf("plain HTTP must not claim HSTS: %q", got)
	}
}

func TestServeStopsOnContextCancel(t *testing.T) {
	s := NewServer(quietLogger())
	s.Handle(stdhttp.MethodGet, "/thing", okHandler(nil))
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- s.Serve(ctx, "127.0.0.1:0") }()
	time.Sleep(50 * time.Millisecond)
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Errorf("Serve = %v, want a clean shutdown", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Serve did not return after cancellation")
	}
}

func TestServeCannotStartTwice(t *testing.T) {
	s := NewServer(quietLogger())
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = s.Serve(ctx, "127.0.0.1:0") }()
	time.Sleep(50 * time.Millisecond)
	if err := s.Serve(ctx, "127.0.0.1:0"); err == nil {
		t.Error("starting twice must be an error, not a second listener")
	}
}

func TestServeReportsListenFailure(t *testing.T) {
	s := NewServer(quietLogger())
	// Port 0 is chosen by the kernel; a port above the 16-bit range cannot
	// be parsed, so ListenAndServe fails immediately.
	if err := s.Serve(context.Background(), "127.0.0.1:99999"); err == nil {
		t.Error("an unbindable address must be reported")
	}
}

// The TLS branch takes a different call path (ListenAndServeTLS); an
// unbindable address exercises it without needing a certificate.
func TestServeTLSReportsListenFailure(t *testing.T) {
	s := NewServer(quietLogger())
	s.TLS = &tls.Config{MinVersion: tls.VersionTLS12}
	if err := s.Serve(context.Background(), "127.0.0.1:99999"); err == nil {
		t.Error("an unbindable TLS address must be reported")
	}
}

func TestAltSlashForm(t *testing.T) {
	if got := altSlashForm("/a"); got != "/a/" {
		t.Errorf("got %q", got)
	}
	if got := altSlashForm("/a/"); got != "/a" {
		t.Errorf("got %q", got)
	}
}
