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
	"crypto/tls"
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

// TLSDialer completes a TLS handshake on top of the connection its Base
// opens, using the shared TLSOptions so every dhs client negotiates the same
// posture (TLS 1.2 floor, RootCAs, client certs). It is the dial-side
// counterpart of stdNet.Listen's tls.NewListener — TLS decided in one place,
// not per connector — and the seam a connector injects to speak a "…s"
// variant of its protocol (mqtts, etc.) over the same Dialer interface.
type TLSDialer struct {
	// Base opens the raw connection. Nil ⇒ TCPDialer{} (default socket policy).
	Base Dialer

	// TLS is the posture to negotiate. A zero value (Enable false) makes this
	// dialer plaintext — it returns Base's connection untouched — so a caller
	// can hand it an unconditional TLSDialer and let the options decide.
	TLS TLSOptions
}

// DialContext implements Dialer: it dials through Base, then runs the TLS
// handshake bounded by ctx. When TLSOptions leaves ServerName empty it is
// filled from the dialled host, so certificate verification has a name to
// check without the caller repeating the address.
func (d TLSDialer) DialContext(ctx context.Context, network, address string) (net.Conn, error) {
	base := d.Base
	if base == nil {
		base = TCPDialer{}
	}
	raw, err := base.DialContext(ctx, network, address)
	if err != nil {
		return nil, err
	}

	cfg, err := d.TLS.Client()
	if err != nil {
		_ = raw.Close()
		return nil, err
	}
	if cfg == nil {
		// TLS not enabled: a TLSDialer with Enable=false is plaintext by
		// definition, so return the raw connection rather than fail.
		return raw, nil
	}
	if cfg.ServerName == "" && !cfg.InsecureSkipVerify {
		if host, _, splitErr := net.SplitHostPort(address); splitErr == nil {
			cfg = cfg.Clone()
			cfg.ServerName = host
		}
	}

	tc := tls.Client(raw, cfg)
	if err := tc.HandshakeContext(ctx); err != nil {
		_ = raw.Close()
		return nil, err
	}
	return tc, nil
}
