package http

import (
	"bufio"
	"context"
	"crypto/rand"
	"crypto/tls"
	"encoding/base64"
	"errors"
	"fmt"
	"net"
	stdhttp "net/http"
	"net/url"
	"strings"
	"time"
)

// DialWebSocket opens a client-side RFC 6455 connection to wsURL
// (`ws://...` or `wss://...`) and completes the upgrade handshake.
// Returns a WebSocket whose outgoing frames are masked per spec.
//
// `extra` headers are sent verbatim alongside the mandatory upgrade
// headers — useful for `Authorization`, `Origin`, etc. Pass nil if
// none.
//
// The returned *WebSocket is ready for SendText / ReadText. The
// caller owns Close.
func DialWebSocket(ctx context.Context, wsURL string, extra stdhttp.Header) (*WebSocket, error) {
	u, err := url.Parse(wsURL)
	if err != nil {
		return nil, fmt.Errorf("nmos/http: parse ws url %q: %w", wsURL, err)
	}
	var defaultPort string
	var useTLS bool
	switch strings.ToLower(u.Scheme) {
	case "ws":
		defaultPort = "80"
	case "wss":
		defaultPort = "443"
		useTLS = true
	default:
		return nil, fmt.Errorf("nmos/http: ws url %q: scheme must be ws or wss", wsURL)
	}
	host := u.Hostname()
	port := u.Port()
	if port == "" {
		port = defaultPort
	}

	dialer := &net.Dialer{Timeout: 10 * time.Second}
	if dl, ok := ctx.Deadline(); ok {
		dialer.Deadline = dl
	}
	addr := net.JoinHostPort(host, port)
	var conn net.Conn
	if useTLS {
		conn, err = tls.DialWithDialer(dialer, "tcp", addr, &tls.Config{ServerName: host})
	} else {
		conn, err = dialer.DialContext(ctx, "tcp", addr)
	}
	if err != nil {
		return nil, fmt.Errorf("nmos/http: dial %s: %w", addr, err)
	}
	if dl, ok := ctx.Deadline(); ok {
		_ = conn.SetDeadline(dl)
	}

	var keyBytes [16]byte
	if _, err := rand.Read(keyBytes[:]); err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("nmos/http: rand key: %w", err)
	}
	secKey := base64.StdEncoding.EncodeToString(keyBytes[:])

	path := u.RequestURI()
	if path == "" {
		path = "/"
	}

	var b strings.Builder
	fmt.Fprintf(&b, "GET %s HTTP/1.1\r\n", path)
	fmt.Fprintf(&b, "Host: %s\r\n", u.Host)
	b.WriteString("Upgrade: websocket\r\n")
	b.WriteString("Connection: Upgrade\r\n")
	fmt.Fprintf(&b, "Sec-WebSocket-Key: %s\r\n", secKey)
	b.WriteString("Sec-WebSocket-Version: 13\r\n")
	for k, vs := range extra {
		for _, v := range vs {
			fmt.Fprintf(&b, "%s: %s\r\n", k, v)
		}
	}
	b.WriteString("\r\n")

	if _, err := conn.Write([]byte(b.String())); err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("nmos/http: write upgrade: %w", err)
	}

	bufr := bufio.NewReader(conn)
	resp, err := stdhttp.ReadResponse(bufr, &stdhttp.Request{Method: "GET"})
	if err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("nmos/http: read upgrade response: %w", err)
	}
	if resp.StatusCode != stdhttp.StatusSwitchingProtocols {
		_ = conn.Close()
		return nil, fmt.Errorf("nmos/http: ws handshake: status %d", resp.StatusCode)
	}
	if !strings.EqualFold(resp.Header.Get("Upgrade"), "websocket") {
		_ = conn.Close()
		return nil, errors.New("nmos/http: ws handshake: missing Upgrade: websocket")
	}
	wantAccept := wsAcceptKey(secKey)
	if got := resp.Header.Get("Sec-WebSocket-Accept"); got != wantAccept {
		_ = conn.Close()
		return nil, fmt.Errorf("nmos/http: ws handshake: bad Sec-WebSocket-Accept %q", got)
	}

	// Clear deadlines we set on the connection during the handshake.
	_ = conn.SetDeadline(time.Time{})

	return &WebSocket{conn: conn, bufr: bufr, clientSide: true}, nil
}
