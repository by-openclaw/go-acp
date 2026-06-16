package probelsw02p

// Test seams for branches that production code paths cannot reach
// because an upstream layer pre-validates the input. These are the
// SAME transparent nil-hook-in-prod pattern used elsewhere in the
// codebase: in production every hook is nil, so `decoded` is a pure
// passthrough and `treeBuildErr` is always nil — the guarded branches
// behave exactly as written. Tests set a hook to drive the otherwise
// dead defensive arm, never to weaken it.
//
// Why each seam exists (see provider/CLAUDE.md + codec/framer.go):
//
//   - decodeErrHook / decoded: codec.Unpack consults PayloadSize /
//     PayloadLen and only yields a Frame whose MESSAGE is exactly the
//     length every per-command codec.DecodeXxx requires. By the time a
//     frame reaches a handler the only DecodeXxx error possible is
//     ErrWrongCommand, which dispatch never triggers (it routes by
//     matching ID). So the handler-decode-error arm in session.run
//     ("herr != nil") is unreachable from the wire. Routing one
//     handler's decode through `decoded` lets a test force that error
//     and prove the session absorbs it (HandlerDecodeFailed) and keeps
//     reading, without touching the guard body.
//
//   - treeBuildErrHook: newTree never returns a non-nil error for any
//     canonical.Export (it degrades gracefully, returning an empty tree
//     for nil / malformed input). newServer's "tree build failed"
//     fallback is therefore defensive-only. The hook lets a test inject
//     the failure and prove newServer logs + substitutes an empty tree
//     rather than panicking on a nil tree.

// decodeErrHook, when non-nil, forces the next `decoded` call to return
// its error instead of the real decode result. Nil in production.
var decodeErrHook func() error

// decoded routes a per-command codec.DecodeXxx result through the test
// seam. In production (hook nil) it is an identity passthrough — the
// (value, err) pair is returned verbatim, so the guard that checks the
// error behaves exactly as if the codec were called directly. When the
// hook is set it overrides err, leaving value at its zero/decoded form.
func decoded[T any](v T, err error) (T, error) {
	if decodeErrHook != nil {
		return v, decodeErrHook()
	}
	return v, err
}

// treeBuildErrHook, when non-nil, forces newServer's newTree call to be
// treated as failed. Nil in production, where newTree never errors.
var treeBuildErrHook func() error
