package ws

import (
	"bufio"
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"time"
)

// DialOptions configures a Dial call.
type DialOptions struct {
	// TLSConfig controls the TLS handshake when the URL scheme is wss://.
	// nil means use a default *tls.Config (verifies certificates).
	TLSConfig *tls.Config

	// Header carries extra HTTP headers to merge into the upgrade
	// request (e.g. Authorization). The required RFC 6455 set is
	// always emitted; entries here are added on top.
	Header http.Header

	// MaxPayload caps incoming frame size. 0 means DefaultMaxPayload.
	MaxPayload int64

	// Dialer opens the TCP connection the upgrade runs over. nil means a
	// plain net.Dialer.
	Dialer Dialer
}

// Dialer opens one connection. *net.Dialer satisfies it, and so does
// transport.TCPDialer — the interface is declared here rather than imported
// because this package stays stdlib-only so it remains lift-ready (doc.go).
// Go interfaces are structural, so the two are interchangeable at the call
// site without either package knowing about the other.
//
// It was *net.Dialer, which meant the only substitution possible was another
// real dialer: you could change the timeout but you could not hand the
// upgrade a pipe. The handshake was therefore only testable against a live
// listener.
type Dialer interface {
	DialContext(ctx context.Context, network, address string) (net.Conn, error)
}

// Dial connects to urlStr (ws:// or wss://), performs the RFC 6455
// upgrade, and returns a ready-to-use *Conn.
func Dial(ctx context.Context, urlStr string, opts *DialOptions) (*Conn, error) {
	if opts == nil {
		opts = &DialOptions{}
	}
	u, err := url.Parse(urlStr)
	if err != nil {
		return nil, fmt.Errorf("ws: parse url: %w", err)
	}
	host := u.Host
	if u.Port() == "" {
		switch u.Scheme {
		case "ws":
			host = net.JoinHostPort(u.Hostname(), "80")
		case "wss":
			host = net.JoinHostPort(u.Hostname(), "443")
		default:
			return nil, fmt.Errorf("ws: unsupported scheme %q", u.Scheme)
		}
		u.Host = host
	}

	dialer := opts.Dialer
	if dialer == nil {
		dialer = &net.Dialer{}
	}
	rawConn, err := dialer.DialContext(ctx, "tcp", host)
	if err != nil {
		return nil, fmt.Errorf("ws: tcp dial: %w", err)
	}

	nc := rawConn
	if u.Scheme == "wss" {
		cfg := opts.TLSConfig
		if cfg == nil {
			// The floor matches transport.MinTLSVersion. It is spelled out
			// here rather than imported because this package stays
			// stdlib-only (see doc.go) — a caller that wants the shared
			// posture builds its config with transport.TLSOptions.
			cfg = &tls.Config{
				MinVersion: tls.VersionTLS12,
				ServerName: u.Hostname(),
			}
		} else if cfg.ServerName == "" {
			cfg = cfg.Clone()
			cfg.ServerName = u.Hostname()
		}
		tlsConn := tls.Client(rawConn, cfg)
		if err := tlsConn.HandshakeContext(ctx); err != nil {
			_ = rawConn.Close()
			return nil, fmt.Errorf("ws: tls handshake: %w", err)
		}
		nc = tlsConn
	}

	if d, ok := ctx.Deadline(); ok {
		_ = nc.SetDeadline(d)
	}
	key, err := upgradeRequest(nc, u, opts.Header)
	if err != nil {
		_ = nc.Close()
		return nil, fmt.Errorf("ws: write upgrade: %w", err)
	}
	br := bufio.NewReader(nc)
	if err := readUpgradeResponse(br, key); err != nil {
		_ = nc.Close()
		return nil, err
	}
	// Clear the dial deadline; per-call deadlines take over.
	_ = nc.SetDeadline(time.Time{})

	return newConn(nc, br, opts.MaxPayload), nil
}
