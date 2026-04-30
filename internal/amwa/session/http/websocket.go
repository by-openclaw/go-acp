// Hand-rolled minimal RFC 6455 WebSocket server. Stdlib-only —
// `crypto/sha1`, `encoding/base64`, `bufio`, `io`, `net`, `net/http`.
// We implement only what IS-04 §5.2 needs:
//
//   - HTTP/1.1 Upgrade handshake (server side).
//   - Send text frames (server emits unmasked).
//   - Receive text frames (client masks per RFC 6455 §5.3).
//   - Ping/Pong (server replies to ping).
//   - Close frame.
//
// No fragmentation, no compression extensions. The Registry's
// subscription stream sends one IS-04 grain envelope per text frame.

package http

import (
	"bufio"
	"crypto/sha1"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	stdhttp "net/http"
	"strings"
	"sync"
	"time"
)

// websocketGUID is the magic constant from RFC 6455 §1.3.
const websocketGUID = "258EAFA5-E914-47DA-95CA-C5AB0DC85B11"

// WebSocket opcode constants (RFC 6455 §5.2).
const (
	wsOpcodeContinuation byte = 0x0
	wsOpcodeText         byte = 0x1
	wsOpcodeBinary       byte = 0x2
	wsOpcodeClose        byte = 0x8
	wsOpcodePing         byte = 0x9
	wsOpcodePong         byte = 0xA
)

// WebSocket caps payload size (per-frame). Subscription envelopes are
// in the kilobytes range; 1 MiB is comfortable.
const wsMaxPayload = 1 << 20

// ErrWebSocketClosed is returned by ReadText after the peer closes.
var ErrWebSocketClosed = errors.New("nmos/http: websocket closed")

// WebSocket wraps a hijacked TCP connection and provides text-frame
// I/O.
type WebSocket struct {
	conn   net.Conn
	bufr   *bufio.Reader
	mu     sync.Mutex
	closed bool
}

// AcceptWebSocket completes the RFC 6455 handshake on r and returns a
// WebSocket. Must be called from inside an http.Handler — the
// underlying http.ResponseWriter must implement http.Hijacker, which
// the stdlib net/http does for HTTP/1.1.
func AcceptWebSocket(w stdhttp.ResponseWriter, r *stdhttp.Request) (*WebSocket, error) {
	if !strings.EqualFold(r.Header.Get("Upgrade"), "websocket") {
		return nil, errors.New("nmos/http: missing Upgrade: websocket header")
	}
	connHeader := strings.ToLower(r.Header.Get("Connection"))
	if !strings.Contains(connHeader, "upgrade") {
		return nil, errors.New("nmos/http: missing `Upgrade` token in Connection header")
	}
	key := r.Header.Get("Sec-WebSocket-Key")
	if key == "" {
		return nil, errors.New("nmos/http: missing Sec-WebSocket-Key")
	}

	hijacker, ok := w.(stdhttp.Hijacker)
	if !ok {
		return nil, errors.New("nmos/http: ResponseWriter does not support Hijack")
	}
	conn, brw, err := hijacker.Hijack()
	if err != nil {
		return nil, fmt.Errorf("nmos/http: hijack: %w", err)
	}

	accept := wsAcceptKey(key)
	resp := "HTTP/1.1 101 Switching Protocols\r\n" +
		"Upgrade: websocket\r\n" +
		"Connection: Upgrade\r\n" +
		"Sec-WebSocket-Accept: " + accept + "\r\n\r\n"
	if _, err := conn.Write([]byte(resp)); err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("nmos/http: write upgrade: %w", err)
	}
	return &WebSocket{conn: conn, bufr: brw.Reader}, nil
}

// wsAcceptKey computes Sec-WebSocket-Accept per RFC 6455 §4.2.2.
func wsAcceptKey(secKey string) string {
	h := sha1.New()
	h.Write([]byte(secKey + websocketGUID))
	return base64.StdEncoding.EncodeToString(h.Sum(nil))
}

// SendText emits a single unfragmented text frame. Caller-side
// concurrency is serialised via the WebSocket's mutex.
func (w *WebSocket) SendText(payload []byte) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.closed {
		return ErrWebSocketClosed
	}
	return writeFrame(w.conn, wsOpcodeText, payload)
}

// SendPing emits a Ping frame; the peer should reply with Pong.
func (w *WebSocket) SendPing(payload []byte) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.closed {
		return ErrWebSocketClosed
	}
	return writeFrame(w.conn, wsOpcodePing, payload)
}

// Close emits a Close frame and tears down the underlying connection.
// Idempotent.
func (w *WebSocket) Close() error {
	w.mu.Lock()
	if w.closed {
		w.mu.Unlock()
		return nil
	}
	w.closed = true
	// Write Close frame (best-effort), then close TCP.
	_ = writeFrame(w.conn, wsOpcodeClose, []byte{0x03, 0xE8}) // 1000 = normal closure
	w.mu.Unlock()
	return w.conn.Close()
}

// ReadText blocks until a text frame arrives, returning its payload.
// Ping frames trigger an automatic Pong reply and are not surfaced.
// Close frames return ErrWebSocketClosed.
func (w *WebSocket) ReadText() ([]byte, error) {
	for {
		if w.closed {
			return nil, ErrWebSocketClosed
		}
		opcode, payload, err := readFrame(w.bufr)
		if err != nil {
			return nil, err
		}
		switch opcode {
		case wsOpcodeText:
			return payload, nil
		case wsOpcodeBinary:
			// Spec allows binary; IS-04 only uses text. Loop, ignore.
			continue
		case wsOpcodePing:
			w.mu.Lock()
			_ = writeFrame(w.conn, wsOpcodePong, payload)
			w.mu.Unlock()
		case wsOpcodePong:
			// We don't track pongs; just drop.
		case wsOpcodeClose:
			_ = w.Close()
			return nil, ErrWebSocketClosed
		}
	}
}

// SetReadDeadline applies a deadline to the underlying connection;
// useful for a client-disconnect detection loop.
func (w *WebSocket) SetReadDeadline(t time.Time) error {
	return w.conn.SetReadDeadline(t)
}

// writeFrame emits a single unmasked frame. Per RFC 6455 §5.3,
// servers MUST NOT mask.
func writeFrame(w io.Writer, opcode byte, payload []byte) error {
	hdr := make([]byte, 0, 10)
	hdr = append(hdr, 0x80|opcode) // FIN=1, RSV=0, opcode

	plen := len(payload)
	switch {
	case plen <= 125:
		hdr = append(hdr, byte(plen))
	case plen <= 65535:
		hdr = append(hdr, 126)
		var ext [2]byte
		binary.BigEndian.PutUint16(ext[:], uint16(plen))
		hdr = append(hdr, ext[:]...)
	default:
		hdr = append(hdr, 127)
		var ext [8]byte
		binary.BigEndian.PutUint64(ext[:], uint64(plen))
		hdr = append(hdr, ext[:]...)
	}
	if _, err := w.Write(hdr); err != nil {
		return err
	}
	if _, err := w.Write(payload); err != nil {
		return err
	}
	return nil
}

// readFrame reads one unfragmented frame from r, unmasking the
// payload (clients always mask). Returns opcode + payload.
func readFrame(r *bufio.Reader) (byte, []byte, error) {
	hdr := make([]byte, 2)
	if _, err := io.ReadFull(r, hdr); err != nil {
		return 0, nil, err
	}
	fin := hdr[0]&0x80 != 0
	opcode := hdr[0] & 0x0F
	masked := hdr[1]&0x80 != 0
	plen := int(hdr[1] & 0x7F)
	switch plen {
	case 126:
		var ext [2]byte
		if _, err := io.ReadFull(r, ext[:]); err != nil {
			return 0, nil, err
		}
		plen = int(binary.BigEndian.Uint16(ext[:]))
	case 127:
		var ext [8]byte
		if _, err := io.ReadFull(r, ext[:]); err != nil {
			return 0, nil, err
		}
		plen = int(binary.BigEndian.Uint64(ext[:]))
	}
	if plen > wsMaxPayload {
		return 0, nil, fmt.Errorf("nmos/http: frame payload %d exceeds %d", plen, wsMaxPayload)
	}

	var maskKey [4]byte
	if masked {
		if _, err := io.ReadFull(r, maskKey[:]); err != nil {
			return 0, nil, err
		}
	}

	payload := make([]byte, plen)
	if plen > 0 {
		if _, err := io.ReadFull(r, payload); err != nil {
			return 0, nil, err
		}
	}
	if masked {
		for i := range payload {
			payload[i] ^= maskKey[i%4]
		}
	}
	if !fin {
		// We don't support fragmented messages — IS-04 envelopes are
		// small enough to fit in one frame, and pulling apart
		// fragmentation handling here is scope-creep. Reject.
		return 0, nil, errors.New("nmos/http: fragmented frames unsupported")
	}
	return opcode, payload, nil
}
