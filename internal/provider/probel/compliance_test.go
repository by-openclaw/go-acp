package probel

import (
	"io"
	"log/slog"
	"testing"

	iprobel "acp/internal/probel"
	"acp/internal/export/canonical"
)

// newComplianceServer builds a minimal provider against a 4×4 single-level
// demo matrix; used by compliance tests that want to observe Profile
// mutations without the TCP listener.
func newComplianceServer(t *testing.T) *server {
	t.Helper()
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
	return newServer(slog.New(slog.NewTextHandler(io.Discard, nil)), exp)
}

// TestProfileUnsupportedCommand: dispatching a frame with a CMD byte
// outside the handler table increments UnsupportedCommand.
func TestProfileUnsupportedCommand(t *testing.T) {
	srv := newComplianceServer(t)
	before := srv.profile.Snapshot()[UnsupportedCommand]
	if _, err := srv.handle(iprobel.Frame{ID: iprobel.CommandID(0xDE)}); err != nil {
		t.Fatalf("handle: %v", err)
	}
	after := srv.profile.Snapshot()[UnsupportedCommand]
	if after != before+1 {
		t.Errorf("UnsupportedCommand counter %d -> %d; want +1", before, after)
	}
}

// TestProfileHandlerError: a handler that returns an error (here a
// Protect Connect on an unknown matrix) produces a non-nil err from
// handle. The session.dispatch path notes HandlerRejected; we cover
// that path via session_test, not here — this test just verifies the
// error surfaces so the dispatch layer has something to count.
func TestProfileHandlerError(t *testing.T) {
	srv := newComplianceServer(t)
	frame := iprobel.EncodeProtectConnect(iprobel.ProtectConnectParams{
		MatrixID: 99, LevelID: 0, DestinationID: 0, DeviceID: 1,
	})
	if _, err := srv.handle(frame); err == nil {
		t.Fatal("handle: want error on unknown matrix; got nil")
	}
}

// TestComplianceProfileAccessor: server exposes its profile.
func TestComplianceProfileAccessor(t *testing.T) {
	srv := newComplianceServer(t)
	if srv.ComplianceProfile() == nil {
		t.Fatal("ComplianceProfile() returned nil")
	}
}
