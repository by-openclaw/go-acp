// Package codec is the stdlib-only byte codec for Open Sound Control
// (OSC) 1.0 and 1.1. It is lift-to-own-repo ready: no imports outside
// stdlib.
//
// Layout:
//
//	message.go  : Message encode/decode + OSC-string / OSC-blob / arg helpers
//	bundle.go   : Bundle encode/decode + nested-element recursion
//	tagstring.go: type-tag string parser (1.0 required tags + 1.1 T/F/N/I + arrays)
//	slip.go     : RFC 1055 SLIP framing (double-END) for OSC 1.1 TCP / serial
//
// Spec authority:
//   - https://opensoundcontrol.stanford.edu/spec-1_0.html
//   - https://opensoundcontrol.stanford.edu/spec-1_1.html
//
// Versioning follows Pattern A (separate registry entries per version)
// per memory/feedback_protocol_versioning.md.
package codec
