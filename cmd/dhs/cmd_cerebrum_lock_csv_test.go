package main

import (
	"strings"
	"testing"
)

func TestFormatCerebrumLockCSV(t *testing.T) {
	locks := []cerebrumLockSpec{
		{Dest: "12", Level: "1", State: "LOCKED", LockedBy: "Admin"},
		{Dest: "12", Level: "3", State: "LOCKED", LockedBy: "Admin"},
		{Dest: "12", Level: "2", State: "LOCKED", LockedBy: "Admin"},
		{Dest: "2", Level: "14", State: "PROTECTED", LockedBy: "YOB"},
		// Baseline cells never export: unlocked is absence.
		{Dest: "5", Level: "1", State: "RELEASED", LockedBy: ""},
		{Dest: "6", Level: "1", State: "", LockedBy: ""},
	}
	got := formatCerebrumLockCSV(locks)
	want := "dest,state,levels,locked_by\n" +
		"2,PROTECTED,14,YOB\n" +
		"12,LOCKED,1;2;3,Admin\n"
	if got != want {
		t.Fatalf("lock CSV mismatch:\ngot:\n%s\nwant:\n%s", got, want)
	}
}

func TestParseCerebrumLockCSVRoundTrip(t *testing.T) {
	csv := "dest,state,levels,locked_by\n2,PROTECTED,14,YOB\n12,LOCKED,1;2;3,Admin\n20047,RELEASED,14,\n"
	rows, err := parseCerebrumLockCSV([]byte(csv), "test.csv")
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 3 {
		t.Fatalf("rows = %d, want 3", len(rows))
	}
	if rows[1].Dest != "12" || rows[1].State != "LOCKED" || len(rows[1].Levels) != 3 || rows[1].LockedBy != "Admin" {
		t.Fatalf("row 1 = %+v", rows[1])
	}
	if rows[2].State != "RELEASED" {
		t.Fatalf("explicit release row lost: %+v", rows[2])
	}
}

func TestParseCerebrumLockCSVRejectsBadState(t *testing.T) {
	// UNLOCKED is spec text that NACKs on every live Cerebrum — the parser
	// must refuse it so the CSV can never fabricate a dead wire value.
	_, err := parseCerebrumLockCSV([]byte("dest,state,levels\n1,UNLOCKED,1\n"), "test.csv")
	if err == nil || !strings.Contains(err.Error(), "RELEASED") {
		t.Fatalf("want state-enum error naming RELEASED, got %v", err)
	}
}

func TestDiffCerebrumLocks(t *testing.T) {
	live := []cerebrumLockSpec{
		{Dest: "12", Level: "1", State: "LOCKED"},
		{Dest: "12", Level: "2", State: "RELEASED"},
		{Dest: "2", Level: "14", State: "PROTECTED"},
	}
	want := []cerebrumLockRow{
		{Dest: "12", State: "LOCKED", Levels: []string{"1", "2"}}, // 1 converged, 2 changes
		{Dest: "2", State: "PROTECTED", Levels: []string{"14"}},   // converged
		{Dest: "9", State: "RELEASED", Levels: []string{"1"}},     // live-absent = already RELEASED
		{Dest: "7", State: "LOCKED", Levels: []string{"3"}},       // live-absent -> set
	}
	got := diffCerebrumLocks(live, want)
	if len(got) != 2 {
		t.Fatalf("changes = %+v, want 2", got)
	}
	if got[0].Dest != "12" || got[0].Level != "2" || got[0].From != "RELEASED" || got[0].To != "LOCKED" {
		t.Fatalf("change 0 = %+v", got[0])
	}
	if got[1].Dest != "7" || got[1].From != "RELEASED" || got[1].To != "LOCKED" {
		t.Fatalf("change 1 = %+v", got[1])
	}
}
