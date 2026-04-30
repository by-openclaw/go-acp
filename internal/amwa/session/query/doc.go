// Package query is the IS-04 Query API client — Layer 2 session
// helper used by the Controller plugin.
//
// It speaks the Query API spec (https://specs.amwa.tv/is-04/) with a
// per-API-version base path
// (`http(s)://<host>:<port>/x-nmos/query/<api_ver>/`). Every endpoint
// returns canonical IS-04 resources; the package wires through to the
// versioned [is04.Codec] for decoding so the same Client speaks every
// supported wire minor (v1.1 / v1.2 / v1.3).
//
// Per the layered architecture in `internal/amwa/docs/dependencies.md`,
// this package is allowed to import:
//
//   - `internal/amwa/codec/is04` — for the Codec interface
//   - `internal/amwa/codec/spec` — for Versioned + ComplianceEvent
//   - `internal/amwa/session/http` — typed JSON HTTP client
//
// It MUST NOT import any plugin layer.
package query
