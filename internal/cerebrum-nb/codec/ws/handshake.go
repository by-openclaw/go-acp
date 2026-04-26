package ws

import (
	"bufio"
	"crypto/rand"
	"crypto/sha1"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

// rfc6455GUID is the constant from RFC 6455 §1.3 used to derive the
// Sec-WebSocket-Accept hash.
const rfc6455GUID = "258EAFA5-E914-47DA-95CA-C5AB0DC85B11"

// upgradeRequest writes the RFC 6455 §4.1 client handshake to w and
// returns the random Sec-WebSocket-Key used (so the caller can verify
// the server's Accept). u must already have host/port resolved.
//
// Cerebrum doesn't use a URL path; the resource name we emit is "/".
// extraHeaders are merged on top of the required RFC 6455 set.
func upgradeRequest(w io.Writer, u *url.URL, extraHeaders http.Header) (key string, err error) {
	var nonce [16]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return "", err
	}
	key = base64.StdEncoding.EncodeToString(nonce[:])

	resource := u.RequestURI()
	if resource == "" || resource == "*" {
		resource = "/"
	}
	host := u.Host

	var b strings.Builder
	fmt.Fprintf(&b, "GET %s HTTP/1.1\r\n", resource)
	fmt.Fprintf(&b, "Host: %s\r\n", host)
	b.WriteString("Upgrade: websocket\r\n")
	b.WriteString("Connection: Upgrade\r\n")
	fmt.Fprintf(&b, "Sec-WebSocket-Key: %s\r\n", key)
	b.WriteString("Sec-WebSocket-Version: 13\r\n")
	for hk, hvs := range extraHeaders {
		// Skip headers we own to avoid duplication.
		switch http.CanonicalHeaderKey(hk) {
		case "Upgrade", "Connection", "Sec-Websocket-Key", "Sec-Websocket-Version", "Host":
			continue
		}
		for _, v := range hvs {
			fmt.Fprintf(&b, "%s: %s\r\n", hk, v)
		}
	}
	b.WriteString("\r\n")
	if _, err := io.WriteString(w, b.String()); err != nil {
		return "", err
	}
	return key, nil
}

// readUpgradeResponse reads + validates the server's 101 response.
// Returns the body reader (which the caller must continue to use for
// post-handshake frames) on success.
func readUpgradeResponse(br *bufio.Reader, sentKey string) error {
	resp, err := http.ReadResponse(br, nil)
	if err != nil {
		return fmt.Errorf("ws: read upgrade response: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusSwitchingProtocols {
		return fmt.Errorf("ws: upgrade failed: HTTP %d %s", resp.StatusCode, resp.Status)
	}
	if !strings.EqualFold(resp.Header.Get("Upgrade"), "websocket") {
		return errors.New("ws: response missing Upgrade: websocket")
	}
	if !headerHasToken(resp.Header.Get("Connection"), "Upgrade") {
		return errors.New("ws: response missing Connection: Upgrade")
	}
	got := resp.Header.Get("Sec-WebSocket-Accept")
	if got == "" {
		return errors.New("ws: response missing Sec-WebSocket-Accept")
	}
	if want := computeAccept(sentKey); got != want {
		return fmt.Errorf("ws: Sec-WebSocket-Accept mismatch (got %q want %q)", got, want)
	}
	return nil
}

// computeAccept derives the Sec-WebSocket-Accept value per RFC 6455
// §1.3: base64(SHA1(key + GUID)).
func computeAccept(key string) string {
	h := sha1.New()
	h.Write([]byte(key))
	h.Write([]byte(rfc6455GUID))
	return base64.StdEncoding.EncodeToString(h.Sum(nil))
}

// headerHasToken returns true if hv contains tok as one of its
// comma-separated tokens (case-insensitive).
func headerHasToken(hv, tok string) bool {
	for _, t := range strings.Split(hv, ",") {
		if strings.EqualFold(strings.TrimSpace(t), tok) {
			return true
		}
	}
	return false
}
