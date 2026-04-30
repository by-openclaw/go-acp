package probelsw02p

import (
	"testing"

	"acp/internal/probel-sw02p/codec"
)

// TestExtendedProtectDisconnectNoOpReturnsNone locks in the no-op
// path — rx 104 on an unprotected destination broadcasts tx 098
// with State=None and does not fire any compliance event.
func TestExtendedProtectDisconnectNoOpReturnsNone(t *testing.T) {
	srv := newBareTestServer(t)

	res, err := srv.dispatch(codec.EncodeExtendedProtectDisconnect(codec.ExtendedProtectDisconnectParams{
		Destination: 42, Device: 7,
	}))
	if err != nil {
		t.Fatalf("dispatch rx 104: %v", err)
	}
	if len(res.broadcast) != 1 || res.broadcast[0].ID != codec.TxExtendedProtectDisconnected {
		t.Fatalf("broadcast = %+v; want 1 x tx 098", res.broadcast)
	}
	d, _ := codec.DecodeExtendedProtectDisconnected(res.broadcast[0])
	if d.Protect != codec.ProtectNone || d.Destination != 42 {
		t.Errorf("tx 098 = %+v; want State=None dst=42", d)
	}
}

// TestExtendedProtectDisconnectOwnerCanUnlock seeds an entry via
// rx 102, then the same device clears it via rx 104 — expects
// clean unlock (tx 098 with State=None, tree entry gone, no
// compliance events).
func TestExtendedProtectDisconnectOwnerCanUnlock(t *testing.T) {
	srv := newBareTestServer(t)

	if _, err := srv.dispatch(codec.EncodeExtendedProtectConnect(codec.ExtendedProtectConnectParams{
		Destination: 42, Device: 7,
	})); err != nil {
		t.Fatalf("setup rx 102: %v", err)
	}
	beforeUnauth := srv.profile.Snapshot()[ProtectUnauthorized]
	beforeOverride := srv.profile.Snapshot()[ProtectOverrideImmutable]

	res, err := srv.dispatch(codec.EncodeExtendedProtectDisconnect(codec.ExtendedProtectDisconnectParams{
		Destination: 42, Device: 7,
	}))
	if err != nil {
		t.Fatalf("rx 104: %v", err)
	}
	d, _ := codec.DecodeExtendedProtectDisconnected(res.broadcast[0])
	if d.Protect != codec.ProtectNone {
		t.Errorf("tx 098.Protect = %d; want ProtectNone after owner unlock", d.Protect)
	}

	if _, ok := srv.tree.protectLookup(42); ok {
		t.Error("protect[42] still present after owner unlock")
	}
	if srv.profile.Snapshot()[ProtectUnauthorized] != beforeUnauth {
		t.Error("ProtectUnauthorized fired on authorized unlock")
	}
	if srv.profile.Snapshot()[ProtectOverrideImmutable] != beforeOverride {
		t.Error("ProtectOverrideImmutable fired on authorized unlock")
	}
}

// TestExtendedProtectDisconnectRejectsDifferentOwner locks in the
// core "only the original locker can unlock" rule — any other
// device clearing is rejected + fires ProtectUnauthorized, tree
// entry stays intact, tx 098 echoes unchanged state.
func TestExtendedProtectDisconnectRejectsDifferentOwner(t *testing.T) {
	srv := newBareTestServer(t)

	// Device 7 protects dst=42.
	_, _ = srv.dispatch(codec.EncodeExtendedProtectConnect(codec.ExtendedProtectConnectParams{
		Destination: 42, Device: 7,
	}))
	before := srv.profile.Snapshot()[ProtectUnauthorized]

	// Device 99 tries to unlock — should be rejected.
	res, err := srv.dispatch(codec.EncodeExtendedProtectDisconnect(codec.ExtendedProtectDisconnectParams{
		Destination: 42, Device: 99,
	}))
	if err != nil {
		t.Fatalf("rx 104: %v", err)
	}
	d, _ := codec.DecodeExtendedProtectDisconnected(res.broadcast[0])
	if d.Protect != codec.ProtectProBel || d.Device != 7 {
		t.Errorf("tx 098 = %+v; want unchanged ProtectProBel owner=7", d)
	}
	if after := srv.profile.Snapshot()[ProtectUnauthorized]; after != before+1 {
		t.Errorf("ProtectUnauthorized = %d; want %d (one reject fired)", after, before+1)
	}

	// Entry survives.
	entry, ok := srv.tree.protectLookup(42)
	if !ok || entry.OwnerDevice != 7 {
		t.Errorf("protect[42] = %+v ok=%v; want unchanged owner=7", entry, ok)
	}
}

// TestExtendedProtectDisconnectRejectsProbelOverride locks the
// ProbelOverride hard-block — even the "owner" can't unlock a
// ProbelOverride via rx 104 (fires ProtectOverrideImmutable).
func TestExtendedProtectDisconnectRejectsProbelOverride(t *testing.T) {
	srv := newBareTestServer(t)

	// Seed ProbelOverride directly (no Rx path sets it).
	srv.tree.mu.Lock()
	st, ok := srv.tree.matrices[matrixKey{matrix: 0, level: 0}]
	if !ok {
		st = &matrixState{sources: map[uint16]uint16{}}
		srv.tree.matrices[matrixKey{matrix: 0, level: 0}] = st
	}
	if st.protect == nil {
		st.protect = map[uint16]protectEntry{}
	}
	st.protect[42] = protectEntry{State: codec.ProtectProBelOverride, OwnerDevice: 5}
	srv.tree.mu.Unlock()

	before := srv.profile.Snapshot()[ProtectOverrideImmutable]
	res, err := srv.dispatch(codec.EncodeExtendedProtectDisconnect(codec.ExtendedProtectDisconnectParams{
		Destination: 42, Device: 5,
	}))
	if err != nil {
		t.Fatalf("rx 104: %v", err)
	}
	d, _ := codec.DecodeExtendedProtectDisconnected(res.broadcast[0])
	if d.Protect != codec.ProtectProBelOverride {
		t.Errorf("tx 098.Protect = %d; want unchanged ProbelOverride", d.Protect)
	}
	if after := srv.profile.Snapshot()[ProtectOverrideImmutable]; after != before+1 {
		t.Errorf("ProtectOverrideImmutable = %d; want %d", after, before+1)
	}

	// Entry survives ProbelOverride even when requester is the owner.
	entry, _ := srv.tree.protectLookup(42)
	if entry.State != codec.ProtectProBelOverride {
		t.Errorf("protect[42].State = %d; want ProbelOverride (remotely immutable)", entry.State)
	}
}

// TestExtendedProtectDisconnectHandlerDecodeError verifies guards.
func TestExtendedProtectDisconnectHandlerDecodeError(t *testing.T) {
	srv := newBareTestServer(t)
	bad := codec.Frame{ID: codec.RxExtendedProtectDisconnect, Payload: []byte{0x00, 0x01, 0x02}}
	_, err := srv.dispatch(bad)
	if err == nil {
		t.Fatal("want ErrShortPayload; got nil")
	}
}
