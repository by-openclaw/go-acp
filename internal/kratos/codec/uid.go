package codec

import "strings"

// UIDs and query paths. A UID is the full slash path of a value,
// rooted at the system root: "cpiasd/antenna/motor/status/targetPlatAz"
// (§5.15.4). The REST queries address the same value WITHOUT the root:
// consumer/cvtItem/antenna/motor/status/targetPlatAz answers the row
// whose UID is cpiasd/antenna/motor/status/targetPlatAz. MQTT topics
// are the UID exactly, root included (§7).

// QueryPath returns the part of a UID a REST query takes: the UID
// without its first segment (the root). A UID without a slash is a
// root and has an empty query path.
func QueryPath(uid string) string {
	uid = strings.Trim(uid, "/")
	if i := strings.IndexByte(uid, '/'); i >= 0 {
		return uid[i+1:]
	}
	return ""
}

// Root returns the first segment of a UID ("cpiasd").
func Root(uid string) string {
	uid = strings.Trim(uid, "/")
	if i := strings.IndexByte(uid, '/'); i >= 0 {
		return uid[:i]
	}
	return uid
}

// NormalisePath turns what an operator types into a slash path:
// "antenna.motor.status.targetPlatAz", "antenna/motor/status/targetPlatAz"
// and "/antenna/motor/…" are the same path. Dots become slashes; UIDs
// in the documented examples carry no dot.
func NormalisePath(p string) string {
	p = strings.TrimSpace(p)
	p = strings.ReplaceAll(p, ".", "/")
	return strings.Trim(p, "/")
}

// UIDFor returns the UID of a path given with or without the root.
func UIDFor(root, path string) string {
	path = NormalisePath(path)
	if root == "" || path == root || strings.HasPrefix(path, root+"/") {
		return path
	}
	if path == "" {
		return root
	}
	return root + "/" + path
}

// MatchArray reports whether a CVT UID is an instance of an SVT array
// template ending in "#" (§2.2 note 2): "a/b/#" matches "a/b/3", and
// "a/b#" matches "a/b12". The "#" stands for one index of digits.
func MatchArray(template, uid string) bool {
	if !strings.HasSuffix(template, "#") {
		return false
	}
	prefix := strings.TrimSuffix(template, "#")
	if !strings.HasPrefix(uid, prefix) {
		return false
	}
	idx := uid[len(prefix):]
	if idx == "" {
		return false
	}
	for _, r := range idx {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// TopicMatches reports whether an MQTT topic matches a subscription
// filter (MQTT 3.1.1 §4.7): "+" one level, "#" the rest, last only.
func TopicMatches(filter, topic string) bool {
	fs := strings.Split(filter, "/")
	ts := strings.Split(topic, "/")
	for i, f := range fs {
		if f == "#" {
			return i == len(fs)-1
		}
		if i >= len(ts) {
			return false
		}
		if f != "+" && f != ts[i] {
			return false
		}
	}
	return len(fs) == len(ts)
}
