package acp1

import (
	"context"
	"net"
	"testing"
	"time"

	an2 "dhs/internal/acp2/codec"
)

// TestServeAN2_AddrErrors covers the ResolveTCPAddr and ListenTCP failure
// returns.
func TestServeAN2_AddrErrors(t *testing.T) {
	s := newTestServer(t)
	if err := s.ServeAN2(context.Background(), "not-an-addr:::"); err == nil {
		t.Error("bad addr: want resolve error")
	}
	// Port 999999 is out of range → ListenTCP fails (resolve may pass).
	if err := s.ServeAN2(context.Background(), "127.0.0.1:999999"); err == nil {
		t.Error("out-of-range port: want listen error")
	}
}

// TestServeAN2_InternalFuncs drives GetDeviceInfo, GetSlotInfo (absent
// slot), and an unknown func ID (default → error reply).
func TestServeAN2_InternalFuncs(t *testing.T) {
	s := newTestServer(t)
	addr := startAN2Server(t, s)
	conn, err := net.DialTimeout("tcp4", addr, 2*time.Second)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer func() { _ = conn.Close() }()

	internalReq := func(payload []byte) *an2.AN2Frame {
		return &an2.AN2Frame{Proto: an2.AN2ProtoInternal, Slot: 0, MTID: 1,
			Type: an2.AN2TypeRequest, Payload: payload}
	}

	// GetDeviceInfo.
	sendAN2(t, conn, internalReq([]byte{an2.AN2FuncGetDeviceInfo}))
	if r := recvAN2(t, conn); len(r.Payload) < 2 {
		t.Errorf("GetDeviceInfo reply too short: %v", r.Payload)
	}
	// GetSlotInfo for an absent slot (present_flag=0).
	sendAN2(t, conn, internalReq([]byte{an2.AN2FuncGetSlotInfo, 200}))
	if r := recvAN2(t, conn); len(r.Payload) < 3 || r.Payload[2] != 0 {
		t.Errorf("GetSlotInfo absent slot: %v", r.Payload)
	}
	// EnableProtocolEvents (ok reply).
	sendAN2(t, conn, internalReq([]byte{an2.AN2FuncEnableProtocolEvents, byte(an2.AN2ProtoACP1)}))
	_ = recvAN2(t, conn)
	// Unknown func → error reply.
	sendAN2(t, conn, internalReq([]byte{0xFE}))
	if r := recvAN2(t, conn); r.Type != an2.AN2TypeError {
		t.Errorf("unknown func reply type = %v, want error", r.Type)
	}
}

// TestServeAN2_UnsupportedProtoAndBadACP1 covers the unsupported-proto
// dispatch arm and the ACP1 non-data / malformed-payload guards.
func TestServeAN2_UnsupportedProtoAndBadACP1(t *testing.T) {
	s := newTestServer(t)
	addr := startAN2Server(t, s)
	conn, err := net.DialTimeout("tcp4", addr, 2*time.Second)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer func() { _ = conn.Close() }()

	// Unsupported proto (ACP2 over this ACP1 producer) → debug-logged skip.
	sendAN2(t, conn, &an2.AN2Frame{Proto: an2.AN2ProtoACP2, Slot: 0, MTID: 0,
		Type: an2.AN2TypeData, Payload: []byte{0x01}})
	// ACP1 frame that is NOT a data frame → handleAN2ACP1Frame early return.
	sendAN2(t, conn, &an2.AN2Frame{Proto: an2.AN2ProtoACP1, Slot: 0, MTID: 0,
		Type: an2.AN2TypeRequest, Payload: []byte{0x01}})
	// ACP1 data frame with a malformed (too-short) ACP1 payload → decode warn.
	sendAN2(t, conn, &an2.AN2Frame{Proto: an2.AN2ProtoACP1, Slot: 0, MTID: 0,
		Type: an2.AN2TypeData, Payload: []byte{0x00}})

	// Give the server time to process before the deferred close.
	time.Sleep(50 * time.Millisecond)
}

// TestServeAN2_WriteErrorOnClosedPeer: send an ACP1 request then close the
// client immediately so the server's writer Write fails on the reply.
func TestServeAN2_WriteErrorOnClosedPeer(t *testing.T) {
	s := newTestServer(t)
	addr := startAN2Server(t, s)
	for i := 0; i < 8; i++ {
		conn, err := net.DialTimeout("tcp4", addr, 2*time.Second)
		if err != nil {
			continue
		}
		// ACP1 getValue(identity,0) wrapped in an AN2 data frame.
		acp1 := []byte{0x00, 0x00, 0x00, 0x01, 0x01, 0x01, 0x01, 0x00, 0x01, 0x00}
		frame := append([]byte{0xC6, 0x35, byte(an2.AN2ProtoACP1), 0x00, 0x00, byte(an2.AN2TypeData),
			byte(len(acp1) >> 8), byte(len(acp1))}, acp1...)
		_, _ = conn.Write(frame)
		_ = conn.Close()
	}
	time.Sleep(100 * time.Millisecond)
}

// TestServeAN2_BadMagicAndTruncated drives readAN2Frame's bad-magic and
// truncated-body error returns (the session reader then logs + breaks).
func TestServeAN2_BadMagicAndTruncated(t *testing.T) {
	s := newTestServer(t)
	addr := startAN2Server(t, s)

	// Bad magic: 8 header bytes with the wrong leading u16.
	c1, err := net.DialTimeout("tcp4", addr, 2*time.Second)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	_, _ = c1.Write([]byte{0x00, 0x00, 0x00, 0x00, 0x00, 0x04, 0x00, 0x00})
	time.Sleep(30 * time.Millisecond)
	_ = c1.Close()

	// Truncated body: a valid header advertising dlen>0 then close before
	// the body arrives → ReadFull body error.
	c2, err := net.DialTimeout("tcp4", addr, 2*time.Second)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	// magic 0xC635, proto=1, slot=0, mtid=0, type=4(data), dlen=10, no body.
	_, _ = c2.Write([]byte{0xC6, 0x35, 0x01, 0x00, 0x00, 0x04, 0x00, 0x0A})
	_ = c2.Close()
	time.Sleep(30 * time.Millisecond)
}

// TestBroadcastAN2Announce_NoRegistry covers the nil-registry early return,
// and a present registry path.
func TestBroadcastAN2Announce_NoRegistry(t *testing.T) {
	s := newTestServer(t)
	s.an2Registry = nil
	s.broadcastAN2Announce([]byte{0x01}) // no-op, must not panic
	s.an2Registry = newAN2SessionRegistry(discardLogger())
	s.broadcastAN2Announce([]byte{0x01}) // registry present → broadcastACP1
}

// TestAN2BroadcastACP1_DropsOnFull drives the slow-consumer drop arm: a
// session with ACP1 events enabled and a full send channel.
func TestAN2BroadcastACP1_DropsOnFull(t *testing.T) {
	reg := newAN2SessionRegistry(discardLogger())
	full := make(chan []byte, 1)
	full <- []byte{0} // fill it
	sess := &an2Session{
		id: 1, ip: "1.2.3.4", send: full,
		eventsEnabled: map[an2.AN2Proto]bool{an2.AN2ProtoACP1: true},
	}
	reg.mu.Lock()
	reg.sessions[1] = sess
	reg.mu.Unlock()
	reg.broadcastACP1([]byte{0x01}) // channel full → drop arm
}

// TestHandleAN2Internal_NotRequest covers the non-request / empty-payload
// guard of handleAN2Internal.
func TestHandleAN2Internal_NotRequest(t *testing.T) {
	s := newTestServer(t)
	if r := s.handleAN2Internal(&an2.AN2Frame{Type: an2.AN2TypeReply}, &an2Session{}); r != nil {
		t.Error("non-request internal frame: want nil reply")
	}
	if r := s.handleAN2Internal(&an2.AN2Frame{Type: an2.AN2TypeRequest, Payload: nil}, &an2Session{}); r != nil {
		t.Error("empty-payload internal frame: want nil reply")
	}
}
