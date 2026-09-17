package main

import (
	"context"
	"flag"
	"fmt"
	"os"
)

// pidfileRunning treats the presence of the pidfile as "serving": serve writes
// it on start and removes it on graceful exit; stop removes it too. Simple and
// cross-platform (no liveness syscall). A stale file after a hard crash is the
// documented edge case.
func pidfileRunning(pidfile string) bool {
	_, err := os.Stat(pidfile)
	return err == nil
}

// producerEnsurePlan is the pure ADR-0007 decision for the producer lifecycle:
// given the desired state and whether an instance is currently running, return
// the current-state string and whether a change is needed. ok=false for an
// unrecognised state (the caller emits the exit-2 validation error). Kept pure
// so the truth table is unit-tested without touching processes or stdout.
func producerEnsurePlan(state string, running bool) (from string, changed, ok bool) {
	from = "absent"
	if running {
		from = "present"
	}
	switch state {
	case "present":
		return from, !running, true
	case "absent":
		return from, running, true
	default:
		return from, false, false
	}
}

// runProducerEnsure is the canonical producer `ensure` verb (ADR-0007):
// declaratively converge a serving instance to --state present|absent, keyed on
// its --pidfile.
//
//   - absent: idempotent teardown — stop the running instance (reusing
//     signalPIDFile) or no-op if already stopped.
//   - present: no-op if already running; on --check report the drift. Actually
//     starting a foreground service isn't ensure's job (it can't start-and-
//     return), so apply-present when stopped errors with a pointer to `serve`
//     under a supervisor — the honest boundary, not a fake success.
//
// Emits the same {changed|would_change, diff[]} shape as every other ensure.
func runProducerEnsure(_ context.Context, protoName string, args []string) error {
	fs := flag.NewFlagSet("producer "+protoName+" ensure", flag.ContinueOnError)
	state := fs.String("state", "present", "desired state: present | absent (ADR-0007)")
	pidfile := fs.String("pidfile", "", "PID file the target serve wrote (required)")
	check := fs.Bool("check", false, "dry-run: report would_change, do nothing")
	output := fs.String("output", "text", "output format: text | json (ADR-0002)")
	if err := parseVerbFlags(fs, args); err != nil {
		return err
	}
	if *pidfile == "" {
		return fmt.Errorf("producer %s ensure: --pidfile is required", protoName)
	}
	jsonOut, oerr := resolveEnsureOutput(*output, false)
	if oerr != nil {
		return oerr
	}

	const field = "producer"
	running := pidfileRunning(*pidfile)
	from, changed, ok := producerEnsurePlan(*state, running)
	if !ok {
		return ensureValErr(fmt.Sprintf("--state %q: expected present | absent", *state))
	}

	// Dry-run: report drift, touch nothing.
	if *check {
		return emitEnsure(jsonOut, ensureResult{WouldChange: &changed, Current: from, Target: *state, Diff: ensureFieldDiff(changed, field, from, *state)})
	}
	// Already converged.
	if !changed {
		return emitEnsure(jsonOut, ensureResult{Changed: &changed, Previous: from, Current: from, Diff: []ensureDiff{}})
	}
	// Apply. present-when-stopped is the honest boundary: ensure can't
	// foreground-start-and-return, so it errors with a pointer to `serve` under a
	// supervisor rather than faking changed:true.
	if *state == "present" {
		return fmt.Errorf("producer %s ensure --state present: not running (pidfile %s absent) — a foreground service can't be started by ensure; run `dhs producer %s serve --pidfile %s ...` under your service manager",
			protoName, *pidfile, protoName, *pidfile)
	}
	// absent + running → stop.
	if _, err := signalPIDFile(*pidfile); err != nil {
		return err
	}
	applied := true
	return emitEnsure(jsonOut, ensureResult{Changed: &applied, Previous: "present", Current: "absent", Diff: ensureFieldDiff(true, field, "present", "absent")})
}
