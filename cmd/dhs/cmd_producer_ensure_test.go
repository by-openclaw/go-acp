package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"dhs/internal/consumer"
)

// TestProducerEnsurePlan pins the ADR-0007 truth table for the producer
// lifecycle: present converges an already-running instance to no-op and flags a
// stopped one as drift; absent is the mirror; an unknown state is not ok (the
// caller turns that into an exit-2 validation error). Run-twice idempotency
// follows directly — after the first apply the running flag flips, so the second
// call returns changed=false.
func TestProducerEnsurePlan(t *testing.T) {
	cases := []struct {
		name        string
		state       string
		running     bool
		wantFrom    string
		wantChanged bool
		wantOK      bool
	}{
		{"present + running = no-op", "present", true, "present", false, true},
		{"present + stopped = drift", "present", false, "absent", true, true},
		{"absent + running = stop", "absent", true, "present", true, true},
		{"absent + stopped = no-op", "absent", false, "absent", false, true},
		{"unknown state not ok", "paused", true, "present", false, false},
		{"empty state not ok", "", false, "absent", false, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			from, changed, ok := producerEnsurePlan(tc.state, tc.running)
			if from != tc.wantFrom || changed != tc.wantChanged || ok != tc.wantOK {
				t.Errorf("producerEnsurePlan(%q,%v) = (%q,%v,%v), want (%q,%v,%v)",
					tc.state, tc.running, from, changed, ok, tc.wantFrom, tc.wantChanged, tc.wantOK)
			}
		})
	}
}

// TestPidfileRunning pins the state indicator: pidfile present = serving,
// absent = stopped. Presence, not liveness, keeps it cross-platform.
func TestPidfileRunning(t *testing.T) {
	dir := t.TempDir()
	missing := filepath.Join(dir, "none.pid")
	if pidfileRunning(missing) {
		t.Errorf("pidfileRunning(%q) = true, want false (file absent)", missing)
	}
	present := filepath.Join(dir, "there.pid")
	if err := os.WriteFile(present, []byte("123\n"), 0o644); err != nil {
		t.Fatalf("seed pidfile: %v", err)
	}
	if !pidfileRunning(present) {
		t.Errorf("pidfileRunning(%q) = false, want true (file present)", present)
	}
}

// TestRunProducerEnsure_RequiresPidfile pins that --pidfile is mandatory: no
// state can be decided without it, so the verb errors before doing anything.
func TestRunProducerEnsure_RequiresPidfile(t *testing.T) {
	if err := runProducerEnsure(context.Background(), "emberplus", []string{"--state", "absent"}); err == nil {
		t.Fatal("runProducerEnsure without --pidfile: err = nil, want error")
	}
}

// TestRunProducerEnsure_UnknownStateIsExit2 pins that a bad --state is a
// client-side validation error (exit 2), not a runtime failure.
func TestRunProducerEnsure_UnknownStateIsExit2(t *testing.T) {
	pid := filepath.Join(t.TempDir(), "x.pid")
	err := runProducerEnsure(context.Background(), "emberplus", []string{"--state", "paused", "--pidfile", pid})
	if err == nil {
		t.Fatal("runProducerEnsure --state paused: err = nil, want validation error")
	}
	var verr *consumer.ValidationError
	if !errors.As(err, &verr) {
		t.Errorf("err = %T, want *consumer.ValidationError (exit 2)", err)
	}
}

// TestRunProducerEnsure_AbsentNoop pins that absent against a stopped instance
// is a clean no-op (no error, no file to signal).
func TestRunProducerEnsure_AbsentNoop(t *testing.T) {
	pid := filepath.Join(t.TempDir(), "stopped.pid")
	if err := runProducerEnsure(context.Background(), "emberplus", []string{"--state", "absent", "--pidfile", pid, "--output", "json"}); err != nil {
		t.Fatalf("absent no-op: err = %v, want nil", err)
	}
}

// TestRunProducerEnsure_PresentApplyStoppedErrors pins the honest boundary:
// applying present to a stopped instance can't foreground-start it, so it errors
// rather than reporting a fake change. --check on the same state must NOT error
// (it only reports drift).
func TestRunProducerEnsure_PresentApplyStoppedErrors(t *testing.T) {
	pid := filepath.Join(t.TempDir(), "stopped.pid")
	if err := runProducerEnsure(context.Background(), "emberplus", []string{"--state", "present", "--pidfile", pid}); err == nil {
		t.Fatal("present apply on stopped instance: err = nil, want error (cannot foreground-start)")
	}
	if err := runProducerEnsure(context.Background(), "emberplus", []string{"--state", "present", "--pidfile", pid, "--check", "--output", "json"}); err != nil {
		t.Fatalf("present --check on stopped instance: err = %v, want nil (drift report, not error)", err)
	}
}

// TestRunProducerEnsure_AbsentCheckLeavesPidfile pins that --check is a true
// dry-run: it reports would_change but does not remove the pidfile.
func TestRunProducerEnsure_AbsentCheckLeavesPidfile(t *testing.T) {
	pid := filepath.Join(t.TempDir(), "running.pid")
	if err := os.WriteFile(pid, []byte("123\n"), 0o644); err != nil {
		t.Fatalf("seed pidfile: %v", err)
	}
	if err := runProducerEnsure(context.Background(), "emberplus", []string{"--state", "absent", "--pidfile", pid, "--check", "--output", "json"}); err != nil {
		t.Fatalf("absent --check: err = %v, want nil", err)
	}
	if !pidfileRunning(pid) {
		t.Error("absent --check removed the pidfile; dry-run must not touch state")
	}
}
