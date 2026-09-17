package main

// Daily-rotation tests. Every one of these drives time with the injected fake
// clock — no sleeps, so they behave identically on the ubuntu / windows /
// macos CI runners under -race.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"dhs/internal/clock"
)

func mustWrite(t *testing.T, w *dailyWriter, s string) {
	t.Helper()
	if _, err := w.Write([]byte(s)); err != nil {
		t.Fatalf("write %q: %v", s, err)
	}
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(b)
}

// The core promise: crossing local midnight starts a new file, and the
// previous day's file keeps its content.
func TestDailyWriterRollsAtMidnight(t *testing.T) {
	dir := t.TempDir()
	start := time.Date(2026, 9, 4, 23, 59, 0, 0, time.UTC)
	clk := clock.NewFake(start)

	w, err := newDailyWriter(filepath.Join(dir, "watch.log"), 0, clk)
	if err != nil {
		t.Fatalf("newDailyWriter: %v", err)
	}
	defer func() { _ = w.Close() }()

	mustWrite(t, w, "before midnight\n")
	if got, want := w.CurrentPath(), filepath.Join(dir, "watch-2026-09-04.log"); got != want {
		t.Fatalf("CurrentPath = %s, want %s", got, want)
	}

	clk.Advance(2 * time.Minute) // 00:01 the next day
	mustWrite(t, w, "after midnight\n")

	if got, want := w.CurrentPath(), filepath.Join(dir, "watch-2026-09-05.log"); got != want {
		t.Fatalf("after roll CurrentPath = %s, want %s", got, want)
	}
	day4 := readFile(t, filepath.Join(dir, "watch-2026-09-04.log"))
	day5 := readFile(t, filepath.Join(dir, "watch-2026-09-05.log"))
	if !strings.Contains(day4, "before midnight") || strings.Contains(day4, "after midnight") {
		t.Fatalf("day-4 file has the wrong content: %q", day4)
	}
	if !strings.Contains(day5, "after midnight") || strings.Contains(day5, "before midnight") {
		t.Fatalf("day-5 file has the wrong content: %q", day5)
	}
}

// A restart mid-day must APPEND, not truncate. Losing the morning's evidence
// because the watcher was restarted at noon is the failure mode this guards.
func TestDailyWriterAppendsOnReopen(t *testing.T) {
	dir := t.TempDir()
	clk := clock.NewFake(time.Date(2026, 9, 4, 9, 0, 0, 0, time.UTC))
	path := filepath.Join(dir, "watch.log")

	w1, err := newDailyWriter(path, 0, clk)
	if err != nil {
		t.Fatalf("first open: %v", err)
	}
	mustWrite(t, w1, "first run\n")
	if cerr := w1.Close(); cerr != nil {
		t.Fatalf("close: %v", cerr)
	}

	clk.Advance(3 * time.Hour) // same calendar day
	w2, err := newDailyWriter(path, 0, clk)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer func() { _ = w2.Close() }()
	mustWrite(t, w2, "second run\n")

	body := readFile(t, filepath.Join(dir, "watch-2026-09-04.log"))
	if !strings.Contains(body, "first run") {
		t.Fatalf("reopen truncated the day's log — first run lost:\n%s", body)
	}
	if !strings.Contains(body, "second run") {
		t.Fatalf("reopen did not append:\n%s", body)
	}
}

// Retention prunes days older than the window and keeps the rest.
func TestDailyWriterRetentionPrunes(t *testing.T) {
	dir := t.TempDir()
	// Pre-seed ten days of history plus one unrelated file.
	for d := 1; d <= 10; d++ {
		name := filepath.Join(dir, "watch-2026-09-"+twoDigit(d)+".log")
		if err := os.WriteFile(name, []byte("old\n"), 0o644); err != nil {
			t.Fatalf("seed: %v", err)
		}
	}
	keepMe := filepath.Join(dir, "unrelated.log")
	if err := os.WriteFile(keepMe, []byte("not ours\n"), 0o644); err != nil {
		t.Fatalf("seed unrelated: %v", err)
	}

	clk := clock.NewFake(time.Date(2026, 9, 11, 0, 0, 1, 0, time.UTC))
	w, err := newDailyWriter(filepath.Join(dir, "watch.log"), 3, clk)
	if err != nil {
		t.Fatalf("newDailyWriter: %v", err)
	}
	defer func() { _ = w.Close() }()

	// retain=3 → cutoff is 2026-09-08; 09-01..09-07 go, 09-08..09-10 stay.
	for d := 1; d <= 7; d++ {
		p := filepath.Join(dir, "watch-2026-09-"+twoDigit(d)+".log")
		if _, serr := os.Stat(p); serr == nil {
			t.Errorf("%s should have been pruned (retain=3)", filepath.Base(p))
		}
	}
	for d := 8; d <= 10; d++ {
		p := filepath.Join(dir, "watch-2026-09-"+twoDigit(d)+".log")
		if _, serr := os.Stat(p); serr != nil {
			t.Errorf("%s was pruned but is inside the retention window", filepath.Base(p))
		}
	}
	if _, serr := os.Stat(keepMe); serr != nil {
		t.Error("retention deleted a file that is not one of ours (unrelated.log)")
	}
}

// retain=0 is the default and must never delete anything.
func TestDailyWriterRetentionZeroKeepsEverything(t *testing.T) {
	dir := t.TempDir()
	old := filepath.Join(dir, "watch-2020-01-01.log")
	if err := os.WriteFile(old, []byte("ancient\n"), 0o644); err != nil {
		t.Fatalf("seed: %v", err)
	}
	clk := clock.NewFake(time.Date(2026, 9, 4, 12, 0, 0, 0, time.UTC))
	w, err := newDailyWriter(filepath.Join(dir, "watch.log"), 0, clk)
	if err != nil {
		t.Fatalf("newDailyWriter: %v", err)
	}
	defer func() { _ = w.Close() }()

	if _, serr := os.Stat(old); serr != nil {
		t.Fatal("retain=0 must keep every day, but an old file was deleted")
	}
}

// Rolling across several days in one jump must land on the right file and not
// leave the writer confused about which day it is on.
func TestDailyWriterMultiDayJump(t *testing.T) {
	dir := t.TempDir()
	clk := clock.NewFake(time.Date(2026, 9, 4, 12, 0, 0, 0, time.UTC))
	w, err := newDailyWriter(filepath.Join(dir, "watch.log"), 0, clk)
	if err != nil {
		t.Fatalf("newDailyWriter: %v", err)
	}
	defer func() { _ = w.Close() }()
	mustWrite(t, w, "day4\n")

	clk.Advance(72 * time.Hour)
	mustWrite(t, w, "day7\n")

	if got, want := w.CurrentPath(), filepath.Join(dir, "watch-2026-09-07.log"); got != want {
		t.Fatalf("CurrentPath = %s, want %s", got, want)
	}
	if body := readFile(t, filepath.Join(dir, "watch-2026-09-07.log")); !strings.Contains(body, "day7") {
		t.Fatalf("day-7 content missing: %q", body)
	}
}

// A path with no extension still rotates, defaulting to .log.
func TestDailyWriterDefaultsExtension(t *testing.T) {
	dir := t.TempDir()
	clk := clock.NewFake(time.Date(2026, 9, 4, 0, 0, 0, 0, time.UTC))
	w, err := newDailyWriter(filepath.Join(dir, "watch"), 0, clk)
	if err != nil {
		t.Fatalf("newDailyWriter: %v", err)
	}
	defer func() { _ = w.Close() }()
	if got, want := w.CurrentPath(), filepath.Join(dir, "watch-2026-09-04.log"); got != want {
		t.Fatalf("CurrentPath = %s, want %s", got, want)
	}
}

// Close must be idempotent — cleanup() can run more than once on error paths.
func TestDailyWriterCloseIdempotent(t *testing.T) {
	dir := t.TempDir()
	w, err := newDailyWriter(filepath.Join(dir, "watch.log"), 0, clock.NewFake(time.Time{}))
	if err != nil {
		t.Fatalf("newDailyWriter: %v", err)
	}
	if cerr := w.Close(); cerr != nil {
		t.Fatalf("first close: %v", cerr)
	}
	if cerr := w.Close(); cerr != nil {
		t.Fatalf("second close must be a no-op, got: %v", cerr)
	}
}

func twoDigit(n int) string {
	if n < 10 {
		return "0" + string(rune('0'+n))
	}
	return string(rune('0'+n/10)) + string(rune('0'+n%10))
}
