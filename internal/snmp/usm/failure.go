package usm

import "fmt"

// Why an Open failed, for the one caller that is REQUIRED to say.
//
// [Engine.Open] deliberately tells a peer nothing: a decoder that
// answers "wrong password" rather than "no" is an oracle, and an
// attacker with an oracle does not need the password. That stands.
//
// But an AGENT is not free to be silent. RFC 3414 §3.2 requires it to
// answer a message it could not process with a Report naming the
// matching usmStats counter, and §4's time synchronisation is BUILT on
// one of them: a manager whose clock for the agent has gone stale
// learns it from usmStatsNotInTimeWindows and from nothing else. An
// agent that answered every failure with the same counter would leave
// every manager in the plant unable to recover from its own reboot,
// which is a worse outcome than the oracle — and it is not what any
// agent in the field does.
//
// So the classification exists, it is exported, and the only thing that
// reads it is the agent's Report path. A manager never sends one, and
// nothing here turns a Failure into words for a peer: the agent maps it
// to a counter, and the counter is what RFC 3414 §5 already publishes.
type Failure int

const (
	// FailureOther is any failure with no counter of its own.
	FailureOther Failure = iota
	// FailureUnknownUser is usmStatsUnknownUserNames.
	FailureUnknownUser
	// FailureWrongDigest is usmStatsWrongDigests — a bad password, or
	// the right password under another protocol.
	FailureWrongDigest
	// FailureNotInTimeWindow is usmStatsNotInTimeWindows — the message
	// is outside RFC 3414 §2.2.3's window around this engine's clock.
	FailureNotInTimeWindow
	// FailureUnknownEngineID is usmStatsUnknownEngineIDs — the message
	// is addressed to an engine this one is not.
	FailureUnknownEngineID
	// FailureUnsupportedLevel is usmStatsUnsupportedSecLevels — the
	// message claims a level this user is not configured for.
	FailureUnsupportedLevel
	// FailureDecryption is usmStatsDecryptionErrors.
	FailureDecryption
)

// AuthError is an authentication failure, carrying its classification.
//
// errors.Is(err, ErrAuth) is true for every one of them, so a caller
// that only needs "this did not authenticate" keeps working unchanged.
type AuthError struct {
	// Why is the classification an agent maps to a usmStats counter.
	Why Failure
	// Detail is for this process's own logs. It is never sent to a peer.
	Detail string
}

func (e *AuthError) Error() string {
	if e.Detail == "" {
		return ErrAuth.Error()
	}
	return fmt.Sprintf("%s: %s", ErrAuth.Error(), e.Detail)
}

// Is makes errors.Is(err, ErrAuth) true, which is what every caller
// that does not care about the classification asks.
func (e *AuthError) Is(target error) bool { return target == ErrAuth }

// authFailure is the constructor Open uses.
func authFailure(why Failure, format string, args ...any) *AuthError {
	return &AuthError{Why: why, Detail: fmt.Sprintf(format, args...)}
}
