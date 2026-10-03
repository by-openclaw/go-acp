package rollcall

import (
	"context"
	"net"
	"sync"
	"testing"
	"time"

	"dhs/internal/clock"
	"dhs/internal/snell-rollcall/session"
)

// firstReadConn notes, at the first read, whether the provider knew the link.
type firstReadConn struct {
	net.Conn
	p          *Provider
	once       sync.Once
	registered int
}

func (c *firstReadConn) Read(b []byte) (int, error) {
	c.once.Do(func() {
		c.p.mu.RLock()
		c.registered = len(c.p.links)
		c.p.mu.RUnlock()
	})
	return c.Conn.Read(b)
}

// TestServeConn_RegistersTheLinkBeforeReading pins the order serveConn works
// in. Every handler looks the link up in p.links; a reader started before
// that entry existed dispatched a client's first GetDevInfo to a handler
// that found nothing and answered nothing, and the client sat through its
// timeout. Measured on a loaded runner (#1240).
//
// With the link registered before Start, the first read cannot precede the
// entry, whatever the scheduler does. The session package pins the other
// half: an idle link reads nothing until started.
func TestServeConn_RegistersTheLinkBeforeReading(t *testing.T) {
	deps := testDeps(clock.NewFake(time.Time{}))
	p := New(deps, nil)
	t.Cleanup(func() { _ = p.Stop() })

	ours, theirs := net.Pipe()
	conn := &firstReadConn{Conn: theirs, p: p}
	p.serveConn(conn)

	cl := session.NewLink(ours, session.Config{}, deps)
	t.Cleanup(func() { _ = cl.Close() })
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, err := cl.Handshake(ctx); err != nil {
		t.Fatalf("handshake: %v", err)
	}
	if conn.registered != 1 {
		t.Fatalf("the provider asked to read with %d links registered, want 1", conn.registered)
	}
}
