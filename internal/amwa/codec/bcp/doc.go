// Package bcp is the AMWA NMOS Best Current Practice validator
// infrastructure. Stdlib-only.
//
// Each BCP describes a constraint layered onto an existing IS-* /
// MS-05-02 wire format. A BCP validator package
// (`internal/amwa/codec/bcp/bcpNNNNN/`) implements [Validator] and
// registers itself at init() time so that callers can ask the
// per-host registry for every validator that applies to a given
// payload kind.
//
// Layer assignment per `internal/amwa/docs/dependencies.md`:
//
//	LAYER 1 — CODEC
//	  internal/amwa/codec/bcp/        (this package)
//	  internal/amwa/codec/bcp/bcpNNNNN/   (per-BCP validators)
//
// BCP packages MAY import their host spec package (is04, is05,
// ms05) but MUST NOT import siblings or session/ / consumer/
// packages.
package bcp
