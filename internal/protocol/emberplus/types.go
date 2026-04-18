// Package emberplus implements the Ember+ protocol plugin for the
// acp toolset. Consumer-side only (connects to Ember+ providers).
//
// Architecture:
//   - ber/    — ASN.1 BER codec (Glow subset)
//   - s101/   — S101 TCP framing (BOF/EOF, CRC, escaping)
//   - glow/   — Glow DTD types and encode/decode
//   - plugin  — Protocol interface implementation
//   - session — TCP connection, S101 reader/writer, keep-alive
//
// Reference: Ember+ specification (Lawo)
// Cross-reference: github.com/dufourgilles/emberlib (read-only)
package emberplus

// Default Ember+ port.
const DefaultPort = 9000
