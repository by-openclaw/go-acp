// Package auth is the JWT / bearer-token concern, in one place.
//
// It is a SIBLING of internal/transport, not a child, and the line between
// them is worth stating because it decides what belongs here:
//
//	transport  secures and carries the CHANNEL — TLS, keepalive, framing.
//	           It moves an Authorization header without caring what is in it.
//	auth       decides WHO the caller is — parse a token, verify a signature,
//	           check the clock. It knows nothing about sockets.
//
// Putting token validation inside transport would mean the pipe knowing about
// issuers, audiences and scopes; by that logic OAuth flows would live there
// too. Putting it inside a protocol is what we had, and it does not survive
// the second consumer.
//
// # Why it exists
//
// The JWT machinery grew inside internal/amwa/codec/is10, which was
// reasonable while NMOS was the only thing that authenticated: IS-10 IS the
// NMOS authorization spec, so its "wire format" really is tokens and key
// sets. It stops being reasonable the moment a second connector needs a
// token, because cross-protocol imports are forbidden — CCM cannot import an
// AMWA codec — and because codec/ is stdlib-only and lift-ready (ADR-0006),
// so it cannot import a shared package either. The choice was one shared
// package or two implementations of RFC 7519.
//
// # What is here and what is not
//
// Here: everything RFC 7519 and RFC 7517 define — the JOSE header, the
// registered claims, key sets, signature verification, and the exp/iat/nbf
// clock rules. Nothing in this package knows the word "nmos".
//
// Not here: what a claim MEANS to a protocol. Audience matching against a
// server's identities, scope-to-verb mapping, which private-claim namespace
// carries permissions — those are protocol policy and stay with the protocol.
// Claims.Private hands the raw remainder over for exactly that.
//
// Stdlib only, and it stays that way: crypto/rsa, crypto/sha512,
// encoding/json, encoding/base64. A JWT library would be one dependency for
// code the standard library already provides — and this package is the
// evidence, having replaced none of it with anything.
package auth
