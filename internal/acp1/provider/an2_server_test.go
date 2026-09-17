package acp1

import (
	"context"
	"io"
	"net"
	"testing"
	"time"

	"dhs/internal/acp1/codec"
	an2 "dhs/internal/acp2/codec"
)

func startAN2Server(t *testing.T, s *server) string {
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
		done <- s.ServeAN2(ctx, addr)
	}()

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
			t.Errorf("ServeAN2 did not return within 2s of cancel")
		}
	})
	return addr
}

func sendAN2(t *testing.T, conn net.Conn, f *an2.AN2Frame) {
	t.Helper()
	b, err := an2.EncodeAN2Frame(f)
	if err != nil {
		t.Fatalf("encode an2: %v", err)
	}
	if _, err := conn.Write(b); err != nil {
		t.Fatalf("write an2: %v", err)
	}
}

func recvAN2(t *testing.T, conn net.Conn) *an2.AN2Frame {
	t.Helper()
	var hdr [an2.AN2HeaderSize]byte
	if _, err := io.ReadFull(conn, hdr[:]); err != nil {
		t.Fatalf("read header: %v", err)
	}
	dlen := int(uint16(hdr[6])<<8 | uint16(hdr[7]))
	body := make([]byte, dlen)
	if dlen > 0 {
		if _, err := io.ReadFull(conn, body); err != nil {
			t.Fatalf("read body: %v", err)
		}
	}
	full := append(append([]byte{}, hdr[:]...), body...)
	f, _, err := an2.DecodeAN2Frame(full)
	if err != nil {
		t.Fatalf("decode an2: %v", err)
	}
	return f
}

func TestServeAN2_GetVersion(t *testing.T) {
	s := newTestServer(t)
	addr := startAN2Server(t, s)

	conn, err := net.DialTimeout("tcp4", addr, time.Second)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer func() { _ = conn.Close() }()
	_ = conn.SetDeadline(time.Now().Add(2 * time.Second))

	sendAN2(t, conn, &an2.AN2Frame{
		Proto: an2.AN2ProtoInternal, Slot: 0, MTID: 1, Type: an2.AN2TypeRequest,
		Payload: []byte{an2.AN2FuncGetVersion},
	})
	rep := recvAN2(t, conn)
	if rep.Proto != an2.AN2ProtoInternal || rep.Type != an2.AN2TypeReply {
		t.Fatalf("unexpected reply: %+v", rep)
	}
	if len(rep.Payload) < 3 || rep.Payload[0] != an2.AN2FuncGetVersion {
		t.Fatalf("payload = %v, want [GetVersion, major, minor]", rep.Payload)
	}
}

func TestServeAN2_ACP1Roundtrip(t *testing.T) {
	s := newTestServer(t)
	addr := startAN2Server(t, s)

	conn, err := net.DialTimeout("tcp4", addr, time.Second)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer func() { _ = conn.Close() }()
	_ = conn.SetDeadline(time.Now().Add(2 * time.Second))

	// Wrap an ACP1 getValue request inside an AN2 data frame proto=1.
	acp1Req := &codec.Message{
		MTID:     0xCAFE,
		MType:    codec.MTypeRequest,
		MAddr:    1,
		MCode:    byte(codec.MethodGetValue),
		ObjGroup: codec.GroupIdentity,
		ObjID:    0,
	}
	body, err := acp1Req.Encode()
	if err != nil {
		t.Fatalf("acp1 encode: %v", err)
	}
	sendAN2(t, conn, &an2.AN2Frame{
		Proto: an2.AN2ProtoACP1, Slot: 1, MTID: 0, Type: an2.AN2TypeData,
		Payload: body,
	})

	rep := recvAN2(t, conn)
	if rep.Proto != an2.AN2ProtoACP1 || rep.Type != an2.AN2TypeData {
		t.Fatalf("unexpected reply frame: %+v", rep)
	}
	acp1Rep, err := codec.Decode(rep.Payload)
	if err != nil {
		t.Fatalf("decode acp1 reply: %v", err)
	}
	if acp1Rep.MTID != 0xCAFE {
		t.Fatalf("MTID mirror broken: got %d", acp1Rep.MTID)
	}
	if string(acp1Rep.Value) != "GIO-12\x00" {
		t.Fatalf("value = %q", acp1Rep.Value)
	}
}

func TestServeAN2_AnnouncesGatedByEnableProtocolEvents(t *testing.T) {
	s := newTestServer(t)
	addr := startAN2Server(t, s)

	// Client A: connects but does NOT call EnableProtocolEvents.
	connA, err := net.DialTimeout("tcp4", addr, time.Second)
	if err != nil {
		t.Fatalf("dial A: %v", err)
	}
	defer func() { _ = connA.Close() }()
	_ = connA.SetDeadline(time.Now().Add(3 * time.Second))

	// Client B: enables events.
	connB, err := net.DialTimeout("tcp4", addr, time.Second)
	if err != nil {
		t.Fatalf("dial B: %v", err)
	}
	defer func() { _ = connB.Close() }()
	_ = connB.SetDeadline(time.Now().Add(3 * time.Second))

	time.Sleep(50 * time.Millisecond)

	sendAN2(t, connB, &an2.AN2Frame{
		Proto: an2.AN2ProtoInternal, Slot: 0, MTID: 1, Type: an2.AN2TypeRequest,
		Payload: []byte{an2.AN2FuncEnableProtocolEvents, byte(an2.AN2ProtoACP1)},
	})
	enableRep := recvAN2(t, connB)
	if enableRep.Type != an2.AN2TypeReply {
		t.Fatalf("EnableProtocolEvents reply: %+v", enableRep)
	}

	// Trigger an announce by sending a SetValue over the AN2 wire path.
	// handleAN2ACP1Frame returns reply + announce; announce goes through
	// broadcastACP1 which honours the per-session events-enabled flag.
	setReq := &codec.Message{
		MTID: 2, MType: codec.MTypeRequest, MAddr: 1,
		MCode:    byte(codec.MethodSetValue),
		ObjGroup: codec.GroupControl, ObjID: 0,
		Value: []byte{0x00, 0x07}, // i16 BE = 7
	}
	setBody, err := setReq.Encode()
	if err != nil {
		t.Fatalf("encode setValue: %v", err)
	}
	sendAN2(t, connB, &an2.AN2Frame{
		Proto: an2.AN2ProtoACP1, Slot: 1, MTID: 0, Type: an2.AN2TypeData,
		Payload: setBody,
	})

	// B receives reply first, then announce. Drain until we see the
	// MTID=0 announce (reply has MTID=2).
	gotAnnounce := false
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		_ = connB.SetReadDeadline(time.Now().Add(500 * time.Millisecond))
		f := recvAN2(t, connB)
		inner, derr := codec.Decode(f.Payload)
		if derr != nil {
			continue
		}
		if inner.MTID == 0 {
			gotAnnounce = true
			break
		}
	}
	if !gotAnnounce {
		t.Fatal("B did not receive the announce")
	}

	// A should NOT receive anything — short read deadline ensures we
	// don't block forever.
	_ = connA.SetReadDeadline(time.Now().Add(300 * time.Millisecond))
	if _, err := io.ReadFull(connA, make([]byte, 1)); err == nil {
		t.Fatal("client A received an announce despite not calling EnableProtocolEvents")
	}
}

func TestAN2SessionRegistry_PerIPCap(t *testing.T) {
	reg := newAN2SessionRegistry(nil)
	for i := 0; i < tcpMaxSessionsPerIP; i++ {
		if !reg.tryAdd("10.0.0.5") {
			t.Fatalf("tryAdd #%d should succeed", i)
		}
	}
	if reg.tryAdd("10.0.0.5") {
		t.Fatal("over-cap tryAdd should fail")
	}
}

func TestAN2Session_EnableProtos(t *testing.T) {
	sess := &an2Session{eventsEnabled: map[an2.AN2Proto]bool{}}
	if sess.acp1EventsEnabled() {
		t.Fatal("default should be disabled")
	}
	sess.enableProtos([]byte{byte(an2.AN2ProtoACP1)})
	if !sess.acp1EventsEnabled() {
		t.Fatal("ACP1 should be enabled after enableProtos")
	}
}

func TestServeAN2_GetSlotInfo_PresentSlot(t *testing.T) {
	s := newTestServer(t)
	addr := startAN2Server(t, s)

	conn, err := net.DialTimeout("tcp4", addr, time.Second)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer func() { _ = conn.Close() }()
	_ = conn.SetDeadline(time.Now().Add(2 * time.Second))

	sendAN2(t, conn, &an2.AN2Frame{
		Proto: an2.AN2ProtoInternal, Slot: 0, MTID: 1, Type: an2.AN2TypeRequest,
		Payload: []byte{an2.AN2FuncGetSlotInfo, 1},
	})
	rep := recvAN2(t, conn)
	if rep.Type != an2.AN2TypeReply {
		t.Fatalf("type = %v, want reply", rep.Type)
	}
	if len(rep.Payload) < 3 {
		t.Fatalf("payload = %v, want at least 3 bytes", rep.Payload)
	}
	if rep.Payload[0] != an2.AN2FuncGetSlotInfo {
		t.Fatalf("payload[0] = %d, want GetSlotInfo", rep.Payload[0])
	}
	if rep.Payload[1] != 1 {
		t.Fatalf("payload[1] = %d, want slot=1", rep.Payload[1])
	}
	if rep.Payload[2] != 1 {
		t.Fatalf("present_flag = %d, want 1 (slot 1 has objects in test fixture)", rep.Payload[2])
	}
}
