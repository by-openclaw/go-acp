package consumer

import "strings"

// ResolveObjectIndex finds the index of the object addressed by label, dotted
// path, or id within a canonical []Object tree, in priority order:
//  1. label  (label != "")
//  2. dotted path  (path != "")
//  3. id     (fallback)
//
// label and path are checked before id deliberately: ValueRequest's zero-value
// id is 0, which is indistinguishable from an explicit "id 0", so a request that
// set only Label/Path (leaving id at its zero value) must still resolve by
// Label/Path. Callers address an object by exactly one of label / path / id.
//
// This is the single, connector-agnostic resolution rule. Every protocol plugin
// (and the CLI) resolves --id / --label / --path the same way against its
// []Object tree, so addressing behaves identically across acp1, acp2, Ember+,
// probel, … — no per-connector special cases.
//
// Path matching accepts the full dotted path
// (e.g. "ROOT_NODE_V2.OUTPUT.IP.AUDIO.STREAM 1.LEG 1.Destination Port") or a
// root-stripped suffix ("OUTPUT.IP.AUDIO.STREAM 1.LEG 1.Destination Port") — the
// form controllers like Cerebrum use. Exact matches win over suffix matches.
//
// Returns (index, true) on a match, (-1, false) otherwise. A caller addressing
// by an explicit id that isn't in the tree gets (-1, false) and may still use
// the raw id directly (type unknown).
func ResolveObjectIndex(objs []Object, path, label string, id int) (int, bool) {
	if label != "" {
		for i := range objs {
			if objs[i].Label == label {
				return i, true
			}
		}
		return -1, false
	}
	if path != "" {
		want := strings.Split(path, ".")
		for i := range objs {
			if pathEqual(objs[i].Path, want) {
				return i, true
			}
		}
		for i := range objs {
			if pathHasSuffix(objs[i].Path, want) {
				return i, true
			}
		}
		return -1, false
	}
	for i := range objs {
		if objs[i].ID == id {
			return i, true
		}
	}
	return -1, false
}

// PathMatches reports whether objPath equals or ends with the dotted want path.
// Used by per-object call sites (e.g. announce filters) that match a single
// object's path without an []Object slice. Same rule as ResolveObjectIndex's
// path branch: exact or root-stripped suffix.
func PathMatches(objPath []string, want string) bool {
	if want == "" {
		return false
	}
	w := strings.Split(want, ".")
	return pathEqual(objPath, w) || pathHasSuffix(objPath, w)
}

func pathEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func pathHasSuffix(full, want []string) bool {
	if len(want) == 0 || len(want) > len(full) {
		return false
	}
	off := len(full) - len(want)
	for i := range want {
		if full[off+i] != want[i] {
			return false
		}
	}
	return true
}
