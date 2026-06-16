package probelsw02p

import "dhs/internal/probel-sw02p/codec"

// decodeErrHook is a test-only injection seam for the post-Unpack decode
// guards scattered across the per-command Send / Subscribe methods.
//
// Every reply / listener frame this consumer decodes has already passed
// codec.Unpack — which validates SOM, command id, MESSAGE length, and the
// 7-bit checksum. By the time a frame reaches a codec.DecodeXxx call here
// it is therefore structurally sound, so the `derr != nil` guard bodies
// (wrap-and-return on the Send path, return-false on a matcher, skip on a
// Subscribe listener) cannot be provoked through the public API.
//
// The codeowner's decision (cf. the bcastDialErrHook idiom in the acp1
// provider and the encodeManifest / closeFile seams in internal/manifest)
// is to KEEP those defensive guards and add this single shared seam so a
// test can reach them. In production decodeErrHook is nil and decoded is a
// transparent passthrough — behaviour is identical to calling the codec
// function directly. A test installs a (typically stateful) hook to force a
// synthetic decode error on the call it wants to drive: returning an error
// on the matcher invocation exercises the return-false / skip arms, while
// returning nil first (matcher accepts) then an error exercises the
// post-Send wrap-and-return arm.
//
// The hook is parameterless so the helper can consume a codec.DecodeXxx
// (value, error) result directly via Go's f(g()) call-forwarding — the id
// is not needed because tests drive one Send at a time and sequence the
// hook by call count.
var decodeErrHook func() error

// decoded routes a codec.DecodeXxx (value, error) result through the
// decodeErrHook seam. It is the single funnel every otherwise-unreachable
// decode-error guard in this package goes through. When the underlying
// decode already failed, or the hook is nil (production), the pair is
// returned unchanged; only when the real decode succeeded AND a test has
// installed a hook that returns non-nil does decoded substitute the forced
// error.
func decoded[T any](v T, err error) (T, error) {
	if err == nil && decodeErrHook != nil {
		if ferr := decodeErrHook(); ferr != nil {
			return v, ferr
		}
	}
	return v, err
}

// routerConfigExtraMatch is a test-only injection seam for
// SendRouterConfigRequest (cmd_rx075). Its reply matcher accepts exactly
// tx 076 / tx 077, the same two IDs the post-Send switch handles, so the
// trailing "unexpected reply ID" invariant guard is unreachable through
// the public API. When non-nil this hook lets a test widen the matcher to
// accept a third frame the switch then rejects, exercising that guard. Nil
// in production — matcher behaviour is unchanged.
var routerConfigExtraMatch func(codec.Frame) bool

// commandName routes validate.go's per-trame catalogue lookup through a
// test seam. codec.Unpack already rejects any command id absent from the
// codec's PayloadSize registry, and that registry is exactly the set
// codec.CommandName knows, so a trame that survives Unpack always has a
// non-empty name — leaving the validate.go unknown-command invariant
// branch unreachable through input. In production commandName is the real
// codec.CommandName; a test overrides it to force the empty-name case.
var commandName = codec.CommandName
