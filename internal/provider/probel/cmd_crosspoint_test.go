package probel

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"testing"
	"time"

	iprobel "acp/internal/probel"
	"acp/internal/export/canonical"
	probelproto "acp/internal/protocol/probel"
)

// TestCrosspointInterrogateLoopback exercises the full rx 001 → tx 003
// path end-to-end: consumer dials provider, sends an interrogate, and
// verifies the tally reply reports the expected source.
func TestCrosspointInterrogateLoopback(t *testing.T) {
	// 1. Build a demo tree and seed one crosspoint via the API path.
	exp := demoMatrixExport(16, 16)
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	srv := newServer(logger, exp)

	// Seed: matrix=0 level=0 dst=2 → src=7
	if _, err := srv.SetValue(context.Background(), "0.0.2", 7); err != nil {
		t.Fatalf("seed: %v", err)
	}

	// 2. Start serving on a random port.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	serveDone := make(chan error, 1)
	go func() { serveDone <- srv.Serve(ctx, "127.0.0.1:0") }()
	addr := waitBound(t, srv)

	// 3. Connect via the consumer plugin.
	host, port := splitAddr(t, addr)
	f := &probelproto.Factory{}
	plugin := f.New(logger).(*probelproto.Plugin)
	dialCtx, cancelDial := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancelDial()
	if err := plugin.Connect(dialCtx, host, port); err != nil {
		t.Fatalf("plugin.Connect: %v", err)
	}
	defer func() { _ = plugin.Disconnect() }()

	// 4. Interrogate and verify.
	callCtx, cancelCall := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancelCall()
	reply, err := plugin.CrosspointInterrogate(callCtx, 0, 0, 2)
	if err != nil {
		t.Fatalf("CrosspointInterrogate: %v", err)
	}
	if reply.MatrixID != 0 || reply.LevelID != 0 ||
		reply.DestinationID != 2 || reply.SourceID != 7 {
		t.Errorf("tally = %+v; want matrix=0 level=0 dst=2 src=7", reply)
	}

	// 5. Unrouted destination returns SourceID=0.
	reply2, err := plugin.CrosspointInterrogate(callCtx, 0, 0, 3)
	if err != nil {
		t.Fatalf("CrosspointInterrogate unrouted: %v", err)
	}
	if reply2.DestinationID != 3 || reply2.SourceID != 0 {
		t.Errorf("unrouted tally = %+v; want dst=3 src=0", reply2)
	}

	cancel()
	select {
	case <-serveDone:
	case <-time.After(2 * time.Second):
		t.Fatal("Serve did not return after cancel")
	}
}

// TestHandleCrosspointInterrogateUnit — pure-function test of the
// dispatcher for fast feedback without sockets. Covers general form and
// ensures the tree.currentSource fallback (unknown matrix) returns a
// tally with SourceID=0 rather than error.
func TestHandleCrosspointInterrogateUnit(t *testing.T) {
	srv := &server{
		logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
		tree:   &tree{matrices: map[matrixKey]*matrixState{}},
	}
	// Populate one (matrix, level).
	st := &matrixState{
		targetCount: 4, sourceCount: 4,
		sources: []int16{-1, -1, 9, -1},
	}
	srv.tree.matrices[matrixKey{matrix: 0, level: 0}] = st

	req := iprobel.EncodeCrosspointInterrogate(iprobel.CrosspointInterrogateParams{
		MatrixID: 0, LevelID: 0, DestinationID: 2,
	})
	res, err := srv.handle(req)
	if err != nil {
		t.Fatalf("handle: %v", err)
	}
	if res.reply == nil {
		t.Fatal("reply is nil")
	}
	tally, err := iprobel.DecodeCrosspointTally(*res.reply)
	if err != nil {
		t.Fatalf("decode tally: %v", err)
	}
	if tally.SourceID != 9 {
		t.Errorf("src = %d; want 9", tally.SourceID)
	}

	// Unknown matrix → tally with SourceID=0 (no error).
	req2 := iprobel.EncodeCrosspointInterrogate(iprobel.CrosspointInterrogateParams{
		MatrixID: 7, LevelID: 0, DestinationID: 2,
	})
	res2, err := srv.handle(req2)
	if err != nil {
		t.Fatalf("handle unknown: %v", err)
	}
	if res2.reply == nil {
		t.Fatal("reply nil for unknown matrix")
	}
	tally2, _ := iprobel.DecodeCrosspointTally(*res2.reply)
	if tally2.SourceID != 0 {
		t.Errorf("unknown-matrix src = %d; want 0", tally2.SourceID)
	}
}

// --- test helpers ---------------------------------------------------------

func demoMatrixExport(targets, sources int) *canonical.Export {
	return &canonical.Export{
		Root: &canonical.Node{
			Header: canonical.Header{
				Number: 1, Identifier: "router", OID: "1",
				Children: []canonical.Element{
					&canonical.Matrix{
						Header: canonical.Header{
							Number: 1, Identifier: "matrix-0", OID: "1.1",
						},
						Type: canonical.MatrixOneToN, Mode: canonical.ModeLinear,
						TargetCount: int64(targets), SourceCount: int64(sources),
						Labels: []canonical.MatrixLabel{
							{BasePath: "router.matrix-0.video"},
						},
					},
				},
			},
		},
	}
}

func waitBound(t *testing.T, srv *server) string {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		srv.mu.Lock()
		if srv.listener != nil {
			addr := srv.listener.Addr().String()
			srv.mu.Unlock()
			return addr
		}
		srv.mu.Unlock()
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("server never bound")
	return ""
}

func splitAddr(t *testing.T, addr string) (string, int) {
	t.Helper()
	for i := len(addr) - 1; i >= 0; i-- {
		if addr[i] == ':' {
			var p int
			if _, err := fmt.Sscanf(addr[i+1:], "%d", &p); err != nil {
				t.Fatalf("parse port in %q: %v", addr, err)
			}
			return addr[:i], p
		}
	}
	t.Fatalf("bad addr %q", addr)
	return "", 0
}
