package acp1

import "testing"

// TestEventPath guards against the watcher path-duplication bug where a
// nested obj.Path ([group, label]) plus an unconditional label append
// produced "control.#Out-Mode.#Out-Mode".
func TestEventPath(t *testing.T) {
	tests := []struct {
		name    string
		objPath []string
		label   string
		want    string
	}{
		{"nested path already ends in label (no dup)",
			[]string{"control", "#Out-Mode"}, "#Out-Mode", "control.#Out-Mode"},
		{"sub-group nested leaf",
			[]string{"control", "DOWN CONV", "Dn_CtrlA"}, "Dn_CtrlA", "control.DOWN CONV.Dn_CtrlA"},
		{"flat path=[group] gets label appended (back-compat)",
			[]string{"control"}, "Out-Mode", "control.Out-Mode"},
		{"empty path, only label",
			nil, "Out-Mode", "Out-Mode"},
		{"empty label keeps path as-is",
			[]string{"control", "Out-Mode"}, "", "control.Out-Mode"},
		{"both empty",
			nil, "", ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := eventPath(tc.objPath, tc.label); got != tc.want {
				t.Errorf("eventPath(%v, %q) = %q, want %q", tc.objPath, tc.label, got, tc.want)
			}
		})
	}
}
