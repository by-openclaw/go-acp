package probelsw02p

import (
	"testing"

	"acp/internal/probel-sw02p/codec"
)

// TestExtendedProtectConnectAcceptsNoneAndBroadcasts locks in the
// baseline case — a PROTECT CONNECT on an unprotected destination
// accepts, records owner + state=ProtectProBel, and broadcasts
// tx 097 fan-out with the new state.
func TestExtendedProtectConnectAcceptsNoneAndBroadcasts(t *testing.T) {
	srv := newBareTestServer(t)

	in := codec.EncodeExtendedProtectConnect(codec.ExtendedProtectConnectParams{
		Destination: 42, Device: 7,
	})
	res, err := srv.dispatch(in)
	if err != nil {
		t.Fatalf("dispatch rx 102: %v", err)
	}
	if res.reply != nil {
		t.Errorf("rx 102 returned point-to-point reply; §3.2.61 requires broadcast")
	}
	if len(res.broadcast) != 1 || res.broadcast[0].ID != codec.TxExtendedProtectConnected {
		t.Fatalf("broadcast = %+v; want 1 x tx 097", res.broadcast)
	}
	p, err := codec.DecodeExtendedProtectConnected(res.broadcast[0])
	if err != nil {
		t.Fatalf("decode tx 097: %v", err)
	}
	if p.Protect != codec.ProtectProBel || p.Destination != 42 || p.Device != 7 {
		t.Errorf("tx 097 = %+v; want ProtectProBel dst=42 device=7", p)
	}

	// Tree records the entry.
	entry, ok := srv.tree.protectLookup(42)
	if !ok || entry.State != codec.ProtectProBel || entry.OwnerDevice != 7 {
		t.Errorf("protect[42] = (%+v, ok=%v); want ProtectProBel owner=7", entry, ok)
	}
}

// TestExtendedProtectConnectOwnerCanReapply verifies the owner can
// re-issue rx 102 on an already-protected destination with no side
// effects beyond re-broadcasting tx 097. Fires no unauthorized event.
func TestExtendedProtectConnectOwnerCanReapply(t *testing.T) {
	srv := newBareTestServer(t)

	if _, err := srv.dispatch(codec.EncodeExtendedProtectConnect(codec.ExtendedProtectConnectParams{
		Destination: 42, Device: 7,
	})); err != nil {
		t.Fatalf("initial: %v", err)
	}
	before := srv.profile.Snapshot()[ProtectUnauthorized]

	res, err := srv.dispatch(codec.EncodeExtendedProtectConnect(codec.ExtendedProtectConnectParams{
		Destination: 42, Device: 7,
	}))
	if err != nil {
		t.Fatalf("reapply: %v", err)
	}
	p, _ := codec.DecodeExtendedProtectConnected(res.broadcast[0])
	if p.Protect != codec.ProtectProBel || p.Device != 7 {
		t.Errorf("reapply tx 097 = %+v; want ProtectProBel owner=7", p)
	}
	if after := srv.profile.Snapshot()[ProtectUnauthorized]; after != before {
		t.Errorf("ProtectUnauthorized moved on owner reapply: %d → %d", before, after)
	}
}

// TestExtendedProtectConnectRejectsDifferentOwner verifies the core
// owner-only rule: device != stored owner → reject + unauthorized
// event + tx 097 echoes the unchanged original state and owner.
func TestExtendedProtectConnectRejectsDifferentOwner(t *testing.T) {
	srv := newBareTestServer(t)

	// Device 7 protects dst=42.
	_, _ = srv.dispatch(codec.EncodeExtendedProtectConnect(codec.ExtendedProtectConnectParams{
		Destination: 42, Device: 7,
	}))
	before := srv.profile.Snapshot()[ProtectUnauthorized]

	// Device 99 attempts to change the same dst — should be rejected.
	res, err := srv.dispatch(codec.EncodeExtendedProtectConnect(codec.ExtendedProtectConnectParams{
		Destination: 42, Device: 99,
	}))
	if err != nil {
		t.Fatalf("dispatch: %v", err)
	}
	p, _ := codec.DecodeExtendedProtectConnected(res.broadcast[0])
	if p.Protect != codec.ProtectProBel || p.Device != 7 {
		t.Errorf("reject tx 097 = %+v; want unchanged ProtectProBel owner=7", p)
	}
	if after := srv.profile.Snapshot()[ProtectUnauthorized]; after != before+1 {
		t.Errorf("ProtectUnauthorized = %d; want %d (one reject fired)", after, before+1)
	}

	// Tree still owns the original entry.
	entry, _ := srv.tree.protectLookup(42)
	if entry.OwnerDevice != 7 {
		t.Errorf("protect[42].OwnerDevice = %d; want 7 (unchanged)", entry.OwnerDevice)
	}
}

// TestExtendedProtectConnectRejectsProbelOverride locks in the
// ProbelOverride hard-block: once a destination is in state=2 per
// §3.2.60 "Cannot be altered remotely", no rx 102 can change it
// (fires ProtectOverrideImmutable).
func TestExtendedProtectConnectRejectsProbelOverride(t *testing.T) {
	srv := newBareTestServer(t)

	// Seed ProbelOverride directly — there is no Rx command that
	// sets ProbelOverride; it's a local-admin / physical-console
	// state. This test proxies that via direct tree mutation.
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
	// Even the original "owner" can't unlock or change ProbelOverride
	// from rx 102 — the state is §3.2.60 immutable remotely.
	res, err := srv.dispatch(codec.EncodeExtendedProtectConnect(codec.ExtendedProtectConnectParams{
		Destination: 42, Device: 5,
	}))
	if err != nil {
		t.Fatalf("dispatch: %v", err)
	}
	p, _ := codec.DecodeExtendedProtectConnected(res.broadcast[0])
	if p.Protect != codec.ProtectProBelOverride || p.Device != 5 {
		t.Errorf("tx 097 = %+v; want unchanged ProbelOverride owner=5", p)
	}
	if after := srv.profile.Snapshot()[ProtectOverrideImmutable]; after != before+1 {
		t.Errorf("ProtectOverrideImmutable = %d; want %d", after, before+1)
	}
}
