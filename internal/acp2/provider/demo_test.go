package acp2

import (
	"context"
	"dhs/internal/plugin"
	"testing"
	"time"

	"dhs/internal/acp2/codec"
	"dhs/internal/export/canonical"
)

// demo_test.go covers RunAnnounceDemo + emitFloatAnnounce — the
// device-initiated announce path. It reuses the live serve harness so a
// subscribed client actually receives the demo fanout.

// floatDemoExport returns an export whose slot-1/obj-18 is an RW float
// in [-60,20], matching RunAnnounceDemo's target shape.
func floatDemoExport() *canonical.Export {
	str := func(s string) *string { return &s }
	gain := &canonical.Parameter{
		Header: canonical.Header{Number: 18, Identifier: "GainFloat",
			Access: canonical.AccessReadWrite, Children: canonical.EmptyChildren()},
		Type:    canonical.ParamReal,
		Value:   float64(0),
		Minimum: float64(-60),
		Maximum: float64(20),
		Format:  str("float"),
	}
	root := &canonical.Node{Header: canonical.Header{
		Number: 1, Identifier: "ROOT_NODE_V2", Access: canonical.AccessRead,
		Children: []canonical.Element{gain},
	}}
	slot1 := &canonical.Node{Header: canonical.Header{
		Number: 1, Identifier: "slot-1", Access: canonical.AccessRead,
		Children: []canonical.Element{root},
	}}
	return &canonical.Export{Root: &canonical.Node{Header: canonical.Header{
		Number: 1, Identifier: "device", Access: canonical.AccessRead,
		Children: []canonical.Element{slot1},
	}}}
}

// TestEmitFloatAnnounce_ToSubscriber verifies one demo tick mutates the
// tree and fans a value announce out to a subscribed client.
func TestEmitFloatAnnounce_ToSubscriber(t *testing.T) {
	srv, addr, stop := startServe(t, floatDemoExport())
	defer stop()

	c := dialFakeClient(t, addr)
	defer c.close()
	c.handshake(true)

	if !waitFor(t, time.Second, func() bool {
		srv.mu.Lock()
		defer srv.mu.Unlock()
		return len(srv.sessions) == 1
	}) {
		t.Fatal("session never registered")
	}

	srv.emitFloatAnnounce(1, 18, 1.0) // phase 1 rad → non-mid value

	if !waitFor(t, time.Second, func() bool { return c.announceCount() >= 1 }) {
		t.Fatal("subscriber got no demo announce")
	}
	ann := c.lastAnnounce()
	if ann.Type != codec.ACP2TypeAnnounce || ann.PID != codec.PIDValue {
		t.Fatalf("announce type=%d pid=%d", ann.Type, ann.PID)
	}
	// Tree value moved off zero.
	e, _ := srv.tree.lookup(1, 18)
	if e.param.Value.(float64) == 0 {
		t.Error("emitFloatAnnounce did not mutate the value")
	}
}

// TestEmitFloatAnnounce_GuardBranches covers the early-returns:
// missing object and wrong object type.
func TestEmitFloatAnnounce_GuardBranches(t *testing.T) {
	srv := newServer(plugin.Deps{Logger: quietLogger()}, buildServeExport())

	// Missing obj-id → silent return, no panic.
	srv.emitFloatAnnounce(1, 9999, 0.5)

	// Obj 10 (Gain) is an integer s32, not Number+Float → guard return.
	srv.emitFloatAnnounce(1, 10, 0.5)

	// Both should leave Gain unchanged at 0.
	e, _ := srv.tree.lookup(1, 10)
	if e.param.Value != int64(0) {
		t.Errorf("non-float obj was mutated: %v", e.param.Value)
	}
}

// TestRunAnnounceDemo_TicksThenCancel runs the demo loop with a short
// interval and confirms at least one tick fired before ctx cancel, then
// that the goroutine returns promptly.
func TestRunAnnounceDemo_TicksThenCancel(t *testing.T) {
	srv, addr, stop := startServe(t, floatDemoExport())
	defer stop()

	c := dialFakeClient(t, addr)
	defer c.close()
	c.handshake(true)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		srv.RunAnnounceDemo(ctx, 1, 18, 5*time.Millisecond)
		close(done)
	}()

	if !waitFor(t, 2*time.Second, func() bool { return c.announceCount() >= 1 }) {
		cancel()
		t.Fatal("demo loop fired no announces")
	}
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("RunAnnounceDemo did not exit after cancel")
	}
}

// TestRunAnnounceDemo_DefaultInterval covers the interval<=0 default
// branch: passing 0 must coerce to a positive ticker and still exit on
// cancel without firing immediately.
func TestRunAnnounceDemo_DefaultInterval(t *testing.T) {
	srv := newServer(plugin.Deps{Logger: quietLogger()}, floatDemoExport())
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		srv.RunAnnounceDemo(ctx, 1, 18, 0) // 0 → default 2s
		close(done)
	}()
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("default-interval demo did not exit after cancel")
	}
}
