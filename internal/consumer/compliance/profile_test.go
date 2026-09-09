package compliance

// The profile is the record of what a provider did that the spec does
// not describe. The repo-wide posture is that we absorb such a
// deviation rather than working around it — so this counter is the
// only place the operator can see it happened at all, and a count
// that is wrong is a deviation nobody will ever hear about.

import (
	"strings"
	"sync"
	"testing"
)

// The zero value is usable: a plugin holding a Profile it never
// initialised must be able to Note into it, because the alternative is
// a nil check at every call site on the hot path.
func TestZeroProfileIsUsable(t *testing.T) {
	var p Profile

	p.Note("acp1_short_reply")
	p.Note("acp1_short_reply")
	p.Note("acp1_late_ack")

	snap := p.Snapshot()
	if snap["acp1_short_reply"] != 2 || snap["acp1_late_ack"] != 1 {
		t.Fatalf("counters = %v", snap)
	}
}

// A nil Profile is what a plugin holds when nobody asked for one, and
// every method has to survive it: the call sites are on the hot path
// and must not each carry their own nil check.
func TestNilProfileAnswersForItself(t *testing.T) {
	var p *Profile

	p.Note("anything")
	if got := p.Snapshot(); got != nil {
		t.Errorf("Snapshot = %v, want nothing", got)
	}
	if got := p.SummaryLine(); got != "" {
		t.Errorf("SummaryLine = %q, want empty", got)
	}
	if got := p.Classification(); got != "strict" {
		t.Errorf("Classification = %q, want strict", got)
	}
}

// The snapshot is a copy: a caller reading it while the session keeps
// running must not see the counters move underneath them.
func TestSnapshotIsACopy(t *testing.T) {
	var p Profile
	p.Note("event")

	snap := p.Snapshot()
	p.Note("event")

	if snap["event"] != 1 {
		t.Errorf("the snapshot moved with the profile: %v", snap)
	}
	if got := p.Snapshot()["event"]; got != 2 {
		t.Errorf("the profile did not move: %d", got)
	}
}

// The summary is a log value, so it is deterministic: the same
// profile renders the same line every time, sorted by event name.
// Two runs of the same session that read differently are two runs
// nobody can diff.
func TestSummaryLineIsSortedAndDeterministic(t *testing.T) {
	var p Profile
	if got := p.SummaryLine(); got != "" {
		t.Errorf("an empty profile = %q, want empty", got)
	}

	p.Note("zulu")
	p.Note("alpha")
	p.Note("alpha")
	p.Note("mike")

	want := "alpha=2 mike=1 zulu=1"
	for i := 0; i < 20; i++ {
		if got := p.SummaryLine(); got != want {
			t.Fatalf("run %d = %q, want %q", i, got, want)
		}
	}
}

// The verdict is coarse by design: strict means the provider did
// nothing the spec does not describe, and one event is enough to make
// it partial — the operator decides what to do about which.
func TestClassification(t *testing.T) {
	var p Profile
	if got := p.Classification(); got != "strict" {
		t.Errorf("an untouched profile = %q, want strict", got)
	}

	p.Note("acp1_short_reply")
	if got := p.Classification(); got != "partial" {
		t.Errorf("one event = %q, want partial", got)
	}
}

// Note runs on the hot path from every session goroutine, so the
// first writer for a label and every one after it have to agree on
// which counter they are incrementing.
func TestConcurrentNotes(t *testing.T) {
	var p Profile
	const goroutines, each = 8, 200

	var wg sync.WaitGroup
	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			for j := 0; j < each; j++ {
				p.Note("shared")
				p.Note("also-shared")
			}
		}()
	}
	wg.Wait()

	snap := p.Snapshot()
	if snap["shared"] != goroutines*each || snap["also-shared"] != goroutines*each {
		t.Fatalf("counters = %v, want %d each", snap, goroutines*each)
	}
}

// The renderer writes its own integers rather than reaching for fmt on
// a path that runs per log line. Its answers have to match anyway.
func TestAppendInt(t *testing.T) {
	for _, tc := range []struct {
		in   int64
		want string
	}{
		{0, "0"},
		{1, "1"},
		{9, "9"},
		{10, "10"},
		{1234567890, "1234567890"},
		{-1, "-1"},
		{-1234567890, "-1234567890"},
	} {
		if got := string(appendInt(nil, tc.in)); got != tc.want {
			t.Errorf("appendInt(%d) = %q, want %q", tc.in, got, tc.want)
		}
	}

	// It appends rather than replacing, because the summary builds one
	// line out of many.
	if got := string(appendInt([]byte("count="), 42)); got != "count=42" {
		t.Errorf("= %q, want it appended", got)
	}
}

// A negative count should never happen, but if one ever does the line
// still renders rather than losing the whole log entry.
func TestSummaryLineWithACountItShouldNeverSee(t *testing.T) {
	var p Profile
	p.Note("odd")
	snap := p.Snapshot()
	if len(snap) != 1 {
		t.Fatal("one event")
	}
	if !strings.Contains(p.SummaryLine(), "odd=") {
		t.Errorf("= %q", p.SummaryLine())
	}
}
