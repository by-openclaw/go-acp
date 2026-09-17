// Package is10 — canonical wire types for AMWA IS-10 Authorization
// v1.0 (https://specs.amwa.tv/is-10/releases/v1.0.0/), the
// specification BCP-003-02 mandates for NMOS API authorization.
//
// What lives here is what IS-10 actually DEFINES on the wire:
//
//   - Authorization Server metadata (RFC 8414 document, auth_metadata.json);
//   - the token-endpoint success and error bodies;
//   - the DNS-SD service type + well-known metadata path;
//   - the per-minor codec registry.
//
// What does NOT live here is everything IS-10 merely REFERENCES.
// JWS parsing, RS512 verification and JWKS decoding are RFC
// 7515/7517/7519 — the same bytes for every protocol that carries a
// bearer token — so they are in internal/auth. The NMOS-specific
// reading of those claims (audience matching against this server's
// identities, the x-nmos-* permission grammar, the path table from
// Behaviour - Resource Servers.md) is in internal/amwa/session/auth.
//
// The split is forced as well as correct. A codec is stdlib-only and
// never imports dhs/* (ADR-0006), so a codec cannot reach the shared
// JWT package; keeping the NMOS policy here would have meant keeping a
// private second copy of RFC 7519 here too, and a security fix would
// then have to be made twice. Signature verification is not bytes.
//
// Validation doctrine is the repo-wide one: AMWA's own JSON Schemas
// (schemas/v1.0.0/, verbatim) are the authority; Go code carries the
// canonical shapes and only enforces structure the schemas fix.
package is10
