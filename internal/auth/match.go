package auth

import "strings"

// WildcardMatch matches s against pattern where '*' spans zero or more
// characters — the `.*` regex equivalence used by JWT audience values and by
// scope path grammars alike.
//
// Iterative rather than regexp: the patterns come from a token, i.e. from
// outside, and compiling attacker-influenced regexps on every request is a
// denial-of-service waiting to be found. This is linear and allocation-light.
func WildcardMatch(pattern, s string) bool {
	parts := strings.Split(pattern, "*")
	if len(parts) == 1 {
		return pattern == s
	}
	if !strings.HasPrefix(s, parts[0]) {
		return false
	}
	s = s[len(parts[0]):]
	for i := 1; i < len(parts)-1; i++ {
		idx := strings.Index(s, parts[i])
		if idx < 0 {
			return false
		}
		s = s[idx+len(parts[i]):]
	}
	return strings.HasSuffix(s, parts[len(parts)-1])
}

// SplitHostPort is a forgiving host[:port] splitter for token values.
//
// Not net.SplitHostPort: that one errors when there is no port, and here "no
// port" is the common, correct case. A trailing colon or a non-numeric tail
// means the colon was part of the name, so the input is returned whole rather
// than truncated at a scheme separator.
func SplitHostPort(s string) (host, port string, ok bool) {
	i := strings.LastIndexByte(s, ':')
	if i < 0 {
		return s, "", false
	}
	p := s[i+1:]
	if p == "" {
		return s, "", false
	}
	for _, r := range p {
		if r < '0' || r > '9' {
			return s, "", false
		}
	}
	return s[:i], p, true
}
