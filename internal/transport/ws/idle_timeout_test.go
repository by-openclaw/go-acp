package ws

// Coverage for the per-frame idle read deadline.
//
// This is the mechanism that makes a half-open WebSocket detectable: without
// it a read blocks in the kernel forever when a NAT or firewall drops the flow
// without an RST, and a 24/7 watcher goes silent without failing. The deadline
// is re-armed before EVERY frame — control frames included — so a connection
// carrying only Pongs stays alive rather than being killed by the watchdog it
// is answering.

import (
	"bufio"
	"context"
	"errors"
	"net"
	"os"
	"testing"
	"time"
)

// pipeConn wires a Conn to one end of a real TCP pair so deadlines behave the
// way they do in production (net.Pipe ignores SetReadDeadline semantics we
// care about here).
func tcpPair(t *testing.T) (client net.Conn, server net.Conn) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer func() { _ = ln.Close() }()

	done := make(chan net.Conn, 1)
	go func() {
		c, aerr := ln.Accept()
		if aerr != nil {
			done <- nil
			return
		}
		done <- c
	}()
	c, err := net.Dial("tcp", ln.Addr().String())
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	s := <-done
	if s == nil {
		t.Fatal("accept failed")
	}
	t.Cleanup(func() { _ = c.Close(); _ = s.Close() })
	return c, s
}

func TestSetIdleTimeoutRoundTrip(t *testing.T) {
	c, _ := tcpPair(t)
	conn := newConn(c, bufio.NewReader(c), 0)

	if got := conn.IdleTimeout(); got != 0 {
		t.Fatalf("IdleTimeout = %v on a fresh conn, want 0", got)
	}
	conn.SetIdleTimeout(45 * time.Second)
	if got := conn.IdleTimeout(); got != 45*time.Second {
		t.Fatalf("IdleTimeout = %v, want 45s", got)
	}
	conn.SetIdleTimeout(0)
	if got := conn.IdleTimeout(); got != 0 {
		t.Fatalf("IdleTimeout = %v after disable, want 0", got)
	}
	// Negative is clamped to 0 (disabled), never passed through as a
	// deadline in the past.
	conn.SetIdleTimeout(-5 * time.Second)
	if got := conn.IdleTimeout(); got != 0 {
		t.Fatalf("IdleTimeout = %v for a negative input, want 0", got)
	}
}

// The headline behaviour: a peer that says nothing trips the deadline, and the
// error is a net timeout so the session layer can classify it as idle-timeout.
func TestReadMessageIdleDeadlineFires(t *testing.T) {
	c, _ := tcpPair(t) // server end never writes
	conn := newConn(c, bufio.NewReader(c), 0)
	conn.SetIdleTimeout(100 * time.Millisecond)

	start := time.Now()
	_, _, err := conn.ReadMessage(context.Background())
	if err == nil {
		t.Fatal("ReadMessage returned nil error on a silent peer — the reader is hung")
	}
	if !errors.Is(err, os.ErrDeadlineExceeded) && !isTimeout(err) {
		t.Fatalf("err = %v, want a deadline/timeout error", err)
	}
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Fatalf("took %v to notice silence; the deadline did not apply", elapsed)
	}
}

// An explicit ctx deadline wins over the rolling idle timeout: a bounded
// request must not be extended by the unbounded-watch mechanism.
func TestReadMessageCtxDeadlineWins(t *testing.T) {
	c, _ := tcpPair(t)
	conn := newConn(c, bufio.NewReader(c), 0)
	conn.SetIdleTimeout(time.Hour) // would otherwise park us for an hour

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	start := time.Now()
	if _, _, err := conn.ReadMessage(ctx); err == nil {
		t.Fatal("expected the ctx deadline to fire")
	}
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Fatalf("ctx deadline ignored in favour of the idle timeout (%v)", elapsed)
	}
}

// Setting the timeout while a read is already blocked must re-arm the socket
// so the new, tighter bound takes effect — otherwise a tightened window waits
// out the old one, which may never expire.
func TestSetIdleTimeoutAppliesToBlockedReader(t *testing.T) {
	c, _ := tcpPair(t)
	conn := newConn(c, bufio.NewReader(c), 0)

	errCh := make(chan error, 1)
	go func() {
		_, _, err := conn.ReadMessage(context.Background())
		errCh <- err
	}()

	// Reader is now blocked with NO deadline. Arm a short one after the fact.
	time.Sleep(50 * time.Millisecond)
	conn.SetIdleTimeout(100 * time.Millisecond)

	select {
	case err := <-errCh:
		if err == nil {
			t.Fatal("blocked read returned nil error")
		}
	case <-time.After(10 * time.Second):
		t.Fatal("a reader already blocked never picked up the newly-armed deadline")
	}
}

func isTimeout(err error) bool {
	var ne net.Error
	return errors.As(err, &ne) && ne.Timeout()
}

// The arm-the-deadline call can itself fail — on a socket the peer or the
// local side has already closed. ReadMessage must surface that rather than
// proceeding to read with a stale (or absent) deadline.
func TestReadMessageSetDeadlineFailure(t *testing.T) {
	c, _ := tcpPair(t)
	conn := newConn(c, bufio.NewReader(c), 0)
	conn.SetIdleTimeout(time.Second)

	// Close the underlying socket: SetReadDeadline on a closed conn errors.
	if err := c.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	if _, _, err := conn.ReadMessage(context.Background()); err == nil {
		t.Fatal("ReadMessage returned nil after the deadline could not be armed")
	}
}
