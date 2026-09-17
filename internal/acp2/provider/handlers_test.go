package acp2

import (
	"dhs/internal/acp2/codec"
	"io"
	"log/slog"
	"net"
	"sync"
	"testing"
	"time"
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
	// Mark slots 0 and 1 as present in perSlot so slotInfo returns
	// AN2Internal+ACP2 per spec. Empty map values are fine for tests
	// that only exercise AN2 internal handshake (not ACP2 walks).
	server.tree.perSlot[0] = map[uint32]*entry{}
	server.tree.perSlot[1] = map[uint32]*entry{}

	a, b := net.Pipe() // a = session side, b = test side
	sess := newSession(server, a)
	return sess, b
}

// roundtrip runs one request frame through the provider and waits up
// to 500 ms for a single reply frame on the test side.
func roundtrip(t *testing.T, sess *session, peer net.Conn, req *codec.AN2Frame) *codec.AN2Frame {
	t.Helper()

	raw, err := codec.EncodeAN2Frame(req)
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
		frame, err := codec.ReadAN2Frame(sess.conn)
		if err != nil {
			return
		}
		sess.dispatch(frame)
	}()

	// Consumer: read reply from the peer side.
	done := make(chan *codec.AN2Frame, 1)
	errCh := make(chan error, 1)
	go func() {
		rep, err := codec.ReadAN2Frame(peer)
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

// TestAN2Handshake_GetVersion pins the spec §3.3.1 reply shape:
// payload = func(u8) + ver(u16 BE), dlen=3. Wire bytes for AN2 v1
// are [0x00, 0x00, 0x01], matching real EVS Neuron firmware.
func TestAN2Handshake_GetVersion(t *testing.T) {
	sess, peer := newTestSession(t)
	defer func() { _ = sess.conn.Close() }()
	defer func() { _ = peer.Close() }()

	req := &codec.AN2Frame{
		Proto:   codec.AN2ProtoInternal,
		Slot:    0,
		MTID:    1,
		Type:    codec.AN2TypeRequest,
		Payload: []byte{codec.AN2FuncGetVersion},
	}
	rep := roundtrip(t, sess, peer, req)
	if rep.Proto != codec.AN2ProtoInternal || rep.Type != codec.AN2TypeReply || rep.MTID != 1 {
		t.Fatalf("reply frame wrong: %+v", rep)
	}
	want := []byte{codec.AN2FuncGetVersion, 0x00, 0x01}
	if !bytesEq(rep.Payload, want) {
		t.Fatalf("GetVersion payload=%x want %x", rep.Payload, want)
	}
}

// TestAN2Handshake_GetDeviceInfo pins §3.3.2: info = total slot count
// including slot 0. With slots {0, 1} present, max+1 = 2 — matches
// real EVS Neuron's "{RC=slot0, card=slot1} → info=2" wire shape.
func TestAN2Handshake_GetDeviceInfo(t *testing.T) {
	sess, peer := newTestSession(t)
	defer func() { _ = sess.conn.Close() }()
	defer func() { _ = peer.Close() }()

	req := &codec.AN2Frame{
		Proto:   codec.AN2ProtoInternal,
		Slot:    0,
		MTID:    2,
		Type:    codec.AN2TypeRequest,
		Payload: []byte{codec.AN2FuncGetDeviceInfo},
	}
	rep := roundtrip(t, sess, peer, req)
	want := []byte{codec.AN2FuncGetDeviceInfo, 2}
	if !bytesEq(rep.Payload, want) {
		t.Fatalf("GetDeviceInfo payload=%x want %x", rep.Payload, want)
	}
}

// TestAN2Handshake_GetDeviceInfo_SparseSlots exercises fix #3 of the
// §3.3.2 audit: when the present slot map has gaps (e.g. {0, 5}), the
// reply count must be max-slot+1 = 6, not the cardinality of the map
// (which would be 2 and skip empty intermediates 1-4).
func TestAN2Handshake_GetDeviceInfo_SparseSlots(t *testing.T) {
	sess, peer := newTestSession(t)
	defer func() { _ = sess.conn.Close() }()
	defer func() { _ = peer.Close() }()

	// Replace the default {0, 1} layout with a sparse {0, 5} map.
	delete(sess.srv.tree.perSlot, 1)
	sess.srv.tree.perSlot[5] = map[uint32]*entry{}

	req := &codec.AN2Frame{
		Proto:   codec.AN2ProtoInternal,
		Slot:    0,
		MTID:    7,
		Type:    codec.AN2TypeRequest,
		Payload: []byte{codec.AN2FuncGetDeviceInfo},
	}
	rep := roundtrip(t, sess, peer, req)
	want := []byte{codec.AN2FuncGetDeviceInfo, 6}
	if !bytesEq(rep.Payload, want) {
		t.Fatalf("sparse GetDeviceInfo payload=%x want %x (max slot 5 + 1)",
			rep.Payload, want)
	}
}

// TestAN2Handshake_GetSlotInfo pins §3.3.3 + §3.2.1: a present ACP2
// slot's protos list is [ACP2] only. proto=0 (AN2 internal) is the
// signalling layer per §1.2.3 and is never advertised as a payload
// proto on the wire — real EVS Neuron slot 1 emits [2,3], never [0,…].
func TestAN2Handshake_GetSlotInfo(t *testing.T) {
	sess, peer := newTestSession(t)
	defer func() { _ = sess.conn.Close() }()
	defer func() { _ = peer.Close() }()

	req := &codec.AN2Frame{
		Proto:   codec.AN2ProtoInternal,
		Slot:    0,
		MTID:    3,
		Type:    codec.AN2TypeRequest,
		Payload: []byte{codec.AN2FuncGetSlotInfo},
	}
	rep := roundtrip(t, sess, peer, req)
	want := []byte{codec.AN2FuncGetSlotInfo, slotStatusPresent, 1,
		uint8(codec.AN2ProtoACP2)}
	if !bytesEq(rep.Payload, want) {
		t.Fatalf("slot 0 info=%x want %x", rep.Payload, want)
	}
}

func TestAN2Handshake_GetSlotInfo_CardSlot(t *testing.T) {
	sess, peer := newTestSession(t)
	defer func() { _ = sess.conn.Close() }()
	defer func() { _ = peer.Close() }()

	req := &codec.AN2Frame{
		Proto:   codec.AN2ProtoInternal,
		Slot:    1,
		MTID:    4,
		Type:    codec.AN2TypeRequest,
		Payload: []byte{codec.AN2FuncGetSlotInfo},
	}
	rep := roundtrip(t, sess, peer, req)
	want := []byte{codec.AN2FuncGetSlotInfo, slotStatusPresent, 1,
		uint8(codec.AN2ProtoACP2)}
	if !bytesEq(rep.Payload, want) {
		t.Fatalf("slot 1 info=%x want %x", rep.Payload, want)
	}
}

// TestAN2Handshake_GetSlotInfo_ManifestProtos pins the fixture-declared
// advertisement override (manifest slot "protos"): Cerebrum's Neuron
// driver polls GetSlotInfo and refuses to proceed past a [2]-only reply
// where the real card advertises [2,3,4] / [2,3] (wire-proven
// 2026-08-20, staging capture). A slot without an override keeps the
// default [ACP2] advertisement.
func TestAN2Handshake_GetSlotInfo_ManifestProtos(t *testing.T) {
	sess, peer := newTestSession(t)
	defer func() { _ = sess.conn.Close() }()
	defer func() { _ = peer.Close() }()

	sess.srv.SetSlotProtos(map[uint8][]uint8{0: {2, 3, 4}})

	req := &codec.AN2Frame{
		Proto:   codec.AN2ProtoInternal,
		Slot:    0,
		MTID:    6,
		Type:    codec.AN2TypeRequest,
		Payload: []byte{codec.AN2FuncGetSlotInfo},
	}
	rep := roundtrip(t, sess, peer, req)
	want := []byte{codec.AN2FuncGetSlotInfo, slotStatusPresent, 3, 2, 3, 4}
	if !bytesEq(rep.Payload, want) {
		t.Fatalf("slot 0 info=%x want %x", rep.Payload, want)
	}

	// Slot 1 has no override → default [ACP2].
	req2 := &codec.AN2Frame{
		Proto:   codec.AN2ProtoInternal,
		Slot:    1,
		MTID:    7,
		Type:    codec.AN2TypeRequest,
		Payload: []byte{codec.AN2FuncGetSlotInfo},
	}
	rep2 := roundtrip(t, sess, peer, req2)
	want2 := []byte{codec.AN2FuncGetSlotInfo, slotStatusPresent, 1,
		uint8(codec.AN2ProtoACP2)}
	if !bytesEq(rep2.Payload, want2) {
		t.Fatalf("slot 1 info=%x want %x", rep2.Payload, want2)
	}
}

func TestAN2Handshake_GetSlotInfo_OutOfRange(t *testing.T) {
	sess, peer := newTestSession(t)
	defer func() { _ = sess.conn.Close() }()
	defer func() { _ = peer.Close() }()

	// Slot 9 (> slotN=2): status=empty, protos=[]
	req := &codec.AN2Frame{
		Proto:   codec.AN2ProtoInternal,
		Slot:    9,
		MTID:    5,
		Type:    codec.AN2TypeRequest,
		Payload: []byte{codec.AN2FuncGetSlotInfo},
	}
	rep := roundtrip(t, sess, peer, req)
	want := []byte{codec.AN2FuncGetSlotInfo, slotStatusEmpty, 0}
	if !bytesEq(rep.Payload, want) {
		t.Fatalf("slot 9 info=%x want %x", rep.Payload, want)
	}
}

// TestAN2Handshake_EnableProtocolEvents pins §3.3.4: reply payload is
// the single func byte, dlen=1. Cerebrum drops the session at its 5 s
// timeout if the reply carries any trailing bytes.
func TestAN2Handshake_EnableProtocolEvents(t *testing.T) {
	sess, peer := newTestSession(t)
	defer func() { _ = sess.conn.Close() }()
	defer func() { _ = peer.Close() }()

	req := &codec.AN2Frame{
		Proto: codec.AN2ProtoInternal,
		Slot:  0,
		MTID:  6,
		Type:  codec.AN2TypeRequest,
		// count=1, proto=ACP2
		Payload: []byte{codec.AN2FuncEnableProtocolEvents, 1, uint8(codec.AN2ProtoACP2)},
	}
	rep := roundtrip(t, sess, peer, req)
	want := []byte{codec.AN2FuncEnableProtocolEvents}
	if !bytesEq(rep.Payload, want) {
		t.Fatalf("enable payload=%x want %x", rep.Payload, want)
	}
	if !sess.enabled[codec.AN2ProtoACP2] {
		t.Errorf("ACP2 announce subscription not recorded")
	}
}

// TestACP2Error_NoBody verifies the error reply is exactly the 4-byte
// ACP2 header per spec §"Error" (line 1207-1250 of acp2_protocol.docx).
// The error table lists only type/mtid/stat/pid; no body row.
//
// Request:  ACP2 get_object for obj-id 999 (not in tree).
// Expected reply (AN2 frame payload, total = 4 bytes):
//
//	| byte 0 | byte 1 | byte 2     | byte 3 |
//	| type=3 | mtid=N | stat=1     | pid=0  |
func TestACP2Error_NoBody(t *testing.T) {
	sess, peer := newTestSession(t)
	defer func() { _ = sess.conn.Close() }()
	defer func() { _ = peer.Close() }()

	// Build an ACP2 GetObject request for an obj-id that's not in the
	// tree (slot 1 was initialised empty by newTestSession).
	getObj := &codec.ACP2Message{
		Type:  codec.ACP2TypeRequest,
		MTID:  42,
		Func:  codec.ACP2FuncGetObject,
		PID:   0,
		ObjID: 999,
		Idx:   0,
	}
	body, err := codec.EncodeACP2Message(getObj)
	if err != nil {
		t.Fatalf("encode acp2 request: %v", err)
	}
	req := &codec.AN2Frame{
		Proto:   codec.AN2ProtoACP2,
		Slot:    1,
		MTID:    0, // AN2 mtid for data frames
		Type:    codec.AN2TypeData,
		Payload: body,
	}
	rep := roundtrip(t, sess, peer, req)

	if rep.Proto != codec.AN2ProtoACP2 {
		t.Fatalf("reply proto=%v want ACP2", rep.Proto)
	}
	if rep.Type != codec.AN2TypeData {
		t.Fatalf("reply AN2 type=%v want data(4)", rep.Type)
	}
	if len(rep.Payload) != 4 {
		t.Fatalf("error reply payload len=%d want 4 (spec §Error: header only, no body); got %x",
			len(rep.Payload), rep.Payload)
	}
	if rep.Payload[0] != byte(codec.ACP2TypeError) {
		t.Errorf("byte 0 (type)=%d want %d (error)", rep.Payload[0], codec.ACP2TypeError)
	}
	if rep.Payload[1] != 42 {
		t.Errorf("byte 1 (mtid)=%d want 42 (matches request)", rep.Payload[1])
	}
	if rep.Payload[2] != byte(codec.ErrInvalidObjID) {
		t.Errorf("byte 2 (stat)=%d want %d (invalid_obj_id)", rep.Payload[2], codec.ErrInvalidObjID)
	}
	if rep.Payload[3] != 0 {
		t.Errorf("byte 3 (pid)=%d want 0", rep.Payload[3])
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
