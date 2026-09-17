package main

import (
	"errors"
	"strings"
	"testing"

	"dhs/internal/consumer"
	"dhs/internal/emberplus/codec/matrix"
)

func TestBehaviorFromGlowName(t *testing.T) {
	cases := map[string]string{
		"oneToOne": "1to1", "oneToN": "1toN", "nToN": "NtoM",
		"ONETOONE": "1to1", "weird": "dynamic", "": "dynamic",
	}
	for in, want := range cases {
		if got := behaviorFromGlowName(in); got != want {
			t.Errorf("behaviorFromGlowName(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestMatrixDescCSVRoundTrip(t *testing.T) {
	in := []matrixDesc{{
		Matrix: "router.nToN.matrix", Behavior: "NtoM",
		Targets: 16, Sources: 16,
		MaxConnectsPerTarget: 4, MaxTotalConnects: 64, Label: "nToN",
	}}
	got, err := parseMatrixDescCSV([]byte(formatMatrixDescCSV(in)), "t.csv")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0] != in[0] {
		t.Fatalf("round-trip = %+v, want %+v", got, in)
	}
}

func TestMatrixDescCSVRejectsBadBehavior(t *testing.T) {
	_, err := parseMatrixDescCSV([]byte("matrix,behavior\nm,nToN\n"), "t.csv")
	if err == nil || !strings.Contains(err.Error(), "ADR-0023") {
		t.Fatalf("protocol-vocabulary behavior must be rejected (ADR-0023 names only), got %v", err)
	}
}

func TestDiffMatrixXpoints_NtoM(t *testing.T) {
	desc := matrixDesc{Matrix: "m", Behavior: "NtoM", MaxConnectsPerTarget: 3}
	live := []matrix.TargetState{
		{Target: 6, Sources: []int32{6}},
		{Target: 12, Sources: []int32{12}},
	}
	rows := []cerebrumXpointRow{
		{Dest: "6", Srce: "3", Levels: []string{"0"}},
		{Dest: "6", Srce: "6", Levels: []string{"0"}},
		{Dest: "12", Srce: "9", Levels: []string{"0"}}, // replace 12 with 9
	}
	got, err := diffMatrixXpoints(desc, live, rows)
	if err != nil {
		t.Fatal(err)
	}
	// target 6: connect {3}; target 12: connect {9} + disconnect {12}.
	if len(got) != 3 {
		t.Fatalf("changes = %+v, want 3", got)
	}
	if got[0].Target != 6 || got[0].Op != xpOpConnect || len(got[0].Sources) != 1 || got[0].Sources[0] != 3 {
		t.Fatalf("change 0 = %+v", got[0])
	}
	if got[1].Op != xpOpConnect || got[1].Sources[0] != 9 {
		t.Fatalf("change 1 = %+v", got[1])
	}
	if got[2].Op != xpOpDisconnect || got[2].Sources[0] != 12 {
		t.Fatalf("change 2 = %+v (surplus must be EXPLICITLY disconnected)", got[2])
	}
}

func TestDiffMatrixXpoints_OneToN_AbsoluteNoToggle(t *testing.T) {
	desc := matrixDesc{Matrix: "m", Behavior: "1toN"}
	live := []matrix.TargetState{{Target: 1, Sources: []int32{5}}}
	rows := []cerebrumXpointRow{
		{Dest: "1", Srce: "7", Levels: []string{"0"}},
		{Dest: "2", Srce: "5", Levels: []string{"0"}},
	}
	got, err := diffMatrixXpoints(desc, live, rows)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("changes = %+v, want 2", got)
	}
	for _, c := range got {
		if c.Op != xpOpAbsolute {
			t.Fatalf("1toN converge must use absolute takes, got op %d", c.Op)
		}
	}
}

func TestDiffMatrixXpoints_Converged(t *testing.T) {
	desc := matrixDesc{Matrix: "m", Behavior: "NtoM"}
	live := []matrix.TargetState{{Target: 6, Sources: []int32{3, 6}}}
	rows := []cerebrumXpointRow{
		{Dest: "6", Srce: "6", Levels: []string{"0"}},
		{Dest: "6", Srce: "3", Levels: []string{"0"}}, // order must not matter
	}
	got, err := diffMatrixXpoints(desc, live, rows)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatalf("converged state produced changes: %+v", got)
	}
}

func TestDiffMatrixXpoints_Validation(t *testing.T) {
	// Non-zero level on a tree matrix.
	_, err := diffMatrixXpoints(matrixDesc{Behavior: "NtoM"}, nil,
		[]cerebrumXpointRow{{Dest: "1", Srce: "2", Levels: []string{"1"}}})
	if !errors.Is(err, consumer.ErrValidationFailed) {
		t.Fatalf("non-zero level: want validation error, got %v", err)
	}
	// Two sources on a 1toN dest.
	_, err = diffMatrixXpoints(matrixDesc{Behavior: "1toN"}, nil,
		[]cerebrumXpointRow{
			{Dest: "1", Srce: "2", Levels: []string{"0"}},
			{Dest: "1", Srce: "3", Levels: []string{"0"}},
		})
	if !errors.Is(err, consumer.ErrValidationFailed) {
		t.Fatalf("1toN multi-source: want validation error, got %v", err)
	}
	// Over max_connects_per_target on NtoM.
	_, err = diffMatrixXpoints(matrixDesc{Behavior: "NtoM", MaxConnectsPerTarget: 1}, nil,
		[]cerebrumXpointRow{
			{Dest: "1", Srce: "2", Levels: []string{"0"}},
			{Dest: "1", Srce: "3", Levels: []string{"0"}},
		})
	if !errors.Is(err, consumer.ErrValidationFailed) {
		t.Fatalf("NtoM over cap: want validation error, got %v", err)
	}
}

func TestFormatMatrixLabelCSV_MultiGroup(t *testing.T) {
	groups := []string{"Primary", "Short"}
	byGroup := map[string]map[string]string{
		"Primary": {"3": "AES-S-3", "6": "AES-S-6"},
		"Short":   {"3": "S3"}, // hole at 6 must not drop the row
	}
	got := formatMatrixLabelCSV("srce", groups, byGroup)
	want := "srce,Primary,Short\n3,AES-S-3,S3\n6,AES-S-6,\n"
	if got != want {
		t.Fatalf("label CSV:\ngot:\n%s\nwant:\n%s", got, want)
	}
}
