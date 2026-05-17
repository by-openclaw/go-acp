package main

import (
	"fmt"
	"strings"

	"dhs/internal/consumer"
)

// validatePathOrOID is the front-line syntax check for every verb's
// `--path` flag. It accepts two forms:
//
//   - Numeric OID: `^[0-9]+(\.[0-9]+)*$` (e.g. `1.6.1`, `1`)
//   - Dotted label: any non-empty string that contains at least one
//     non-digit character (e.g. `identity.types.vInteger`)
//
// A label that happens to be all-digit-and-dot collides with the OID
// regex — the resolver tries the OID index first and falls back to the
// label index, so this remains unambiguous in practice. The validator
// is strictly a syntax guard: missing-from-tree maps to
// plugin:object-not-found, not validation:invalid-oid.
//
// Returns:
//   - nil for a valid OID or a dotted label
//   - ErrInvalidOID for an input that is numeric-shape but malformed
//     (leading dot, trailing dot, empty segment, internal whitespace)
//
// Refs R21 #486.
func validatePathOrOID(s string) error {
	if s == "" {
		return fmt.Errorf("--path is empty")
	}
	if !looksLikeNumericOID(s) {
		// Treat as a dotted label — resolver decides hit/miss.
		return nil
	}
	// Numeric-shape: now enforce strict OID syntax.
	if strings.HasPrefix(s, ".") || strings.HasSuffix(s, ".") {
		return fmt.Errorf("%w: --path %q (leading or trailing dot is not a valid OID)", consumer.ErrInvalidOID, s)
	}
	for _, seg := range strings.Split(s, ".") {
		if seg == "" {
			return fmt.Errorf("%w: --path %q (empty segment between dots)", consumer.ErrInvalidOID, s)
		}
	}
	return nil
}

// looksLikeNumericOID returns true when s is composed solely of digits
// and dots — the gate for the OID syntax branch in validatePathOrOID.
// Empty string is not considered numeric-shape (caller handles that).
func looksLikeNumericOID(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r == '.' {
			continue
		}
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}
