// Package http is the HTTP client transport, shared by every connector
// that speaks HTTP.
//
// It used to live at internal/amwa/session/http, where it worked well and
// nobody else could reach it: cross-plugin imports are forbidden, so the
// CCM connector — the only other HTTP speaker in the tree — hand-rolled its
// own client and left out the one thing that matters most here, the body
// cap. `io.ReadAll(resp.Body)` on a device that answers with a gigabyte is
// the HTTP shape of the same class of bug as a reader with no deadline.
//
// So the hardening lives at the transport layer, once:
//
//   - a body cap on every response, error paths included (DefaultMaxBody)
//   - a per-exchange timeout (DefaultTimeout)
//   - Content-Type enforced on JSON GETs
//   - DisallowUnknownFields, so a peer's non-spec keys surface instead of
//     being absorbed
//   - one place to attach a Bearer token (TokenSource)
//
// TLS is NOT configured here — that is transport.TLSOptions, which builds
// the *tls.Config a caller installs on its own http.Transport. One posture
// for TCP, WebSocket and HTTP alike.
//
// Stdlib-only and plugin-agnostic: it must not import any connector.
// internal/amwa/session/http keeps the NMOS-specific surface (the routed
// Server, the BCP-003-02 auth gate) and aliases Client from here.
package http
