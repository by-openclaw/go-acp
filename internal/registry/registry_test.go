package registry_test

import (
	"context"
	"log/slog"
	"testing"

	"acp/internal/registry"
)

type stubFactory struct {
	meta registry.Meta
}

func (f *stubFactory) Meta() registry.Meta                       { return f.meta }
func (f *stubFactory) New(_ *slog.Logger) registry.Registry      { return &stubRegistry{} }

type stubRegistry struct{}

func (*stubRegistry) Serve(ctx context.Context, _ registry.ServeOptions) error {
	<-ctx.Done()
	return nil
}
func (*stubRegistry) Stop() error            { return nil }
func (*stubRegistry) Stats() registry.Stats  { return registry.Stats{} }

func TestRegisterLookupList(t *testing.T) {
	registry.Register(&stubFactory{meta: registry.Meta{Name: "test-a", DefaultPort: 9000}})
	registry.Register(&stubFactory{meta: registry.Meta{Name: "test-b", DefaultPort: 9001}})

	if _, ok := registry.Lookup("test-a"); !ok {
		t.Fatalf("expected test-a registered")
	}
	if _, ok := registry.Lookup("test-missing"); ok {
		t.Fatalf("expected test-missing absent")
	}

	names := registry.List()
	// Other tests/plugins may register too — assert ours appear in sorted order.
	var sawA, sawB bool
	var idxA, idxB int
	for i, n := range names {
		if n == "test-a" {
			sawA, idxA = true, i
		}
		if n == "test-b" {
			sawB, idxB = true, i
		}
	}
	if !sawA || !sawB {
		t.Fatalf("expected both names registered, got %v", names)
	}
	if idxA > idxB {
		t.Fatalf("expected test-a before test-b in sorted output, got %v", names)
	}
}

func TestRegisterDuplicatePanics(t *testing.T) {
	registry.Register(&stubFactory{meta: registry.Meta{Name: "dup-test"}})
	defer func() {
		if r := recover(); r == nil {
			t.Fatalf("expected panic on duplicate registration")
		}
	}()
	registry.Register(&stubFactory{meta: registry.Meta{Name: "dup-test"}})
}
