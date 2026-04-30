package probelsw02p

import (
	"io"
	"log/slog"
	"testing"

	"acp/internal/export/canonical"
	"acp/internal/provider"
)

// TestFactoryMeta verifies the provider registration contract.
func TestFactoryMeta(t *testing.T) {
	f, ok := provider.Lookup("probel-sw02p")
	if !ok {
		t.Fatal("probel-sw02p provider not registered")
	}
	m := f.Meta()
	if m.Name != "probel-sw02p" {
		t.Errorf("meta.Name = %q; want probel-sw02p", m.Name)
	}
	if m.DefaultPort != DefaultPort {
		t.Errorf("meta.DefaultPort = %d; want %d", m.DefaultPort, DefaultPort)
	}
}

// TestNewServerWithEmptyTree exercises the newServer happy path with a
// minimal canonical tree. Confirms metrics + profile are initialised.
func TestNewServerWithEmptyTree(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	exp := &canonical.Export{Root: &canonical.Node{Header: canonical.Header{Number: 1, Identifier: "root", OID: "1"}}}
	srv := newServer(logger, exp)
	if srv.Metrics() == nil {
		t.Error("Metrics() = nil; want non-nil")
	}
	if srv.ComplianceProfile() == nil {
		t.Error("ComplianceProfile() = nil; want non-nil")
	}
	if srv.tree == nil {
		t.Error("tree = nil; want non-nil")
	}
}
