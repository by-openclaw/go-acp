package identity

import (
	"testing"
	"unicode/utf8"
)

// truncateUTF8 never splits a rune: a cut that lands mid-sequence backs
// up to the previous boundary, and a string within the limit is returned
// as is.
func TestTruncateUTF8KeepsRunesWhole(t *testing.T) {
	s := "abé" // 'é' is two bytes
	if got := truncateUTF8(s, 10); got != s {
		t.Errorf("within limit: %q, want %q", got, s)
	}
	got := truncateUTF8(s, 3) // would cut inside 'é'
	if got != "ab" || !utf8.ValidString(got) {
		t.Errorf("mid-rune cut: %q, want %q", got, "ab")
	}
	if got := truncateUTF8("é", 1); got != "" {
		t.Errorf("a limit smaller than the first rune: %q, want empty", got)
	}
}
