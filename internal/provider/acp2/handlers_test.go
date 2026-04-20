package acp2

import (
	"io"
	"log/slog"
	"net"
	"sync"
	"testing"
	"time"

	iacp2 "acp/internal/protocol/acp2"
)

// newTestSession builds a session bound to a net.Pipe so handshake
// round-trips can be exercised without opening a real TCP listener.
func newTestSession(t *testing.T) (*session, net.Conn) {
	t.Helper()
	server := &server{
		logger:   slog.New(slog.NewTextHandler(io.Discard, nil)),
		tree:     emptyTree(),
		sessions: map[*session]struct{}{},
	}
	server.tree.slotN = 2

	a, b := net.Pipe() // a = session side, b = test side
	sess := newSession(server, a)
	return sess, b
}

// roundtrip runs one request frame through the provider and waits up
// to 500 ms for a single reply frame on the test side.
func roundtrip(t *testing.T, sess *session, peer net.Conn, req *iacp2.AN2Frame) *iacp2.AN2Frame {
	t.Helper()

	raw, err := iacp2.EncodeAN2Frame(req)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}

	var wg sync.WaitGroup
	wg.Add(1)

	// Producer: write request onto the peer side.
	go func() {
		defer wg.Done()
		if _, err := peer.Write(raw); err != nil {
			t.Errorf("peer write: %v", err)
		}
	}()

	// Session goroutine reads + dispatches + writes reply.
	go func() {
		frame, err := iacp2.ReadAN2Frame(sess.conn)
		if err != nil {
			return
		}
		sess.dispatch(frame)
	}()

	// Consumer: read reply from the peer side.
	done := make(chan *iacp2.AN2Frame, 1)
	errCh := make(chan error, 1)
	go func() {
		rep, err := iacp2.ReadAN2Frame(peer)
		if err != nil {
			errCh <- err
			return
		}
		done <- rep
	}()

	select {
	case rep := <-done:
		wg.Wait()
		return rep
	case err := <-errCh:
		t.Fatalf("read reply: %v", err)
	case <-time.After(500 * time.Millisecond):
		t.Fatal("timeout waiting for reply")
	}
	return nil
}

func TestAN2Handshake_GetVersion(t *testing.T) {
	sess, peer := newTestSession(t)
	defer func() { _ = sess.conn.Close() }()
	defer func() { _ = peer.Close() }()

	req := &iacp2.AN2Frame{
		Proto:   iacp2.AN2ProtoInternal,
		Slot:    0,
		MTID:    1,
		Type:    iacp2.AN2TypeRequest,
		Payload: []byte{iacp2.AN2FuncGetVersion},
	}
	rep := roundtrip(t, sess, peer, req)
	if rep.Proto != iacp2.AN2ProtoInternal || rep.Type != iacp2.AN2TypeReply || rep.MTID != 1 {
		t.Fatalf("reply frame wrong: %+v", rep)
	}
	// Payload: [funcID=0, major, minor]
	if len(rep.Payload) != 3 ||
		rep.Payload[0] != iacp2.AN2FuncGetVersion ||
		rep.Payload[1] != an2VersionMajor ||
		rep.Payload[2] != an2VersionMinor {
		t.Fatalf("GetVersion payload=%v want [0, %d, %d]",
			rep.Payload, an2VersionMajor, an2VersionMinor)
	}
}

func TestAN2Handshake_GetDeviceInfo(t *testing.T) {
	sess, peer := newTestSession(t)
	defer func() { _ = sess.conn.Close() }()
	defer func() { _ = peer.Close() }()

	req := &iacp2.AN2Frame{
		Proto:   iacp2.AN2ProtoInternal,
		Slot:    0,
		MTID:    2,
		Type:    iacp2.AN2TypeRequest,
		Payload: []byte{iacp2.AN2FuncGetDeviceInfo},
	}
	rep := roundtrip(t, sess, peer, req)
	if len(rep.Payload) != 2 ||
		rep.Payload[0] != iacp2.AN2FuncGetDeviceInfo ||
		rep.Payload[1] != 2 {
		t.Fatalf("GetDeviceInfo payload=%v want [1, 2]", rep.Payload)
	}
}

func TestAN2Handshake_GetSlotInfo(t *testing.T) {
	sess, peer := newTestSession(t)
	defer func() { _ = sess.conn.Close() }()
	defer func() { _ = peer.Close() }()

	// Slot 0 (controller): status=present, protos=[AN2Internal]
	req := &iacp2.AN2Frame{
		Proto:   iacp2.AN2ProtoInternal,
		Slot:    0,
		MTID:    3,
		Type:    iacp2.AN2TypeRequest,
		Payload: []byte{iacp2.AN2FuncGetSlotInfo, 0},
	}
	rep := roundtrip(t, sess, peer, req)
	// payload: [funcID=2, status=2, num_protos=1, proto=AN2Internal]
	want := []byte{iacp2.AN2FuncGetSlotInfo, slotStatusPresent, 1, uint8(iacp2.AN2ProtoInternal)}
	if !bytesEq(rep.Payload, want) {
		t.Fatalf("slot 0 info=%x want %x", rep.Payload, want)
	}
}

func TestAN2Handshake_GetSlotInfo_CardSlot(t *testing.T) {
	sess, peer := newTestSession(t)
	defer func() { _ = sess.conn.Close() }()
	defer func() { _ = peer.Close() }()

	// Slot 1 (card): present, protos=[AN2Internal, ACP2]
	req := &iacp2.AN2Frame{
		Proto:   iacp2.AN2ProtoInternal,
		Slot:    1,
		MTID:    4,
		Type:    iacp2.AN2TypeRequest,
		Payload: []byte{iacp2.AN2FuncGetSlotInfo, 1},
	}
	rep := roundtrip(t, sess, peer, req)
	want := []byte{iacp2.AN2FuncGetSlotInfo, slotStatusPresent, 2,
		uint8(iacp2.AN2ProtoInternal), uint8(iacp2.AN2ProtoACP2)}
	if !bytesEq(rep.Payload, want) {
		t.Fatalf("slot 1 info=%x want %x", rep.Payload, want)
	}
}

func TestAN2Handshake_GetSlotInfo_OutOfRange(t *testing.T) {
	sess, peer := newTestSession(t)
	defer func() { _ = sess.conn.Close() }()
	defer func() { _ = peer.Close() }()

	// Slot 9 (> slotN=2): status=empty, protos=[]
	req := &iacp2.AN2Frame{
		Proto:   iacp2.AN2ProtoInternal,
		Slot:    9,
		MTID:    5,
		Type:    iacp2.AN2TypeRequest,
		Payload: []byte{iacp2.AN2FuncGetSlotInfo, 9},
	}
	rep := roundtrip(t, sess, peer, req)
	want := []byte{iacp2.AN2FuncGetSlotInfo, slotStatusEmpty, 0}
	if !bytesEq(rep.Payload, want) {
		t.Fatalf("slot 9 info=%x want %x", rep.Payload, want)
	}
}

func TestAN2Handshake_EnableProtocolEvents(t *testing.T) {
	sess, peer := newTestSession(t)
	defer func() { _ = sess.conn.Close() }()
	defer func() { _ = peer.Close() }()

	req := &iacp2.AN2Frame{
		Proto:   iacp2.AN2ProtoInternal,
		Slot:    0,
		MTID:    6,
		Type:    iacp2.AN2TypeRequest,
		// count=1, proto=ACP2
		Payload: []byte{iacp2.AN2FuncEnableProtocolEvents, 1, uint8(iacp2.AN2ProtoACP2)},
	}
	rep := roundtrip(t, sess, peer, req)
	want := []byte{iacp2.AN2FuncEnableProtocolEvents, 0}
	if !bytesEq(rep.Payload, want) {
		t.Fatalf("enable payload=%x want %x", rep.Payload, want)
	}
	if !sess.enabled[iacp2.AN2ProtoACP2] {
		t.Errorf("ACP2 announce subscription not recorded")
	}
}

func bytesEq(a, b []byte) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
