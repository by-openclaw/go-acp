package probelsw08p

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	"acp/internal/probel-sw08p/codec"
	probelproto "acp/internal/probel-sw08p/consumer"
)

// TestMaintenanceClearProtectsUnit exercises the rx 007 ClearProtects
// path directly on the dispatcher. After seeding a fake protect on
// (0, 0, 3) the handler must wipe it and the tree must reflect that.
func TestMaintenanceClearProtectsUnit(t *testing.T) {
	srv := &server{
		logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
		tree:   &tree{matrices: map[matrixKey]*matrixState{}},
	}
	st := &matrixState{
		targetCount: 4, sourceCount: 4,
		sources:  map[uint16]uint16{},
		protects: map[uint16]protectRecord{3: {deviceID: 42, state: 1}},
	}
	srv.tree.matrices[matrixKey{matrix: 0, level: 0}] = st

	req := codec.EncodeMaintenance(codec.MaintenanceParams{
		Function: codec.MaintClearProtects, MatrixID: 0, LevelID: 0,
	})
	if _, err := srv.handle(req); err != nil {
		t.Fatalf("handle: %v", err)
	}
	if len(st.protects) != 0 {
		t.Errorf("protects not cleared: %+v", st.protects)
	}

	// All-matrices wildcard also clears.
	st.protects[1] = protectRecord{deviceID: 7, state: 1}
	req2 := codec.EncodeMaintenance(codec.MaintenanceParams{
		Function: codec.MaintClearProtects, MatrixID: 0xFF, LevelID: 0xFF,
	})
	if _, err := srv.handle(req2); err != nil {
		t.Fatalf("handle wildcard: %v", err)
	}
	if len(st.protects) != 0 {
		t.Errorf("wildcard did not clear protects: %+v", st.protects)
	}
}

// TestDualControllerStatusLoopback — consumer dials, queries, receives
// the canned single-controller reply (MASTER + Active + !IdleFaulty).
func TestDualControllerStatusLoopback(t *testing.T) {
	exp := demoMatrixExport(4, 4)
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	srv := newServer(logger, exp)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = srv.Serve(ctx, "127.0.0.1:0") }()
	addr := waitBound(t, srv)
	host, port := splitAddr(t, addr)

	f := &probelproto.Factory{}
	plugin := f.New(logger).(*probelproto.Plugin)
	dc, cancelDC := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancelDC()
	if err := plugin.Connect(dc, host, port); err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer func() { _ = plugin.Disconnect() }()

	r, err := plugin.DualControllerStatus(dc)
	if err != nil {
		t.Fatalf("DualControllerStatus: %v", err)
	}
	if r.SlaveActive {
		t.Error("got SLAVE active; want MASTER")
	}
	if !r.Active {
		t.Error("got !Active; want Active")
	}
	if r.IdleControllerFaulty {
		t.Error("got IdleFaulty; want !IdleFaulty")
	}
}
