package acp1

import (
	"context"
	"encoding/binary"
	"errors"
	"io"
	"log/slog"
	"net"
	"strings"
	"sync"
	"testing"
	"time"

	"dhs/internal/acp1/codec"
)

// startTCPServer launches s.ServeTCP on a kernel-assigned loopback port.
// Returns the listen address and a cancel function. Blocks until the
// server is actually accepting (a 50ms readiness probe).
func startTCPServer(t *testing.T, s *server) (string, context.CancelFunc) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())

	ln, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		cancel()
		t.Fatalf("temp listener: %v", err)
	}
	addr := ln.Addr().String()
	_ = ln.Close()

	done := make(chan error, 1)
	go func() {
		done <- s.ServeTCP(ctx, addr)
	}()

	// Probe-ready loop.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		c, err := net.DialTimeout("tcp4", addr, 50*time.Millisecond)
		if err == nil {
			_ = c.Close()
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	t.Cleanup(func() {
		cancel()
		select {
		case <-done:
		case <-time.After(2 * time.Second):
			t.Errorf("ServeTCP did not return within 2s of cancel")
		}
	})
	return addr, cancel
}

// dialAndExchange opens one TCP session, sends the encoded request, and
// reads exactly one MLEN-framed reply. Closes the connection.
func dialAndExchange(t *testing.T, addr string, req *codec.Message) *codec.Message {
	t.Helper()
	conn, err := net.DialTimeout("tcp4", addr, time.Second)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer func() { _ = conn.Close() }()
	_ = conn.SetDeadline(time.Now().Add(2 * time.Second))

	payload, err := req.Encode()
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	var lenBuf [4]byte
	binary.BigEndian.PutUint32(lenBuf[:], uint32(len(payload)))
	if _, err := conn.Write(lenBuf[:]); err != nil {
		t.Fatalf("write len: %v", err)
	}
	if _, err := conn.Write(payload); err != nil {
		t.Fatalf("write payload: %v", err)
	}

	if _, err := io.ReadFull(conn, lenBuf[:]); err != nil {
		t.Fatalf("read len: %v", err)
	}
	mlen := binary.BigEndian.Uint32(lenBuf[:])
	if mlen > tcpMaxFrameBytes {
		t.Fatalf("reply MLEN %d > max %d", mlen, tcpMaxFrameBytes)
	}
	body := make([]byte, mlen)
	if _, err := io.ReadFull(conn, body); err != nil {
		t.Fatalf("read body: %v", err)
	}
	rep, err := codec.Decode(body)
	if err != nil {
		t.Fatalf("decode reply: %v", err)
	}
	return rep
}

func TestServeTCP_GetValue_RoundTrip(t *testing.T) {
	s := newTestServer(t)
	addr, _ := startTCPServer(t, s)

	req := &codec.Message{
		MTID:     0xABCD,
		MType:    codec.MTypeRequest,
		MAddr:    1,
		MCode:    byte(codec.MethodGetValue),
		ObjGroup: codec.GroupIdentity,
		ObjID:    0,
	}
	rep := dialAndExchange(t, addr, req)
	if rep.MType != codec.MTypeReply {
		t.Fatalf("MType = %d, want reply", rep.MType)
	}
	if rep.MTID != 0xABCD {
		t.Fatalf("MTID mirror broken: got %d", rep.MTID)
	}
	if string(rep.Value) != "GIO-12\x00" {
		t.Fatalf("value = %q, want GIO-12\\x00", rep.Value)
	}
}

func TestServeTCP_FrameMaxBound_ClosesSession(t *testing.T) {
	s := newTestServer(t)
	addr, _ := startTCPServer(t, s)

	conn, err := net.DialTimeout("tcp4", addr, time.Second)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer func() { _ = conn.Close() }()
	_ = conn.SetDeadline(time.Now().Add(2 * time.Second))

	// Forge an oversized MLEN.
	var lenBuf [4]byte
	binary.BigEndian.PutUint32(lenBuf[:], uint32(tcpMaxFrameBytes+10))
	if _, err := conn.Write(lenBuf[:]); err != nil {
		t.Fatalf("write len: %v", err)
	}
	// Server should close the connection on bad MLEN — read EOF.
	one := make([]byte, 1)
	_, err = conn.Read(one)
	if err == nil {
		t.Fatal("expected EOF / connection close on oversized MLEN")
	}
}

func TestTCPSessionRegistry_PerIPCap(t *testing.T) {
	reg := newTCPSessionRegistry(slog.Default())
	for i := 0; i < tcpMaxSessionsPerIP; i++ {
		if !reg.tryAdd("10.0.0.1") {
			t.Fatalf("tryAdd #%d should succeed", i)
		}
	}
	if reg.tryAdd("10.0.0.1") {
		t.Fatal("33rd tryAdd should fail (cap reached)")
	}
	// Different IP unaffected.
	if !reg.tryAdd("10.0.0.2") {
		t.Fatal("different IP should not share the cap")
	}
}

func TestTCPSessionRegistry_RemoveDecrementsPerIP(t *testing.T) {
	reg := newTCPSessionRegistry(slog.Default())
	if !reg.tryAdd("10.0.0.1") {
		t.Fatal("tryAdd should succeed")
	}
	send := make(chan []byte, 1)
	conn, _ := net.Pipe()
	defer func() { _ = conn.Close() }()
	tcpConn := &net.TCPConn{} // placeholder; registry only stores it
	_ = conn
	sess := reg.register("10.0.0.1", tcpConn, send)
	reg.remove("10.0.0.1", sess.id)
	// After remove: cap reset, session count = 0.
	if got := reg.activeSessions(); got != 0 {
		t.Fatalf("active = %d, want 0", got)
	}
	if !reg.tryAdd("10.0.0.1") {
		t.Fatal("tryAdd after remove should succeed")
	}
}

func TestTCPSessionRegistry_BroadcastDropsOnFull(t *testing.T) {
	reg := newTCPSessionRegistry(slog.Default())
	send := make(chan []byte) // unbuffered — first push fills, second drops
	tcpConn := &net.TCPConn{}
	_ = reg.register("10.0.0.1", tcpConn, send)

	// Drain in a goroutine to allow ONE successful broadcast then no
	// more (we cancel the goroutine).
	var wg sync.WaitGroup
	wg.Add(1)
	stop := make(chan struct{})
	go func() {
		defer wg.Done()
		select {
		case <-send:
		case <-stop:
		}
	}()

	reg.broadcast([]byte("first"))
	close(stop)
	wg.Wait()

	// Second broadcast: receiver gone, registry must drop without
	// blocking.
	done := make(chan struct{})
	go func() {
		reg.broadcast([]byte("second"))
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("broadcast blocked on full channel")
	}
}

func TestRemoteIP_StripsPort(t *testing.T) {
	addr := &net.TCPAddr{IP: net.ParseIP("10.6.239.113"), Port: 12345}
	if got := remoteIP(addr); got != "10.6.239.113" {
		t.Fatalf("got %q, want 10.6.239.113", got)
	}
}

func TestServeTCP_AnnounceBroadcast_AcrossSessions(t *testing.T) {
	s := newTestServer(t)
	addr, _ := startTCPServer(t, s)

	// Connection A: subscriber that just reads.
	connA, err := net.DialTimeout("tcp4", addr, time.Second)
	if err != nil {
		t.Fatalf("dial A: %v", err)
	}
	defer func() { _ = connA.Close() }()
	_ = connA.SetDeadline(time.Now().Add(3 * time.Second))

	// Wait for server registration to complete.
	time.Sleep(50 * time.Millisecond)

	// Connection B: triggers a SetValue on the writable Level field.
	connB, err := net.DialTimeout("tcp4", addr, time.Second)
	if err != nil {
		t.Fatalf("dial B: %v", err)
	}
	defer func() { _ = connB.Close() }()
	_ = connB.SetDeadline(time.Now().Add(3 * time.Second))

	time.Sleep(50 * time.Millisecond)

	setReq := &codec.Message{
		MTID: 1, MType: codec.MTypeRequest, MAddr: 1,
		MCode: byte(codec.MethodSetValue),
		ObjGroup: codec.GroupControl, ObjID: 0,
		Value: []byte{0x00, 0x05}, // i16 BE = 5
	}
	payload, _ := setReq.Encode()
	var lenBuf [4]byte
	binary.BigEndian.PutUint32(lenBuf[:], uint32(len(payload)))
	if _, err := connB.Write(lenBuf[:]); err != nil {
		t.Fatalf("write set: %v", err)
	}
	if _, err := connB.Write(payload); err != nil {
		t.Fatalf("write set body: %v", err)
	}

	// Connection A should see the announce. Read MLEN+payload.
	gotAnnounce := false
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		_ = connA.SetReadDeadline(time.Now().Add(500 * time.Millisecond))
		var lb [4]byte
		if _, err := io.ReadFull(connA, lb[:]); err != nil {
			if isTimeout(err) {
				continue
			}
			break
		}
		mlen := binary.BigEndian.Uint32(lb[:])
		body := make([]byte, mlen)
		if _, err := io.ReadFull(connA, body); err != nil {
			break
		}
		msg, err := codec.Decode(body)
		if err != nil {
			continue
		}
		if msg.MTID == 0 && msg.MType == codec.MTypeReply &&
			msg.ObjGroup == codec.GroupControl && msg.ObjID == 0 {
			gotAnnounce = true
			break
		}
	}
	if !gotAnnounce {
		t.Fatal("connection A did not receive the announce broadcast")
	}
}

func isTimeout(err error) bool {
	var ne net.Error
	if errors.As(err, &ne) {
		return ne.Timeout()
	}
	return strings.Contains(err.Error(), "i/o timeout")
}
