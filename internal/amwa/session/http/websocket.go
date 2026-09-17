// WebSocket for the NMOS session layer.
//
// This used to be a second, independent RFC 6455 implementation: its own
// handshake, its own frame writer and reader, its own masking rules. It is now
// a thin adapter over internal/transport/ws, which is the one WebSocket in the
// tree.
//
// Why that matters beyond tidiness: NMOS uses WebSocket in BOTH directions —
// a Node SERVES the IS-07 event stream and the IS-12 control channel, while a
// Controller DIALS the Query API's subscription socket. With two
// implementations, every transport-level fix had to be made twice and, in
// practice, was not: the half-open-connection watchdog (a per-frame idle
// deadline, re-armed on every frame so Pongs count as liveness) landed in one
// of them first. Sharing the transport means a 24/7 fix lands once for every
// connector that speaks WebSocket.
//
// The exported API here is unchanged, so the ~25 call sites across the
// provider, registry and consumer keep working untouched.

package http

import (
	"context"
	"errors"
	"io"
	stdhttp "net/http"
	"sync/atomic"
	"time"

	"dhs/internal/transport/ws"
)

// wsMaxPayload caps a single inbound frame. IS-04 subscription envelopes are
// in the kilobytes range; 1 MiB is comfortable.
const wsMaxPayload = 1 << 20

// wsNormalClosure is RFC 6455 §7.4.1 status code 1000.
const wsNormalClosure = 1000

// ErrWebSocketClosed is returned by ReadText / SendText / SendPing once the
// peer has closed or Close has been called.
var ErrWebSocketClosed = errors.New("nmos/http: websocket closed")

// WebSocket is text-frame I/O over one RFC 6455 connection, in either role.
// Accepted connections do not mask; dialled ones do — the transport handles
// that distinction.
type WebSocket struct {
	c *ws.Conn

	// closed preserves this package's contract that operations after a close
	// report ErrWebSocketClosed rather than a raw transport error.
	closed atomic.Bool
}

// AcceptWebSocket completes the server-side handshake on r and returns a
// WebSocket. Must be called from inside an http.Handler — the ResponseWriter
// must implement http.Hijacker, which net/http does for HTTP/1.1.
func AcceptWebSocket(w stdhttp.ResponseWriter, r *stdhttp.Request) (*WebSocket, error) {
	c, err := ws.Accept(w, r, &ws.AcceptOptions{MaxPayload: wsMaxPayload})
	if err != nil {
		return nil, err
	}
	return &WebSocket{c: c}, nil
}

// SetIdleTimeout arms (d > 0) or disables (d <= 0) the per-frame read
// deadline that detects a half-open connection. Re-armed on every inbound
// frame, so a peer answering our pings is never torn down by it.
func (w *WebSocket) SetIdleTimeout(d time.Duration) { w.c.SetIdleTimeout(d) }

// IdleTimeout reports the currently armed per-frame read deadline.
func (w *WebSocket) IdleTimeout() time.Duration { return w.c.IdleTimeout() }

// SendText emits a single unfragmented text frame.
func (w *WebSocket) SendText(payload []byte) error {
	if w.closed.Load() {
		return ErrWebSocketClosed
	}
	return w.c.WriteText(context.Background(), payload)
}

// SendPing emits a Ping; a conformant peer answers with Pong. On a quiet
// subscription this is the only thing that distinguishes idle from dead.
func (w *WebSocket) SendPing(payload []byte) error {
	if w.closed.Load() {
		return ErrWebSocketClosed
	}
	return w.c.Ping(context.Background(), payload)
}

// Close emits a Close frame and tears down the connection. Idempotent.
func (w *WebSocket) Close() error {
	if w.closed.Swap(true) {
		return nil
	}
	return w.c.Close(wsNormalClosure, "")
}

// ReadText blocks until a text frame arrives, returning its payload. Ping
// frames are answered with Pong by the transport and never surfaced; Binary
// frames are skipped (IS-04/IS-07/IS-12 are text-only); a Close frame — or a
// local Close — yields ErrWebSocketClosed.
func (w *WebSocket) ReadText() ([]byte, error) {
	for {
		if w.closed.Load() {
			return nil, ErrWebSocketClosed
		}
		opcode, payload, err := w.c.ReadMessage(context.Background())
		if err != nil {
			if errors.Is(err, io.EOF) {
				w.closed.Store(true)
				return nil, ErrWebSocketClosed
			}
			return nil, err
		}
		if opcode == ws.OpText {
			return payload, nil
		}
		// Binary: allowed by the spec, unused by NMOS. Keep reading.
	}
}

// SetReadDeadline applies a one-shot deadline to the underlying connection.
// Prefer SetIdleTimeout for a rolling liveness bound.
func (w *WebSocket) SetReadDeadline(t time.Time) error { return w.c.SetReadDeadline(t) }
