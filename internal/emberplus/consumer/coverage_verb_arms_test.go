package emberplus

import (
	"context"
	"testing"
	"time"

	"dhs/internal/consumer"
	"dhs/internal/emberplus/codec/glow"
	"dhs/internal/emberplus/codec/matrix"
)

// emptyConnectedPlugin returns a plugin that passes the session-liveness
// gate (sessionConnected + a dead-writer session) but has an EMPTY tree,
// so ensureWalked → Walk → SendGetDirectory fails on the dead session,
// driving the "walk for <op>" error arms of the networked verbs.
func emptyConnectedPlugin() *Plugin {
	p := freshPlugin()
	p.sessionConnected = true
	p.session = deadSession()
	return p
}

// TestVerbs_EnsureWalkedError drives the ensureWalked-failure arm of
// SetValue, Subscribe, MatrixConnect, and InvokeFunction: an empty tree
// forces a Walk, which fails because the session has no writer.
func TestVerbs_EnsureWalkedError(t *testing.T) {
	ctx := context.Background()

	t.Run("SetValue", func(t *testing.T) {
		p := emptyConnectedPlugin()
		if _, err := p.SetValue(ctx, consumer.ValueRequest{Path: "x", ID: -1},
			consumer.Value{Kind: consumer.KindInt, Int: 1}); err == nil {
			t.Error("want walk error")
		}
	})
	t.Run("Subscribe", func(t *testing.T) {
		p := emptyConnectedPlugin()
		if err := p.Subscribe(consumer.ValueRequest{Path: "x"}, func(consumer.Event) {}); err == nil {
			t.Error("want walk error")
		}
	})
	t.Run("MatrixConnect", func(t *testing.T) {
		p := emptyConnectedPlugin()
		if err := p.MatrixConnect(ctx, "x", 0, []int32{0}, glow.ConnOpConnect); err == nil {
			t.Error("want walk error")
		}
	})
	t.Run("InvokeFunction", func(t *testing.T) {
		p := emptyConnectedPlugin()
		if _, err := p.InvokeFunction(ctx, "x", nil); err == nil {
			t.Error("want walk error")
		}
	})
}

// TestSetValue_EnumLabelError covers the resolveEnumLabelToIndex error
// arm: an enum value given as a label when the Parameter has no enumMap.
func TestSetValue_EnumLabelError(t *testing.T) {
	p := freshPlugin()
	p.sessionConnected = true
	p.session = deadSession()
	p.numIndex["1.1"] = &treeEntry{
		numericPath: []int32{1, 1},
		glowParam:   &glow.Parameter{Path: []int32{1, 1}, Type: glow.ParamTypeInteger}, // no EnumMap
		obj:         consumer.Object{OID: "1.1", Kind: consumer.KindEnum},
	}
	if _, err := p.SetValue(context.Background(), consumer.ValueRequest{Path: "1.1", ID: -1},
		consumer.Value{Kind: consumer.KindEnum, Str: "BadLabel"}); err == nil {
		t.Error("want enum-label resolution error")
	}
}

// TestSubscribe_PerOIDStream covers the per-OID (non-wildcard) Subscribe
// path for a stream Parameter: subscribePath falls back to numericPath
// when glowParam.Path is empty, then SendSubscribe runs. On a dead
// session SendSubscribe errors — which still exercises the path.
func TestSubscribe_PerOIDStream(t *testing.T) {
	s, cleanup := pipeSession(t)
	defer cleanup()
	p := freshPlugin()
	p.session = s
	p.numIndex["1.1"] = &treeEntry{
		numericPath: []int32{1, 1}, // glowParam.Path empty → fallback
		glowParam:   &glow.Parameter{Type: glow.ParamTypeInteger, HasStreamIdentifier: true, StreamIdentifier: 7},
		obj:         consumer.Object{OID: "1.1"},
	}
	p.pathIndex["str"] = p.numIndex["1.1"]
	if err := p.Subscribe(consumer.ValueRequest{Path: "str"}, func(consumer.Event) {}); err != nil {
		t.Errorf("per-OID stream subscribe (live pipe) should succeed: %v", err)
	}
}

// TestSubscribe_PerOIDPlainImplicit covers the implicit-subscribe path:
// a plain (non-stream) Parameter registers the callback locally and
// returns nil without sending Command 30.
func TestSubscribe_PerOIDPlainImplicit(t *testing.T) {
	p := freshPlugin()
	p.session = deadSession()
	p.numIndex["1.1"] = &treeEntry{
		numericPath: []int32{1, 1},
		glowParam:   &glow.Parameter{Path: []int32{1, 1}, Type: glow.ParamTypeInteger},
		obj:         consumer.Object{OID: "1.1"},
	}
	p.pathIndex["plain"] = p.numIndex["1.1"]
	if err := p.Subscribe(consumer.ValueRequest{Path: "plain"}, func(consumer.Event) {}); err != nil {
		t.Errorf("plain implicit subscribe should return nil: %v", err)
	}
}

// TestSubscribe_WildcardGoroutineWalkError drives the wildcard "*"
// Subscribe whose background goroutine's ensureWalked fails (empty tree +
// dead session) → the failure-log branch runs.
func TestSubscribe_WildcardGoroutineWalkError(t *testing.T) {
	p := freshPlugin()
	p.session = deadSession() // Walk inside the goroutine fails
	if err := p.Subscribe(consumer.ValueRequest{ID: -1}, func(consumer.Event) {}); err != nil {
		t.Fatalf("wildcard Subscribe should return nil immediately: %v", err)
	}
	// The goroutine runs ensureWalked (fails) then subscribeAllParameters.
	// Give it time to execute the failure-log branch.
	time.Sleep(100 * time.Millisecond)
}

// TestUnsubscribe_StreamSendsCommand31 covers the wasStream &&
// glowParam.Path>0 arm of Unsubscribe (sends Command 31).
func TestUnsubscribe_StreamSendsCommand31(t *testing.T) {
	s, cleanup := pipeSession(t)
	defer cleanup()
	p := freshPlugin()
	p.session = s
	e := &treeEntry{
		numericPath: []int32{1, 1},
		glowParam:   &glow.Parameter{Path: []int32{1, 1}, Type: glow.ParamTypeInteger,
			HasStreamIdentifier: true, StreamIdentifier: 7},
		obj: consumer.Object{OID: "1.1"},
	}
	p.numIndex["1.1"] = e
	p.pathIndex["str"] = e
	// Pre-register as a stream sub so wasStream becomes true.
	p.subsMu.Lock()
	p.subs["1.1"] = func(consumer.Event) {}
	p.streamSubs["1.1"] = []int32{1, 1}
	p.subsMu.Unlock()
	if err := p.Unsubscribe(consumer.ValueRequest{Path: "str", ID: -1}); err != nil {
		t.Errorf("stream Unsubscribe (live pipe) should succeed: %v", err)
	}
}

// TestMatrixConnect_SourceStealAccepted drives the oneToOne source-steal
// detection arm: target 1 already holds source 3; connecting target 2 to
// source 3 (absolute) steals it, firing OneToOneSourceStealAccepted.
func TestMatrixConnect_SourceStealAccepted(t *testing.T) {
	s, cleanup := pipeSession(t)
	defer cleanup()
	p := freshPlugin()
	p.session = s
	m := &glow.Matrix{
		Number: 1, Identifier: "mtx", Path: []int32{1, 1},
		MatrixType: glow.MatrixTypeOneToOne, TargetCount: 4, SourceCount: 4,
		Connections: []glow.Connection{{Target: 1, Sources: []int32{3}}},
	}
	e := &treeEntry{
		numericPath: []int32{1, 1},
		glowMatrix:  m,
		matrixState: matrix.NewStateFromGlow(m),
		obj:         consumer.Object{OID: "1.1"},
	}
	p.numIndex["1.1"] = e
	p.pathIndex["mtx"] = e
	if err := p.MatrixConnect(context.Background(), "mtx", 2, []int32{3}, glow.ConnOpAbsolute); err != nil {
		t.Errorf("oneToOne source-steal connect should succeed: %v", err)
	}
	if p.profile.Snapshot()[OneToOneSourceStealAccepted] == 0 {
		t.Error("expected OneToOneSourceStealAccepted compliance event")
	}
}
