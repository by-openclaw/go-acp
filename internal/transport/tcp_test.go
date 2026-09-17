package transport

// The TCP arms the shared conformance battery cannot reach: the framing
// errors a misbehaving peer produces (a bad MLEN, a frame cut short), the
// write side of a dead socket, and the two arms of the read path that sit
// BETWEEN the MLEN read and the payload read.
//
// Everything here runs against a real loopback socket. The seams in tcp.go
// are used only where a live socket cannot take the arm at all, or cannot
// take it at a deterministic instant.

import (
	"context"
	"errors"
	"fmt"
	"io"
	"math"
	"net"
	"strconv"
	"strings"
	"testing"
	"time"
)

// tcpPeer dials a TCPConn against a loopback listener and hands back the
// server side as a raw net.Conn, so a test can play the peer byte for byte.
func tcpPeer(t *testing.T) (*TCPConn, net.Conn) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() { _ = ln.Close() })

	host, port := splitAddr(t, ln.Addr().String())
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	c, err := DialTCP(ctx, host, port)
	if err != nil {
		t.Fatalf("DialTCP: %v", err)
	}
	t.Cleanup(func() { _ = c.Close() })

	// The dial has completed its handshake, so Accept cannot block.
	peer, err := ln.Accept()
	if err != nil {
		t.Fatalf("accept: %v", err)
	}
	t.Cleanup(func() { _ = peer.Close() })
	return c, peer
}

// mlenHeader is the 4-byte big-endian MLEN prefix, spelled out rather than
// taken from Send so the test asserts the wire format, not the code.
func mlenHeader(n uint32) []byte {
	return []byte{byte(n >> 24), byte(n >> 16), byte(n >> 8), byte(n)}
}

// A "tcp" dial always yields a *net.TCPConn; the arm needs the seam.
func TestDialTCPUnexpectedConnType(t *testing.T) {
	orig := dialTCPAssert
	dialTCPAssert = func(net.Conn) (*net.TCPConn, bool) { return nil, false }
	t.Cleanup(func() { dialTCPAssert = orig })

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	host, port := splitAddr(t, ln.Addr().String())

	_, err = DialTCP(context.Background(), host, port)
	if !errors.Is(err, ErrWrongConnType) {
		t.Fatalf("DialTCP = %v, want ErrWrongConnType", err)
	}
}

// Every method tolerates a nil receiver: a connector that failed to dial
// still calls Close and logs RemoteAddr in a defer.
func TestTCPConnNilGuards(t *testing.T) {
	var c *TCPConn
	ctx := context.Background()
	if err := c.Send(ctx, []byte("x")); !errors.Is(err, ErrNilConn) {
		t.Errorf("Send on nil = %v, want ErrNilConn", err)
	}
	if _, err := c.Receive(ctx, 16); !errors.Is(err, ErrNilConn) {
		t.Errorf("Receive on nil = %v, want ErrNilConn", err)
	}
	if err := c.Close(); err != nil {
		t.Errorf("Close on nil = %v, want nil", err)
	}
	if a := c.RemoteAddr(); a != nil {
		t.Errorf("RemoteAddr on nil = %v, want nil", a)
	}
}

func TestTCPConnRemoteAddr(t *testing.T) {
	c, peer := tcpPeer(t)
	if got := c.RemoteAddr(); got == nil || got.String() != peer.LocalAddr().String() {
		t.Errorf("RemoteAddr = %v, want %v", got, peer.LocalAddr())
	}
}

// MLEN is a u32 on the wire (ACP1 spec §"ACP Header" p. 10): a payload that
// does not fit is refused before a byte is written, not truncated.
func TestMLENForRejectsPayloadOver4GiB(t *testing.T) {
	if got, err := mlenFor(8); err != nil || got != 8 {
		t.Fatalf("mlenFor(8) = %d, %v; want 8, nil", got, err)
	}
	if strconv.IntSize < 64 {
		t.Skip("a 32-bit int cannot hold a length above MLEN's range")
	}
	var over uint64 = math.MaxUint32 + 1
	_, err := mlenFor(int(over))
	if !errors.Is(err, ErrPayloadTooLarge) {
		t.Fatalf("mlenFor(2^32) = %v, want ErrPayloadTooLarge", err)
	}
}

// Send hands the refusal back untouched and writes nothing — through the
// seam, because provoking it for real means a 4 GiB payload.
func TestTCPSendPropagatesMLENRefusal(t *testing.T) {
	orig := mlenFor
	mlenFor = func(n int) (uint32, error) {
		return 0, fmt.Errorf("%w: tcp payload %d > 4GiB", ErrPayloadTooLarge, n)
	}
	t.Cleanup(func() { mlenFor = orig })

	c, peer := tcpPeer(t)
	if err := c.Send(context.Background(), []byte("12345678")); !errors.Is(err, ErrPayloadTooLarge) {
		t.Fatalf("Send = %v, want ErrPayloadTooLarge", err)
	}
	// Nothing reached the wire: the peer's read hits its deadline, not data.
	_ = peer.SetReadDeadline(time.Now().Add(100 * time.Millisecond))
	if n, err := peer.Read(make([]byte, 1)); err == nil {
		t.Fatalf("peer read %d byte(s) after a refused Send; want none", n)
	}
}

// Both write-side arms of a dead socket: the deadline cannot be armed, and
// without a deadline the write itself fails.
func TestTCPSendOnClosedSocket(t *testing.T) {
	c, _ := tcpPeer(t)
	if err := c.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := c.Send(ctx, []byte("12345678")); !errors.Is(err, ErrSetDeadlineFailed) {
		t.Errorf("Send with deadline on closed = %v, want ErrSetDeadlineFailed", err)
	}
	err := c.Send(context.Background(), []byte("12345678"))
	if !errors.Is(err, ErrWriteFailed) || !strings.Contains(err.Error(), "write len") {
		t.Errorf("Send on closed = %v, want ErrWriteFailed naming the len write", err)
	}
}

// The write deadline reached between the MLEN write and the payload write:
// the arm names the payload. The deadline is the ctx's own and in the
// future; the tcpWrite seam is the clock reaching it in the gap, which no
// stalled peer can be relied on to produce (Windows loopback absorbs tens
// of megabytes without blocking the sender).
func TestTCPSendPayloadWriteHonoursDeadline(t *testing.T) {
	c, _ := tcpPeer(t)
	orig := tcpWrite
	calls := 0
	tcpWrite = func(tc *net.TCPConn, b []byte) (int, error) {
		calls++
		if calls == 2 {
			_ = tc.SetWriteDeadline(time.Now())
		}
		return orig(tc, b)
	}
	t.Cleanup(func() { tcpWrite = orig })

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	err := c.Send(ctx, []byte("12345678"))
	if !errors.Is(err, ErrWriteFailed) || !strings.Contains(err.Error(), "write payload") {
		t.Fatalf("Send = %v, want ErrWriteFailed naming the payload write", err)
	}
}

func TestTCPReceiveOnClosedSocket(t *testing.T) {
	c, _ := tcpPeer(t)
	if err := c.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if _, err := c.Receive(ctx, 64); !errors.Is(err, ErrSetDeadlineFailed) {
		t.Errorf("Receive with deadline on closed = %v, want ErrSetDeadlineFailed", err)
	}
	_, err := c.Receive(context.Background(), 64)
	if !errors.Is(err, ErrReadFailed) || !strings.Contains(err.Error(), "read len") {
		t.Errorf("Receive on closed = %v, want ErrReadFailed naming the len read", err)
	}
}

// A socket deadline that expires with the context still live is a timeout,
// reported as context.DeadlineExceeded so the retry loop can tell it from a
// hard socket error. pureDeadline is what isolates that arm from the cancel
// one (see cancel_test.go).
func TestTCPReceiveTimesOutWaitingForMLEN(t *testing.T) {
	c, _ := tcpPeer(t)
	_, err := c.Receive(pureDeadline(t, 100*time.Millisecond), 64)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Receive from a silent peer = %v, want context.DeadlineExceeded", err)
	}
}

// Spec floor and caller ceiling: MLEN below 8 (one-byte-MDATA error reply is
// the shortest valid frame) or above maxPayload is a framing error, and
// resync would be guesswork.
func TestTCPReceiveRejectsMLENOutOfRange(t *testing.T) {
	tests := []struct {
		name string
		mlen uint32
		max  int
	}{
		{"below the spec floor", 7, 64},
		{"above the caller's max", 65, 64},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			c, peer := tcpPeer(t)
			if _, err := peer.Write(mlenHeader(tc.mlen)); err != nil {
				t.Fatalf("peer write: %v", err)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			_, err := c.Receive(ctx, tc.max)
			if !errors.Is(err, ErrMLENOutOfRange) {
				t.Fatalf("Receive = %v, want ErrMLENOutOfRange", err)
			}
		})
	}
}

// The payload read's three failure arms. The first is a real peer fault;
// the other two need a cancel or an expiry placed BETWEEN the MLEN read and
// the payload read, which the readFull seam does at a deterministic instant
// rather than by racing a timer against the header read.
func TestTCPReceivePayloadReadErrors(t *testing.T) {
	// interceptSecondRead runs hook just before Receive's second read — the
	// payload one — and leaves the reads themselves real.
	interceptSecondRead := func(t *testing.T, hook func()) {
		t.Helper()
		orig := readFull
		calls := 0
		readFull = func(r io.Reader, b []byte) (int, error) {
			calls++
			if calls == 2 {
				hook()
			}
			return orig(r, b)
		}
		t.Cleanup(func() { readFull = orig })
	}

	t.Run("peer closes mid-frame", func(t *testing.T) {
		c, peer := tcpPeer(t)
		if _, err := peer.Write(mlenHeader(16)); err != nil {
			t.Fatalf("peer write: %v", err)
		}
		_ = peer.Close()
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_, err := c.Receive(ctx, 64)
		if !errors.Is(err, ErrReadFailed) || !strings.Contains(err.Error(), "read payload") {
			t.Fatalf("Receive of a cut frame = %v, want ErrReadFailed naming the payload read", err)
		}
	})

	t.Run("cancelled between header and payload", func(t *testing.T) {
		c, peer := tcpPeer(t)
		if _, err := peer.Write(mlenHeader(16)); err != nil {
			t.Fatalf("peer write: %v", err)
		}
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		interceptSecondRead(t, cancel)
		if _, err := c.Receive(ctx, 64); !errors.Is(err, context.Canceled) {
			t.Fatalf("Receive = %v, want context.Canceled", err)
		}
	})

	t.Run("deadline expires between header and payload", func(t *testing.T) {
		c, peer := tcpPeer(t)
		if _, err := peer.Write(mlenHeader(16)); err != nil {
			t.Fatalf("peer write: %v", err)
		}
		// The deadline is real and in the future; the hook is the clock
		// reaching it while the payload is still outstanding.
		interceptSecondRead(t, func() { _ = c.conn.SetReadDeadline(time.Now()) })
		_, err := c.Receive(pureDeadline(t, 5*time.Second), 64)
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("Receive = %v, want context.DeadlineExceeded", err)
		}
	})
}

// A second Close reports net.ErrClosed and is tolerated; any OTHER close
// error is a real fault and must surface as ErrCloseFailed. No fd state
// produces one, so the arm goes through the seam.
func TestTCPCloseReportsUnexpectedError(t *testing.T) {
	orig := closeConn
	closeConn = func(net.Conn) error { return errors.New("boom") }
	t.Cleanup(func() { closeConn = orig })

	c, _ := tcpPeer(t)
	if err := c.Close(); !errors.Is(err, ErrCloseFailed) {
		t.Fatalf("Close = %v, want ErrCloseFailed", err)
	}
}
