package query_test

import (
	"context"
	"errors"
	stdhttp "net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"dhs/internal/amwa/codec/is04"
	amwahttp "dhs/internal/amwa/session/http"
	"dhs/internal/amwa/session/query"
)

// A minimal grain frame: grain_type is set, so is04.DecodeGrain accepts
// it. The client's Watch never inspects the body beyond decoding it.
const grainFrame = `{"grain_type":"event","grain":{"topic":"/senders/","data":[]}}`

// wsServer stands up an in-process Query WS endpoint using the session
// layer's own AcceptWebSocket (the server side of the same transport the
// client dials) so Watch is exercised over a real RFC 6455 upgrade with
// no third-party dependency. onConn runs with the accepted socket.
func wsServer(t *testing.T, onConn func(ws *amwahttp.WebSocket)) string {
	t.Helper()
	srv := httptest.NewServer(stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
		ws, err := amwahttp.AcceptWebSocket(w, r)
		if err != nil {
			return
		}
		onConn(ws)
	}))
	t.Cleanup(srv.Close)
	return "ws" + strings.TrimPrefix(srv.URL, "http")
}

// TestWatchStreamsGrainThenStopsOnFuncError: the happy path — dial, read
// a grain, hand it to fn — and fn returning an error is the documented
// way a caller stops the watch; that error must be what Watch returns.
// KeepAlive is armed so the ping goroutine is set up and torn down (via
// done) even though fn stops the stream before a tick fires.
func TestWatchStreamsGrainThenStopsOnFuncError(t *testing.T) {
	url := wsServer(t, func(ws *amwahttp.WebSocket) {
		_ = ws.SendText([]byte(grainFrame))
		time.Sleep(300 * time.Millisecond)
		_ = ws.Close()
	})

	stop := errors.New("caller done")
	seen := 0
	err := query.Watch(context.Background(), url, func(*is04.Grain) error {
		seen++
		return stop
	}, query.WatchOptions{KeepAlive: 20 * time.Millisecond, ReadTimeout: -1})
	if !errors.Is(err, stop) {
		t.Fatalf("err = %v, want the fn's own error", err)
	}
	if seen != 1 {
		t.Fatalf("fn called %d times, want 1", seen)
	}
}

// TestWatchSkipsNonGrainFrames: a well-formed JSON frame that is not a
// grain envelope is a peer deviation, not a fatal condition — Watch must
// skip it and keep reading, so the following real grain still arrives.
func TestWatchSkipsNonGrainFrames(t *testing.T) {
	url := wsServer(t, func(ws *amwahttp.WebSocket) {
		_ = ws.SendText([]byte(`{"not":"a grain"}`)) // valid JSON, empty grain → ErrNotGrain
		_ = ws.SendText([]byte(grainFrame))
		time.Sleep(300 * time.Millisecond)
		_ = ws.Close()
	})

	stop := errors.New("stop")
	got := 0
	err := query.Watch(context.Background(), url, func(*is04.Grain) error {
		got++
		return stop
	}, query.WatchOptions{KeepAlive: -1, ReadTimeout: -1})
	if !errors.Is(err, stop) {
		t.Fatalf("err = %v, want stop after the real grain", err)
	}
	if got != 1 {
		t.Fatalf("fn saw %d grains, want exactly the one real grain", got)
	}
}

// TestWatchReturnsDecodeError: a frame that is not even valid JSON is a
// genuine decode failure (not the tolerated non-grain case) and must
// abort the watch with that error.
func TestWatchReturnsDecodeError(t *testing.T) {
	url := wsServer(t, func(ws *amwahttp.WebSocket) {
		_ = ws.SendText([]byte(`}{ not json`))
		time.Sleep(200 * time.Millisecond)
		_ = ws.Close()
	})

	err := query.Watch(context.Background(), url, func(*is04.Grain) error { return nil },
		query.WatchOptions{KeepAlive: -1, ReadTimeout: -1})
	if err == nil || errors.Is(err, query.ErrNoWSHref) {
		t.Fatalf("err = %v, want a grain decode error", err)
	}
}

// TestWatchStopsWhenContextCancelledBetweenFrames: cancelling the
// context is the normal shutdown. When it is cancelled between frames,
// the loop's top-of-iteration check returns the context error rather
// than blocking on another read.
func TestWatchStopsWhenContextCancelledBetweenFrames(t *testing.T) {
	url := wsServer(t, func(ws *amwahttp.WebSocket) {
		_ = ws.SendText([]byte(grainFrame))
		time.Sleep(300 * time.Millisecond)
		_ = ws.Close()
	})

	ctx, cancel := context.WithCancel(context.Background())
	err := query.Watch(ctx, url, func(*is04.Grain) error {
		cancel() // cancel from inside fn, then let the loop re-check
		return nil
	}, query.WatchOptions{KeepAlive: -1, ReadTimeout: -1})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled from the between-frames check", err)
	}
}

// TestWatchContextCancelDuringRead: a quiet subscription is blocked in
// the read when the context is cancelled; the watchdog goroutine closes
// the socket so the read unblocks, and Watch must report the context
// error, not a raw socket error. The armed KeepAlive also exercises the
// ping goroutine's ctx.Done() exit.
func TestWatchContextCancelDuringRead(t *testing.T) {
	url := wsServer(t, func(ws *amwahttp.WebSocket) {
		time.Sleep(500 * time.Millisecond) // stay open, send nothing
		_ = ws.Close()
	})

	ctx, cancel := context.WithCancel(context.Background())
	go func() { time.Sleep(50 * time.Millisecond); cancel() }()
	err := query.Watch(ctx, url, func(*is04.Grain) error { return nil },
		query.WatchOptions{KeepAlive: 20 * time.Millisecond, ReadTimeout: -1})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled while blocked in read", err)
	}
}

// TestWatchReportsReadError: the peer closing the socket while the
// context is still live is a real read failure — Watch must surface it
// (a half-open 24/7 watch reporting nothing is exactly the defect the
// read path guards against).
func TestWatchReportsReadError(t *testing.T) {
	url := wsServer(t, func(ws *amwahttp.WebSocket) {
		_ = ws.Close() // hang up immediately after the upgrade
	})

	err := query.Watch(context.Background(), url, func(*is04.Grain) error { return nil },
		query.WatchOptions{KeepAlive: -1, ReadTimeout: -1})
	if err == nil {
		t.Fatal("a peer close with the context still live must be an error")
	}
	if !strings.Contains(err.Error(), "read frame") {
		t.Fatalf("err = %v, want it to name the read failure", err)
	}
}

// TestWatchPingFailureTearsDown: on a quiet subscription the client's
// own keep-alive ping is the only liveness signal. When the socket has
// gone away, the ping send fails and the keep-alive goroutine must tear
// the connection down rather than leave it half-open. fn blocks long
// enough (holding the read loop out of ReadText) for several ping ticks
// to land against the dead socket, making the failure branch fire.
func TestWatchPingFailureTearsDown(t *testing.T) {
	url := wsServer(t, func(ws *amwahttp.WebSocket) {
		_ = ws.SendText([]byte(grainFrame))
		_ = ws.Close() // drop the socket right after the grain
	})

	fnEntered := make(chan struct{})
	err := query.Watch(context.Background(), url, func(*is04.Grain) error {
		close(fnEntered)
		time.Sleep(200 * time.Millisecond) // hold the read loop; let pings fail
		return nil
	}, query.WatchOptions{KeepAlive: 10 * time.Millisecond, ReadTimeout: -1})
	select {
	case <-fnEntered:
	default:
		t.Fatal("fn was never called — the grain did not arrive")
	}
	// However the teardown races (ping failure vs the post-sleep read of
	// a closed socket), Watch must return an error, not hang.
	if err == nil {
		t.Fatal("a dropped socket must end the watch with an error")
	}
}

// ----------------------------------------------------------------------
// Subscribe: the request-construction and transport error arms that the
// httptest-based tests in watch_test.go cannot reach.
// ----------------------------------------------------------------------

// TestSubscribeRequestBuildError: a control character in the base makes
// the subscriptions URL unbuildable; that must be reported as a build
// error, never panicked over. The Client is constructed by literal to
// place the bad base past NewClient's own validation.
func TestSubscribeRequestBuildError(t *testing.T) {
	c := &query.Client{Base: "http://h\x7f", Codec: v13(t)}
	_, err := c.Subscribe(context.Background(), query.SubscribeRequest{ResourcePath: "/nodes/"})
	if err == nil || !strings.Contains(err.Error(), "build subscription request") {
		t.Fatalf("err = %v, want a build-request error", err)
	}
}

// TestSubscribeTransportError: an unreachable Registry surfaces the
// POST transport failure rather than a nil subscription.
func TestSubscribeTransportError(t *testing.T) {
	c := &query.Client{Base: "http://127.0.0.1:1", Codec: v13(t)}
	_, err := c.Subscribe(context.Background(), query.SubscribeRequest{ResourcePath: "/nodes/"})
	if err == nil || !strings.Contains(err.Error(), "POST") {
		t.Fatalf("err = %v, want a POST transport error", err)
	}
}
