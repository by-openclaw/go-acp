package transport

// The dial seam — the thing a connector takes instead of opening its own pipe.
//
// Architecture Principle #2 says connectors take transport, logger, tree and
// clock as constructor parameters. On the LISTEN side that is now true. On the
// DIAL side it was not: every consumer built a net.Dialer inside the function
// that used it, which has two consequences.
//
// The first is duplication. A connector that dials for itself also decides for
// itself whether to set SO_KEEPALIVE or Nagle, and mostly decided not to — the
// same divergence the accept paths had.
//
// The second is that reconnect has nowhere to live. A supervisor that wants to
// re-establish a lost session needs to ask something for a NEW connection, and
// a package-local `var d net.Dialer` is not something you can ask. That is why
// acp2 and cerebrum-nb each grew their own reconnect loop: only the connector
// knew how to make its own pipe. With the dialer injected, reconnect is
// protocol-agnostic — dial, set up, wait for death, back off — and there is
// one of it instead of one per protocol.
//
// Deliberately the same method set as *net.Dialer, so the stdlib type
// satisfies it with no adapter and no behaviour change at any call site that
// keeps the default.

import (
	"context"
	"net"
	"time"
)

// Dialer opens one connection. *net.Dialer satisfies it as-is.
type Dialer interface {
	DialContext(ctx context.Context, network, address string) (net.Conn, error)
}

// TCPDialer is the default Dialer: it dials, then applies the shared
// socket policy — the same ApplySocketOptions the accept paths use, so a
// dialled connection and an accepted one end up with the same options.
type TCPDialer struct {
	// Timeout bounds the connect handshake. 0 leaves it to the context.
	Timeout time.Duration

	// Options are applied to every connection opened. The zero value means
	// keepalive on at DefaultTCPKeepalivePeriod, Nagle untouched.
	Options SocketOptions
}

// DialContext implements Dialer.
//
// A socket option that fails to apply does not fail the dial, for the same
// reason it does not fail an Accept: the connection is usable either way, and
// refusing it would trade a detectable problem for an outright outage.
func (d TCPDialer) DialContext(ctx context.Context, network, address string) (net.Conn, error) {
	nd := net.Dialer{Timeout: d.Timeout}
	conn, err := nd.DialContext(ctx, network, address)
	if err != nil {
		return nil, err
	}
	_ = ApplySocketOptions(conn, d.Options)
	return conn, nil
}
