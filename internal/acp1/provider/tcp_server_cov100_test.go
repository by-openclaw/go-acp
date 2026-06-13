package acp1

import (
	"context"
	"encoding/binary"
	"net"
	"testing"
	"time"

	"dhs/internal/acp1/codec"
)

// TestServeTCP_AddrErrors covers ServeTCP's resolve and listen failures.
func TestServeTCP_AddrErrors(t *testing.T) {
	s := newTestServer(t)
	if err := s.ServeTCP(context.Background(), "not-an-addr:::"); err == nil {
		t.Error("bad addr: want resolve error")
	}
	if err := s.ServeTCP(context.Background(), "127.0.0.1:999999"); err == nil {
		t.Error("out-of-range port: want listen error")
	}
}

// TestNewTCPSessionRegistry_NilLogger covers the nil-logger default.
func TestNewTCPSessionRegistry_NilLogger(t *testing.T) {
	r := newTCPSessionRegistry(nil)
	if r.logger == nil {
		t.Error("nil logger should default")
	}
}

// TestEnqueue_DropsOnFull covers the channel-full drop arm.
func TestEnqueue_DropsOnFull(t *testing.T) {
	ch := make(chan []byte, 1)
	ch <- []byte{0} // fill it
	enqueue(ch, []byte{1}, "test", discardLogger()) // must not block; drops
	if len(ch) != 1 {
		t.Fatalf("channel len = %d, want 1 (second enqueue dropped)", len(ch))
	}
}

// TestWriteMLENFrame_Oversize covers the outbound-too-big guard.
func TestWriteMLENFrame_Oversize(t *testing.T) {
	// A nil conn is fine because the size check returns before any write.
	big := make([]byte, tcpMaxFrameBytes+1)
	if err := writeMLENFrame(nil, big); err == nil {
		t.Fatal("oversize frame: want error")
	}
}

// TestServeTCP_MLENTooSmall sends an MLEN below the minimum so the server's
// readMLENFrame rejects it (and the reader logs + breaks).
func TestServeTCP_MLENTooSmall(t *testing.T) {
	s := newTestServer(t)
	addr, _ := startTCPServer(t, s)
	conn, err := net.DialTimeout("tcp4", addr, 2*time.Second)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer func() { _ = conn.Close() }()
	var lb [4]byte
	binary.BigEndian.PutUint32(lb[:], 1) // below tcpMinMLEN
	_, _ = conn.Write(lb[:])
	time.Sleep(50 * time.Millisecond)
}

// TestServeTCP_DecodeError sends a valid-MLEN frame whose ACP1 payload is
// malformed → the reader's decode-error arm logs and continues.
func TestServeTCP_DecodeError(t *testing.T) {
	s := newTestServer(t)
	addr, _ := startTCPServer(t, s)
	conn, err := net.DialTimeout("tcp4", addr, 2*time.Second)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer func() { _ = conn.Close() }()
	// MLEN=8 (passes the >=tcpMinMLEN check) but the 8-byte ACP1 payload has
	// an invalid MTYPE so codec.Decode rejects it.
	body := []byte{0x00, 0x00, 0x00, 0x05, 0x01, 0x09, 0x00, 0x00}
	var lb [4]byte
	binary.BigEndian.PutUint32(lb[:], uint32(len(body)))
	_, _ = conn.Write(lb[:])
	_, _ = conn.Write(body)
	time.Sleep(50 * time.Millisecond) // let the reader process + continue
}

// TestServeTCP_WriteErrorOnClosedPeer: send a request then immediately close
// the client so the server's writer Write fails when it pushes the reply.
func TestServeTCP_WriteErrorOnClosedPeer(t *testing.T) {
	s := newTestServer(t)
	addr, _ := startTCPServer(t, s)
	for i := 0; i < 8; i++ {
		conn, err := net.DialTimeout("tcp4", addr, 2*time.Second)
		if err != nil {
			continue
		}
		// Valid getValue request (identity[0] on slot 1).
		body := []byte{0x00, 0x00, 0x00, 0x01, 0x01, 0x01, 0x01, byte(codec.MethodGetValue), byte(codec.GroupIdentity), 0x00}
		var lb [4]byte
		binary.BigEndian.PutUint32(lb[:], uint32(len(body)))
		_, _ = conn.Write(lb[:])
		_, _ = conn.Write(body)
		_ = conn.Close() // close before the server flushes its reply
	}
	time.Sleep(100 * time.Millisecond)
}

// TestServeTCP_TruncatedBody: an MLEN prefix promising more bytes than are
// sent (then close) drives readMLENFrame's body-read error.
func TestServeTCP_TruncatedBody(t *testing.T) {
	s := newTestServer(t)
	addr, _ := startTCPServer(t, s)
	conn, err := net.DialTimeout("tcp4", addr, 2*time.Second)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	var lb [4]byte
	binary.BigEndian.PutUint32(lb[:], 20) // promise 20 bytes
	_, _ = conn.Write(lb[:])
	_, _ = conn.Write([]byte{0x00, 0x00}) // send only 2
	_ = conn.Close()
	time.Sleep(50 * time.Millisecond)
}

// TestTCPRegistry_PerIPCapRefuses drives the per-ip cap rejection by
// exhausting tryAdd for one IP.
func TestTCPRegistry_PerIPCapRefuses(t *testing.T) {
	r := newTCPSessionRegistry(discardLogger())
	for i := 0; i < tcpMaxSessionsPerIP; i++ {
		if !r.tryAdd("1.2.3.4") {
			t.Fatalf("tryAdd %d unexpectedly refused", i)
		}
	}
	if r.tryAdd("1.2.3.4") {
		t.Fatal("tryAdd past cap should be refused")
	}
}
