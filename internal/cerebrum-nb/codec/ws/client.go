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

	// Dialer overrides the underlying net.Dialer (useful for tests).
	Dialer *net.Dialer
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

	nc := net.Conn(rawConn)
	if u.Scheme == "wss" {
		cfg := opts.TLSConfig
		if cfg == nil {
			cfg = &tls.Config{ServerName: u.Hostname()}
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
