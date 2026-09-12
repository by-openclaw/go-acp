package codec

import (
	"fmt"
	"strconv"
	"strings"
)

// The key a device model is filed under: Model@SwRev, per ADR-0022.
//
// A client names what it walked by asking the card who it is, and files the
// result under this key. A provider asked to serve that file has to read the
// same key back into an identity. Both halves live here, so they cannot drift:
// the key is written by one function and read by its inverse, and a change to
// one without the other fails the round trip in the test beside them.
//
// The model half is the vendor's own product name for the type id, made safe
// for a filename. The revision half is the version without its alpha
// character — "5.0.cs5" — which is what every key already on disk uses, and
// why reading one back cannot recover the alpha. Every device measured so far
// reports it blank.

// DMKey is the key a device model with this identity is filed under.
func DMKey(typeID uint16, v Version) string {
	return fmt.Sprintf("%s@%d.%d.cs%d", dmToken(UnitTypeName(typeID)), v.Major, v.Minor, v.CmdSet)
}

// ParseDMKey reads a key back into the type id and version it was made from.
//
// It refuses rather than guesses. A model half that matches no type, or that
// two types both reduce to — the filename-safe form discards punctuation, so
// two names differing only in a slash would collide — reports false, and so
// does a revision that is not exactly what DMKey writes. The alpha character,
// which the key never carried, comes back blank.
func ParseDMKey(key string) (typeID uint16, v Version, ok bool) {
	i := strings.LastIndexByte(key, '@')
	if i <= 0 || i == len(key)-1 {
		return 0, Version{}, false
	}
	v, ok = parseDMRev(key[i+1:])
	if !ok {
		return 0, Version{}, false
	}
	typeID, ok = typeForToken(key[:i])
	if !ok {
		return 0, Version{}, false
	}
	return typeID, v, true
}

// parseDMRev reads "5.0.cs5", and only the form DMKey writes: "05.0.cs5" is a
// version of the same number, but it is not a key this connector wrote, and a
// reader that accepted it would be guessing at who did.
func parseDMRev(rev string) (Version, bool) {
	parts := strings.Split(rev, ".")
	if len(parts) != 3 || !strings.HasPrefix(parts[2], "cs") {
		return Version{}, false
	}
	major, err1 := strconv.ParseUint(parts[0], 10, 8)
	minor, err2 := strconv.ParseUint(parts[1], 10, 8)
	cmdset, err3 := strconv.ParseUint(parts[2][2:], 10, 8)
	if err1 != nil || err2 != nil || err3 != nil {
		return Version{}, false
	}
	if fmt.Sprintf("%d.%d.cs%d", major, minor, cmdset) != rev {
		return Version{}, false
	}
	return Version{Major: uint8(major), Minor: uint8(minor), Alpha: ' ', CmdSet: uint8(cmdset)}, true
}

// typeForToken finds the one type whose name reduces to token.
//
// A type the vendor's table does not list is filed by its number, as
// "unit-type-N", and is read back the same way — but only when that is what
// DMKey would have written for N. A listed type is always filed by its name,
// so "unit-type-562" is not a key for the IQDBE00 and is refused.
func typeForToken(token string) (uint16, bool) {
	var found uint16
	matches := 0
	for _, t := range unitTypes {
		if dmToken(t.Label()) == token {
			found = t.ID
			matches++
		}
	}
	if matches == 0 {
		if s, ok := strings.CutPrefix(token, "unit-type-"); ok {
			if n, err := strconv.ParseUint(s, 10, 16); err == nil && dmToken(UnitTypeName(uint16(n))) == token {
				return uint16(n), true
			}
		}
	}
	return found, matches == 1
}

// dmToken makes a vendor type name safe to use as a filename.
//
// The names in the vendor's table carry spaces, dots and slashes — "4929 AES
// O/P card" is one of them — and the key becomes a path under .cache/dm.
func dmToken(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9',
			r == '-', r == '_':
			b.WriteRune(r)
		default:
			b.WriteRune('-')
		}
	}
	return strings.Trim(b.String(), "-")
}
