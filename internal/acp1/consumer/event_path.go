package acp1

import "strings"

// eventPath builds the dot-joined display path for an announcement event.
//
// browser.go builds the walked object's Path as the full
// [group, (sub-group,) label] hierarchy, so the leaf label is already the
// trailing element. eventPath therefore appends the label only when it is
// NOT already the last element (or the path is empty) — preventing the
// duplicate that showed up as "control.#Out-Mode.#Out-Mode" in the watch
// view. It stays backward-compatible with an older flat Path=[group] shape,
// where the label still gets appended once.
func eventPath(objPath []string, label string) string {
	parts := append([]string(nil), objPath...)
	if label != "" && (len(parts) == 0 || parts[len(parts)-1] != label) {
		parts = append(parts, label)
	}
	return strings.Join(parts, ".")
}
