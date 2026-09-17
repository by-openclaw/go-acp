package http

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	stdhttp "net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestServerHappyPath(t *testing.T) {
	srv := NewServer(nil)
	srv.Handle(stdhttp.MethodGet, "/x-nmos/system/v1.0/", func(ctx context.Context, r *stdhttp.Request) (int, any, error) {
		return 0, []string{"global/"}, nil
	})

	addr, stop := startServer(t, srv)
	defer stop()

	resp, err := stdhttp.Get("http://" + addr + "/x-nmos/system/v1.0/")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != stdhttp.StatusOK {
		t.Fatalf("status %d", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "application/json" {
		t.Fatalf("Content-Type = %q", ct)
	}
	body, _ := io.ReadAll(resp.Body)
	var idx []string
	if err := json.Unmarshal(body, &idx); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(idx) != 1 || idx[0] != "global/" {
		t.Fatalf("body = %v", idx)
	}
}

func TestServerNotFound(t *testing.T) {
	srv := NewServer(nil)
	addr, stop := startServer(t, srv)
	defer stop()

	resp, err := stdhttp.Get("http://" + addr + "/missing")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != stdhttp.StatusNotFound {
		t.Fatalf("status %d", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	var eb ErrorBody
	if err := json.Unmarshal(body, &eb); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if eb.Code != 404 || eb.Error == "" {
		t.Fatalf("body = %+v", eb)
	}
}

func TestServerMethodNotAllowed(t *testing.T) {
	srv := NewServer(nil)
	srv.Handle(stdhttp.MethodGet, "/global", func(ctx context.Context, r *stdhttp.Request) (int, any, error) {
		return 0, struct{}{}, nil
	})
	addr, stop := startServer(t, srv)
	defer stop()

	req, _ := stdhttp.NewRequest(stdhttp.MethodPost, "http://"+addr+"/global", nil)
	resp, err := stdhttp.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != stdhttp.StatusMethodNotAllowed {
		t.Fatalf("status %d", resp.StatusCode)
	}
}

func TestServerHandlerError(t *testing.T) {
	srv := NewServer(nil)
	srv.Handle(stdhttp.MethodGet, "/boom", func(ctx context.Context, r *stdhttp.Request) (int, any, error) {
		return 0, nil, errors.New("intentional")
	})
	addr, stop := startServer(t, srv)
	defer stop()

	resp, err := stdhttp.Get("http://" + addr + "/boom")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != stdhttp.StatusInternalServerError {
		t.Fatalf("status %d", resp.StatusCode)
	}
}

func TestServerDuplicateRoutePanics(t *testing.T) {
	srv := NewServer(nil)
	srv.Handle(stdhttp.MethodGet, "/x", func(ctx context.Context, r *stdhttp.Request) (int, any, error) { return 0, nil, nil })
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic on duplicate route")
		}
	}()
	srv.Handle(stdhttp.MethodGet, "/x", func(ctx context.Context, r *stdhttp.Request) (int, any, error) { return 0, nil, nil })
}

func TestServerCannotStartTwice(t *testing.T) {
	srv := NewServer(nil)
	addr, stop := startServer(t, srv)
	defer stop()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	err := srv.Serve(ctx, addr)
	if err == nil || !strings.Contains(err.Error(), "already started") {
		t.Fatalf("expected already-started error, got %v", err)
	}
}

// startServer binds to an ephemeral port and returns its host:port.
// The returned stop closes the server.
func startServer(t *testing.T, srv *Server) (string, func()) {
	t.Helper()
	// Use port 0 by binding via a httptest first, then transferring.
	ln := httptest.NewServer(stdhttp.NewServeMux())
	ln.Close()
	addr := strings.TrimPrefix(ln.URL, "http://")

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- srv.Serve(ctx, addr) }()

	// Probe until reachable, capped at 2 s.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		resp, err := stdhttp.Get("http://" + addr + "/__startup_probe__")
		if err == nil {
			_ = resp.Body.Close()
			break
		}
		select {
		case err := <-done:
			t.Fatalf("server exited early: %v", err)
		default:
		}
		time.Sleep(20 * time.Millisecond)
	}
	stop := func() {
		cancel()
		select {
		case err := <-done:
			if err != nil {
				t.Logf("server stopped with: %v", err)
			}
		case <-time.After(2 * time.Second):
			t.Log("server stop timed out")
		}
	}
	return addr, stop
}

func TestServerEmitsCORSHeaderOnSuccess(t *testing.T) {
	srv := NewServer(nil)
	srv.Handle(stdhttp.MethodGet, "/x", func(ctx context.Context, r *stdhttp.Request) (int, any, error) {
		return 0, []string{"y/"}, nil
	})
	addr, stop := startServer(t, srv)
	defer stop()

	resp, err := stdhttp.Get("http://" + addr + "/x")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if got := resp.Header.Get("Access-Control-Allow-Origin"); got != "*" {
		t.Fatalf("Access-Control-Allow-Origin = %q", got)
	}
}

func TestServerEmitsCORSHeaderOnError(t *testing.T) {
	srv := NewServer(nil)
	addr, stop := startServer(t, srv)
	defer stop()

	resp, err := stdhttp.Get("http://" + addr + "/missing-too")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if got := resp.Header.Get("Access-Control-Allow-Origin"); got != "*" {
		t.Fatalf("Access-Control-Allow-Origin = %q (404 must still set CORS)", got)
	}
}

func TestServerOPTIONSPreflight(t *testing.T) {
	srv := NewServer(nil)
	srv.Handle(stdhttp.MethodGet, "/r", func(ctx context.Context, r *stdhttp.Request) (int, any, error) {
		return 0, nil, nil
	})
	srv.Handle(stdhttp.MethodPut, "/r", func(ctx context.Context, r *stdhttp.Request) (int, any, error) {
		return 0, nil, nil
	})
	addr, stop := startServer(t, srv)
	defer stop()

	req, _ := stdhttp.NewRequest(stdhttp.MethodOptions, "http://"+addr+"/r", nil)
	resp, err := stdhttp.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("OPTIONS: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != stdhttp.StatusOK {
		t.Fatalf("OPTIONS status = %d, want 200", resp.StatusCode)
	}
	if got := resp.Header.Get("Access-Control-Allow-Origin"); got != "*" {
		t.Fatalf("Access-Control-Allow-Origin = %q", got)
	}
	allow := resp.Header.Get("Access-Control-Allow-Methods")
	if !strings.Contains(allow, "GET") || !strings.Contains(allow, "PUT") || !strings.Contains(allow, "OPTIONS") {
		t.Fatalf("Access-Control-Allow-Methods = %q must list GET, PUT, OPTIONS", allow)
	}
	if resp.Header.Get("Allow") == "" {
		t.Fatalf("Allow header missing on OPTIONS preflight")
	}
}

func TestServerOPTIONSUnknownPath(t *testing.T) {
	srv := NewServer(nil)
	addr, stop := startServer(t, srv)
	defer stop()

	req, _ := stdhttp.NewRequest(stdhttp.MethodOptions, "http://"+addr+"/none", nil)
	resp, err := stdhttp.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("OPTIONS: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != stdhttp.StatusOK {
		t.Fatalf("OPTIONS on unknown path = %d, want 200 (CORS preflight is global)", resp.StatusCode)
	}
}

// Sanity check — ensure error-body decode shape.
func TestErrorBodyShape(t *testing.T) {
	eb := ErrorBody{Code: 404, Error: "Not Found", Debug: "/x"}
	b, err := json.Marshal(eb)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if !strings.Contains(string(b), `"code":404`) {
		t.Fatalf("body = %s", b)
	}
	// Debug is omitempty
	eb2 := ErrorBody{Code: 500, Error: "Internal Server Error"}
	b2, _ := json.Marshal(eb2)
	if strings.Contains(string(b2), "debug") {
		t.Fatalf("debug should be omitempty: %s", b2)
	}
	_ = fmt.Sprintf
}
