package osc

import "testing"

// Spec-compliance sweep for OSC 1.0 address-pattern matcher.
// Reference: https://opensoundcontrol.stanford.edu/spec-1_0.html
// Section "OSC Address Pattern Matching".
func TestAddressMatches_FullSpec(t *testing.T) {
	cases := []struct {
		name    string
		pattern string
		addr    string
		want    bool
	}{
		// empty + exact
		{"empty matches all", "", "/anything/here", true},
		{"exact match", "/exact", "/exact", true},
		{"exact mismatch suffix", "/exact", "/exact/nope", false},
		{"exact mismatch prefix", "/foo/bar", "/foo", false},

		// '*' single-segment
		{"star matches single segment", "/foo/*", "/foo/bar", true},
		{"star does not match deeper", "/foo/*", "/foo/bar/baz", false},
		{"star matches empty segment (spec: zero or more chars)", "/foo/*", "/foo/", true},
		{"star at end matches", "/*", "/x", true},
		{"star middle of segment", "/mixer/ch*", "/mixer/ch1", true},
		{"star middle of segment 2", "/mixer/ch*", "/mixer/channel", true},
		{"star never crosses /", "/mixer/ch*", "/mixer/ch1/gain", false},
		{"star prefix segment", "/mixer/*er", "/mixer/fader", true},

		// '?' single-character
		{"question mark single char", "/ch?", "/ch1", true},
		{"question mark requires char", "/ch?", "/ch", false},
		{"question mark single segment only", "/ch?", "/ch1/x", false},
		{"multiple question marks", "/ch??", "/ch12", true},
		{"question mark plus literal", "/ch?/lvl", "/ch1/lvl", true},

		// '[chars]' character class
		{"class single", "/ch[12]", "/ch1", true},
		{"class single match second", "/ch[12]", "/ch2", true},
		{"class single no match", "/ch[12]", "/ch3", false},
		{"class range", "/ch[1-4]", "/ch3", true},
		{"class range out", "/ch[1-4]", "/ch5", false},
		{"class range backwards", "/ch[4-1]", "/ch2", true},
		{"class negated", "/ch[!0]", "/ch1", true},
		{"class negated rejects", "/ch[!0]", "/ch0", false},
		{"class with letters", "/[abc]/x", "/b/x", true},

		// '{a,b,c}' alternation
		{"alt pick first", "/{pgm,pvw}", "/pgm", true},
		{"alt pick second", "/{pgm,pvw}", "/pvw", true},
		{"alt no match", "/{pgm,pvw}", "/aux", false},
		{"alt with prefix", "/mix/{lo,hi}", "/mix/lo", true},
		{"alt suffix", "/{a,b}/x", "/a/x", true},
		{"alt suffix 2", "/{a,b}/x", "/b/x", true},

		// combined wildcards
		{"star + alt", "/mix/*/{gain,mute}", "/mix/ch1/gain", true},
		{"star + alt mismatch tail", "/mix/*/{gain,mute}", "/mix/ch1/pan", false},
		{"star + class + alt", "/ch[1-9]/{gain,mute}", "/ch4/mute", true},

		// segment count must match
		{"segment count diff", "/a/b/c", "/a/b", false},
		{"trailing slash differs", "/a/b/", "/a/b", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := addressMatches(c.pattern, c.addr); got != c.want {
				t.Errorf("match(%q, %q) = %v, want %v", c.pattern, c.addr, got, c.want)
			}
		})
	}
}

func TestAddressMatches_ClassRanges(t *testing.T) {
	cases := []struct {
		class string
		c     byte
		want  bool
	}{
		{"a-z", 'a', true},
		{"a-z", 'm', true},
		{"a-z", 'z', true},
		{"a-z", 'A', false},
		{"A-Z", 'M', true},
		{"0-9", '5', true},
		{"0-9a-f", 'b', true}, // mixed range + literal
		{"0-9a-f", 'g', false},
		{"!a-z", 'a', false}, // negated
		{"!a-z", '0', true},
		{"abc", 'b', true},
		{"abc", 'd', false},
		{"!abc", 'a', false},
		{"!abc", 'd', true},
	}
	for _, c := range cases {
		got := classMatches(c.class, c.c)
		if got != c.want {
			t.Errorf("classMatches(%q, %q) = %v, want %v", c.class, c.c, got, c.want)
		}
	}
}
