package admin

import (
	"context"
	"encoding/json"
	"io"
	"path/filepath"
	"testing"
	"time"
)

// TestAdminRoundTrip exercises a happy-path call: Server.Register a
// verb that returns a small payload, Server.Serve starts the listener,
// Call dials + reads the response.
func TestAdminRoundTrip(t *testing.T) {
	socketPath := filepath.Join(t.TempDir(), "test.sock")
	srv := NewServer(io.Discard)
	srv.Register("ping:hello", func(ctx context.Context, _ json.RawMessage) (json.RawMessage, error) {
		return json.RawMessage(`{"msg":"hi"}`), nil
	})

	ctx, cancel := context.WithCancel(context.Background())
	go func() { _ = srv.Serve(ctx, socketPath) }()
	// Give the listener a moment to come up (Serve is async start).
	time.Sleep(50 * time.Millisecond)
	defer cancel()

	resp, err := Call(context.Background(), socketPath, Request{Verb: "ping:hello"})
	if err != nil {
		t.Fatalf("Call: %v", err)
	}
	if !resp.OK {
		t.Fatalf("resp.OK = false; error=%s", resp.Error)
	}
	if string(resp.Data) != `{"msg":"hi"}` {
		t.Errorf("data = %s; want {\"msg\":\"hi\"}", resp.Data)
	}
}

// TestAdminVerbNotImplemented asserts unknown verbs surface
// admin:verb-not-implemented per R25 spec.
func TestAdminVerbNotImplemented(t *testing.T) {
	socketPath := filepath.Join(t.TempDir(), "ni.sock")
	srv := NewServer(io.Discard)
	ctx, cancel := context.WithCancel(context.Background())
	go func() { _ = srv.Serve(ctx, socketPath) }()
	time.Sleep(50 * time.Millisecond)
	defer cancel()

	resp, err := Call(context.Background(), socketPath, Request{Verb: "bogus:action"})
	if err != nil {
		t.Fatalf("Call: %v", err)
	}
	if resp.OK {
		t.Error("OK true for unknown verb; want false")
	}
	if got := resp.Error; got != "admin:verb-not-implemented: bogus:action" {
		t.Errorf("error = %q; want admin:verb-not-implemented: bogus:action", got)
	}
}
