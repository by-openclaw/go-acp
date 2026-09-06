package probelsw08p

import (
	"dhs/internal/plugin"
	"io"
	"log/slog"
	"testing"
)

// TestFactoryNew: the Factory.New constructor builds a live server bound
// to the supplied export. Covers plugin.go New (previously 0%).
func TestFactoryNew(t *testing.T) {
	f := &Factory{}
	p := f.New(plugin.Deps{Logger: slog.New(slog.NewTextHandler(io.Discard, nil))}, demoMatrixExport(8, 8))
	if p == nil {
		t.Fatal("Factory.New returned nil")
	}
	srv, ok := p.(*server)
	if !ok {
		t.Fatalf("New returned %T; want *server", p)
	}
	if srv.tree.Size() != 1 {
		t.Errorf("tree size = %d; want 1 level from the demo export", srv.tree.Size())
	}
}

// TestFactoryMeta: the static descriptor reports the Probel name + port.
func TestFactoryMeta(t *testing.T) {
	m := (&Factory{}).Meta()
	if m.Name != "probel-sw08p" {
		t.Errorf("Meta().Name = %q; want probel-sw08p", m.Name)
	}
	if m.DefaultPort != DefaultPort {
		t.Errorf("Meta().DefaultPort = %d; want %d", m.DefaultPort, DefaultPort)
	}
}
