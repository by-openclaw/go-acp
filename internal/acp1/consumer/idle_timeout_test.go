package acp1

import (
	"testing"
	"time"
)

// SetIdleTimeout guards the per-read deadline that makes a half-open TCP
// session detectable. Negative input must disable rather than arm a deadline
// in the past.
func TestTCPClientSetIdleTimeout(t *testing.T) {
	c := &TCPClient{}
	if got := c.IdleTimeout(); got != 0 {
		t.Fatalf("IdleTimeout = %v on a fresh client, want 0", got)
	}
	c.SetIdleTimeout(90 * time.Second)
	if got := c.IdleTimeout(); got != 90*time.Second {
		t.Fatalf("IdleTimeout = %v, want 90s", got)
	}
	c.SetIdleTimeout(0)
	if got := c.IdleTimeout(); got != 0 {
		t.Fatalf("IdleTimeout = %v after disable, want 0", got)
	}
	c.SetIdleTimeout(-time.Second)
	if got := c.IdleTimeout(); got != 0 {
		t.Fatalf("IdleTimeout = %v for negative input, want 0 (disabled)", got)
	}
}
