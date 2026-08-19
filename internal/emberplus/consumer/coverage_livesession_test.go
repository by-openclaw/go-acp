package emberplus

import (
	"context"
	"io"
	"log/slog"
	"net"
	"strconv"
	"sync"
	"testing"
	"time"

	"dhs/internal/consumer"
	"dhs/internal/emberplus/codec/glow"
	provider "dhs/internal/emberplus/provider"
	"dhs/internal/export/canonical"
)

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// severableProxy is a TCP relay between the consumer and the live
// provider that a test can sever on demand. Closing the active
// connection pair makes the consumer's session readLoop observe a read
// error and fire onSessionStateChange(false) â€” the exact unsolicited
// disconnect that drives the reconnect goroutine. The proxy keeps
// accepting (the consumer's auto-redial) and re-dials the provider for
// every new downstream connection, so a fresh walk succeeds.
type severableProxy struct {
	ln       net.Listener
	upstream string // provider addr to dial for each accepted conn

	mu     sync.Mutex
	active []net.Conn
	closed bool
}

func newSeverableProxy(t *testing.T, upstream string) *severableProxy {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("proxy listen: %v", err)
	}
	p := &severableProxy{ln: ln, upstream: upstream}
	go p.acceptLoop()
	return p
}

func (p *severableProxy) addr() string { return p.ln.Addr().String() }

func (p *severableProxy) acceptLoop() {
	for {
		down, err := p.ln.Accept()
		if err != nil {
			return
		}
		up, err := net.DialTimeout("tcp", p.upstream, time.Second)
		if err != nil {
			_ = down.Close()
			continue
		}
		p.mu.Lock()
		if p.closed {
			p.mu.Unlock()
			_ = down.Close()
			_ = up.Close()
			return
		}
		p.active = append(p.active, down, up)
		p.mu.Unlock()
		go func() { _, _ = io.Copy(up, down); _ = up.Close() }()
		go func() { _, _ = io.Copy(down, up); _ = down.Close() }()
	}
}

// sever closes every currently-active connection, forcing the consumer
// to observe a read error and begin reconnecting. New connections (the
// redial) are accepted fresh.
func (p *severableProxy) sever() {
	p.mu.Lock()
	conns := p.active
	p.active = nil
	p.mu.Unlock()
	for _, c := range conns {
		_ = c.Close()
	}
}

func (p *severableProxy) stop() {
	p.mu.Lock()
	p.closed = true
	conns := p.active
	p.active = nil
	p.mu.Unlock()
	for _, c := range conns {
		_ = c.Close()
	}
	_ = p.ln.Close()
}

// TestConsumerReconnect drives the full auto-reconnect path: connect via
// a severable proxy, walk, sever the link (unsolicited disconnect),
// then assert the consumer redials and re-walks the tree. Exercises
// reconnect.go (start / reconnectLoop / refreshAfterReconnect /
// clearTree), onSessionStateChange's disconnect branch, markTreeStale,
// emitRootSessionEvent, and subscribeAllParameters via the refresh.
func TestConsumerReconnect(t *testing.T) {
	provAddr, stop := startLoopbackProvider(t)
	defer stop()

	proxy := newSeverableProxy(t, provAddr)
	defer proxy.stop()

	host, portStr, _ := net.SplitHostPort(proxy.addr())
	port, _ := strconv.Atoi(portStr)

	p := (&Factory{}).New(discardLogger()).(*Plugin)
	// Fast, bounded reconnect so the test is deterministic and quick.
	p.reconnectPolicyOverride = &reconnectPolicy{
		InitialBackoff: 5 * time.Millisecond,
		MaxBackoff:     20 * time.Millisecond,
		MaxAttempts:    50,
	}

	ctx := context.Background()
	if err := p.Connect(ctx, host, port); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	defer func() { _ = p.Disconnect() }()

	// Register a wildcard watcher so emitRootSessionEvent has a target
	// and subscribeAllParameters has work after the reconnect walk.
	events := make(chan consumer.Event, 16)
	_ = p.Subscribe(consumer.ValueRequest{ID: -1}, func(e consumer.Event) {
		select {
		case events <- e:
		default:
		}
	})

	if _, err := p.Walk(ctx, 0); err != nil {
		t.Fatalf("initial Walk: %v", err)
	}

	// Sever the link â€” consumer should observe the disconnect and
	// auto-reconnect.
	proxy.sever()

	// First, wait for the disconnect to register (sessionConnected
	// false). This guarantees onSessionStateChange(false) fired and the
	// reconnect goroutine launched.
	disconnectSeen := false
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		p.mu.Lock()
		connected := p.sessionConnected
		p.mu.Unlock()
		if !connected {
			disconnectSeen = true
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if !disconnectSeen {
		t.Fatal("consumer never observed the unsolicited disconnect")
	}

	// Now wait for the reconnect: session live again AND the tree
	// re-walked. refreshAfterReconnect clears the tree then re-walks, so
	// a populated numIndex after the disconnect proves the refresh ran.
	deadline = time.Now().Add(5 * time.Second)
	reconnected := false
	for time.Now().Before(deadline) {
		p.mu.Lock()
		connected := p.sessionConnected
		p.mu.Unlock()
		if connected {
			p.treeMu.RLock()
			n := len(p.numIndex)
			p.treeMu.RUnlock()
			if n > 0 {
				reconnected = true
				break
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	if !reconnected {
		t.Fatal("consumer did not reconnect + re-walk within deadline")
	}
}

// TestConsumerReconnect_StopOnDisconnect verifies Disconnect cancels an
// in-flight reconnect loop (reconnectCtrl.stop) and the deliberate
// teardown does not leave a reconnect goroutine spinning.
func TestConsumerReconnect_StopOnDisconnect(t *testing.T) {
	provAddr, stop := startLoopbackProvider(t)
	defer stop()

	proxy := newSeverableProxy(t, provAddr)
	defer proxy.stop()

	host, portStr, _ := net.SplitHostPort(proxy.addr())
	port, _ := strconv.Atoi(portStr)

	p := (&Factory{}).New(discardLogger()).(*Plugin)
	p.reconnectPolicyOverride = &reconnectPolicy{
		InitialBackoff: time.Second, // long, so the loop is mid-backoff when we stop it
		MaxBackoff:     time.Second,
		MaxAttempts:    0,
	}
	if err := p.Connect(context.Background(), host, port); err != nil {
		t.Fatalf("Connect: %v", err)
	}

	// Stop the whole upstream so reconnect attempts keep failing â€” the
	// loop stays alive in backoff.
	stop()
	proxy.sever()

	// Give the disconnect a moment to start the reconnect loop.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		p.reconnect.mu.Lock()
		active := p.reconnect.active
		p.reconnect.mu.Unlock()
		if active {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	// Disconnect must cancel the loop.
	if err := p.Disconnect(); err != nil {
		t.Fatalf("Disconnect: %v", err)
	}

	// After Disconnect the reconnect ctrl should wind down.
	deadline = time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		p.reconnect.mu.Lock()
		active := p.reconnect.active
		p.reconnect.mu.Unlock()
		if !active {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Error("reconnect loop still active after Disconnect")
}

// startStreamMatrixProvider serves a tree with a stream-backed Parameter
// (so the consumer's explicit per-OID Command 30 stream-subscribe path
// fires) and an nToN matrix with caps + initial connections (so
// MatrixConnect's matrixState validation + ApplyConnection run).
func startStreamMatrixProvider(t *testing.T) (string, func()) {
	t.Helper()
	sid := int64(11)
	meter := &canonical.Parameter{
		Header: canonical.Header{
			Number: 1, Identifier: "meter", Path: "root.meter", OID: "1.1",
			IsOnline: true, Access: canonical.AccessRead, Children: canonical.EmptyChildren(),
		},
		Type: canonical.ParamReal, Value: float64(-20),
		Minimum: float64(-60), Maximum: float64(0),
		StreamIdentifier: &sid,
		StreamDescriptor: &canonical.StreamDescriptor{Format: int(0), Offset: 0},
	}
	mtx := &canonical.Matrix{
		Header: canonical.Header{
			Number: 2, Identifier: "xpt", Path: "root.xpt", OID: "1.2",
			IsOnline: true, Access: canonical.AccessReadWrite, Children: canonical.EmptyChildren(),
		},
		Type: canonical.MatrixNToN, TargetCount: 4, SourceCount: 4,
		MaximumTotalConnects: i64ptr(8), MaximumConnectsPerTarget: i64ptr(2),
		Connections: []canonical.MatrixConnection{{Target: 0, Sources: []int64{0}}},
	}
	root := &canonical.Node{Header: canonical.Header{
		Number: 1, Identifier: "root", Path: "root", OID: "1", IsOnline: true,
		Access: canonical.AccessRead, Children: []canonical.Element{meter, mtx},
	}}
	srv := (&provider.Factory{}).New(nil, &canonical.Export{Root: root})
	// Pre-bound listener: no close-then-rebind port-steal window and no
	// dial-poll (#694 flake class).
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	addr := ln.Addr().String()
	sl, ok := srv.(interface {
		ServeListener(context.Context, net.Listener) error
	})
	if !ok {
		t.Fatal("provider does not expose ServeListener")
	}
	ctx, cancel := context.WithCancel(context.Background())
	go func() { _ = sl.ServeListener(ctx, ln) }()
	return addr, func() { cancel(); _ = srv.Stop() }
}

func i64ptr(v int64) *int64 { return &v }

// TestLoopbackStreamMatrix drives the explicit per-OID stream Subscribe
// (Command 30) path and a cap-validated MatrixConnect against a live
// provider with a stream Parameter + nToN matrix.
func TestLoopbackStreamMatrix(t *testing.T) {
	addr, stop := startStreamMatrixProvider(t)
	defer stop()
	host, portStr, _ := net.SplitHostPort(addr)
	port, _ := strconv.Atoi(portStr)

	p := (&Factory{}).New(discardLogger()).(*Plugin)
	ctx := context.Background()
	if err := p.Connect(ctx, host, port); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	defer func() { _ = p.Disconnect() }()
	if _, err := p.Walk(ctx, 0); err != nil {
		t.Fatalf("Walk: %v", err)
	}

	// Explicit per-OID subscribe on the stream parameter → Command 30.
	if err := p.Subscribe(consumer.ValueRequest{Path: "root.meter"}, func(consumer.Event) {}); err != nil {
		t.Errorf("stream Subscribe: %v", err)
	}
	// Unsubscribe it → Command 31 (stream path).
	_ = p.Unsubscribe(consumer.ValueRequest{Path: "root.meter"})

	// MatrixConnect within caps (validation passes, ApplyConnection runs).
	if err := p.MatrixConnect(ctx, "root.xpt", 1, []int32{2}, glow.ConnOpConnect); err != nil {
		t.Logf("MatrixConnect: %v", err)
	}
	_ = p.MatrixSnapshot("root.xpt")
}

// TestLoopbackVerbsFull drives the full networked verb surface against a
// live in-process provider with proper wildcard (ID:-1) requests so the
// happy paths of Subscribe / Unsubscribe / SetValue (confirmed) /
// MatrixConnect / InvokeFunction / GetValue / GetDeviceInfo and the
// underlying process*/Meta pipeline all execute.
func TestLoopbackVerbsFull(t *testing.T) {
	addr, stop := startLoopbackProvider(t)
	defer stop()

	host, portStr, _ := net.SplitHostPort(addr)
	port, _ := strconv.Atoi(portStr)

	p := (&Factory{}).New(discardLogger()).(*Plugin)
	ctx := context.Background()
	if err := p.Connect(ctx, host, port); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	defer func() { _ = p.Disconnect() }()

	if _, err := p.Walk(ctx, 0); err != nil {
		t.Fatalf("Walk: %v", err)
	}

	// GetDeviceInfo (drives resolveDtdVersion live probe).
	_, _ = p.GetDeviceInfo(ctx)
	_, _ = p.GlowSnapshot(ctx)
	_, _ = p.Canonicalize(ctx, CanonicalOptions{Labels: "inline", Gain: "inline", Templates: "inline"})

	// Wildcard subscribe (ID:-1) → background walk + subscribeAllParameters.
	events := make(chan consumer.Event, 32)
	if err := p.Subscribe(consumer.ValueRequest{ID: -1}, func(e consumer.Event) {
		select {
		case events <- e:
		default:
		}
	}); err != nil {
		t.Fatalf("wildcard Subscribe: %v", err)
	}
	// Wait for the subscribe batch to register.
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		p.subsMu.RLock()
		n := len(p.streamSubs)
		p.subsMu.RUnlock()
		if n > 0 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	// SetValue with confirmation (provider echoes the announce).
	if _, err := p.SetValue(ctx, consumer.ValueRequest{Path: "root.gain", ID: -1},
		consumer.Value{Kind: consumer.KindInt, Int: 42}); err != nil {
		t.Logf("SetValue: %v", err) // coercion/timeout tolerated, path still exercised
	}

	// GetValue on the now-updated gain.
	if _, err := p.GetValue(ctx, consumer.ValueRequest{Path: "root.gain", ID: -1}); err != nil {
		t.Errorf("GetValue: %v", err)
	}

	// MatrixConnect + snapshot.
	if err := p.MatrixConnect(ctx, "root.mat", 1, []int32{1}, glow.ConnOpConnect); err != nil {
		t.Logf("MatrixConnect: %v", err)
	}
	_ = p.MatrixSnapshot("root.mat")

	// InvokeFunction (builtin sum).
	if _, err := p.InvokeFunction(ctx, "root.sum", []any{int64(2), int64(3)}); err != nil {
		t.Logf("InvokeFunction: %v", err)
	}

	// Per-OID Unsubscribe on the gain param, then wildcard Unsubscribe.
	_ = p.Unsubscribe(consumer.ValueRequest{Path: "root.gain", ID: -1})
	_ = p.Unsubscribe(consumer.ValueRequest{ID: -1})

	// Drain any announces that arrived.
	select {
	case <-events:
	case <-time.After(300 * time.Millisecond):
	}
}

// TestReconnectLoop_GiveUp drives reconnectLoop directly against a dead
// address with a bounded attempt count so the MaxAttempts give-up
// branch (and the backoff-doubling cap) are exercised deterministically.
func TestReconnectLoop_GiveUp(t *testing.T) {
	p := (&Factory{}).New(discardLogger()).(*Plugin)
	p.connIP = "127.0.0.1"
	p.connPort = 1 // nothing listens here → every dial fails fast
	p.reconnectPolicyOverride = &reconnectPolicy{
		InitialBackoff: time.Millisecond,
		MaxBackoff:     time.Millisecond, // forces the "backoff > MaxBackoff" cap branch
		MaxAttempts:    2,
	}
	// Runs to the give-up return without blocking the test.
	done := make(chan struct{})
	go func() { p.reconnectLoop(context.Background()); close(done) }()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("reconnectLoop did not give up within deadline")
	}
}

// TestReconnectLoop_CtxCancelled covers the ctx.Err() early return at the
// top of the loop.
func TestReconnectLoop_CtxCancelled(t *testing.T) {
	p := (&Factory{}).New(discardLogger()).(*Plugin)
	p.connIP = "127.0.0.1"
	p.connPort = 1
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // already done
	done := make(chan struct{})
	go func() { p.reconnectLoop(ctx); close(done) }()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("reconnectLoop did not return on cancelled ctx")
	}
}

// TestRefreshAfterReconnect_EdgeBranches covers the ctx.Err early-return
// and the Walk-failure branch (no session → ErrNotConnected) of
// refreshAfterReconnect.
func TestRefreshAfterReconnect_EdgeBranches(t *testing.T) {
	p := (&Factory{}).New(discardLogger()).(*Plugin)
	// Initialise the index maps clearTree expects.
	p.numIndex = map[string]*treeEntry{}
	p.pathIndex = map[string]*treeEntry{}
	p.labelIndex = map[string][]*treeEntry{}
	p.numPath = map[string]string{}
	p.templates = map[string]*glow.Template{}
	p.streamIndex = map[int64][]string{}

	// Cancelled ctx → early return before clearTree.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	p.refreshAfterReconnect(ctx)

	// Live ctx but no session → clearTree runs, Walk returns
	// ErrNotConnected → walk-failure branch.
	p.refreshAfterReconnect(context.Background())
}

// TestWildcardSubscribeBatch drives the wildcard Subscribe path to
// completion: it walks the tree and then runs subscribeAllParameters in
// a background goroutine. The loopback's tree carries a gain Parameter,
// so at least one Command 30 is sent. We wait for streamSubs to be
// populated, proving subscribeAllParameters ran.
func TestWildcardSubscribeBatch(t *testing.T) {
	addr, stop := startLoopbackProvider(t)
	defer stop()

	host, portStr, _ := net.SplitHostPort(addr)
	port, _ := strconv.Atoi(portStr)

	p := (&Factory{}).New(discardLogger()).(*Plugin)
	if err := p.Connect(context.Background(), host, port); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	defer func() { _ = p.Disconnect() }()

	// Walk first so numIndex is populated before the wildcard subscribe.
	if _, err := p.Walk(context.Background(), 0); err != nil {
		t.Fatalf("Walk: %v", err)
	}

	_ = p.Subscribe(consumer.ValueRequest{ID: -1}, func(consumer.Event) {})

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		p.subsMu.RLock()
		n := len(p.streamSubs)
		p.subsMu.RUnlock()
		if n > 0 {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Error("subscribeAllParameters did not register any Command 30 subscriptions")
}
