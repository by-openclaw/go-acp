package probel

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	"acp/internal/protocol/probel/codec"
	"acp/internal/export/canonical"
)

// TestTreeParsesDemoMatrix loads the demo fixture and verifies both
// levels register in the tree with the expected counts and labels.
func TestTreeParsesDemoMatrix(t *testing.T) {
	exp := &canonical.Export{
		Root: &canonical.Node{
			Header: canonical.Header{
				Number: 1, Identifier: "router", OID: "1",
				Children: []canonical.Element{
					&canonical.Matrix{
						Header: canonical.Header{
							Number: 1, Identifier: "matrix-0", OID: "1.1",
						},
						Type: canonical.MatrixOneToN, Mode: canonical.ModeLinear,
						TargetCount: 4, SourceCount: 4,
						Labels: []canonical.MatrixLabel{
							{BasePath: "router.matrix-0.video", Description: strPtr("Video")},
							{BasePath: "router.matrix-0.audio", Description: strPtr("Audio")},
						},
						TargetLabels: map[string]map[string]string{
							"Video": {"0": "VID DST 01"},
							"Audio": {"0": "AUD DST 01"},
						},
						SourceLabels: map[string]map[string]string{
							"Video": {"0": "VID SRC 01"},
							"Audio": {"0": "AUD SRC 01"},
						},
					},
				},
			},
		},
	}
	tr, err := newTree(exp)
	if err != nil {
		t.Fatalf("newTree: %v", err)
	}
	if got, want := tr.Size(), 2; got != want {
		t.Fatalf("tree size = %d, want %d", got, want)
	}
	video, ok := tr.lookup(0, 0)
	if !ok {
		t.Fatal("video level not found")
	}
	if video.targetCount != 4 || video.sourceCount != 4 {
		t.Errorf("video counts = %d×%d, want 4×4", video.targetCount, video.sourceCount)
	}
	if video.targetLabels[0] != "VID DST 01" {
		t.Errorf("video dst[0] = %q, want VID DST 01", video.targetLabels[0])
	}
	audio, ok := tr.lookup(0, 1)
	if !ok {
		t.Fatal("audio level not found")
	}
	if audio.sourceLabels[0] != "AUD SRC 01" {
		t.Errorf("audio src[0] = %q, want AUD SRC 01", audio.sourceLabels[0])
	}
}

func strPtr(s string) *string { return &s }

// TestServerLoopback spins up the provider on a random port, connects a
// Probel consumer, and verifies the scaffold acceptance criterion: one
// framed request round-trips (ACK received, session stays open).
func TestServerLoopback(t *testing.T) {
	exp := &canonical.Export{
		Root: &canonical.Node{
			Header: canonical.Header{
				Number: 1, Identifier: "router", OID: "1",
				Children: []canonical.Element{
					&canonical.Matrix{
						Header: canonical.Header{Number: 1, Identifier: "matrix-0", OID: "1.1"},
						Type:   canonical.MatrixOneToN, Mode: canonical.ModeLinear,
						TargetCount: 4, SourceCount: 4,
						Labels: []canonical.MatrixLabel{{BasePath: "router.matrix-0.video"}},
					},
				},
			},
		},
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	srv := newServer(logger, exp)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	serveErr := make(chan error, 1)
	go func() { serveErr <- srv.Serve(ctx, "127.0.0.1:0") }()

	// Wait for Serve to bind.
	deadline := time.Now().Add(2 * time.Second)
	var addr string
	for time.Now().Before(deadline) {
		srv.mu.Lock()
		if srv.listener != nil {
			addr = srv.listener.Addr().String()
			srv.mu.Unlock()
			break
		}
		srv.mu.Unlock()
		time.Sleep(5 * time.Millisecond)
	}
	if addr == "" {
		t.Fatal("server failed to bind")
	}

	// Use the lower-level client directly — the Protocol methods are
	// stubbed in the scaffold, so the interesting thing to test is that
	// the client can frame a request and the provider ACKs it.
	disable := false
	dialCtx, cancelDial := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancelDial()
	cli, err := codec.Dial(dialCtx, addr, logger, codec.ClientConfig{WireHexLog: &disable})
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer func() { _ = cli.Close() }()

	// Send a Maintenance frame (no reply expected by the scaffold —
	// provider ACKs at the framer level but does not reply at the
	// application level until per-command PRs land).
	sendCtx, cancelSend := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancelSend()
	if _, err := cli.Send(sendCtx, codec.Frame{
		ID: codec.RxMaintenance, Payload: []byte{byte(codec.MaintSoftReset)},
	}, nil); err != nil {
		t.Fatalf("send: %v", err)
	}

	// Give the session goroutine a beat to log and close.
	time.Sleep(50 * time.Millisecond)

	cancel()
	select {
	case <-serveErr:
	case <-time.After(2 * time.Second):
		t.Fatal("Serve did not return after ctx cancel")
	}
}
