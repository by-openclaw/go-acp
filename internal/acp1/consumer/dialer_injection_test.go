package acp1

// The payoff test for the dial seam: connectAN2 opens whatever the injected
// dialer hands it, with no socket, no port and no listener anywhere.
//
// Before the seam this was untestable — connectAN2 built its own net.Dialer
// and immediately type-asserted the result to *net.TCPConn, so the only way
// to exercise it was to stand up a real listener.

import (
	"context"
	"errors"
	"io"
	"net"
	"testing"

	"dhs/internal/transport"
)

// fakeDialer hands back a canned connection instead of opening a socket.
type fakeDialer struct {
	conn    net.Conn
	err     error
	network string
	address string
}

func (d *fakeDialer) DialContext(_ context.Context, network, address string) (net.Conn, error) {
	d.network, d.address = network, address
	if d.err != nil {
		return nil, d.err
	}
	return d.conn, nil
}

func TestConnectAN2UsesTheInjectedDialer(t *testing.T) {
	ours, theirs := net.Pipe()
	// The AN2 client sends EnableProtocolEvents as soon as it starts, so
	// the far end has to read or that write blocks forever.
	go func() { _, _ = io.Copy(io.Discard, theirs) }()
	t.Cleanup(func() { _ = theirs.Close() })

	d := &fakeDialer{conn: ours}
	p := &Plugin{logger: discardLogger(), dialer: d}

	if err := p.connectAN2(context.Background(), "10.6.239.113", 2072); err != nil {
		t.Fatalf("connectAN2: %v", err)
	}
	t.Cleanup(func() { _ = p.client.Close() })

	if d.network != "tcp4" {
		t.Errorf("dialed network %q, want tcp4", d.network)
	}
	if d.address != "10.6.239.113:2072" {
		t.Errorf("dialed address %q, want 10.6.239.113:2072", d.address)
	}

	c, ok := p.client.(*AN2Client)
	if !ok {
		t.Fatalf("plugin client is %T, want *AN2Client", p.client)
	}
	if c.conn != ours {
		t.Error("AN2Client is not using the connection the dialer returned")
	}
}

// A dialer that refuses is reported as a TransportError, same as a refused
// socket was.
func TestConnectAN2ReportsInjectedDialerFailure(t *testing.T) {
	d := &fakeDialer{err: errors.New("no route")}
	p := &Plugin{logger: discardLogger(), dialer: d}

	err := p.connectAN2(context.Background(), "10.6.239.113", 2072)
	if err == nil {
		t.Fatal("connectAN2 succeeded with a failing dialer")
	}
	if p.client != nil {
		t.Error("a failed dial left a client behind")
	}
}

// With nothing injected the plugin uses the shared default, which keeps
// Nagle off for ACP1's small frames.
func TestPluginDialDefaults(t *testing.T) {
	p := &Plugin{}
	td, ok := p.dial().(transport.TCPDialer)
	if !ok {
		t.Fatalf("dial() = %T, want transport.TCPDialer", p.dial())
	}
	if !td.Options.NoDelay {
		t.Error("the default dialer left Nagle on")
	}

	injected := &fakeDialer{}
	p.dialer = injected
	if p.dial() != transport.Dialer(injected) {
		t.Error("dial() replaced an injected dialer")
	}
}
