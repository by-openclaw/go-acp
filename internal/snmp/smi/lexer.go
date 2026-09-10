package smi

import (
	"fmt"
	"strings"
)

type tokKind int

const (
	tEOF    tokKind = iota
	tIdent          // identifiers and keywords: OBJECT-TYPE, sysDescr, val_1_5_dB
	tNumber         // decimal, optionally negative
	tString         // a "quoted" string; the token text is its contents
	tBinHex         // 'FF'H or '0101'B, kept as written
	tPunct          // ::=  ..  and every single-character symbol
)

type token struct {
	kind tokKind
	text string
	line int
}

// lex splits MIB source into tokens.
//
// Comments run from -- to the end of the line. ASN.1 also lets a second
// -- close a comment early, but no module in the sets this compiles puts
// code after one, and tools/mibcheck — which reads the same sets — strips
// to end of line too; one rule across both tools is worth more than the
// corner the other rule covers.
func lex(src string) ([]token, error) {
	var out []token
	line := 1
	n := len(src)
	for i := 0; i < n; {
		c := src[i]
		switch {
		case c == '\n':
			line++
			i++
		case c == ' ' || c == '\t' || c == '\r' || c == '\f' || c == '\v':
			i++
		case c == '-' && i+1 < n && src[i+1] == '-':
			for i < n && src[i] != '\n' {
				i++
			}
		case c == '"':
			start := line
			var b strings.Builder
			j := i + 1
			for {
				if j >= n {
					return nil, fmt.Errorf("line %d: unterminated string", start)
				}
				if src[j] == '"' {
					// "" is a quote inside a string.
					if j+1 < n && src[j+1] == '"' {
						b.WriteByte('"')
						j += 2
						continue
					}
					break
				}
				if src[j] == '\n' {
					line++
				}
				b.WriteByte(src[j])
				j++
			}
			out = append(out, token{tString, b.String(), start})
			i = j + 1
		case c == '\'':
			start := line
			j := i + 1
			for j < n && src[j] != '\'' {
				if src[j] == '\n' {
					line++
				}
				j++
			}
			if j >= n {
				return nil, fmt.Errorf("line %d: unterminated quoted literal", start)
			}
			k := j + 1
			if k < n && strings.IndexByte("HhBb", src[k]) >= 0 {
				k++
			}
			out = append(out, token{tBinHex, src[i:k], start})
			i = k
		case isDigit(c) || (c == '-' && i+1 < n && isDigit(src[i+1])):
			j := i + 1
			for j < n && isDigit(src[j]) {
				j++
			}
			out = append(out, token{tNumber, src[i:j], line})
			i = j
		case isLetter(c):
			j := i + 1
			for j < n {
				d := src[j]
				if isLetter(d) || isDigit(d) || d == '_' {
					j++
					continue
				}
				// A hyphen continues an identifier (OBJECT-TYPE,
				// SNMPv2-SMI) unless it opens a comment or ends the word.
				if d == '-' && j+1 < n && src[j+1] != '-' &&
					(isLetter(src[j+1]) || isDigit(src[j+1])) {
					j++
					continue
				}
				break
			}
			out = append(out, token{tIdent, src[i:j], line})
			i = j
		case strings.HasPrefix(src[i:], "::="):
			out = append(out, token{tPunct, "::=", line})
			i += 3
		case strings.HasPrefix(src[i:], ".."):
			out = append(out, token{tPunct, "..", line})
			i += 2
		default:
			out = append(out, token{tPunct, string(c), line})
			i++
		}
	}
	return append(out, token{tEOF, "", line}), nil
}

func isDigit(c byte) bool  { return c >= '0' && c <= '9' }
func isLetter(c byte) bool { return (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') }
