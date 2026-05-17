package main

import (
	"errors"
	"fmt"
	"testing"

	"dhs/internal/consumer"
	"dhs/internal/emberplus/codec/matrix"
	embconsumer "dhs/internal/emberplus/consumer"
	"dhs/internal/errcode"
	"dhs/internal/transport"
)

// TestExitCode_LockedContract pins the cross-OS 0/1/2 contract per memory
// feedback_error_contract_cross_os. Every category resolves through the
// errcode.Code chain (or the ValidationError struct fallback) to the
// right small-integer exit code.
func TestExitCode_LockedContract(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want int
	}{
		// Success
		{name: "nil err → 0", err: nil, want: 0},

		// Class 1 — runtime / wire / protocol
		{name: "transport:refused → 1", err: transport.ErrRefused, want: 1},
		{name: "transport:timeout → 1", err: transport.ErrTimeout, want: 1},
		{name: "transport:dial-failed → 1", err: transport.ErrDialFailed, want: 1},
		{name: "transport:close-failed → 1", err: transport.ErrCloseFailed, want: 1},
		{name: "matrix:target-locked → 1", err: matrix.ErrTargetLocked, want: 1},
		{name: "matrix:cardinality-exceeded → 1", err: matrix.ErrCardinalityExceeded, want: 1},
		{name: "matrix:max-connects-per-target → 1", err: matrix.ErrMaxConnectsPerTarget, want: 1},
		{name: "emberplus:invocation-failed → 1", err: embconsumer.ErrInvocationFailed, want: 1},
		{name: "session:write-timeout → 1", err: consumer.ErrWriteTimeout, want: 1},
		{name: "session:write-coerced → 1", err: consumer.ErrWriteCoerced, want: 1},
		{name: "session:write-rejected → 1", err: consumer.ErrWriteRejected, want: 1},

		// Class 2 — usage / validation / state
		{name: "validation:invalid-host → 2", err: transport.ErrInvalidHost, want: 2},
		{name: "validation:invalid-port → 2", err: transport.ErrInvalidPort, want: 2},
		{name: "validation:invalid-max-size → 2", err: transport.ErrInvalidMaxSize, want: 2},
		{name: "validation:empty-payload → 2", err: transport.ErrEmptyPayload, want: 2},
		{name: "validation:failed → 2", err: consumer.ErrValidationFailed, want: 2},
		{name: "plugin:not-connected → 2", err: consumer.ErrNotConnected, want: 2},
		{name: "plugin:not-implemented → 2", err: consumer.ErrNotImplemented, want: 2},
		{name: "plugin:unknown-label → 2", err: consumer.ErrUnknownLabel, want: 2},
		{name: "plugin:object-not-found → 2", err: consumer.ErrObjectNotFound, want: 2},
		{name: "plugin:identity-unresolved → 2", err: consumer.ErrIdentityUnresolved, want: 2},
		{name: "validation:invalid-format → 2", err: consumer.ErrInvalidFormat, want: 2},
		{name: "validation:out-of-range-low → 2", err: consumer.ErrOutOfRangeLow, want: 2},
		{name: "validation:out-of-range-high → 2", err: consumer.ErrOutOfRangeHigh, want: 2},
		{name: "validation:step-misaligned → 2", err: consumer.ErrStepMisaligned, want: 2},
		{name: "validation:invalid-enum-label → 2", err: consumer.ErrInvalidEnumLabel, want: 2},
		{name: "validation:enum-not-supported → 2", err: consumer.ErrEnumNotSupported, want: 2},
		{name: "validation:round-not-applicable → 2", err: consumer.ErrRoundNotApplicable, want: 2},

		// Wrapped chains — the typed code is still discovered.
		{name: "wrapped transport:refused → 1", err: fmt.Errorf("%w: connect 127.0.0.1:9100: ...", transport.ErrRefused), want: 1},
		{name: "wrapped validation:invalid-host → 2", err: fmt.Errorf("%w: empty host", transport.ErrInvalidHost), want: 2},
		{name: "wrapped matrix:target-locked → 1", err: fmt.Errorf("%w: target 2 is locked", matrix.ErrTargetLocked), want: 1},

		// Legacy ValidationError struct → 2 (back-compat bridge).
		{name: "legacy ValidationError struct → 2", err: &consumer.ValidationError{Field: "value", Reason: "invalid integer"}, want: 2},

		// Untyped runtime error → 1 (safe runtime fallback).
		{name: "untyped error → 1", err: errors.New("something went wrong"), want: 1},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := exitCode(tc.err); got != tc.want {
				t.Errorf("exitCode(%v) = %d, want %d", tc.err, got, tc.want)
			}
		})
	}
}

// TestExitCode_NeverThreeOrMore pins the cross-OS contract — exitCode
// MUST return only 0, 1, or 2. Never 3+.
func TestExitCode_NeverThreeOrMore(t *testing.T) {
	// Every typed sentinel we know about.
	sentinels := []*errcode.Code{
		transport.ErrRefused, transport.ErrTimeout, transport.ErrDialFailed,
		transport.ErrListenFailed, transport.ErrNilConn, transport.ErrPayloadTooLarge,
		transport.ErrSetDeadlineFailed, transport.ErrWriteFailed, transport.ErrShortWrite,
		transport.ErrReadFailed, transport.ErrOversizedDatagram, transport.ErrMLENOutOfRange,
		transport.ErrWrongConnType, transport.ErrCloseFailed, transport.ErrCaptureCreateFailed,
		transport.ErrInvalidHost, transport.ErrInvalidPort, transport.ErrEmptyPayload,
		transport.ErrInvalidMaxSize,
		matrix.ErrTargetLocked, matrix.ErrCardinalityExceeded,
		matrix.ErrMaxConnectsPerTarget, matrix.ErrMaxTotalConnects,
		embconsumer.ErrInvocationFailed, embconsumer.ErrInvocationFailedWithDescription,
		consumer.ErrNotImplemented, consumer.ErrNotConnected, consumer.ErrUnknownLabel,
		consumer.ErrWriteTimeout, consumer.ErrWriteCoerced, consumer.ErrWriteRejected,
		consumer.ErrObjectNotFound, consumer.ErrValidationFailed, consumer.ErrIdentityUnresolved,
		consumer.ErrInvalidFormat,
		consumer.ErrOutOfRangeLow, consumer.ErrOutOfRangeHigh, consumer.ErrStepMisaligned,
		consumer.ErrInvalidEnumLabel, consumer.ErrEnumNotSupported, consumer.ErrRoundNotApplicable,
	}
	for _, s := range sentinels {
		got := exitCode(s)
		if got != 1 && got != 2 {
			t.Errorf("exitCode(%v) = %d, want 1 or 2 (never 3+)", s, got)
		}
	}
	// And untyped runtime errors must also bucket to 1 or 2.
	if got := exitCode(errors.New("foo")); got != 1 {
		t.Errorf("exitCode(untyped) = %d, want 1", got)
	}
	if got := exitCode(nil); got != 0 {
		t.Errorf("exitCode(nil) = %d, want 0", got)
	}
}
