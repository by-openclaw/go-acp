package export

import (
	"encoding/json"
	"fmt"
	"io"
)

// WriteJSON emits a Snapshot as pretty-printed JSON. Uses stdlib
// encoding/json — no external dependencies.
//
// The Snapshot type is tagged for JSON field names already, so this
// function is a thin wrapper. It exists in its own file so the CLI can
// swap formats with a single function pointer.
func WriteJSON(w io.Writer, s *Snapshot) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	if err := enc.Encode(s); err != nil {
		return fmt.Errorf("json encode: %w", err)
	}
	return nil
}

// ReadJSON parses a Snapshot from JSON bytes. Returns a descriptive
// error on malformed input so `acp import` can surface it to the user.
func ReadJSON(r io.Reader) (*Snapshot, error) {
	dec := json.NewDecoder(r)
	dec.DisallowUnknownFields()
	var s Snapshot
	if err := dec.Decode(&s); err != nil {
		return nil, fmt.Errorf("json decode: %w", err)
	}
	return &s, nil
}
