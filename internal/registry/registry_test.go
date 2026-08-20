package registry_test

import (
	"context"
	"fmt"
	"log/slog"
	"sync/atomic"
	"testing"

	"dhs/internal/registry"
)

// regSeq makes registered names unique per test iteration: the
// registry is package-global and Register panics on duplicates, so
// fixed names break `go test -count=N` / stress runs (the flake-hunt
// tool must be able to re-run every test in one process).
var regSeq atomic.Int64

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
	n := regSeq.Add(1)
	nameA := fmt.Sprintf("test-a-%d", n)
	nameB := fmt.Sprintf("test-b-%d", n)
	registry.Register(&stubFactory{meta: registry.Meta{Name: nameA, DefaultPort: 9000}})
	registry.Register(&stubFactory{meta: registry.Meta{Name: nameB, DefaultPort: 9001}})

	if _, ok := registry.Lookup(nameA); !ok {
		t.Fatalf("expected %s registered", nameA)
	}
	if _, ok := registry.Lookup("test-missing"); ok {
		t.Fatalf("expected test-missing absent")
	}

	names := registry.List()
	// Other tests/plugins may register too — assert ours appear in sorted order.
	var sawA, sawB bool
	var idxA, idxB int
	for i, nm := range names {
		if nm == nameA {
			sawA, idxA = true, i
		}
		if nm == nameB {
			sawB, idxB = true, i
		}
	}
	if !sawA || !sawB {
		t.Fatalf("expected both names registered, got %v", names)
	}
	if idxA > idxB {
		t.Fatalf("expected %s before %s in sorted output, got %v", nameA, nameB, names)
	}
}

func TestRegisterDuplicatePanics(t *testing.T) {
	name := fmt.Sprintf("dup-test-%d", regSeq.Add(1))
	registry.Register(&stubFactory{meta: registry.Meta{Name: name}})
	defer func() {
		if r := recover(); r == nil {
			t.Fatalf("expected panic on duplicate registration")
		}
	}()
	registry.Register(&stubFactory{meta: registry.Meta{Name: name}})
}
