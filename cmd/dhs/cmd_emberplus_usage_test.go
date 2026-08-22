package main

import (
	"context"
	"strings"
	"testing"

	"dhs/internal/consumer"
	"dhs/internal/emberplus/codec/matrix"
)

func TestEmberUsageLabelMap(t *testing.T) {
	o := consumer.Object{Meta: map[string]any{
		"sourceLabels": map[string]map[string]string{
			"primary": {"3": "SDI In 1", "bogus": "X", "9": ""},
			"alt":     {"3": "Alt 3"},
		},
	}}
	// Default group = first alphabetically ("alt").
	if m := emberUsageLabelMap(o, "sourceLabels", ""); m[3] != "Alt 3" || len(m) != 1 {
		t.Fatalf("default group map = %v", m)
	}
	// Explicit group; non-int ids and empty labels dropped.
	if m := emberUsageLabelMap(o, "sourceLabels", "primary"); m[3] != "SDI In 1" || len(m) != 1 {
		t.Fatalf("primary map = %v", m)
	}
	// Unknown group / missing meta → nil.
	if m := emberUsageLabelMap(o, "sourceLabels", "nope"); m != nil {
		t.Fatalf("unknown group = %v", m)
	}
	if m := emberUsageLabelMap(consumer.Object{Meta: map[string]any{}}, "sourceLabels", ""); m != nil {
		t.Fatalf("no meta = %v", m)
	}
}

func TestEmberValidSourceIDs(t *testing.T) {
	o := consumer.Object{Meta: map[string]any{
		"sourceLabels": map[string]map[string]string{
			"labels": {"3": "S3", "6": "S6"},
			"alt":    {"9": "S9", "bogus": "X"},
		},
	}}
	v := emberValidSourceIDs(o)
	if len(v) != 3 || !v[3] || !v[6] || !v[9] {
		t.Fatalf("valid ids = %v", v)
	}
	if len(emberValidSourceIDs(consumer.Object{Meta: map[string]any{}})) != 0 {
		t.Fatal("no labels must yield empty set")
	}
}

// Fixture: target 0 fed by src 3; target 2 fed by srcs 3+7 (NtoM
// shape); target 5 fed by src 9 (untouched by a 3→7 replace).
func emberUsageFixture() []matrix.TargetState {
	return []matrix.TargetState{
		{Target: 2, Sources: []int32{3, 7}},
		{Target: 0, Sources: []int32{3}},
		{Target: 5, Sources: []int32{9}},
	}
}

func TestEmberUsageRows(t *testing.T) {
	rows := emberUsageRows(emberUsageFixture())
	want := []probelUsageRow{
		{Src: 3, Dst: 0, Levels: "0"}, {Src: 3, Dst: 2, Levels: "0"},
		{Src: 7, Dst: 2, Levels: "0"}, {Src: 9, Dst: 5, Levels: "0"},
	}
	if len(rows) != len(want) {
		t.Fatalf("rows = %+v", rows)
	}
	for i, w := range want {
		if rows[i] != w {
			t.Fatalf("row %d = %+v, want %+v", i, rows[i], w)
		}
	}
}

func TestEmberReplacePlan_AbsoluteBehaviors(t *testing.T) {
	// 1toN: every carrying target becomes an absolute take of the
	// substituted set — never a toggle (pinned oneToOne semantics).
	plan := emberReplacePlan("1toN", emberUsageFixture(), 3, 7)
	if len(plan) != 2 {
		t.Fatalf("plan = %+v", plan)
	}
	if plan[0].Target != 0 || plan[0].Op != xpOpAbsolute || joinInt32s(plan[0].Sources) != "7" {
		t.Fatalf("target 0 change = %+v", plan[0])
	}
	// Target 2 carried 3 AND 7 — substitution dedups to {7}.
	if plan[1].Target != 2 || plan[1].Op != xpOpAbsolute || joinInt32s(plan[1].Sources) != "7" {
		t.Fatalf("target 2 change = %+v", plan[1])
	}
	// Target 5 (src 9) untouched.
	for _, c := range plan {
		if c.Target == 5 {
			t.Fatalf("target 5 must not appear: %+v", c)
		}
	}
}

func TestEmberReplacePlan_NtoM(t *testing.T) {
	plan := emberReplacePlan("NtoM", emberUsageFixture(), 3, 7)
	// Target 0: connect 7 + disconnect 3 (two actions).
	// Target 2: 7 already present → disconnect 3 only.
	var t0, t2 []xpointChange
	for _, c := range plan {
		switch c.Target {
		case 0:
			t0 = append(t0, c)
		case 2:
			t2 = append(t2, c)
		default:
			t.Fatalf("unexpected target %d: %+v", c.Target, c)
		}
	}
	if len(t0) != 2 || t0[0].Op != xpOpConnect || joinInt32s(t0[0].Sources) != "7" ||
		t0[1].Op != xpOpDisconnect || joinInt32s(t0[1].Sources) != "3" {
		t.Fatalf("target 0 actions = %+v", t0)
	}
	if len(t2) != 1 || t2[0].Op != xpOpDisconnect || joinInt32s(t2[0].Sources) != "3" {
		t.Fatalf("target 2 actions = %+v", t2)
	}
}

func TestEmberReplacePlan_NoCarrier(t *testing.T) {
	if plan := emberReplacePlan("1toN", emberUsageFixture(), 77, 7); len(plan) != 0 {
		t.Fatalf("absent src produced plan: %+v", plan)
	}
}

func TestRunEmberUsage_Validation(t *testing.T) {
	ctx := context.Background()
	cases := []struct {
		name string
		args []string
		want string
	}{
		{"bad format", []string{"127.0.0.1", "--format", "xml"}, "--format"},
		{"srce+dest", []string{"127.0.0.1", "--srce", "1", "--dest", "2"}, "mutually exclusive"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := runEmberUsage(ctx, c.args)
			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Fatalf("err = %v, want %q", err, c.want)
			}
		})
	}
}

func TestRunEmberReplace_Validation(t *testing.T) {
	ctx := context.Background()
	cases := []struct {
		name string
		args []string
		want string
	}{
		{"missing pair", []string{"127.0.0.1"}, "--srce and --with are required"},
		{"same source", []string{"127.0.0.1", "--srce", "3", "--with", "3"}, "same source"},
		{"bad output", []string{"127.0.0.1", "--srce", "1", "--with", "2", "--output", "xml"}, "expected text | json"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := runEmberReplace(ctx, c.args)
			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Fatalf("err = %v, want %q", err, c.want)
			}
		})
	}
}
