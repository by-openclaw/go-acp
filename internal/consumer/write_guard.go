package consumer

// WriteGuard is the optional contract of a plugin whose writes must be
// confirmed (ADR-0037 P5). The generic set, import and ensure refuse to
// write through such a plugin unless the operator names the target a
// second time with --write-to HOST, exactly as the host argument was
// given; without it the verb reports what it would write and sends
// nothing. The reason is printed with the refusal, so the operator
// learns why this connector asks twice.
//
// Every connector that can write declares it: rrcs first, after the
// incident of ADR-0035, and all the others since 2026-11-02 by the
// codeowner's decision. A plugin whose SetValue answers ErrNotImplemented
// has nothing to guard. A plugin without the method would write at once,
// so a test of cmd/dhs checks that no writing plugin lacks it.
type WriteGuard interface {
	WriteGuardReason() string
}

// WriteGuardEveryConnector is the reason the writing connectors give when
// they refuse a write that does not name its target twice.
const WriteGuardEveryConnector = "every dhs connector names the target of a write twice (ADR-0037 P5, extended to all writing connectors on 2026-11-02)"
