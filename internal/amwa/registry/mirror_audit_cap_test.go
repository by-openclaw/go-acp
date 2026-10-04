package registry

// The audit trail stays readable and bounded (#1311): a burst of
// identical observations is one line and a count, and the file rotates
// at a size cap with one previous generation kept.

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// readAudit returns the events of one audit file, in order.
func readAudit(t *testing.T, path string) []AuditEvent {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	var out []AuditEvent
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		var ev AuditEvent
		if err := json.Unmarshal(sc.Bytes(), &ev); err != nil {
			t.Fatalf("line not JSON: %v (%s)", err, sc.Text())
		}
		out = append(out, ev)
	}
	return out
}

// steppedClock hands out instants a fixed step apart.
func steppedClock(step time.Duration) func() time.Time {
	at := time.Date(2026, 10, 2, 14, 7, 0, 0, time.UTC)
	return func() time.Time {
		at = at.Add(step)
		return at
	}
}

var refusal = map[string]any{"err": "HTTP 400", "op": "POST", "topic": "flows"}

// The storm of 2026-10-02 in miniature: 2 500 identical refusals a
// millisecond apart are one line and three counts, not 2 500 lines.
func TestAuditorCountsABurstOnce(t *testing.T) {
	path := filepath.Join(t.TempDir(), "audit.jsonl")
	a, err := newAuditor(path, 0)
	if err != nil {
		t.Fatal(err)
	}
	a.now = steppedClock(time.Millisecond)
	for i := 0; i < 2500; i++ {
		a.event("forward_failed", refusal)
	}

	// While the burst lasts, the status endpoint shows the count so far.
	recent := a.recent()
	if last := recent[len(recent)-1]; last.Kind != "repeated" || last.Detail["times"] != 499 {
		t.Errorf("the tail of the ring = %+v, want the 499 repeats still being counted", last)
	}

	a.event("resync", nil) // the burst ends
	a.close()

	events := readAudit(t, path)
	var kinds []string
	total := 0
	for _, ev := range events {
		kinds = append(kinds, ev.Kind)
		if ev.Kind == "repeated" {
			if ev.Detail["kind"] != "forward_failed" {
				t.Errorf("a count must say what it counts: %+v", ev)
			}
			total += int(ev.Detail["times"].(float64))
		}
	}
	want := []string{"forward_failed", "repeated", "repeated", "repeated", "resync"}
	if len(kinds) != len(want) {
		t.Fatalf("lines = %v, want %v", kinds, want)
	}
	for i := range want {
		if kinds[i] != want[i] {
			t.Fatalf("lines = %v, want %v", kinds, want)
		}
	}
	if total != 2499 {
		t.Errorf("the counts add up to %d, want the 2 499 that followed the first", total)
	}
	// The count carries the time of the last repeat it stands for.
	if events[1].TS <= events[0].TS {
		t.Errorf("a count is stamped %s, not after its first line %s", events[1].TS, events[0].TS)
	}
}

// The same observation minutes later is a new fact, written in full.
func TestAuditorWritesAnIdenticalEventAgainAfterTheWindow(t *testing.T) {
	path := filepath.Join(t.TempDir(), "audit.jsonl")
	a, err := newAuditor(path, 0)
	if err != nil {
		t.Fatal(err)
	}
	a.now = steppedClock(30 * time.Second)
	for i := 0; i < 3; i++ {
		a.event("target_evicted", map[string]any{"node": "n1"})
	}
	a.close()

	events := readAudit(t, path)
	if len(events) != 3 {
		t.Fatalf("%d lines, want the three evictions in full: %+v", len(events), events)
	}
	for _, ev := range events {
		if ev.Kind != "target_evicted" {
			t.Errorf("unexpected line %+v", ev)
		}
	}
}

// An observation that cannot be encoded is never taken for a repeat,
// and never reaches the file half-written.
func TestAuditorKeepsAnUnencodableEventInTheRingOnly(t *testing.T) {
	path := filepath.Join(t.TempDir(), "audit.jsonl")
	a, err := newAuditor(path, 0)
	if err != nil {
		t.Fatal(err)
	}
	odd := map[string]any{"c": make(chan int)}
	a.event("odd", odd)
	a.event("odd", odd)
	if got := a.recent(); len(got) != 2 {
		t.Errorf("ring = %+v, want both events", got)
	}
	a.close()
	if events := readAudit(t, path); len(events) != 0 {
		t.Errorf("file = %+v, want nothing written", events)
	}
}

// numbered writes n distinct observations.
func numbered(a *auditor, from, n int) {
	for i := from; i < from+n; i++ {
		a.event("serve_query", map[string]any{"n": i})
	}
}

// At its cap the trail moves to <path>.1 and starts again; the next
// rotation replaces that generation. Nothing is lost at a rotation.
func TestAuditorRotatesAtItsCap(t *testing.T) {
	path := filepath.Join(t.TempDir(), "audit.jsonl")
	a, err := newAuditor(path, 700)
	if err != nil {
		t.Fatal(err)
	}
	numbered(a, 0, 12) // about 70 bytes a line: one rotation
	a.close()

	older, newer := readAudit(t, path+".1"), readAudit(t, path)
	if len(older) == 0 || len(newer) == 0 || len(older)+len(newer) != 12 {
		t.Fatalf("after one rotation: %d + %d lines, want the 12 split over two generations", len(older), len(newer))
	}
	if st, _ := os.Stat(path + ".1"); st.Size() > 700 {
		t.Errorf("the rotated generation is %d bytes, past its cap", st.Size())
	}
	if older[0].Detail["n"] != float64(0) || newer[len(newer)-1].Detail["n"] != float64(11) {
		t.Errorf("order lost across the rotation: first %+v, last %+v", older[0], newer[len(newer)-1])
	}

	// A mirror restarted on an existing trail carries on from its size.
	b, err := newAuditor(path, 700)
	if err != nil {
		t.Fatal(err)
	}
	numbered(b, 12, 12)
	b.close()
	older, newer = readAudit(t, path+".1"), readAudit(t, path)
	if older[0].Detail["n"] == float64(0) {
		t.Error("the first generation is still there: the second rotation must replace it")
	}
	if newer[len(newer)-1].Detail["n"] != float64(23) {
		t.Errorf("the newest line is %+v, want n=23", newer[len(newer)-1])
	}
}

// A trail that cannot be rotated keeps growing where it is rather than
// losing what it holds, and does not try again at every line.
func TestAuditorKeepsWritingWhenItCannotRotate(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "audit.jsonl")
	// <path>.1 is a directory with something in it: the rename fails.
	if err := os.MkdirAll(filepath.Join(path+".1", "in-the-way"), 0o755); err != nil {
		t.Fatal(err)
	}
	a, err := newAuditor(path, 300)
	if err != nil {
		t.Fatal(err)
	}
	numbered(a, 0, 12)
	a.close()

	if events := readAudit(t, path); len(events) != 12 {
		t.Errorf("%d lines kept, want all 12 in the one file", len(events))
	}
	if a.max != 0 {
		t.Errorf("max = %d after a failed rotation, want rotation given up", a.max)
	}
}

// A trail whose directory vanished cannot be reopened: the ring goes
// on, the mirror does not stop.
func TestAuditorFallsBackToTheRingWhenItsFileIsGone(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "trail")
	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	a, err := newAuditor(filepath.Join(dir, "audit.jsonl"), 300)
	if err != nil {
		t.Fatal(err)
	}
	numbered(a, 0, 2)
	// The trail's home goes away under it (the handle is closed first
	// so the removal works on every OS the suite runs on).
	a.mu.Lock()
	_ = a.f.Close()
	a.mu.Unlock()
	if err := os.RemoveAll(dir); err != nil {
		t.Fatal(err)
	}
	a.mu.Lock()
	a.size = 299 // the next line is the one that rotates
	a.mu.Unlock()

	numbered(a, 2, 3)

	if a.f != nil {
		t.Error("the file handle must be dropped once the trail cannot be reopened")
	}
	if got := a.recent(); len(got) != 5 {
		t.Errorf("ring holds %d events, want all 5", len(got))
	}
	a.close() // nothing to close: must not panic
}
