package http

// Client-side WebSocket dial for the NMOS Controller role — the Query API
// subscription socket (IS-04 §5.2) and the IS-07 event subscriber.
//
// Like its server-side sibling this is now a thin adapter over
// internal/transport/ws rather than a second handshake implementation. The
// dialled connection masks its frames per RFC 6455 §5.3; the transport owns
// that rule so neither side can get it wrong independently.

import (
	"context"
	"fmt"
	stdhttp "net/http"
	"strings"

	"dhs/internal/transport/ws"
)

// DialWebSocket opens a client-side RFC 6455 connection to wsURL (`ws://…` or
// `wss://…`) and completes the upgrade handshake.
//
// `extra` headers are sent verbatim alongside the mandatory upgrade headers —
// useful for Authorization (IS-10 / BCP-003-02) or Origin. Pass nil if none.
//
// The returned *WebSocket is ready for SendText / ReadText; the caller owns
// Close.
func DialWebSocket(ctx context.Context, wsURL string, extra stdhttp.Header) (*WebSocket, error) {
	switch {
	case strings.HasPrefix(strings.ToLower(wsURL), "ws://"),
		strings.HasPrefix(strings.ToLower(wsURL), "wss://"):
	default:
		return nil, fmt.Errorf("nmos/http: ws url %q: scheme must be ws or wss", wsURL)
	}

	c, err := ws.Dial(ctx, wsURL, &ws.DialOptions{
		Header:     extra,
		MaxPayload: wsMaxPayload,
	})
	if err != nil {
		return nil, fmt.Errorf("nmos/http: dial %s: %w", wsURL, err)
	}
	return &WebSocket{c: c}, nil
}
