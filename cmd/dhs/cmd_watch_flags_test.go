package main

import (
	"context"
	"strings"
	"testing"
)

// Every watch defines its flags before it looks at its arguments. From
// 2026-09-23 two of them were called metrics-addr — the shared consumer
// one and a watch copy — and every watch panicked at start-up, before
// dialling anything. --help printed first and hid it; this reaches the
// definitions.
func TestWatchFlagsAreDefinedOnce(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("defining the watch flags panicked: %v", r)
		}
	}()
	err := runWatch(context.Background(), nil)
	if err == nil || !strings.Contains(err.Error(), "usage") {
		t.Fatalf("err = %v; want the usage error for a missing host", err)
	}
}
