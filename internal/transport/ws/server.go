package ws

// Server-side RFC 6455 upgrade.
//
// This package started life as a client (Dial) because the first connector to
// need WebSocket — Cerebrum NB — only ever consumes. But WebSocket is a
// transport, and transports serve both roles: an NMOS Node SERVES the IS-07
// event stream and the IS-12 control channel, while an NMOS Controller DIALS
// the Query API's subscription socket. Both directions therefore live here, so
// the framing, the masking rules and the idle-deadline watchdog are written
// once instead of once per role.
//
// RFC 6455 §5.3 masking is direction-dependent: a client MUST mask every frame
// it sends, a server MUST NOT. That is the only wire difference between the
// two ends, and it is carried by Conn.clientSide.

import (
	"bufio"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strings"
)

// ErrNotWebSocketUpgrade is returned when the request is not a well-formed
// RFC 6455 upgrade — wrong method, missing Upgrade/Connection tokens, missing
// or malformed Sec-WebSocket-Key, or an unsupported version.
var ErrNotWebSocketUpgrade = errors.New("ws: not a websocket upgrade request")

// ErrHijackUnsupported is returned when the ResponseWriter cannot yield the
// raw connection. WebSocket needs the socket itself; an http.ResponseWriter
// that cannot be hijacked (an HTTP/2 stream, most middleware wrappers) cannot
// carry one.
var ErrHijackUnsupported = errors.New("ws: ResponseWriter does not support Hijack")

// Accept completes the server side of an RFC 6455 handshake and returns the
// resulting connection. The caller must not write to w afterwards — the socket
// belongs to the returned Conn.
//
// The returned Conn does NOT mask its writes, per §5.3.
func Accept(w http.ResponseWriter, r *http.Request, opts *AcceptOptions) (*Conn, error) {
	key, err := upgradeKeyFrom(r)
	if err != nil {
		return nil, err
	}
	hj, ok := w.(http.Hijacker)
	if !ok {
		return nil, ErrHijackUnsupported
	}
	nc, brw, err := hj.Hijack()
	if err != nil {
		return nil, fmt.Errorf("ws: hijack: %w", err)
	}

	resp := "HTTP/1.1 101 Switching Protocols\r\n" +
		"Upgrade: websocket\r\n" +
		"Connection: Upgrade\r\n" +
		"Sec-WebSocket-Accept: " + computeAccept(key) + "\r\n"
	if opts != nil && opts.Subprotocol != "" {
		resp += "Sec-WebSocket-Protocol: " + opts.Subprotocol + "\r\n"
	}
	resp += "\r\n"
	if _, werr := nc.Write([]byte(resp)); werr != nil {
		_ = nc.Close()
		return nil, fmt.Errorf("ws: write handshake response: %w", werr)
	}

	var maxPayload int64
	if opts != nil {
		maxPayload = opts.MaxPayload
	}
	// Reuse the hijacked reader: it may already hold bytes the client sent
	// immediately after the request, and dropping it would lose that frame.
	br := brw.Reader
	if br == nil {
		br = bufio.NewReader(nc)
	}
	c := newConn(nc, br, maxPayload)
	c.clientSide = false
	return c, nil
}

// AcceptOptions tunes the server-side upgrade. The zero value is valid.
type AcceptOptions struct {
	// Subprotocol, when set, is echoed in Sec-WebSocket-Protocol.
	Subprotocol string
	// MaxPayload caps an inbound frame. 0 = DefaultMaxPayload.
	MaxPayload int64
}

// upgradeKeyFrom validates an inbound upgrade request and returns its
// Sec-WebSocket-Key. Every failure is ErrNotWebSocketUpgrade so a caller can
// answer 400 without inspecting the reason.
func upgradeKeyFrom(r *http.Request) (string, error) {
	if r == nil || r.Method != http.MethodGet {
		return "", ErrNotWebSocketUpgrade
	}
	if !headerHasToken(r.Header.Get("Connection"), "upgrade") {
		return "", ErrNotWebSocketUpgrade
	}
	if !strings.EqualFold(strings.TrimSpace(r.Header.Get("Upgrade")), "websocket") {
		return "", ErrNotWebSocketUpgrade
	}
	if v := strings.TrimSpace(r.Header.Get("Sec-WebSocket-Version")); v != "" && v != "13" {
		return "", ErrNotWebSocketUpgrade
	}
	key := strings.TrimSpace(r.Header.Get("Sec-WebSocket-Key"))
	if key == "" {
		return "", ErrNotWebSocketUpgrade
	}
	return key, nil
}

// hijackedConn exposes the underlying transport for callers that need the
// peer address (access logs, per-peer state).
func (c *Conn) netConn() net.Conn { return c.c }
