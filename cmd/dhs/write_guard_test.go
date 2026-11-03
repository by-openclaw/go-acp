package main

import (
	"errors"
	"strings"
	"testing"

	"dhs/internal/consumer"
)

// guardedStub is a plugin that asks for its writes to be confirmed.
type guardedStub struct{ consumer.Protocol }

func (guardedStub) WriteGuardReason() string { return "a test reason" }

// plainStub is a plugin without the guard.
type plainStub struct{ consumer.Protocol }

func TestWriteGuard(t *testing.T) {
	isValidation := func(err error) bool {
		var v *consumer.ValidationError
		return errors.As(err, &v)
	}
	// A guarded plugin without --write-to: refused, with the reason.
	err := writeGuard("set", "192.0.2.10", "", guardedStub{})
	if !isValidation(err) || !strings.Contains(err.Error(), "--write-to 192.0.2.10") || !strings.Contains(err.Error(), "a test reason") {
		t.Fatalf("guarded, no --write-to: %v", err)
	}
	// The same with the host named twice: the write goes.
	if err := writeGuard("set", "192.0.2.10", "192.0.2.10", guardedStub{}); err != nil {
		t.Fatalf("guarded, confirmed: %v", err)
	}
	// Another target named: refused on every plugin.
	for _, p := range []consumer.Protocol{guardedStub{}, plainStub{}} {
		err := writeGuard("import", "192.0.2.10", "192.0.2.11", p)
		if !isValidation(err) || !strings.Contains(err.Error(), "does not match") {
			t.Fatalf("other target on %T: %v", p, err)
		}
	}
	// A plugin without the guard writes at once.
	if err := writeGuard("ensure", "192.0.2.10", "", plainStub{}); err != nil {
		t.Fatalf("plain, no --write-to: %v", err)
	}
}
