//go:build !linux

package lldp

import (
	"context"
	"errors"
	"testing"
)

// On a platform that cannot capture, the answer must be the typed error and
// not an empty result. "No neighbours" would tell an operator their switch is
// misconfigured when the truth is that this host never looked.
func TestCaptureUnsupportedIsTyped(t *testing.T) {
	got, err := Capture{Iface: "eth0"}.Neighbors(context.Background())
	if !errors.Is(err, ErrCaptureUnsupported) {
		t.Errorf("err = %v, want ErrCaptureUnsupported", err)
	}
	if got != nil {
		t.Errorf("got %v, want no map alongside the error", got)
	}
}

func TestSupportedIsFalseHere(t *testing.T) {
	if Supported() {
		t.Error("this build has no capture path and must not claim one")
	}
}
