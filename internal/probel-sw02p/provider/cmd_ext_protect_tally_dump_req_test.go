package probelsw02p

import (
	"testing"

	"acp/internal/probel-sw02p/codec"
)

// TestTallyDumpRequestEmptyReturnsZero confirms a bare-state server
// replies with a single tx 100 carrying Count=0 entries.
func TestTallyDumpRequestEmptyReturnsZero(t *testing.T) {
	srv := newBareTestServer(t)

	res, err := srv.dispatch(codec.EncodeExtendedProtectTallyDumpRequest(codec.ExtendedProtectTallyDumpRequestParams{Count: 5, StartDestination: 0}))
	if err != nil {
		t.Fatalf("dispatch rx 105: %v", err)
	}
	if len(res.broadcast) != 1 || res.broadcast[0].ID != codec.TxExtendedProtectTallyDump {
		t.Fatalf("broadcast = %+v; want 1 x tx 100", res.broadcast)
	}
	dump, _ := codec.DecodeExtendedProtectTallyDump(res.broadcast[0])
	if dump.Reset || len(dump.Entries) != 0 {
		t.Errorf("dump = %+v; want empty", dump)
	}
}

// TestTallyDumpRequestDrainsStoredEntries seeds 3 protect entries via
// the rx 102 wire path and confirms rx 105 reports them in
// ascending-destination order in a single tx 100.
func TestTallyDumpRequestDrainsStoredEntries(t *testing.T) {
	srv := newBareTestServer(t)

	// Seed via rx 102 wire path — exercises authority ladder + real
	// storage rather than direct tree mutation.
	for _, seed := range []struct{ dst, device uint16 }{
		{10, 100}, {5, 200}, {15, 300},
	} {
		if _, err := srv.dispatch(codec.EncodeExtendedProtectConnect(codec.ExtendedProtectConnectParams{
			Destination: seed.dst, Device: seed.device,
		})); err != nil {
			t.Fatalf("seed rx 102 (dst=%d): %v", seed.dst, err)
		}
	}

	// Dump starting at 0 with count=10 (more than we have).
	res, err := srv.dispatch(codec.EncodeExtendedProtectTallyDumpRequest(codec.ExtendedProtectTallyDumpRequestParams{Count: 10, StartDestination: 0}))
	if err != nil {
		t.Fatalf("dispatch rx 105: %v", err)
	}
	if len(res.broadcast) != 1 {
		t.Fatalf("broadcast len = %d; want 1", len(res.broadcast))
	}
	dump, err := codec.DecodeExtendedProtectTallyDump(res.broadcast[0])
	if err != nil {
		t.Fatalf("decode tx 100: %v", err)
	}
	if len(dump.Entries) != 3 {
		t.Fatalf("Entries len = %d; want 3", len(dump.Entries))
	}
	// Ascending destination order: 5, 10, 15.
	wants := []struct {
		dst    uint16
		device uint16
	}{{5, 200}, {10, 100}, {15, 300}}
	for i, w := range wants {
		if dump.Entries[i].Destination != w.dst || dump.Entries[i].Device != w.device {
			t.Errorf("entry %d = %+v; want dst=%d device=%d", i, dump.Entries[i], w.dst, w.device)
		}
		if dump.Entries[i].Protect != codec.ProtectProBel {
			t.Errorf("entry %d.Protect = %d; want ProtectProBel", i, dump.Entries[i].Protect)
		}
	}
}

// TestTallyDumpRequestRespectsStartDestination filters the dump by
// the StartDestination field.
func TestTallyDumpRequestRespectsStartDestination(t *testing.T) {
	srv := newBareTestServer(t)

	for _, seed := range []struct{ dst, device uint16 }{{1, 100}, {10, 200}, {100, 300}} {
		_, _ = srv.dispatch(codec.EncodeExtendedProtectConnect(codec.ExtendedProtectConnectParams{
			Destination: seed.dst, Device: seed.device,
		}))
	}

	// StartDestination=10 should skip dst=1 and return dst=10, 100.
	res, _ := srv.dispatch(codec.EncodeExtendedProtectTallyDumpRequest(codec.ExtendedProtectTallyDumpRequestParams{
		Count: 10, StartDestination: 10,
	}))
	dump, _ := codec.DecodeExtendedProtectTallyDump(res.broadcast[0])
	if len(dump.Entries) != 2 {
		t.Fatalf("Entries len = %d; want 2", len(dump.Entries))
	}
	if dump.Entries[0].Destination != 10 || dump.Entries[1].Destination != 100 {
		t.Errorf("dst order = [%d, %d]; want [10, 100]", dump.Entries[0].Destination, dump.Entries[1].Destination)
	}
}

// TestTallyDumpRequestSplitsLargeCount seeds > 32 entries and
// confirms the handler splits into multiple tx 100 broadcasts per
// §3.2.64's 32-entry cap.
func TestTallyDumpRequestSplitsLargeCount(t *testing.T) {
	srv := newBareTestServer(t)

	// Seed 50 entries.
	for i := uint16(0); i < 50; i++ {
		_, _ = srv.dispatch(codec.EncodeExtendedProtectConnect(codec.ExtendedProtectConnectParams{
			Destination: i, Device: 100 + i,
		}))
	}

	res, _ := srv.dispatch(codec.EncodeExtendedProtectTallyDumpRequest(codec.ExtendedProtectTallyDumpRequestParams{
		Count: 50, StartDestination: 0,
	}))
	// 50 entries / 32 per chunk = 2 frames (32 + 18).
	if len(res.broadcast) != 2 {
		t.Fatalf("broadcast len = %d; want 2 chunks (50 entries / 32 cap)", len(res.broadcast))
	}
	first, _ := codec.DecodeExtendedProtectTallyDump(res.broadcast[0])
	second, _ := codec.DecodeExtendedProtectTallyDump(res.broadcast[1])
	if len(first.Entries) != codec.ExtendedProtectTallyDumpMaxCount || len(second.Entries) != 18 {
		t.Errorf("chunk sizes = %d + %d; want %d + 18", len(first.Entries), len(second.Entries), codec.ExtendedProtectTallyDumpMaxCount)
	}
}

// TestTallyDumpRequestHandlerDecodeError verifies guards.
func TestTallyDumpRequestHandlerDecodeError(t *testing.T) {
	srv := newBareTestServer(t)
	bad := codec.Frame{ID: codec.RxExtendedProtectTallyDumpRequest, Payload: []byte{0x00, 0x01}}
	_, err := srv.dispatch(bad)
	if err == nil {
		t.Fatal("want ErrShortPayload; got nil")
	}
}
