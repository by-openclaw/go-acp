package rollcall

import (
	"context"
	"testing"

	"dhs/internal/snell-rollcall/codec"
)

// TestReleaseDropsOneNodeNotTheLink covers Release: it terminates the sessions
// to one node and leaves the link and everything else open, and it is
// idempotent. This is the firmware-upgrade precondition — free one card
// without leaving the frame.
func TestReleaseDropsOneNodeNotTheLink(t *testing.T) {
	menu := []codec.MenuItem{{MenuIndex: 0, Style: codec.StyleNumber, Command: 5, Text: "Gain"}}
	h := newHarness(t, func(d *device) { d.setMenu(1, menu) })
	ctx := context.Background()

	// Walk opens a control session, ReadFile a file session — both to slot 1's
	// node, so Release must drop both.
	if _, err := h.plugin.Walk(ctx, 1); err != nil {
		t.Fatalf("Walk: %v", err)
	}
	_, _ = h.plugin.ReadFile(ctx, 1, "any.txt")
	if n := sessionCount(h); n == 0 {
		t.Fatal("no node session open after Walk, nothing to release")
	}

	// Release frees that node. The link stays up.
	if err := h.plugin.Release(ctx, 1); err != nil {
		t.Fatalf("Release: %v", err)
	}
	if sessionCount(h) != 0 {
		t.Errorf("the node session survived Release")
	}
	h.plugin.link.mu.Lock()
	closed := h.plugin.link.closed
	h.plugin.link.mu.Unlock()
	if closed {
		t.Error("Release closed the whole link, want only the node's sessions")
	}

	// Idempotent: releasing a node we now hold nothing on is not an error.
	if err := h.plugin.Release(ctx, 1); err != nil {
		t.Errorf("second Release (nothing held) errored: %v", err)
	}

	// And the link still works — the node can be reached again.
	if _, err := h.plugin.Walk(ctx, 1); err != nil {
		t.Errorf("Walk after Release failed; the link did not survive: %v", err)
	}
}

// sessionCount reports how many node control sessions the link holds.
func sessionCount(h *harness) int {
	l := h.plugin.link
	l.mu.Lock()
	defer l.mu.Unlock()
	return len(l.sessions)
}

// TestReleaseErrors covers Release's two guard paths: a plugin that never
// connected, and a slot outside the address range on one that did.
func TestReleaseErrors(t *testing.T) {
	// Not connected: conn() fails before anything is touched.
	p := New(testDeps())
	if err := p.Release(context.Background(), 1); err == nil {
		t.Error("Release on an unconnected plugin should fail")
	}

	// Connected, but a slot that cannot be an address.
	h := newHarness(t, func(*device) {})
	if err := h.plugin.Release(context.Background(), 0x1FF); err == nil {
		t.Error("Release of an out-of-range slot should fail")
	}
}
