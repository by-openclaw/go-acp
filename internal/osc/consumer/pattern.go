package osc

import "strings"

// addressMatches implements OSC 1.0 §"OSC Address Pattern Matching":
//
//   - empty pattern matches everything (dhs convenience extension)
//   - '?'        matches any single character within one path segment
//   - '*'        matches any sequence (including zero) of characters within one segment
//   - '[chars]'  matches any single char in chars; supports ranges via '-'
//                and negation via leading '!'
//   - '{a,b,c}'  matches any of the comma-separated alternatives (segment-bounded)
//   - '/'        is a path separator and is NEVER crossed by *, ?, [, or {
//
// A pattern and address must match exactly across all segments — partial
// matches and trailing-slash differences fail.
//
// Implementation: split both pattern and address on '/' and match each
// segment with a per-segment glob. This guarantees the "no crossing /"
// rule trivially.
func addressMatches(pattern, addr string) bool {
	if pattern == "" {
		return true
	}
	if pattern == addr {
		return true
	}
	pSegs := strings.Split(pattern, "/")
	aSegs := strings.Split(addr, "/")
	if len(pSegs) != len(aSegs) {
		return false
	}
	for i := range pSegs {
		if !segmentMatches(pSegs[i], aSegs[i]) {
			return false
		}
	}
	return true
}

// segmentMatches matches one path segment against the OSC pattern
// language for a single segment (no '/' allowed in either side).
func segmentMatches(pat, seg string) bool {
	return matchHere(pat, 0, seg, 0)
}

// matchHere is a backtracking matcher for the per-segment OSC pattern.
// It walks pattern + segment in lock-step, expanding *, ?, [...], {...}.
func matchHere(pat string, pi int, seg string, si int) bool {
	for pi < len(pat) {
		c := pat[pi]
		switch c {
		case '*':
			// Skip consecutive '*'s.
			for pi < len(pat) && pat[pi] == '*' {
				pi++
			}
			if pi == len(pat) {
				return true // trailing * matches the rest of the segment
			}
			// Try every possible split point for the rest.
			for k := si; k <= len(seg); k++ {
				if matchHere(pat, pi, seg, k) {
					return true
				}
			}
			return false
		case '?':
			if si >= len(seg) {
				return false
			}
			pi++
			si++
		case '[':
			end := strings.IndexByte(pat[pi+1:], ']')
			if end < 0 {
				// No closing bracket — treat literal '['
				if si >= len(seg) || seg[si] != '[' {
					return false
				}
				pi++
				si++
				continue
			}
			class := pat[pi+1 : pi+1+end]
			if si >= len(seg) {
				return false
			}
			if !classMatches(class, seg[si]) {
				return false
			}
			pi += 1 + end + 1 // past the ']'
			si++
		case '{':
			end := strings.IndexByte(pat[pi+1:], '}')
			if end < 0 {
				// No closing brace — treat literal '{'
				if si >= len(seg) || seg[si] != '{' {
					return false
				}
				pi++
				si++
				continue
			}
			alts := strings.Split(pat[pi+1:pi+1+end], ",")
			matched := false
			for _, alt := range alts {
				if strings.HasPrefix(seg[si:], alt) &&
					matchHere(pat, pi+1+end+1, seg, si+len(alt)) {
					matched = true
					break
				}
			}
			return matched
		default:
			if si >= len(seg) || seg[si] != c {
				return false
			}
			pi++
			si++
		}
	}
	return si == len(seg)
}

// classMatches returns true if c matches any character in class
// (OSC '[chars]' bracket expression). Supports ranges (e.g. "a-z")
// and leading '!' negation.
func classMatches(class string, c byte) bool {
	negate := false
	if len(class) > 0 && class[0] == '!' {
		negate = true
		class = class[1:]
	}
	hit := false
	for i := 0; i < len(class); i++ {
		if i+2 < len(class) && class[i+1] == '-' {
			lo, hi := class[i], class[i+2]
			if lo > hi {
				lo, hi = hi, lo
			}
			if c >= lo && c <= hi {
				hit = true
				break
			}
			i += 2
			continue
		}
		if class[i] == c {
			hit = true
			break
		}
	}
	if negate {
		return !hit
	}
	return hit
}
