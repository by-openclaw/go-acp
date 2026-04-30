package probelsw02p

import (
	"testing"

	"acp/internal/probel-sw02p/codec"
)

// TestExtendedProtectInterrogateNoEntryReportsNone locks in the
// empty-state path: rx 101 on an unprotected destination returns
// tx 096 with State=None + Device=0.
func TestExtendedProtectInterrogateNoEntryReportsNone(t *testing.T) {
	srv := newBareTestServer(t)

	in := codec.EncodeExtendedProtectInterrogate(codec.ExtendedProtectInterrogateParams{Destination: 42})
	res, err := srv.dispatch(in)
	if err != nil {
		t.Fatalf("dispatch rx 101: %v", err)
	}
	if res.reply == nil || res.reply.ID != codec.TxExtendedProtectTally {
		t.Fatalf("reply missing / wrong ID: %+v", res.reply)
	}
	tally, err := codec.DecodeExtendedProtectTally(*res.reply)
	if err != nil {
		t.Fatalf("decode tx 096: %v", err)
	}
	if tally.Protect != codec.ProtectNone || tally.Destination != 42 || tally.Device != 0 {
		t.Errorf("tally = %+v; want ProtectNone dst=42 device=0", tally)
	}
}

// TestExtendedProtectInterrogateReportsStoredEntry seeds a protect
// entry directly in the tree and confirms rx 101 round-trips it.
func TestExtendedProtectInterrogateReportsStoredEntry(t *testing.T) {
	srv := newBareTestServer(t)

	// Seed via direct tree mutation (no rx 102 handler yet at this
	// commit boundary — will swap to wire-level seed in the next one).
	srv.tree.mu.Lock()
	key := matrixKey{matrix: 0, level: 0}
	st, ok := srv.tree.matrices[key]
	if !ok {
		st = &matrixState{sources: map[uint16]uint16{}}
		srv.tree.matrices[key] = st
	}
	if st.protect == nil {
		st.protect = map[uint16]protectEntry{}
	}
	st.protect[100] = protectEntry{
		State:       codec.ProtectOEM,
		OwnerDevice: 7,
	}
	srv.tree.mu.Unlock()

	res, err := srv.dispatch(codec.EncodeExtendedProtectInterrogate(codec.ExtendedProtectInterrogateParams{Destination: 100}))
	if err != nil {
		t.Fatalf("dispatch: %v", err)
	}
	tally, _ := codec.DecodeExtendedProtectTally(*res.reply)
	if tally.Protect != codec.ProtectOEM || tally.Destination != 100 || tally.Device != 7 {
		t.Errorf("tally = %+v; want ProtectOEM dst=100 device=7", tally)
	}
}

// TestExtendedProtectInterrogateHandlerDecodeError verifies guards.
func TestExtendedProtectInterrogateHandlerDecodeError(t *testing.T) {
	srv := newBareTestServer(t)
	bad := codec.Frame{ID: codec.RxExtendedProtectInterrogate, Payload: []byte{0x00}}
	_, err := srv.dispatch(bad)
	if err == nil {
		t.Fatal("want ErrShortPayload; got nil")
	}
}
