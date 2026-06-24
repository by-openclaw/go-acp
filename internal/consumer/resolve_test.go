package consumer

import "testing"

func resolveFixture() []Object {
	return []Object{
		{ID: 1, Label: "ROOT_NODE_V2", Path: []string{"ROOT_NODE_V2"}},
		{ID: 12060, Label: "OUTPUT", Path: []string{"ROOT_NODE_V2", "OUTPUT"}},
		{ID: 68255, Label: "Destination Port", Path: []string{"ROOT_NODE_V2", "OUTPUT", "IP", "AUDIO", "STREAM 19", "LEG 1", "Destination Port"}},
		{ID: 68256, Label: "Destination Port", Path: []string{"ROOT_NODE_V2", "OUTPUT", "IP", "AUDIO", "STREAM 20", "LEG 1", "Destination Port"}},
	}
}

func TestResolveObjectIndex(t *testing.T) {
	objs := resolveFixture()
	tests := []struct {
		name        string
		path, label string
		id          int
		wantIdx     int
		wantOK      bool
	}{
		{"by id", "", "", 68255, 2, true},
		{"by id missing", "", "", 99999, -1, false},
		{"by label first match", "", "Destination Port", -1, 2, true},
		{"by label missing", "", "nope", -1, -1, false},
		{"by full path", "ROOT_NODE_V2.OUTPUT.IP.AUDIO.STREAM 19.LEG 1.Destination Port", "", -1, 2, true},
		{"by root-stripped suffix", "OUTPUT.IP.AUDIO.STREAM 20.LEG 1.Destination Port", "", -1, 3, true},
		{"by leaf suffix only", "STREAM 19.LEG 1.Destination Port", "", -1, 2, true},
		{"path not found", "OUTPUT.IP.AUDIO.STREAM 99.LEG 1.Destination Port", "", -1, -1, false},
		{"path wins over id (label/path precede id)", "OUTPUT.IP.AUDIO.STREAM 20.LEG 1.Destination Port", "", 68255, 3, true},
		{"label wins over id", "", "Destination Port", 68256, 2, true},
		{"nothing given", "", "", -1, -1, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			idx, ok := ResolveObjectIndex(objs, tt.path, tt.label, tt.id)
			if ok != tt.wantOK || idx != tt.wantIdx {
				t.Fatalf("ResolveObjectIndex(path=%q,label=%q,id=%d) = (%d,%v); want (%d,%v)",
					tt.path, tt.label, tt.id, idx, ok, tt.wantIdx, tt.wantOK)
			}
		})
	}
}

func TestResolveObjectIndex_ExactBeatsSuffix(t *testing.T) {
	// A short object whose full path equals the query must win over a longer
	// object that merely ends with the same segments.
	objs := []Object{
		{ID: 10, Path: []string{"A", "OUTPUT", "Gain"}},
		{ID: 11, Path: []string{"OUTPUT", "Gain"}},
	}
	idx, ok := ResolveObjectIndex(objs, "OUTPUT.Gain", "", -1)
	if !ok || idx != 1 {
		t.Fatalf("exact match should win: got (%d,%v); want (1,true)", idx, ok)
	}
}

func TestPathMatches(t *testing.T) {
	full := []string{"ROOT_NODE_V2", "OUTPUT", "IP", "AUDIO", "STREAM 19", "LEG 1", "Destination Port"}
	cases := []struct {
		want string
		ok   bool
	}{
		{"ROOT_NODE_V2.OUTPUT.IP.AUDIO.STREAM 19.LEG 1.Destination Port", true},
		{"OUTPUT.IP.AUDIO.STREAM 19.LEG 1.Destination Port", true},
		{"LEG 1.Destination Port", true},
		{"OUTPUT.IP.AUDIO.STREAM 20.LEG 1.Destination Port", false},
		{"", false},
	}
	for _, c := range cases {
		if got := PathMatches(full, c.want); got != c.ok {
			t.Errorf("PathMatches(%q) = %v; want %v", c.want, got, c.ok)
		}
	}
}
