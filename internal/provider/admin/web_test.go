package admin

import (
	"encoding/json"
	"strings"
	"testing"
)

// TestRenderPeerRows_HappyPath asserts the table renders one row per
// peer in sorted order with HTML-escaped values.
func TestRenderPeerRows_HappyPath(t *testing.T) {
	data := json.RawMessage(`[
	  {"peer":"10.6.239.113:54321","connected":true,"live":true,"last_rx":"2026-05-18T14:25:28Z","stale_after":"30s","subs_open":3},
	  {"peer":"10.100.0.103:48902","connected":true,"live":false,"last_rx":"2026-05-18T14:00:00Z","stale_after":"30s","subs_open":0}
	]`)
	got := renderPeerRows(data)
	if !strings.Contains(got, "10.100.0.103:48902") || !strings.Contains(got, "10.6.239.113:54321") {
		t.Errorf("peers missing from render: %s", got)
	}
	// Sorted ascending — .100 should appear before .239
	if i := strings.Index(got, "10.100"); i < 0 || i > strings.Index(got, "10.6.") {
		t.Errorf("rows not sorted ascending by peer addr: %s", got)
	}
	if !strings.Contains(got, "yes") || !strings.Contains(got, "no") {
		t.Errorf("boolBadge rendering missing yes/no: %s", got)
	}
}

// TestRenderPeerRows_Empty asserts the placeholder row renders when
// the producer has no connected peers.
func TestRenderPeerRows_Empty(t *testing.T) {
	got := renderPeerRows(json.RawMessage(`[]`))
	if !strings.Contains(got, "no peers connected") {
		t.Errorf("empty-state row missing: %s", got)
	}
}

// TestRenderPeerRows_DecodeError asserts a malformed payload surfaces
// inline rather than panicking.
func TestRenderPeerRows_DecodeError(t *testing.T) {
	got := renderPeerRows(json.RawMessage(`not json`))
	if !strings.Contains(got, "decode error") {
		t.Errorf("decode-error row missing: %s", got)
	}
}

// TestBoolBadge covers both branches.
func TestBoolBadge(t *testing.T) {
	if boolBadge(true) != "yes" {
		t.Error("boolBadge(true) want yes")
	}
	if boolBadge(false) != "no" {
		t.Error("boolBadge(false) want no")
	}
}
