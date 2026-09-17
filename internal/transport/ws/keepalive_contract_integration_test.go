//go:build integration

package ws

// Characterises a real WebSocket server's keep-alive contract: does it close
// a client that never answers its Ping?
//
// Run against Cerebrum NB:
//
//	CEREBRUM_WS_URL=ws://10.6.250.5:40009/ go test -tags integration \
//	    ./internal/transport/ws -run TestKeepAliveContract -v
//
// Never runs in CI (build tag + env gate), per the repo's integration rules.
//
// The two cases differ in exactly one thing — whether we answer the server's
// Ping — so the difference between them IS the answer. No inference required.

import (
	"context"
	"os"
	"testing"
	"time"
)

func TestKeepAliveContract(t *testing.T) {
	url := os.Getenv("CEREBRUM_WS_URL")
	if url == "" {
		t.Skip("set CEREBRUM_WS_URL to characterise a live server")
	}
	hold := 45 * time.Second
	if v := os.Getenv("KEEPALIVE_HOLD"); v != "" {
		d, err := time.ParseDuration(v)
		if err != nil {
			t.Fatalf("KEEPALIVE_HOLD: %v", err)
		}
		hold = d
	}

	// CASE A — we answer. Draining RX runs ws.Conn's inline Ping->Pong.
	t.Run("answering pings keeps the session open", func(t *testing.T) {
		c, err := Dial(context.Background(), url, nil)
		if err != nil {
			t.Fatalf("dial: %v", err)
		}
		defer func() { _ = c.Close(1000, "") }()

		errCh := make(chan error, 1)
		go func() {
			for {
				if _, _, e := c.ReadMessage(context.Background()); e != nil {
					errCh <- e
					return
				}
			}
		}()

		start := time.Now()
		select {
		case e := <-errCh:
			t.Fatalf("server closed us after %.1fs despite our Pongs: %v",
				time.Since(start).Seconds(), e)
		case <-time.After(hold):
			t.Logf("still open after %s — answering Pings keeps it alive", hold)
		}
	})

	// CASE B — we never answer. No read means ws.Conn never sends a Pong.
	t.Run("not answering pings gets us closed", func(t *testing.T) {
		c, err := Dial(context.Background(), url, nil)
		if err != nil {
			t.Fatalf("dial: %v", err)
		}
		defer func() { _ = c.Close(1000, "") }()

		start := time.Now()
		time.Sleep(hold) // total silence: no Pong is ever emitted

		// If the server hung up for want of a Pong, this read fails at once.
		rctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_, _, rerr := c.ReadMessage(rctx)
		elapsed := time.Since(start).Seconds()

		switch {
		case rerr == nil:
			t.Logf("RESULT after %.1fs: a frame arrived — server did NOT close us", elapsed)
		case rctx.Err() != nil:
			t.Logf("RESULT after %.1fs: read timed out — connection still open, no close", elapsed)
		default:
			t.Logf("RESULT after %.1fs: read failed immediately: %v", elapsed, rerr)
			t.Logf("=> the server CLOSES a client that does not answer its Ping")
		}
	})
}

// CASE C — the variable cases A/B leave untested: does LOGIN arm a
// server-side timer that an anonymous socket never gets?
//
// The consumer's own keepalive-probe verb carries a --send-login flag whose
// stated purpose is exactly this question, so it is worth answering rather
// than assuming an idle anonymous socket characterises a real session.
//
//	CEREBRUM_WS_URL=ws://host:40009/ CEREBRUM_LOGIN_XML='<LOGIN .../>' \
//	  go test -tags integration ./internal/transport/ws -run TestKeepAliveAfterLogin -v
func TestKeepAliveAfterLogin(t *testing.T) {
	url := os.Getenv("CEREBRUM_WS_URL")
	login := os.Getenv("CEREBRUM_LOGIN_XML")
	if url == "" || login == "" {
		t.Skip("set CEREBRUM_WS_URL and CEREBRUM_LOGIN_XML")
	}
	hold := 45 * time.Second
	if v := os.Getenv("KEEPALIVE_HOLD"); v != "" {
		d, err := time.ParseDuration(v)
		if err != nil {
			t.Fatalf("KEEPALIVE_HOLD: %v", err)
		}
		hold = d
	}

	c, err := Dial(context.Background(), url, nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer func() { _ = c.Close(1000, "") }()

	wctx, wcancel := context.WithTimeout(context.Background(), 5*time.Second)
	if werr := c.WriteText(wctx, []byte(login)); werr != nil {
		wcancel()
		t.Fatalf("write LOGIN: %v", werr)
	}
	wcancel()

	// Read exactly the LOGIN_REPLY, then go silent — no further reads, so no
	// Pong is ever emitted for whatever the server sends afterwards.
	rctx, rcancel := context.WithTimeout(context.Background(), 10*time.Second)
	_, reply, rerr := c.ReadMessage(rctx)
	rcancel()
	if rerr != nil {
		t.Fatalf("read LOGIN_REPLY: %v", rerr)
	}
	t.Logf("logged in: %s", string(reply))

	start := time.Now()
	time.Sleep(hold)

	r2, cancel2 := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel2()
	_, _, e2 := c.ReadMessage(r2)
	elapsed := time.Since(start).Seconds()

	switch {
	case e2 == nil:
		t.Logf("RESULT after %.1fs: frame arrived — authenticated session NOT closed", elapsed)
	case r2.Err() != nil:
		t.Logf("RESULT after %.1fs: read timed out — authenticated session still open", elapsed)
	default:
		t.Logf("RESULT after %.1fs: read failed immediately: %v", elapsed, e2)
		t.Logf("=> LOGIN arms a server-side timer; an unanswered Ping then closes the session")
	}
}
