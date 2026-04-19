// Package canonical defines the per-type element shapes emitted by
// every exporter and consumed by every importer across all protocols
// (Ember+, ACP1, ACP2, and future Probel/TSL plugins).
//
// The shapes are authoritative in two places:
//
//   - docs/protocols/schema.md           — cross-cutting rules
//   - docs/protocols/elements/*.md       — per-type fields + samples
//
// A conformance test (tests/unit/export/conformance_test.go) parses every
// fenced JSON block from the element docs into these structs; any key
// drift between the docs and the code fails the test.
//
// Design rules:
//
//  1. Every documented key is always emitted. Nullable keys use pointer
//     types so absent values marshal to null (not omitted).
//  2. children[] always marshals as [] on leaves — never null.
//  3. Pointers (templateReference, basePath, parametersLocation) carry
//     dot-joined numeric OIDs as strings.
//  4. No omitempty on schema-level keys. Only keys that are mode-flag
//     gated (targetLabels, connectionParams, …) may be nil-dropped, and
//     the exporter sets them explicitly per mode.
package canonical
