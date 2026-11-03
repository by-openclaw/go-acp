package codec

import "fmt"

// A transaction key opens every request and is echoed by its answer
// (§6.5): one starting character followed by 10 digits. RRCS starts the
// keys of the requests it sends with 'R'; a control system picks any
// other character, "there are no limitations".
const (
	// TransKeyLen is the length of a transaction key.
	TransKeyLen = 11
	// RRCSPrefix starts the keys RRCS generates.
	RRCSPrefix = 'R'

	transKeyModulus = 10_000_000_000
)

// NewTransKey builds a key from a starting character and a sequence
// number. Only the low ten decimal digits of seq are kept.
func NewTransKey(prefix byte, seq uint64) string {
	return fmt.Sprintf("%c%010d", prefix, seq%transKeyModulus)
}

// ValidTransKey reports whether s has the §6.5 shape: one character
// followed by ten digits.
func ValidTransKey(s string) bool {
	if len(s) != TransKeyLen {
		return false
	}
	for i := 1; i < TransKeyLen; i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}
