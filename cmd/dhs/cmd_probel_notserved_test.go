package main

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	probelproto "dhs/internal/probel-sw08p/consumer"
)

// A request the matrix ACKed and did not answer is reported as what it is —
// a command this matrix does not serve — and keeps the error it came from.
// Anything else passes through untouched.
func TestProbelNotServedNamesAnUnansweredCommand(t *testing.T) {
	if got := probelNotServed(nil); got != nil {
		t.Errorf("nil became %v", got)
	}
	other := errors.New("connection refused")
	if got := probelNotServed(other); got != other {
		t.Errorf("an unrelated error was rewritten: %v", got)
	}

	noReply := fmt.Errorf("probel dual-status: probel cmd 8: %w: %w", probelproto.ErrNoReply, context.DeadlineExceeded)
	got := probelNotServed(noReply)
	if !errors.Is(got, probelproto.ErrNoReply) || !errors.Is(got, context.DeadlineExceeded) {
		t.Errorf("the cause is lost: %v", got)
	}
	if !strings.Contains(got.Error(), "not served by this matrix") {
		t.Errorf("the verdict is not in the message: %v", got)
	}
}
