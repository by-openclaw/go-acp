package main

import (
	"bytes"
	"strings"
	"testing"
)

func cerebrumTestCatalogue() (src, dst, lvl []cerebrumMneRow) {
	src = []cerebrumMneRow{
		{ID: "1", Mnemonic: "CONVERT 1", Levels: []string{"1"}},
		{ID: "1", Mnemonic: "CONVERT 1", Levels: []string{"2"}}, // the per-level repeat a wildcard read emits
		{ID: "2", Mnemonic: "CONVERT 2"},
		{ID: "3", Mnemonic: "SHUFFLER 1"},
		{ID: "13", Mnemonic: "Proc 1"},
		{ID: "14", Mnemonic: "Proc 1"}, // duplicated label
	}
	dst = []cerebrumMneRow{
		{ID: "1", Mnemonic: "CONVERT 1"},
		{ID: "5", Mnemonic: "VIEW 1"},
		{ID: "7", Mnemonic: "VIEW 3"},
		{ID: "11", Mnemonic: "FUSION CH6"},
	}
	lvl = []cerebrumMneRow{
		{ID: "1", Mnemonic: "Video"},
		{ID: "2", Mnemonic: "StrAudio1"},
		{ID: "3", Mnemonic: "StrAudio2"},
		{ID: "4", Mnemonic: "ANC"},
	}
	return
}

func TestCerebrumResolveID(t *testing.T) {
	t.Parallel()
	src, _, lvl := cerebrumTestCatalogue()
	cases := []struct {
		name, kind, given string
		rows              []cerebrumMneRow
		want              string
		errHas            string
	}{
		{"digits pass through", "srce", "7", src, "7", ""},
		{"wildcard passes through", "level", "*", lvl, "*", ""},
		{"exact name", "srce", "CONVERT 2", src, "2", ""},
		{"exact name repeated per level resolves once", "srce", "CONVERT 1", src, "1", ""},
		{"case-insensitive when unique", "level", "video", lvl, "1", ""},
		{"blanks trimmed", "level", "  StrAudio1 ", lvl, "2", ""},
		{"duplicated label refused", "srce", "Proc 1", src, "", `2 srces carry that mnemonic ("Proc 1" (13), "Proc 1" (14))`},
		{"unknown with hint", "srce", "CONVERT", src, "", `containing it: "CONVERT 1" (1), "CONVERT 2" (2)`},
		{"unknown without hint", "level", "Audio 9", lvl, "", "no level has that mnemonic (4 read)"},
		{"empty", "dest", "", nil, "", "--dest: empty"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := cerebrumResolveID(tc.kind, tc.given, tc.rows)
			if tc.errHas != "" {
				if err == nil || !strings.Contains(err.Error(), tc.errHas) {
					t.Fatalf("err = %v, want it to contain %q", err, tc.errHas)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tc.want {
				t.Fatalf("got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestCerebrumResolveLevelsAndSplit(t *testing.T) {
	t.Parallel()
	_, _, lvl := cerebrumTestCatalogue()
	got, err := cerebrumResolveLevels("Video, 2;StrAudio2,Video", lvl)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(got, ",") != "1,2,3" {
		t.Fatalf("levels = %v, want [1 2 3] (order kept, duplicate dropped)", got)
	}
	if _, err := cerebrumResolveLevels("Video,Nope", lvl); err == nil || !strings.Contains(err.Error(), `--level "Nope"`) {
		t.Fatalf("unknown level in a list must fail naming it, got %v", err)
	}
	if _, err := cerebrumResolveLevels(" , ", lvl); err == nil {
		t.Fatal("an empty list must fail")
	}
	if s := cerebrumSplitList(" a ,, b; c "); strings.Join(s, "|") != "a|b|c" {
		t.Fatalf("split = %v", s)
	}
}

func TestCerebrumResolveRoutes(t *testing.T) {
	t.Parallel()
	src, dst, lvl := cerebrumTestCatalogue()
	st := &cerebrumState{Src: src, Dst: dst, Lvl: lvl}
	in := []routeSpec{
		{Dest: "VIEW 3", Srce: "CONVERT 1", Level: "Video"},
		{Dest: "VIEW 3", Srce: "CONVERT 1", Level: "StrAudio1"},
		{Dest: "11", Srce: "2", Level: "1"},
	}
	if !cerebrumRoutesNeedNames(in) || cerebrumRoutesNeedNames(in[2:]) {
		t.Fatal("cerebrumRoutesNeedNames: names present in the first two, none in the third")
	}
	var w bytes.Buffer
	got, err := cerebrumResolveRoutes(in, st, &w)
	if err != nil {
		t.Fatal(err)
	}
	want := []routeSpec{{"7", "1", "1"}, {"7", "1", "2"}, {"11", "2", "1"}}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("route %d = %+v, want %+v", i, got[i], want[i])
		}
	}
	out := w.String()
	for _, line := range []string{`[route] name "VIEW 3" = dest 7`, `[route] name "CONVERT 1" = srce 1`, `[route] name "Video" = level 1`, `[route] name "StrAudio1" = level 2`} {
		if strings.Count(out, line) != 1 {
			t.Errorf("resolution line %q printed %d times, want once:\n%s", line, strings.Count(out, line), out)
		}
	}
	if strings.Contains(out, `"11"`) || strings.Contains(out, `"2" = srce`) {
		t.Errorf("IDs must not be reported as resolutions:\n%s", out)
	}
	if _, err := cerebrumResolveRoutes([]routeSpec{{Dest: "VIEW 9", Srce: "1", Level: "1"}}, st, &w); err == nil {
		t.Fatal("an unknown destination must fail before anything is sent")
	}
}

func TestCerebrumFilterUsageLevels(t *testing.T) {
	t.Parallel()
	rows := []cerebrumUsageRow{
		{Srce: "1", Dest: "5", Levels: []string{"1", "2"}},
		{Srce: "3", Dest: "5", Levels: []string{"2"}},
		{Srce: "2", Dest: "6", Levels: []string{"3"}},
	}
	got := cerebrumFilterUsageLevels(rows, []string{"1", "3"})
	if len(got) != 2 || got[0].Dest != "5" || strings.Join(got[0].Levels, ",") != "1" || got[1].Dest != "6" {
		t.Fatalf("filtered = %+v", got)
	}
}
