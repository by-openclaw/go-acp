package acp1

import (
	"context"
	"net"
	"testing"
	"time"

	"dhs/internal/acp1/codec"
)

// startLoopbackServer binds the real UDP listener on 127.0.0.1:0 and
// returns its bound address. Exercises the socket path the unit handler
// tests skip: Serve -> readLoop -> handleDatagram2 -> handleRequest ->
// WriteToUDP, plus the ctx-cancel close goroutine.
func startLoopbackServer(t *testing.T, s *server) string {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	errc := make(chan error, 1)
	go func() { errc <- s.Serve(ctx, "127.0.0.1:0") }()

	deadline := time.Now().Add(2 * time.Second)
	var addr string
	for time.Now().Before(deadline) {
		s.mu.Lock()
		if s.conn != nil {
			addr = s.conn.LocalAddr().String()
		}
		s.mu.Unlock()
		if addr != "" {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if addr == "" {
		cancel()
		t.Fatal("loopback server did not bind within 2s")
	}
	t.Cleanup(func() {
		cancel()
		select {
		case <-errc:
		case <-time.After(2 * time.Second):
			t.Error("Serve did not return within 2s of cancel")
		}
	})
	return addr
}

// roundTrip dials the server, sends one request frame, and returns the
// decoded reply (or nil on read timeout for fire-and-forget paths).
func roundTrip(t *testing.T, addr string, req *codec.Message, expectReply bool) *codec.Message {
	t.Helper()
	raw, err := req.Encode()
	if err != nil {
		t.Fatalf("encode request: %v", err)
	}
	conn, err := net.Dial("udp4", addr)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer func() { _ = conn.Close() }()
	if _, err := conn.Write(raw); err != nil {
		t.Fatalf("write: %v", err)
	}
	if !expectReply {
		return nil
	}
	_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	buf := make([]byte, 1500)
	n, err := conn.Read(buf)
	if err != nil {
		t.Fatalf("read reply: %v", err)
	}
	rep, err := codec.Decode(buf[:n])
	if err != nil {
		t.Fatalf("decode reply: %v", err)
	}
	return rep
}

func TestServe_Loopback_GetValueRoundTrip(t *testing.T) {
	s := newTestServer(t)
	addr := startLoopbackServer(t, s)

	// slot 1 identity[0] = "GIO-12" (read-only string).
	rep := roundTrip(t, addr, &codec.Message{
		MTID: 99, PVER: 1, MType: codec.MTypeRequest, MAddr: 1,
		MCode: byte(codec.MethodGetValue), ObjGroup: codec.GroupIdentity, ObjID: 0,
	}, true)
	if rep.MType != codec.MTypeReply || rep.MTID != 99 {
		t.Fatalf("reply shape: type=%d mtid=%d", rep.MType, rep.MTID)
	}
	if string(rep.Value) != "GIO-12\x00" {
		t.Fatalf("value=%q want GIO-12\\x00", rep.Value)
	}
}

func TestServe_Loopback_SetValueAnnouncePath(t *testing.T) {
	s := newTestServer(t)
	addr := startLoopbackServer(t, s)

	// Mutating method drives the announce/broadcast fan-out inside
	// handleDatagram2 (ann != nil branch).
	rep := roundTrip(t, addr, &codec.Message{
		MTID: 7, PVER: 1, MType: codec.MTypeRequest, MAddr: 1,
		MCode: byte(codec.MethodSetValue), ObjGroup: codec.GroupControl, ObjID: 0,
		Value: []byte{0x00, 0x05},
	}, true)
	if rep.MType != codec.MTypeReply || string(rep.Value) != string([]byte{0x00, 0x05}) {
		t.Fatalf("setValue reply: type=%d value=%x", rep.MType, rep.Value)
	}
}

func TestServe_Loopback_GarbageDatagramDropped(t *testing.T) {
	s := newTestServer(t)
	addr := startLoopbackServer(t, s)

	// A non-decodable datagram must be logged + dropped (no reply), and
	// the server must keep serving afterwards.
	conn, err := net.Dial("udp4", addr)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	if _, err := conn.Write([]byte{0x01, 0x02}); err != nil { // too short to decode
		t.Fatalf("write garbage: %v", err)
	}
	_ = conn.Close()

	// Follow-up valid request still answered → loop survived the bad frame.
	rep := roundTrip(t, addr, &codec.Message{
		MTID: 1, PVER: 1, MType: codec.MTypeRequest, MAddr: 1,
		MCode: byte(codec.MethodGetValue), ObjGroup: codec.GroupRoot, ObjID: 0,
	}, true)
	if rep.MType != codec.MTypeReply {
		t.Fatalf("server did not survive garbage frame: %+v", rep)
	}
}

func TestServer_Stop_Idempotent(t *testing.T) {
	s := newTestServer(t)
	_ = startLoopbackServer(t, s)
	// Stop closes the sockets; a second call is a no-op.
	if err := s.Stop(); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	if err := s.Stop(); err != nil {
		t.Fatalf("second Stop should be a no-op: %v", err)
	}
}
