// Package identity provides sanitiser helpers for vendor-supplied identity
// strings (card name, software revision, hardware revision, ...). Wire bytes
// are untrusted: they may be NUL-padded, CRLF-littered, contain path-traversal
// sequences, or carry non-UTF-8 bytes. Each persistence sink (filesystem path,
// YAML value, JSON string) needs different escape rules, so this package
// exports three sink-specific helpers driven by one shared input pre-clean.
package identity

import (
	"errors"
	"strings"
	"unicode/utf8"
)

// ErrUnsafe is returned by PathSegment when the input cannot be sanitised
// to a safe filesystem path component (empty after strip, leading dot or
// dash, contains a `..` traversal sequence, or reduces to a reserved name).
var ErrUnsafe = errors.New("identity: input cannot be sanitised safely")

const (
	pathMaxLen = 64
	yamlMaxLen = 256
	jsonMaxLen = 256
)

// PathSegment returns a value safe to use as a single filesystem path
// component. The output uses the strict allowlist [A-Za-z0-9._-]; any other
// byte is replaced with '-'. Returns ErrUnsafe for inputs that cannot be
// made safe.
func PathSegment(s string) (string, error) {
	s = stripNUL(s)
	if s == "" {
		return "", ErrUnsafe
	}
	if strings.Contains(s, "..") {
		return "", ErrUnsafe
	}
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		if isPathSafe(c) {
			b.WriteByte(c)
		} else {
			b.WriteByte('-')
		}
	}
	out := b.String()
	if len(out) > pathMaxLen {
		out = out[:pathMaxLen]
	}
	if out == "" || out == "." || out == ".." {
		return "", ErrUnsafe
	}
	if out[0] == '.' || out[0] == '-' {
		return "", ErrUnsafe
	}
	return out, nil
}

// YAMLValue returns a value safe to embed in a yaml string field. CR/LF/TAB
// collapse to a single space, path separators (/, \) collapse to '.', other
// control bytes are dropped. Empty input returns "" (caller should omit the
// field).
func YAMLValue(s string) string {
	s = stripNUL(s)
	if s == "" {
		return ""
	}
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c == '\r', c == '\n', c == '\t':
			b.WriteByte(' ')
		case c == '/', c == '\\':
			b.WriteByte('.')
		case c < 0x20, c == 0x7f:
			// drop other control bytes
		default:
			b.WriteByte(c)
		}
	}
	out := collapseSpaces(b.String())
	out = replaceInvalidUTF8(out, "?")
	if len(out) > yamlMaxLen {
		out = truncateUTF8(out, yamlMaxLen)
	}
	return out
}

// JSONString returns a value safe to embed in a JSON string field. Non-UTF-8
// bytes are replaced per byte with '?'. The encoding/json package handles
// all other escaping at marshal time, so this helper does no quoting.
func JSONString(s string) string {
	s = stripNUL(s)
	if s == "" {
		return ""
	}
	s = replaceInvalidUTF8(s, "?")
	if len(s) > jsonMaxLen {
		s = truncateUTF8(s, jsonMaxLen)
	}
	return s
}

func replaceInvalidUTF8(s, repl string) string {
	if utf8.ValidString(s) {
		return s
	}
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); {
		r, size := utf8.DecodeRuneInString(s[i:])
		if r == utf8.RuneError && size == 1 {
			b.WriteString(repl)
			i++
			continue
		}
		b.WriteString(s[i : i+size])
		i += size
	}
	return b.String()
}

func truncateUTF8(s string, max int) string {
	if len(s) <= max {
		return s
	}
	out := s[:max]
	for !utf8.ValidString(out) && len(out) > 0 {
		out = out[:len(out)-1]
	}
	return out
}

func isPathSafe(c byte) bool {
	switch {
	case c >= 'A' && c <= 'Z':
		return true
	case c >= 'a' && c <= 'z':
		return true
	case c >= '0' && c <= '9':
		return true
	case c == '.', c == '_', c == '-':
		return true
	}
	return false
}

func stripNUL(s string) string {
	if !strings.ContainsRune(s, 0) {
		return s
	}
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); i++ {
		if s[i] != 0 {
			b.WriteByte(s[i])
		}
	}
	return b.String()
}

func collapseSpaces(s string) string {
	if !strings.Contains(s, "  ") && !strings.HasPrefix(s, " ") && !strings.HasSuffix(s, " ") {
		return s
	}
	var b strings.Builder
	b.Grow(len(s))
	prevSpace := true
	for i := 0; i < len(s); i++ {
		if s[i] == ' ' {
			if !prevSpace {
				b.WriteByte(' ')
				prevSpace = true
			}
		} else {
			b.WriteByte(s[i])
			prevSpace = false
		}
	}
	return strings.TrimRight(b.String(), " ")
}
