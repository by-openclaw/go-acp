package main

import (
	"bytes"
	"io"
	"os"
	"strings"
	"testing"

	"dhs/internal/consumer"
)

// captureStdout runs fn with os.Stdout redirected and returns what it printed.
// The help screens write with fmt.Print, so there is nothing else to hook.
func captureStdout(t *testing.T, fn func()) string {
	t.Helper()

	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	saved := os.Stdout
	os.Stdout = w
	defer func() { os.Stdout = saved }()

	done := make(chan string, 1)
	go func() {
		var buf bytes.Buffer
		_, _ = io.Copy(&buf, r)
		done <- buf.String()
	}()

	fn()
	if err := w.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	return <-done
}

func TestConsumerHelpListsWhatIsRegistered(t *testing.T) {
	// The help screen used to carry its own catalogue, and three protocols had
	// accumulated that it never mentioned. Nothing failed: they worked, they
	// were simply undiscoverable. This is what stops that recurring.
	out := captureStdout(t, func() { printConsumerHelp() })

	registered := consumer.List()
	if len(registered) == 0 {
		t.Fatal("no protocols are registered; the blank imports are missing")
	}
	for _, name := range registered {
		if !strings.Contains(out, name) {
			t.Errorf("%s is registered and `dhs consumer -h` does not mention it", name)
		}
	}
}

func TestListProtocolsNamesEveryRegisteredPlugin(t *testing.T) {
	out := captureStdout(t, func() {
		if err := runListProtocols(); err != nil {
			t.Errorf("list-protocols: %v", err)
		}
	})
	for _, name := range consumer.List() {
		if !strings.Contains(out, name) {
			t.Errorf("list-protocols omits %s", name)
		}
	}
}

func TestAShortDescriptionDoesNotEndMidBracket(t *testing.T) {
	got := shortDescription(
		"EVS Cerebrum Northbound API (XML over WebSocket; a.k.a. Neuron Bridge)", 60)
	if strings.Count(got, "(") != strings.Count(got, ")") {
		t.Errorf("%q leaves a bracket open", got)
	}

	// A description that already fits is left exactly as it was written.
	const fits = "Ember+ (Glow/S101/TCP) consumer"
	if got := shortDescription(fits, 60); got != fits {
		t.Errorf("a description that fits was altered: %q", got)
	}

	// The width counts characters rather than bytes, so a description of
	// multi-byte runes is not cut in the middle of one.
	long := strings.Repeat("é", 100)
	if r := []rune(shortDescription(long, 10)); len(r) != 11 {
		t.Errorf("a long description became %d runes, want 10 and an ellipsis", len(r))
	}
}
