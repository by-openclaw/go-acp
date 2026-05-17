package ber

import "dhs/internal/errcode"

// Code sentinels for the BER (ASN.1 Basic Encoding Rules) decode layer.
//
// Wraps raw decode failures with typed *errcode.Code instances so callers
// can dispatch via errors.Is(err, ber.ErrXxx). All Class 1 (runtime, exit 1).
// Wire form on stderr: "ber:<code>: <human message>".
//
// R1d migration — promotes the prior package-private errors.New sentinels
// to typed errcode.Code and exports them so external callers (glow decoder,
// CLI exit dispatcher) can recognise them.
var (
	// ErrTruncated is returned when the input buffer ends mid-element
	// (insufficient bytes for the declared tag / length / content).
	ErrTruncated = errcode.New(errcode.LayerBER, "truncated", errcode.ClassRuntime)

	// ErrTagTooLong is returned when a high-tag-number form exceeds the
	// 4-byte limit dhs enforces (X.690 §8.1.2.4 permits unbounded
	// extension; dhs caps at 4 bytes for safety).
	ErrTagTooLong = errcode.New(errcode.LayerBER, "tag-too-long", errcode.ClassRuntime)

	// ErrLengthTooLong is returned when a long-form length exceeds 4
	// bytes (X.690 §8.1.3.5 permits unbounded; dhs caps at 4 bytes).
	ErrLengthTooLong = errcode.New(errcode.LayerBER, "length-too-long", errcode.ClassRuntime)

	// ErrInvalidReal is returned when REAL bytes fail to decode per
	// X.690 §8.5 + the Ember+ ecosystem normalised-fraction convention
	// (see memory reference_emberplus_ber_real and TestReal_EcosystemBytes
	// in codec/ber/value_test.go).
	ErrInvalidReal = errcode.New(errcode.LayerBER, "invalid-real", errcode.ClassRuntime)

	// ErrOverflow is returned when an INTEGER's content bytes encode a
	// value outside int64 range.
	ErrOverflow = errcode.New(errcode.LayerBER, "integer-overflow", errcode.ClassRuntime)
)

