package provider

import (
	"context"
	"errors"
	"net"
	"testing"

	"dhs/internal/plugin"
)

// dialNet is a Net whose Dial hands back one end of a pipe and records the
// request, so a test proves Base.Dial goes through the injected transport.
type dialNet struct {
	stoppingNet
	network, addr string
	conn          net.Conn
}

func (n *dialNet) Dial(_ context.Context, network, addr string) (net.Conn, error) {
	n.network, n.addr = network, addr
	c, s := net.Pipe()
	_ = s.Close()
	n.conn = c
	return c, nil
}

// Dial is the push providers' socket path: it goes through the injected
// Net (so the process owns keepalive/TLS/source address and a test
// substitutes a fake), refuses to open sockets once Stop has run, and a
// zero Base falls back to the default transport rather than panicking.
func TestDialThroughInjectedNet(t *testing.T) {
	fake := &dialNet{}
	var b Base[*NoConn]
	b.Init(plugin.Deps{Net: fake})
	c, err := b.Dial(context.Background(), "tcp", "10.0.0.1:9000")
	if err != nil || c != fake.conn {
		t.Fatalf("Dial = %v, %v; want the injected Net's conn", c, err)
	}
	if fake.network != "tcp" || fake.addr != "10.0.0.1:9000" {
		t.Errorf("injected Net saw %s %s", fake.network, fake.addr)
	}
	_ = c.Close()

	_ = b.Stop()
	if _, err := b.Dial(context.Background(), "tcp", "10.0.0.1:9000"); !errors.Is(err, net.ErrClosed) {
		t.Errorf("Dial after Stop = %v, want net.ErrClosed", err)
	}

	var zero Base[*NoConn]
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := zero.Dial(ctx, "tcp", "127.0.0.1:1"); err == nil {
		t.Error("a zero Base must dial through the default transport (and fail on a cancelled ctx), not panic")
	}
}
