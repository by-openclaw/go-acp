package codec

import "errors"

// Errors shared across per-command codecs.
var (
	// ErrShortPayload is returned when an inbound frame's payload is
	// shorter than the command's spec'd minimum.
	ErrShortPayload = errors.New("probel: frame payload too short for this command")

	// ErrWrongCommand is returned when a Decode* is called with a frame
	// whose ID does not match the expected CMD for that decoder.
	ErrWrongCommand = errors.New("probel: frame command ID does not match the expected command")
)
