package main

import (
	"context"
	"strings"
	"testing"
)

// ADR-0037 P1: rrcs answers its own verbs and lets every other verb fall
// through to the generic table, where the registered plugin answers it.
func TestRRCSHandlesOwnVerbsOnly(t *testing.T) {
	for _, v := range []string{"info", "list", "import", "ensure", "set-xp-prio", "line-status"} {
		if !rrcsHandles(v) {
			t.Errorf("rrcsHandles(%q) = false, want true", v)
		}
	}
	for _, v := range []string{"health", "status", "alarm", "diff", "validate", "no-such-verb"} {
		if rrcsHandles(v) {
			t.Errorf("rrcsHandles(%q) = true, want false", v)
		}
	}
}

func TestRRCSNeutralVerbFallsThrough(t *testing.T) {
	out := captureStdout(t, func() {
		if err := dispatchConsumer(context.Background(), []string{"rrcs", "health", "--help"}); err != nil {
			t.Fatalf("dispatchConsumer rrcs health --help: %v", err)
		}
	})
	if !strings.Contains(out, "health") || strings.Contains(out, "unknown verb") {
		t.Fatalf("rrcs health --help did not reach the generic verb:\n%s", out)
	}
	// The dispatcher's own catalogue still answers help in place of a verb.
	out = captureStdout(t, func() {
		if err := dispatchConsumer(context.Background(), []string{"rrcs", "--help"}); err != nil {
			t.Fatalf("dispatchConsumer rrcs --help: %v", err)
		}
	})
	if !strings.Contains(out, "GENERIC VERBS") {
		t.Fatalf("rrcs --help does not name the neutral verbs:\n%s", out)
	}
}
