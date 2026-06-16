package probelsw02p

import (
	"testing"

	"dhs/internal/probel-sw02p/codec"
)

// TestGoHandlerDecodeError confirms a short-payload rx 06 GO flows back
// as a decode error (ErrShortPayload) with no broadcast — §3.2.8 needs
// the 1-byte Operation field.
func TestGoHandlerDecodeError(t *testing.T) {
	srv := newTestServer(t)
	bad := codec.Frame{ID: codec.RxGo, Payload: nil}
	res, err := srv.dispatch(bad)
	if err == nil {
		t.Fatal("want ErrShortPayload; got nil")
	}
	if res.reply != nil || len(res.broadcast) != 0 {
		t.Errorf("got reply/broadcast on decode error; want none")
	}
}

// TestGoSetSkipsOutOfRangeSlot covers the applyConnectLenient==false
// branch in handleGo op=Set: a slot whose dst exceeds the declared
// targetCount (4) is silently skipped — no tx 04 for it, no
// SalvoEmittedConnected increment — while a valid slot in the same
// salvo still applies. Only the valid slot + the trailing tx 13 appear.
func TestGoSetSkipsOutOfRangeSlot(t *testing.T) {
	srv := newTestServer(t) // targetCount = sourceCount = 4

	// Stage one in-range slot (dst=1 src=2) and one out-of-range
	// (dst=999) directly on the pending buffer.
	srv.tree.appendPending(0, 0, pendingSlot{Destination: 1, Source: 2})
	srv.tree.appendPending(0, 0, pendingSlot{Destination: 999, Source: 2})

	before := srv.profile.Snapshot()[SalvoEmittedConnected]
	res, err := srv.dispatch(codec.EncodeGo(codec.GoParams{Operation: codec.GoOpSet}))
	if err != nil {
		t.Fatalf("dispatch rx 06: %v", err)
	}
	// 1 × tx 04 (only the valid slot) + 1 × tx 13.
	if len(res.broadcast) != 2 {
		t.Fatalf("broadcast len = %d; want 2 (1 tx04 + 1 tx13)", len(res.broadcast))
	}
	if res.broadcast[0].ID != codec.TxCrosspointConnected {
		t.Errorf("broadcast[0] ID = %#x; want TxCrosspointConnected", res.broadcast[0].ID)
	}
	cp, _ := codec.DecodeConnected(res.broadcast[0])
	if cp.Destination != 1 || cp.Source != 2 {
		t.Errorf("tx 04 = (%d,%d); want (1,2)", cp.Destination, cp.Source)
	}
	if res.broadcast[1].ID != codec.TxGoDoneAck {
		t.Errorf("broadcast[1] ID = %#x; want TxGoDoneAck", res.broadcast[1].ID)
	}
	if after := srv.profile.Snapshot()[SalvoEmittedConnected]; after != before+1 {
		t.Errorf("SalvoEmittedConnected %d -> %d; want +1 (skipped slot not counted)", before, after)
	}
	// Out-of-range slot never recorded.
	state := srv.tree.matrices[matrixKey{matrix: 0, level: 0}]
	if _, ok := state.sources[999]; ok {
		t.Errorf("tree[999] recorded; want absent (skipped)")
	}
}

// TestGoUnknownOperationAbsorbsAndAcks covers the third arm of
// handleGo: an Operation byte that is neither 0x00 (Set) nor 0x01
// (Clear). The handler absorbs it via HandlerDecodeFailed and still
// replies with a neutral tx 13 GO DONE ACK (op=Set) so the controller
// does not hang. No tree mutation, no tx 04.
func TestGoUnknownOperationAbsorbsAndAcks(t *testing.T) {
	srv := newTestServer(t)

	// Stage a slot first; the unknown-op path drains pending but must
	// NOT apply it.
	srv.tree.appendPending(0, 0, pendingSlot{Destination: 1, Source: 2})
	before := srv.profile.Snapshot()[HandlerDecodeFailed]

	// Operation = 0x02 (unknown). Build the frame directly — EncodeGo
	// would coerce only known ops; a raw 1-byte payload exercises the
	// unknown-op arm.
	res, err := srv.dispatch(codec.Frame{ID: codec.RxGo, Payload: []byte{0x02}})
	if err != nil {
		t.Fatalf("dispatch rx 06 unknown op: %v", err)
	}
	if len(res.broadcast) != 1 {
		t.Fatalf("broadcast len = %d; want 1 (neutral tx 13)", len(res.broadcast))
	}
	if res.broadcast[0].ID != codec.TxGoDoneAck {
		t.Errorf("broadcast[0] ID = %#x; want TxGoDoneAck", res.broadcast[0].ID)
	}
	ack, _ := codec.DecodeGoDoneAck(res.broadcast[0])
	if ack.Operation != codec.GoOpSet {
		t.Errorf("neutral ack op = %#x; want GoOpSet", ack.Operation)
	}
	if after := srv.profile.Snapshot()[HandlerDecodeFailed]; after != before+1 {
		t.Errorf("HandlerDecodeFailed %d -> %d; want +1", before, after)
	}
	// Pending was drained but never applied.
	state := srv.tree.matrices[matrixKey{matrix: 0, level: 0}]
	if _, ok := state.sources[1]; ok {
		t.Errorf("tree[1] applied on unknown-op path; want untouched")
	}
}

// TestGoGroupSalvoHandlerDecodeError confirms a short-payload rx 36
// flows back as a decode error — §3.2.37 needs 2 MESSAGE bytes.
func TestGoGroupSalvoHandlerDecodeError(t *testing.T) {
	srv := newTestServer(t)
	bad := codec.Frame{ID: codec.RxGoGroupSalvo, Payload: []byte{0x00}} // 1 byte
	res, err := srv.dispatch(bad)
	if err == nil {
		t.Fatal("want ErrShortPayload; got nil")
	}
	if res.reply != nil || len(res.broadcast) != 0 {
		t.Errorf("got reply/broadcast on decode error; want none")
	}
}

// TestGoGroupSalvoSetSkipsOutOfRangeSlot covers the
// applyConnectLenient==false skip inside handleGoGroupSalvo op=Set.
func TestGoGroupSalvoSetSkipsOutOfRangeSlot(t *testing.T) {
	srv := newTestServer(t)

	srv.tree.appendPendingGroup(0, 0, 3, pendingSlot{Destination: 1, Source: 2})
	srv.tree.appendPendingGroup(0, 0, 3, pendingSlot{Destination: 999, Source: 2})

	before := srv.profile.Snapshot()[SalvoEmittedConnected]
	res, err := srv.dispatch(codec.EncodeGoGroupSalvo(codec.GoGroupSalvoParams{
		Operation: codec.GoOpSet, SalvoID: 3,
	}))
	if err != nil {
		t.Fatalf("dispatch rx 36: %v", err)
	}
	// 1 × tx 04 + 1 × tx 38.
	if len(res.broadcast) != 2 {
		t.Fatalf("broadcast len = %d; want 2", len(res.broadcast))
	}
	ack, _ := codec.DecodeGoDoneGroupSalvoAck(res.broadcast[1])
	if ack.Result != codec.GoGroupResultSet || ack.SalvoID != 3 {
		t.Errorf("ack = %+v; want Result=Set SalvoID=3", ack)
	}
	if after := srv.profile.Snapshot()[SalvoEmittedConnected]; after != before+1 {
		t.Errorf("SalvoEmittedConnected %d -> %d; want +1", before, after)
	}
}

// TestGoGroupSalvoClearDrainsNonEmptyGroup covers the op=Clear arm with
// a populated SalvoID → Result=Cleared, no tx 04, no tree mutation.
func TestGoGroupSalvoClearDrainsNonEmptyGroup(t *testing.T) {
	srv := newTestServer(t)

	srv.tree.appendPendingGroup(0, 0, 4, pendingSlot{Destination: 1, Source: 2})
	before := srv.profile.Snapshot()[SalvoEmittedConnected]

	res, err := srv.dispatch(codec.EncodeGoGroupSalvo(codec.GoGroupSalvoParams{
		Operation: codec.GoOpClear, SalvoID: 4,
	}))
	if err != nil {
		t.Fatalf("dispatch rx 36 clear: %v", err)
	}
	if len(res.broadcast) != 1 {
		t.Fatalf("broadcast len = %d; want 1 (tx 38 only)", len(res.broadcast))
	}
	ack, _ := codec.DecodeGoDoneGroupSalvoAck(res.broadcast[0])
	if ack.Result != codec.GoGroupResultCleared || ack.SalvoID != 4 {
		t.Errorf("ack = %+v; want Result=Cleared SalvoID=4", ack)
	}
	// No tree mutation on clear.
	state := srv.tree.matrices[matrixKey{matrix: 0, level: 0}]
	if _, ok := state.sources[1]; ok {
		t.Errorf("clear path applied slot; want untouched")
	}
	if after := srv.profile.Snapshot()[SalvoEmittedConnected]; after != before {
		t.Errorf("SalvoEmittedConnected moved on clear: %d -> %d", before, after)
	}
}

// TestGoGroupSalvoClearEmptyGroupReportsEmpty covers the op=Clear arm
// when the SalvoID has no staged slots → Result=Empty.
func TestGoGroupSalvoClearEmptyGroupReportsEmpty(t *testing.T) {
	srv := newTestServer(t)
	res, err := srv.dispatch(codec.EncodeGoGroupSalvo(codec.GoGroupSalvoParams{
		Operation: codec.GoOpClear, SalvoID: 77,
	}))
	if err != nil {
		t.Fatalf("dispatch rx 36 clear empty: %v", err)
	}
	if len(res.broadcast) != 1 {
		t.Fatalf("broadcast len = %d; want 1", len(res.broadcast))
	}
	ack, _ := codec.DecodeGoDoneGroupSalvoAck(res.broadcast[0])
	if ack.Result != codec.GoGroupResultEmpty {
		t.Errorf("Result = %#x; want GoGroupResultEmpty", ack.Result)
	}
}

// TestGoGroupSalvoUnknownOperationAbsorbsAndAcks covers the third arm
// of handleGoGroupSalvo: an Operation byte that is neither Set nor
// Clear is absorbed (HandlerDecodeFailed) and answered with a neutral
// tx 38 (Result=Empty).
func TestGoGroupSalvoUnknownOperationAbsorbsAndAcks(t *testing.T) {
	srv := newTestServer(t)

	srv.tree.appendPendingGroup(0, 0, 5, pendingSlot{Destination: 1, Source: 2})
	before := srv.profile.Snapshot()[HandlerDecodeFailed]

	// Operation = 0x02 (unknown), SalvoID = 5. Build frame directly:
	// byte 0 = Operation, byte 1 = SalvoID (per §3.2.37 layout).
	res, err := srv.dispatch(codec.Frame{ID: codec.RxGoGroupSalvo, Payload: []byte{0x02, 0x05}})
	if err != nil {
		t.Fatalf("dispatch rx 36 unknown op: %v", err)
	}
	if len(res.broadcast) != 1 {
		t.Fatalf("broadcast len = %d; want 1 (neutral tx 38)", len(res.broadcast))
	}
	ack, _ := codec.DecodeGoDoneGroupSalvoAck(res.broadcast[0])
	if ack.Result != codec.GoGroupResultEmpty || ack.SalvoID != 5 {
		t.Errorf("neutral ack = %+v; want Result=Empty SalvoID=5", ack)
	}
	if after := srv.profile.Snapshot()[HandlerDecodeFailed]; after != before+1 {
		t.Errorf("HandlerDecodeFailed %d -> %d; want +1", before, after)
	}
}

// TestConnectOnGoGroupSalvoHandlerDecodeError confirms a short-payload
// rx 35 flows back as a decode error.
func TestConnectOnGoGroupSalvoHandlerDecodeError(t *testing.T) {
	srv := newTestServer(t)
	bad := codec.Frame{ID: codec.RxConnectOnGoGroupSalvo, Payload: []byte{0x00, 0x01, 0x02}} // 3 bytes, want 4
	res, err := srv.dispatch(bad)
	if err == nil {
		t.Fatal("want ErrShortPayload; got nil")
	}
	if res.reply != nil {
		t.Errorf("got reply on decode error; want none")
	}
}

// TestExtendedConnectOnGoGroupSalvoHandlerDecodeError confirms a
// short-payload rx 71 flows back as a decode error.
func TestExtendedConnectOnGoGroupSalvoHandlerDecodeError(t *testing.T) {
	srv := newTestServer(t)
	bad := codec.Frame{ID: codec.RxExtendedConnectOnGoGroupSalvo, Payload: []byte{0x00, 0x01, 0x02, 0x03}} // 4, want 5
	res, err := srv.dispatch(bad)
	if err == nil {
		t.Fatal("want ErrShortPayload; got nil")
	}
	if res.reply != nil {
		t.Errorf("got reply on decode error; want none")
	}
}

// TestExtendedConnectOnGoHandlerDecodeError confirms a short-payload
// rx 69 flows back as a decode error.
func TestExtendedConnectOnGoHandlerDecodeError(t *testing.T) {
	srv := newTestServer(t)
	bad := codec.Frame{ID: codec.RxExtendedConnectOnGo, Payload: []byte{0x00, 0x01, 0x02}} // 3, want 4
	res, err := srv.dispatch(bad)
	if err == nil {
		t.Fatal("want ErrShortPayload; got nil")
	}
	if res.reply != nil {
		t.Errorf("got reply on decode error; want none")
	}
}

// TestExtendedInterrogateHandlerDecodeError confirms a short-payload
// rx 65 flows back as a decode error.
func TestExtendedInterrogateHandlerDecodeError(t *testing.T) {
	srv := newTestServer(t)
	bad := codec.Frame{ID: codec.RxExtendedInterrogate, Payload: []byte{0x00}} // 1, want 2
	res, err := srv.dispatch(bad)
	if err == nil {
		t.Fatal("want ErrShortPayload; got nil")
	}
	if res.reply != nil {
		t.Errorf("got reply on decode error; want none")
	}
}

// TestExtendedConnectHandlerDecodeError confirms a short-payload rx 66
// flows back as a decode error.
func TestExtendedConnectHandlerDecodeError(t *testing.T) {
	srv := newTestServer(t)
	bad := codec.Frame{ID: codec.RxExtendedConnect, Payload: []byte{0x00, 0x01, 0x02}} // 3, want 4
	res, err := srv.dispatch(bad)
	if err == nil {
		t.Fatal("want ErrShortPayload; got nil")
	}
	if res.reply != nil || len(res.broadcast) != 0 {
		t.Errorf("got reply/broadcast on decode error; want none")
	}
}

// TestExtendedConnectSkipsOutOfRangeSlot covers the
// applyConnectLenient==false branch in handleExtendedConnect (rx 66):
// an unprotected dst outside the declared count is dropped with no
// tx 68 broadcast.
func TestExtendedConnectSkipsOutOfRangeSlot(t *testing.T) {
	srv := newTestServer(t) // targetCount = sourceCount = 4
	in := codec.EncodeExtendedConnect(codec.ExtendedConnectParams{Destination: 999, Source: 1})
	res, err := srv.dispatch(in)
	if err != nil {
		t.Fatalf("dispatch rx 66: %v", err)
	}
	if res.reply != nil || len(res.broadcast) != 0 {
		t.Errorf("out-of-range rx 66: reply=%+v broadcast=%d; want empty", res.reply, len(res.broadcast))
	}
	if _, ok := srv.tree.matrices[matrixKey{matrix: 0, level: 0}].sources[999]; ok {
		t.Errorf("tree[999] recorded; want absent")
	}
}

// TestExtendedProtectConnectHandlerDecodeError confirms a short-payload
// rx 102 flows back as a decode error.
func TestExtendedProtectConnectHandlerDecodeError(t *testing.T) {
	srv := newTestServer(t)
	bad := codec.Frame{ID: codec.RxExtendedProtectConnect, Payload: []byte{0x00, 0x01, 0x02}} // 3, want 4
	res, err := srv.dispatch(bad)
	if err == nil {
		t.Fatal("want ErrShortPayload; got nil")
	}
	if res.reply != nil || len(res.broadcast) != 0 {
		t.Errorf("got reply/broadcast on decode error; want none")
	}
}

// TestDualControllerStatusRequestWrongCommandGuard exercises the
// decode-error arm of handleDualControllerStatusRequest. The §3.2.45
// request carries a zero-length MESSAGE, so the decoder has no
// short-payload arm — the only error it can return is ErrWrongCommand
// (mismatched ID). dispatch() never routes a wrong ID here, so the
// guard is reached by calling the handler directly with a mismatched
// frame, the same way a future direct caller would.
func TestDualControllerStatusRequestWrongCommandGuard(t *testing.T) {
	srv := newTestServer(t)
	res, err := srv.handleDualControllerStatusRequest(codec.Frame{ID: codec.RxInterrogate})
	if err == nil {
		t.Fatal("want ErrWrongCommand; got nil")
	}
	if res.reply != nil {
		t.Errorf("got reply on decode error; want none")
	}
}

// TestRouterConfigRequestWrongCommandGuard exercises the decode-error
// arm of handleRouterConfigRequest. Like rx 050, §3.2.57 carries a
// zero-length MESSAGE so the only decoder error is ErrWrongCommand;
// reach it via a direct handler call with a mismatched ID.
func TestRouterConfigRequestWrongCommandGuard(t *testing.T) {
	srv := newTestServer(t)
	res, err := srv.handleRouterConfigRequest(codec.Frame{ID: codec.RxInterrogate})
	if err == nil {
		t.Fatal("want ErrWrongCommand; got nil")
	}
	if res.reply != nil {
		t.Errorf("got reply on decode error; want none")
	}
}

// TestProtectDeviceNameRequestResolvesOwnerName covers the
// protectEntriesByDevice name-resolution branch in
// handleProtectDeviceNameRequest: when a protect entry for the queried
// (non-self) Device Number carries a non-empty OwnerName, the tx 099
// reply echoes that name rather than the empty fallback.
func TestProtectDeviceNameRequestResolvesOwnerName(t *testing.T) {
	srv := newTestServer(t)

	// Seed a protect entry owned by Device 42 with a resolved name.
	srv.tree.mu.Lock()
	st := srv.tree.matrices[matrixKey{matrix: 0, level: 0}]
	if st.protect == nil {
		st.protect = map[uint16]protectEntry{}
	}
	st.protect[1] = protectEntry{
		State:       codec.ProtectProBel,
		OwnerDevice: 42,
		OwnerName:   "VSM-CTRL",
	}
	srv.tree.mu.Unlock()

	// Query Device 42 (not the server's self device number).
	in := codec.EncodeProtectDeviceNameRequest(codec.ProtectDeviceNameRequestParams{Device: 42})
	res, err := srv.dispatch(in)
	if err != nil {
		t.Fatalf("dispatch rx 103: %v", err)
	}
	if res.reply == nil || res.reply.ID != codec.TxProtectDeviceNameResponse {
		t.Fatalf("reply missing / wrong ID: %+v", res.reply)
	}
	out, err := codec.DecodeProtectDeviceNameResponse(*res.reply)
	if err != nil {
		t.Fatalf("decode tx 099: %v", err)
	}
	if out.Device != 42 {
		t.Errorf("tx 099 Device = %d; want 42", out.Device)
	}
	if got := trimName(out.Name); got != "VSM-CTRL" {
		t.Errorf("tx 099 Name = %q; want %q", got, "VSM-CTRL")
	}
}

// trimName strips the space-padding the §3.2.63 encoder applies to the
// fixed 8-char name field.
func trimName(s string) string {
	end := len(s)
	for end > 0 && s[end-1] == ' ' {
		end--
	}
	return s[:end]
}
