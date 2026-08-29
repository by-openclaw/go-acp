// Package is10 — canonical types + token machinery for AMWA IS-10
// Authorization v1.0 (https://specs.amwa.tv/is-10/releases/v1.0.0/),
// the specification BCP-003-02 mandates for NMOS API authorization.
//
// dhs acts as OAuth 2.0 CLIENT (obtaining tokens for its consumer
// verbs) and RESOURCE SERVER (every API we serve validating Bearer
// tokens). This package carries the wire shapes both roles share:
//
//   - the JWT access-token claim set (token_schema.json) with the
//     x-nmos-* private claims and their read/write path specifiers;
//   - RS512 JWS parsing + verification (the ONLY algorithm IS-10
//     permits) against JWKS public keys;
//   - claim validation (iss/sub/aud/exp required, UTC epoch, audience
//     matching incl. RFC 4592-style wildcards);
//   - the path-authorization rules of Behaviour - Resource Servers.md
//     (base paths implicitly readable, deep paths matched against the
//     x-nmos-* claim's wildcarded specifiers after URL normalization);
//   - Authorization Server metadata (RFC 8414) + token responses.
//
// Validation doctrine is the repo-wide one: AMWA's own JSON Schemas
// (schemas/v1.0.0/, verbatim) are the authority; Go code carries the
// canonical shapes and only enforces structure the schemas fix.
package is10
