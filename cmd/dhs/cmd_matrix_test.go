package main

import (
	"testing"

	"dhs/internal/emberplus/codec/matrix"
)

// TestMatrixConverge pins the idempotency decision + projected source set for
// each Connection operation (0 absolute / 1 connect / 2 disconnect). "changed"
// is what decides whether the CLI sends at all (ADR-0007).
func TestMatrixConverge(t *testing.T) {
	cases := []struct {
		name        string
		current     []int32
		desired     []int32
		op          int64
		wantChanged bool
		wantProj    []int32 // set-compared
	}{
		{"absolute already set", []int32{5}, []int32{5}, 0, false, []int32{5}},
		{"absolute replace", []int32{5}, []int32{7}, 0, true, []int32{7}},
		{"absolute set from empty", nil, []int32{7}, 0, true, []int32{7}},
		{"absolute clear", []int32{7}, nil, 0, true, nil},
		{"absolute reorder is no-op", []int32{1, 2}, []int32{2, 1}, 0, false, []int32{1, 2}},
		{"connect adds new", []int32{1}, []int32{2}, 1, true, []int32{1, 2}},
		{"connect already present", []int32{1, 2}, []int32{2}, 1, false, []int32{1, 2}},
		{"disconnect removes present", []int32{1, 2}, []int32{2}, 2, true, []int32{1}},
		{"disconnect absent is no-op", []int32{1}, []int32{9}, 2, false, []int32{1}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			changed, proj := matrixConverge(tc.current, tc.desired, tc.op)
			if changed != tc.wantChanged {
				t.Errorf("changed = %v, want %v", changed, tc.wantChanged)
			}
			if !int32SetEqual(proj, tc.wantProj) {
				t.Errorf("projected = %v, want %v", proj, tc.wantProj)
			}
		})
	}
}

func TestInt32SetEqual(t *testing.T) {
	cases := []struct {
		a, b []int32
		want bool
	}{
		{nil, nil, true},
		{[]int32{1}, []int32{1}, true},
		{[]int32{1, 2}, []int32{2, 1}, true},
		{[]int32{1, 2}, []int32{1}, false},
		{[]int32{1}, []int32{2}, false},
		{[]int32{1, 1}, []int32{1}, true}, // dedup-independent
	}
	for _, tc := range cases {
		if got := int32SetEqual(tc.a, tc.b); got != tc.want {
			t.Errorf("int32SetEqual(%v,%v) = %v, want %v", tc.a, tc.b, got, tc.want)
		}
	}
}

func TestJoinInt32s(t *testing.T) {
	cases := []struct {
		in   []int32
		want string
	}{
		{nil, ""},
		{[]int32{}, ""},
		{[]int32{3, 1, 2}, "1,2,3"}, // sorted
		{[]int32{7}, "7"},
	}
	for _, tc := range cases {
		if got := joinInt32s(tc.in); got != tc.want {
			t.Errorf("joinInt32s(%v) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestMatrixTargetSources(t *testing.T) {
	snap := []matrix.TargetState{
		{Target: 0, Sources: []int32{5}},
		{Target: 3, Sources: []int32{1, 2}},
	}
	if got := matrixTargetSources(snap, 3); !int32SetEqual(got, []int32{1, 2}) {
		t.Errorf("target 3 sources = %v, want [1 2]", got)
	}
	if got := matrixTargetSources(snap, 9); len(got) != 0 {
		t.Errorf("absent target = %v, want empty", got)
	}
}

// TestEnsureFieldDiff pins the general ADR-0007 diff[] builder: one entry when
// changed, empty non-nil slice otherwise (never null).
func TestEnsureFieldDiff(t *testing.T) {
	d := ensureFieldDiff(true, "sources", "5", "7")
	if len(d) != 1 || d[0] != (ensureDiff{Field: "sources", From: "5", To: "7"}) {
		t.Fatalf("diff = %+v, want [{sources 5 7}]", d)
	}
	if e := ensureFieldDiff(false, "sources", "5", "5"); e == nil || len(e) != 0 {
		t.Fatalf("unchanged diff = %+v, want empty non-nil slice", e)
	}
}
