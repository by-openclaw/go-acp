package consumer

// WriteGuard is the optional contract of a plugin whose writes must be
// confirmed (ADR-0037 P5). The generic set, import and ensure refuse to
// write through such a plugin unless the operator names the target a
// second time with --write-to HOST, exactly as the host argument was
// given; without it the verb reports what it would write and sends
// nothing. The reason is printed with the refusal, so the operator
// learns why this connector asks twice.
//
// A plugin without this method writes at once when no dry-run flag is
// given, as it always did; the flags and the refusal text are the same
// on every connector, only the default differs.
type WriteGuard interface {
	WriteGuardReason() string
}
