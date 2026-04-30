// Package http is the Layer-2 NMOS HTTP session wrapper. Used by IS-09
// today and by IS-04 / IS-05 / IS-08 in later phases. Stdlib-only,
// plugin-agnostic — must NOT import internal/amwa/{consumer,provider,registry}
// or any other dhs plugin (enforced via the `nmos-session-no-plugin-imports`
// depguard rule).
//
// Two surfaces:
//
//   - Client: typed JSON GET against a `http://host:port/x-nmos/...`
//     path; enforces JSON content-type, applies a per-call deadline,
//     reads the whole body up to a configurable cap, decodes via the
//     caller-supplied dst (any *T).
//
//   - Server: a Mux with structured per-route handlers. Caller hands
//     each route a function returning `(any, error)` plus an optional
//     status code; the Mux serialises to JSON, sets Content-Type,
//     handles 404 / 405 / 500 with the body shape spec'd by IS-04 §4.4
//     (`{"code":N,"error":...,"debug":...}`).
package http
