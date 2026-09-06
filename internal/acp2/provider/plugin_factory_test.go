package acp2

import (
	"dhs/internal/plugin"
	"testing"

	"dhs/internal/export/canonical"
)

// plugin_factory_test.go covers the Factory entry points and newServer's
// degraded (empty-tree) fallback when the canonical export is malformed.

func TestFactory_Meta(t *testing.T) {
	f := &Factory{}
	m := f.Meta()
	if m.Name != "acp2" {
		t.Errorf("Meta.Name=%q want acp2", m.Name)
	}
	if m.DefaultPort != DefaultPort {
		t.Errorf("Meta.DefaultPort=%d want %d", m.DefaultPort, DefaultPort)
	}
	if m.Description == "" {
		t.Error("Meta.Description empty")
	}
}

func TestFactory_New(t *testing.T) {
	f := &Factory{}
	p := f.New(plugin.Deps{Logger: quietLogger()}, buildServeExport())
	if p == nil {
		t.Fatal("Factory.New returned nil provider")
	}
	srv, ok := p.(*server)
	if !ok {
		t.Fatalf("Factory.New returned %T, want *server", p)
	}
	if srv.tree.count() == 0 {
		t.Error("Factory.New built an empty tree from a valid export")
	}
}

// TestNewServer_NilLoggerAndBadExport covers two degraded paths:
//   - nil logger → defaults to slog.Default (no panic).
//   - nil/invalid export → newTree fails, server falls back to emptyTree.
func TestNewServer_NilLoggerAndBadExport(t *testing.T) {
	// nil export → tree build fails → emptyTree fallback.
	srv := newServer(nil, nil)
	if srv == nil {
		t.Fatal("newServer(nil,nil) returned nil")
	}
	if srv.tree == nil || srv.tree.count() != 0 {
		t.Errorf("expected empty fallback tree, count=%d", srv.tree.count())
	}

	// Root that is not a Node → newTree rejects → emptyTree fallback.
	bad := &canonical.Export{Root: &canonical.Parameter{
		Header: canonical.Header{Number: 1, Identifier: "leaf-root"},
		Type:   canonical.ParamInteger,
	}}
	srv2 := newServer(quietLogger(), bad)
	if srv2.tree == nil || srv2.tree.count() != 0 {
		t.Errorf("non-node root should fall back to empty tree, count=%d", srv2.tree.count())
	}
}
