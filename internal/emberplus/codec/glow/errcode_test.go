package glow

import (
	"errors"
	"strings"
	"testing"

	"dhs/internal/emberplus/codec/ber"
	"dhs/internal/errcode"
)

// TestGlow_Sentinels_ShapeAndClass pins the glow:decode-failed code's
// wire shape + class.
func TestGlow_Sentinels_ShapeAndClass(t *testing.T) {
	if got := ErrDecodeFailed.Error(); got != "glow:decode-failed" {
		t.Errorf("Error() = %q, want %q", got, "glow:decode-failed")
	}
	if ErrDecodeFailed.Layer != errcode.LayerGlow {
		t.Errorf("Layer = %q, want %q", ErrDecodeFailed.Layer, errcode.LayerGlow)
	}
	if ErrDecodeFailed.Class != errcode.ClassRuntime {
		t.Errorf("Class = %d, want ClassRuntime (1)", ErrDecodeFailed.Class)
	}
	if got := errcode.Exit(ErrDecodeFailed); got != 1 {
		t.Errorf("Exit() = %d, want 1", got)
	}
}

// TestDecodeRoot_BadBytes_WrapsBothLayers pins that a BER decode failure
// produces an error that:
//   - matches errors.Is(err, glow.ErrDecodeFailed) — the wrap layer
//   - matches errors.Is(err, ber.ErrTruncated)    — the underlying cause
//
// This is the operator-facing benefit of the layered taxonomy: scripts
// can dispatch at any layer in the chain.
func TestDecodeRoot_BadBytes_WrapsBothLayers(t *testing.T) {
	// Single byte: tag without length/content — triggers ber:truncated.
	_, err := DecodeRoot([]byte{0x60})
	if err == nil {
		t.Fatal("expected decode error, got nil")
	}
	if !errors.Is(err, ErrDecodeFailed) {
		t.Errorf("err = %v, want errors.Is(err, glow.ErrDecodeFailed)", err)
	}
	if !errors.Is(err, ber.ErrTruncated) {
		t.Errorf("err = %v, want errors.Is(err, ber.ErrTruncated) — underlying cause should chain through", err)
	}
	if !strings.HasPrefix(err.Error(), "glow:decode-failed:") {
		t.Errorf("err string = %q, want glow:decode-failed: prefix", err.Error())
	}
}
