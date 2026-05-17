// Code sentinels for the matrix codec layer.
//
// Wraps the pre-flight CanConnect validation errors with typed
// *errcode.Code instances so callers can dispatch via
// errors.Is(err, matrix.ErrXxx). All Class 1 (runtime, exit 1).
// Wire form: "matrix:<code>: <human message>".
//
// R1e migration — replaces the free-text fmt.Errorf strings in
// state.go::CanConnect with typed sentinels per the Ember+ Doc §p.33
// matrix-type cardinality rules + §p.89 ConnectionDisposition.locked.
package matrix

import "dhs/internal/errcode"

// All matrix:* codes are runtime / spec-rejection failures (Class 1,
// exit 1). They fire pre-flight at the consumer (and again at the
// provider side after wire-receive) when a Connect would violate the
// matrix type's cardinality or hit a locked target.
var (
	// ErrTargetLocked is returned when the target's current
	// ConnectionDisposition is Locked(3). Per Ember+ Doc §p.89.
	ErrTargetLocked = errcode.New(errcode.LayerMatrix, "target-locked", errcode.ClassRuntime)

	// ErrCardinalityExceeded fires when a oneToN or oneToOne matrix
	// would have more than one source connected to a target. Per
	// Ember+ Doc §p.33.
	ErrCardinalityExceeded = errcode.New(errcode.LayerMatrix, "cardinality-exceeded", errcode.ClassRuntime)

	// ErrMaxConnectsPerTarget fires when an nToN matrix's per-target
	// limit would be exceeded. Per Ember+ Doc §p.88.
	ErrMaxConnectsPerTarget = errcode.New(errcode.LayerMatrix, "max-connects-per-target", errcode.ClassRuntime)

	// ErrMaxTotalConnects fires when an nToN matrix's total-connections
	// limit would be exceeded across all targets. Per Ember+ Doc §p.88.
	ErrMaxTotalConnects = errcode.New(errcode.LayerMatrix, "max-total-connects", errcode.ClassRuntime)
)
