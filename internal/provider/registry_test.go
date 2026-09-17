package provider

import (
	"context"
	"testing"

	"dhs/internal/export/canonical"
	"dhs/internal/plugin"
)

// The registry had no tests at all: it was exercised only by the real
// plugins registering themselves from init(), which proves nothing about the
// duplicate check or the sort order.

type stubProvider struct{}

func (stubProvider) Serve(context.Context, string) error                { return nil }
func (stubProvider) Stop() error                                        { return nil }
func (stubProvider) SetValue(context.Context, string, any) (any, error) { return nil, nil }

type stubFactory struct{ name string }

func (f stubFactory) Meta() Meta                                  { return Meta{Name: f.name} }
func (f stubFactory) New(plugin.Deps, *canonical.Export) Provider { return stubProvider{} }

// withCleanRegistry runs fn against an empty registry and restores the
// real one afterwards, so the plugins registered by init() are untouched.
func withCleanRegistry(t *testing.T, fn func()) {
	t.Helper()
	mu.Lock()
	saved := factories
	factories = map[string]Factory{}
	mu.Unlock()
	t.Cleanup(func() {
		mu.Lock()
		factories = saved
		mu.Unlock()
	})
	fn()
}

func TestRegisterThenLookup(t *testing.T) {
	withCleanRegistry(t, func() {
		Register(stubFactory{name: "zeta"})
		f, ok := Lookup("zeta")
		if !ok {
			t.Fatal("Lookup did not find what Register installed")
		}
		if f.Meta().Name != "zeta" {
			t.Errorf("Lookup returned %q, want zeta", f.Meta().Name)
		}
		if _, ok := Lookup("nope"); ok {
			t.Error("Lookup found a name that was never registered")
		}
	})
}

// Two plugins cannot share a name: the second registration is a bug in the
// build, not a runtime condition to tolerate.
func TestRegisterPanicsOnDuplicate(t *testing.T) {
	withCleanRegistry(t, func() {
		Register(stubFactory{name: "dup"})
		defer func() {
			if recover() == nil {
				t.Error("a duplicate registration must panic")
			}
		}()
		Register(stubFactory{name: "dup"})
	})
}

// List is sorted so help text and diagnostics are stable across runs; map
// iteration order would shuffle it.
func TestListIsSorted(t *testing.T) {
	withCleanRegistry(t, func() {
		for _, n := range []string{"tsl", "acp1", "osc"} {
			Register(stubFactory{name: n})
		}
		got := List()
		want := []string{"acp1", "osc", "tsl"}
		if len(got) != len(want) {
			t.Fatalf("List = %v, want %v", got, want)
		}
		for i := range want {
			if got[i] != want[i] {
				t.Fatalf("List = %v, want %v", got, want)
			}
		}
	})
}
