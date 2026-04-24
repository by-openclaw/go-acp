package codec

import "errors"

// Errors shared across per-command codecs. Added commands append their
// own sentinel errors as needed.
var (
	// ErrShortPayload is returned when an inbound frame's payload is
	// shorter than the command's spec'd minimum.
	ErrShortPayload = errors.New("probel-sw02p: frame payload too short for this command")

	// ErrWrongCommand is returned when a Decode* is called with a frame
	// whose ID does not match the expected CMD for that decoder.
	ErrWrongCommand = errors.New("probel-sw02p: frame command ID does not match the expected command")

	// ErrUnknownCommand is returned by the stream scanner when the
	// observed command byte has no registered MESSAGE length. Session
	// code treats this as a decode error so the compliance profile can
	// absorb it and a single byte gets dropped for resync.
	ErrUnknownCommand = errors.New("probel-sw02p: unknown command byte — no registered payload length")
)
