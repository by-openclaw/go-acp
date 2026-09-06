package ws

import (
	"context"
	"errors"
	"net"
	"strings"
	"testing"
	"time"
)

// TestDial_UpgradeRequestError covers Dial's upgradeRequest-failure arm
// (close the dialed conn, wrap as "ws: write upgrade") deterministically:
// the rand seam fails the nonce read, so upgradeRequest errors before a
// single byte hits the wire — no TCP reset race involved. Previously
// this branch was only hit when a peer happened to reset the socket
// mid-upgrade (#694 coverage class: single-run statement coverage must
// not depend on scheduling).
func TestDial_UpgradeRequestError(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer func() { _ = ln.Close() }()
	// No Accept needed: the TCP connect completes via the kernel backlog,
	// and Dial fails before the upgrade request is written.

	defer setRandRead(func(b []byte) (int, error) {
		return 0, errors.New("rand broken")
	})()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_, err = Dial(ctx, "ws://"+ln.Addr().String(), nil)
	if err == nil || !strings.Contains(err.Error(), "write upgrade") {
		t.Fatalf("Dial with failing rand: got %v, want 'ws: write upgrade' error", err)
	}
}
