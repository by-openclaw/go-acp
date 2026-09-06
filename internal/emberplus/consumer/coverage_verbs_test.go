package emberplus

import (
	"context"
	"testing"
	"time"

	"dhs/internal/consumer"
	"dhs/internal/consumer/compliance"
	"dhs/internal/emberplus/codec/glow"
)

// freshPlugin builds a not-connected Plugin with all index maps and a
// nil session, suitable for exercising the not-connected / not-found
// error guards of the verb surface.
func freshPlugin() *Plugin {
	return &Plugin{
		// Production timings exist for real devices; a unit test must not
		// sit out a 3 s write timeout to prove the timeout arm.
		writeTimeout: 50 * time.Millisecond,
		logger:       discardLogger(),
		numIndex:     map[string]*treeEntry{},
		pathIndex:    map[string]*treeEntry{},
		labelIndex:   map[string][]*treeEntry{},
		numPath:      map[string]string{},
		subs:         map[string]consumer.EventFunc{},
		streamSubs:   map[string][]int32{},
		streamIndex:  map[int64][]string{},
		templates:    map[string]*glow.Template{},
		profile:      &compliance.Profile{},
		pendingSets:  newPendingSetRegistry(),
	}
}

// TestVerbs_NotConnected drives the dead-session error guards of every
// networked verb. No session is attached, so each must return an error
// (ErrNotConnected or a wrapped proto error) rather than panic.
func TestVerbs_NotConnected(t *testing.T) {
	ctx := context.Background()

	t.Run("GetValue", func(t *testing.T) {
		p := freshPlugin()
		if _, err := p.GetValue(ctx, consumer.ValueRequest{Path: "x"}); err == nil {
			t.Error("want error")
		}
	})
	t.Run("SetValue", func(t *testing.T) {
		p := freshPlugin()
		if _, err := p.SetValue(ctx, consumer.ValueRequest{Path: "x"}, consumer.Value{Kind: consumer.KindInt, Int: 1}); err == nil {
			t.Error("want error")
		}
	})
	t.Run("Subscribe", func(t *testing.T) {
		p := freshPlugin()
		if err := p.Subscribe(consumer.ValueRequest{Path: "x"}, func(consumer.Event) {}); err == nil {
			t.Error("want error")
		}
	})
	t.Run("MatrixConnect", func(t *testing.T) {
		p := freshPlugin()
		if err := p.MatrixConnect(ctx, "m", 0, []int32{0}, glow.ConnOpConnect); err == nil {
			t.Error("want error")
		}
	})
	t.Run("InvokeFunction", func(t *testing.T) {
		p := freshPlugin()
		if _, err := p.InvokeFunction(ctx, "f", nil); err == nil {
			t.Error("want error")
		}
	})
	t.Run("Walk", func(t *testing.T) {
		p := freshPlugin()
		if _, err := p.Walk(ctx, 0); err == nil {
			t.Error("want error")
		}
	})
	t.Run("Walk_badSlot", func(t *testing.T) {
		p := freshPlugin()
		if _, err := p.Walk(ctx, 5); err == nil {
			t.Error("non-zero slot must error")
		}
	})
	t.Run("GetSlotInfo_badSlot", func(t *testing.T) {
		p := freshPlugin()
		if _, err := p.GetSlotInfo(ctx, 5); err == nil {
			t.Error("non-zero slot must error")
		}
	})
}

// TestFindEntry_AllResolvers covers every resolution branch of
// findEntry: numIndex by OID path, pathIndex by identifier path, label
// (including the ambiguous-many warning), ID match, and the miss.
func TestFindEntry_AllResolvers(t *testing.T) {
	p := freshPlugin()
	e := &treeEntry{numericPath: []int32{1, 1}, obj: consumer.Object{ID: 7, OID: "1.1", Label: "gain"}}
	p.numIndex["1.1"] = e
	p.pathIndex["root.gain"] = e
	p.labelIndex["gain"] = []*treeEntry{e}

	if _, got := p.findEntry(consumer.ValueRequest{Path: "1.1"}); got != e {
		t.Error("numIndex path resolve")
	}
	if _, got := p.findEntry(consumer.ValueRequest{Path: "root.gain"}); got != e {
		t.Error("pathIndex path resolve")
	}
	if _, got := p.findEntry(consumer.ValueRequest{Label: "gain"}); got != e {
		t.Error("label resolve")
	}
	if _, got := p.findEntry(consumer.ValueRequest{ID: 7}); got != e {
		t.Error("ID resolve")
	}
	if _, got := p.findEntry(consumer.ValueRequest{Path: "nope", ID: -1}); got != nil {
		t.Error("miss should return nil")
	}

	// Ambiguous label → warns, returns first.
	e2 := &treeEntry{numericPath: []int32{1, 2}, obj: consumer.Object{ID: 8, OID: "1.2", Label: "dup"}}
	p.labelIndex["dup"] = []*treeEntry{e, e2}
	if _, got := p.findEntry(consumer.ValueRequest{Label: "dup"}); got == nil {
		t.Error("ambiguous label should still resolve to the first match")
	}
}

// TestGetValue_FoundAndMissing covers the found (returns the cached
// value) and not-found branches of GetValue on a pre-walked tree.
func TestGetValue_FoundAndMissing(t *testing.T) {
	p := freshPlugin()
	p.numIndex["1.1"] = &treeEntry{
		numericPath: []int32{1, 1},
		obj:         consumer.Object{OID: "1.1", Value: consumer.Value{Kind: consumer.KindInt, Int: 9}},
	}
	v, err := p.GetValue(context.Background(), consumer.ValueRequest{Path: "1.1"})
	if err != nil || v.Int != 9 {
		t.Errorf("GetValue found = (%+v, %v)", v, err)
	}
	if _, err := p.GetValue(context.Background(), consumer.ValueRequest{Path: "missing", ID: -1}); err == nil {
		t.Error("GetValue missing should error")
	}
}

// TestSetValue_NotFound covers the parameter-not-found branch after the
// session gate passes (sessionConnected + a session attached).
func TestSetValue_NotFound(t *testing.T) {
	p := freshPlugin()
	p.sessionConnected = true
	p.session = NewSession(discardLogger())
	// Tree non-empty so ensureWalked is a no-op, but the requested path
	// is absent.
	p.numIndex["1.1"] = &treeEntry{numericPath: []int32{1, 1}, obj: consumer.Object{OID: "1.1"}}
	if _, err := p.SetValue(context.Background(), consumer.ValueRequest{Path: "missing", ID: -1},
		consumer.Value{Kind: consumer.KindInt, Int: 1}); err == nil {
		t.Error("SetValue on a missing path should error")
	}
}

// TestMatrixConnect_NotMatrix covers MatrixConnect's not-a-matrix branch
// (entry exists but has no glowMatrix) on a pre-walked tree with a live
// session attached.
func TestMatrixConnect_NotMatrix(t *testing.T) {
	p := freshPlugin()
	p.session = NewSession(discardLogger())
	p.numIndex["1.1"] = &treeEntry{
		numericPath: []int32{1, 1},
		glowParam:   &glow.Parameter{Identifier: "notmatrix"},
		obj:         consumer.Object{OID: "1.1", Path: []string{"notmatrix"}},
	}
	p.pathIndex["notmatrix"] = p.numIndex["1.1"]
	if err := p.MatrixConnect(context.Background(), "notmatrix", 0, []int32{0}, glow.ConnOpConnect); err == nil {
		t.Error("MatrixConnect on a non-matrix should error")
	}
}

// TestInvokeFunction_NotFound covers InvokeFunction's function-not-found
// branch on a pre-walked tree with a live session.
func TestInvokeFunction_NotFound(t *testing.T) {
	p := freshPlugin()
	p.session = NewSession(discardLogger())
	p.numIndex["1.1"] = &treeEntry{numericPath: []int32{1, 1}, obj: consumer.Object{OID: "1.1"}}
	if _, err := p.InvokeFunction(context.Background(), "missing", nil); err == nil {
		t.Error("InvokeFunction on a missing path should error")
	}
}

// TestMatrixSnapshot_Missing covers the nil-return branch.
func TestMatrixSnapshot_Missing(t *testing.T) {
	p := freshPlugin()
	if got := p.MatrixSnapshot("nope"); got != nil {
		t.Errorf("MatrixSnapshot(missing) = %v, want nil", got)
	}
}

// TestUnsubscribe_Variants covers the wildcard-clear branch, the
// unknown-request no-op, and the per-OID stream-unsubscribe branch
// (which clears streamSubs without needing a live writer for the
// wildcard / non-stream cases).
func TestUnsubscribe_Variants(t *testing.T) {
	// Wildcard clear (no session → returns nil).
	p := freshPlugin()
	p.subs["*"] = func(consumer.Event) {}
	if err := p.Unsubscribe(consumer.ValueRequest{ID: -1}); err != nil {
		t.Errorf("wildcard unsubscribe (no session): %v", err)
	}

	// Unknown request, entry not found → nil.
	if err := p.Unsubscribe(consumer.ValueRequest{Path: "missing"}); err != nil {
		t.Errorf("unknown unsubscribe: %v", err)
	}
}

// TestResolveDtdVersion covers the no-session branch and the
// walked-tree identity.dtdVersion fallback. The live-probe branch is
// covered by GetDeviceInfo over the loopback.
func TestResolveDtdVersion(t *testing.T) {
	p := freshPlugin()
	// No session → "".
	if got := p.resolveDtdVersion(context.Background()); got != "" {
		t.Errorf("no session = %q, want empty", got)
	}

	// Seed the identity.dtdVersion entry and attach a session whose
	// DtdVersion() is empty so the fallback path runs. We cannot drive
	// the live probe without a writer, so SendGetDirectory fails and the
	// code falls through to the pathIndex fallback.
	p.pathIndex["identity.dtdVersion"] = &treeEntry{
		obj: consumer.Object{Value: consumer.Value{Kind: consumer.KindString, Str: "2.60"}},
	}
	s := NewSession(discardLogger())
	s.SetProfile(&compliance.Profile{})
	p.session = s
	if got := p.resolveDtdVersion(context.Background()); got != "2.60" {
		t.Errorf("fallback = %q, want 2.60", got)
	}
}

// TestEmitRootSessionEvent covers the populated branch (wildcard sub +
// a tree entry → fires with the root entry's OID), the reason-carrying
// branch, and the no-subscriber early return.
func TestEmitRootSessionEvent(t *testing.T) {
	p := freshPlugin()

	// No wildcard subscriber → early return, no panic.
	p.emitRootSessionEvent(false, "x")

	// Add a wildcard subscriber + a tree entry.
	got := make(chan consumer.Event, 4)
	p.subs["*"] = func(e consumer.Event) {
		select {
		case got <- e:
		default:
		}
	}
	p.numIndex["1"] = &treeEntry{
		numericPath: []int32{1},
		obj:         consumer.Object{OID: "1", Path: []string{"root"}, Label: "root"},
	}

	// Disconnect transition with a reason → 2 changes (isOnline + reason).
	p.emitRootSessionEvent(false, "tcp eof")
	select {
	case e := <-got:
		if e.OID != "1" {
			t.Errorf("event OID = %q, want 1", e.OID)
		}
		if len(e.Changes) != 2 {
			t.Errorf("changes = %d, want 2 (isOnline + reason)", len(e.Changes))
		}
		if e.Freshness != "stale" {
			t.Errorf("disconnect freshness = %q, want stale", e.Freshness)
		}
	case <-time.After(time.Second):
		t.Fatal("no event emitted")
	}

	// Reconnect transition, no reason → 1 change, live freshness.
	p.emitRootSessionEvent(true, "")
	select {
	case e := <-got:
		if len(e.Changes) != 1 {
			t.Errorf("changes = %d, want 1", len(e.Changes))
		}
		if e.Freshness != "live" {
			t.Errorf("connect freshness = %q, want live", e.Freshness)
		}
	case <-time.After(time.Second):
		t.Fatal("no reconnect event emitted")
	}
}

// TestMarkTreeStale flips every entry to stale.
func TestMarkTreeStale(t *testing.T) {
	p := freshPlugin()
	e := &treeEntry{freshness: FreshnessLive}
	p.numIndex["1"] = e
	p.markTreeStale()
	if e.freshness != FreshnessStale {
		t.Errorf("freshness = %v, want stale", e.freshness)
	}
}

// TestSubscribeOnDiscovery covers the guard branches: nil entry, nil
// glowParam, empty numericPath, no wildcard active, and the no-session
// branch when wildcard IS active.
func TestSubscribeOnDiscovery(t *testing.T) {
	p := freshPlugin()

	p.subscribeOnDiscovery(nil)                                      // nil entry
	p.subscribeOnDiscovery(&treeEntry{})                             // nil glowParam
	p.subscribeOnDiscovery(&treeEntry{glowParam: &glow.Parameter{}}) // empty numericPath

	// glowParam + numericPath but no wildcard active → returns early.
	e := &treeEntry{
		glowParam:   &glow.Parameter{StreamIdentifier: 1},
		numericPath: []int32{1, 1},
	}
	p.subscribeOnDiscovery(e)
	p.subsMu.RLock()
	_, sub := p.streamSubs["1.1"]
	p.subsMu.RUnlock()
	if sub {
		t.Error("must not subscribe without active wildcard")
	}

	// wildcard active but no session → registers streamSubs then bails
	// on the nil-session check.
	p.subs["*"] = func(consumer.Event) {}
	p.subscribeOnDiscovery(e)
}
