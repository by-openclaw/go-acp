// Code sentinels for Ember+ consumer-side semantic failures.
//
// Wraps wire-level rejection signals (InvocationResult.Success=false,
// Connection.Disposition=locked, offlineElement) with typed *errcode.Code
// instances so callers (CLI, scripts, Ansible) can dispatch via
// errors.Is(err, emberplus.ErrXxx).
//
// Ember+ has NO native wire-level error codes — the wire carries only
// Success (boolean) + Disposition (4-value enum) + offlineElement marker.
// These dhs sentinels are this project's invention layered on top of
// those signals; operators won't find them in the Ember+ Documentation
// PDF. Per memory feedback_error_contract_cross_os.
//
// Wire form: "emberplus:<code>: <human message>".
package emberplus

import "dhs/internal/errcode"

// All emberplus:* codes are runtime semantic-rejection failures
// (Class 1, exit 1).
var (
	// ErrInvocationFailed is returned when an Invoke completes with
	// InvocationResult.Success=false on the wire. The caller's
	// InvokeFunction still returns the *glow.InvocationResult so the
	// result tuple (if any) is visible — but the error signals exit 1
	// so scripts dispatch correctly.
	//
	// Per Ember+ Doc §p.92.
	ErrInvocationFailed = errcode.New(errcode.LayerEmberplus, "invocation-failed", errcode.ClassRuntime)

	// ErrInvocationFailedWithDescription is reserved for future use —
	// once the codec parses the optional description field in
	// InvocationResult (currently not decoded), failures carrying a
	// non-empty description will use this code so operators can see
	// the provider's human reason. For now, every Success=false maps
	// to ErrInvocationFailed.
	//
	// Per Ember+ Doc §p.92.
	ErrInvocationFailedWithDescription = errcode.New(errcode.LayerEmberplus, "invocation-failed-with-description", errcode.ClassRuntime)
)
