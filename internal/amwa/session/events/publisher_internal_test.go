package events

// Package-internal tests for the publisher's per-connection loop.
// These reach the paths a well-behaved client cannot provoke from the
// outside: a socket that dies between reading a command and answering
// it, a subscriber whose send fails mid-fanout, and a request context
// that is already gone when serve starts. Each test holds the
// server-side *WebSocket so the fault is injected at the exact point,
// not by racing a close against the loop.

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"dhs/internal/amwa/codec/is07"
	_ "dhs/internal/amwa/codec/is07/v10"
	httpsession "dhs/internal/amwa/session/http"
)

const (
	internalSrc     = "4f4e4d4c-4b4a-4049-8847-464544434241"
	internalTimeout = 5 * time.Second
)

// pair is one accepted connection: the server side as serve sees it,
// the client side as a raw peer.
type pair struct {
	server *httpsession.WebSocket
	client *httpsession.WebSocket
}

// acceptOne mounts a handler that accepts exactly one WebSocket, hands
// the server side to the test and then runs body on it. The dial is
// done here too, so the caller gets both ends already connected.
func acceptOne(t *testing.T, body func(ws *httpsession.WebSocket, r *http.Request)) pair {
	t.Helper()
	serverSide := make(chan *httpsession.WebSocket, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ws, err := httpsession.AcceptWebSocket(w, r)
		if err != nil {
			return
		}
		serverSide <- ws
		body(ws, r)
	}))
	t.Cleanup(srv.Close)

	ctx, cancel := context.WithTimeout(context.Background(), internalTimeout)
	t.Cleanup(cancel)
	client, err := httpsession.DialWebSocket(ctx, strings.Replace(srv.URL, "http://", "ws://", 1), nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	t.Cleanup(func() { _ = client.Close() })
	select {
	case ws := <-serverSide:
		return pair{server: ws, client: client}
	case <-time.After(internalTimeout):
		t.Fatal("the handler never accepted the connection")
		return pair{}
	}
}

// expectPeerClosed proves the server ended the connection: the next
// read on the client side is the close, bounded by a deadline so a
// regression hangs a test rather than the suite.
func expectPeerClosed(t *testing.T, client *httpsession.WebSocket) {
	t.Helper()
	_ = client.SetReadDeadline(time.Now().Add(internalTimeout))
	for {
		_, err := client.ReadText()
		if errors.Is(err, httpsession.ErrWebSocketClosed) {
			return
		}
		if err != nil {
			t.Fatalf("client read = %v, want the peer's close", err)
		}
		// A frame sent before the close (state replay, health) is
		// not the close; keep reading.
	}
}

func quiet() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

func mustEncodeCommand(t *testing.T, c is07.Command) []byte {
	t.Helper()
	b, err := is07.Default().EncodeCommand(c)
	if err != nil {
		t.Fatalf("encode command: %v", err)
	}
	return b
}

func stateFor(src string) is07.EventBoolean {
	return is07.EventBoolean{
		EventCommon: is07.EventCommon{
			Identity:  is07.Identity{SourceID: src},
			Timing:    is07.Timing{CreationTimestamp: "1:0"},
			EventType: "boolean",
		},
		Payload: is07.PayloadBoolean{Value: true},
	}
}

// TestPublishMarksAClientWhoseSendFails: fan-out never blocks on one
// dead subscriber. A send that fails flags the entry (markFailed) and
// Publish still returns nil, because the event WAS published — one
// consumer missing it is that consumer's connection problem, and the
// serve loop drops the entry when the read side sees the same close.
func TestPublishMarksAClientWhoseSendFails(t *testing.T) {
	p := NewPublisher(PublisherOptions{Logger: quiet()})
	defer func() { _ = p.Close() }()

	hold := make(chan struct{})
	defer close(hold)
	dead := acceptOne(t, func(*httpsession.WebSocket, *http.Request) { <-hold })
	if err := dead.server.Close(); err != nil {
		t.Fatalf("close server side: %v", err)
	}
	sub := &subscription{id: 1, ws: dead.server, sources: map[string]struct{}{internalSrc: {}}}
	p.mu.Lock()
	p.clients[sub.id] = sub
	p.mu.Unlock()

	if err := p.Publish(stateFor(internalSrc)); err != nil {
		t.Fatalf("Publish = %v; a dead subscriber must not fail the publish", err)
	}
	if !sub.failed.Load() {
		t.Fatal("the subscriber whose send failed was not marked")
	}
}

// TestServeReturnsWhenTheRequestContextIsGone: the loop checks the
// request context before every read, so a handler whose request was
// cancelled does not sit in a blocking read on a connection net/http
// has already given up on.
func TestServeReturnsWhenTheRequestContextIsGone(t *testing.T) {
	p := NewPublisher(PublisherOptions{Logger: quiet()})
	defer func() { _ = p.Close() }()

	done := make(chan struct{})
	conn := acceptOne(t, func(ws *httpsession.WebSocket, r *http.Request) {
		ctx, cancel := context.WithCancel(r.Context())
		cancel()
		p.serve(ctx, ws, r.RemoteAddr)
		close(done)
	})
	expectPeerClosed(t, conn.client)
	select {
	case <-done:
	case <-time.After(internalTimeout):
		t.Fatal("serve did not return on a cancelled context")
	}
	if n := p.SubscriberCount(); n != 0 {
		t.Fatalf("%d client(s) still registered after serve returned", n)
	}
}

// TestServeEndsWhenStateReplayCannotBeSent: the initial state send
// is the subscription's reply (IS-07 §5.2). If the socket is gone by
// then there is nobody to reply to, and the loop ends rather than
// reading commands from a connection it can no longer answer.
func TestServeEndsWhenStateReplayCannotBeSent(t *testing.T) {
	var (
		mu     sync.Mutex
		target *httpsession.WebSocket
	)
	p := NewPublisher(PublisherOptions{
		Logger: quiet(),
		StateOf: func(string) (is07.Message, bool) {
			// The connection drops between the command and the reply.
			mu.Lock()
			_ = target.Close()
			mu.Unlock()
			return stateFor(internalSrc), true
		},
	})
	defer func() { _ = p.Close() }()

	done := make(chan struct{})
	conn := acceptOne(t, func(ws *httpsession.WebSocket, r *http.Request) {
		mu.Lock()
		target = ws
		mu.Unlock()
		p.serve(r.Context(), ws, r.RemoteAddr)
		close(done)
	})
	if err := conn.client.SendText(mustEncodeCommand(t, is07.CommandSubscription{Sources: []string{internalSrc}})); err != nil {
		t.Fatalf("send subscription: %v", err)
	}
	select {
	case <-done:
	case <-time.After(internalTimeout):
		t.Fatal("serve kept running after the state reply failed to send")
	}
	expectPeerClosed(t, conn.client)
}

// TestServeEndsWhenHealthReplyCannotBeSent: the health reply is what
// keeps the receiver's own watchdog quiet. A reply that cannot be
// sent is logged with the reason and ends the loop, so the sender
// does not keep a connection it has already lost.
func TestServeEndsWhenHealthReplyCannotBeSent(t *testing.T) {
	var (
		mu     sync.Mutex
		target *httpsession.WebSocket
		logs   strings.Builder
		logMu  sync.Mutex
	)
	logger := slog.New(slog.NewTextHandler(lockedWriter{&logMu, &logs}, nil))
	p := NewPublisher(PublisherOptions{
		Logger: logger,
		Codec: hookedCodec{Codec: is07.Default(), beforeEncode: func(m is07.Message) {
			if _, isHealth := m.(is07.MessageHealth); isHealth {
				mu.Lock()
				_ = target.Close()
				mu.Unlock()
			}
		}},
	})
	defer func() { _ = p.Close() }()

	done := make(chan struct{})
	conn := acceptOne(t, func(ws *httpsession.WebSocket, r *http.Request) {
		mu.Lock()
		target = ws
		mu.Unlock()
		p.serve(r.Context(), ws, r.RemoteAddr)
		close(done)
	})
	if err := conn.client.SendText(mustEncodeCommand(t, is07.CommandHealth{Timestamp: "1:0"})); err != nil {
		t.Fatalf("send health: %v", err)
	}
	select {
	case <-done:
	case <-time.After(internalTimeout):
		t.Fatal("serve kept running after the health reply failed to send")
	}
	expectPeerClosed(t, conn.client)
	logMu.Lock()
	defer logMu.Unlock()
	if !strings.Contains(logs.String(), "send health") {
		t.Fatalf("the failed reply was not recorded:\n%s", logs.String())
	}
}

// hookedCodec runs a callback before delegating EncodeMessage, so a
// test can act at the instant between decoding a command and sending
// its reply.
type hookedCodec struct {
	is07.Codec
	beforeEncode func(is07.Message)
}

func (h hookedCodec) EncodeMessage(m is07.Message) ([]byte, error) {
	h.beforeEncode(m)
	return h.Codec.EncodeMessage(m)
}

type lockedWriter struct {
	mu *sync.Mutex
	b  *strings.Builder
}

func (w lockedWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.b.Write(p)
}
