package wiretrace

// Direction is "tx" (sent by dhs) or "rx" (received from peer).
//
// Defined here rather than as an untyped string so JSON unmarshal can
// validate the value at the input boundary per ADR-0021.
type Direction string

const (
	DirectionTx Direction = "tx"
	DirectionRx Direction = "rx"
)

// Trame is one captured wire-level data unit: the bytes that hit the
// socket plus capture metadata. One Trame per line in a `frames.jsonl`
// file (ADR-0021).
//
// Naming: "Trame" is the French / Belgian broadcast term for a
// transport-level data unit. It is used here intentionally to
// disambiguate from "Frame", which is overloaded in this codebase:
//
//   - consumer.KindFrame / SlotStatus / FrameStatus mean a chassis
//     frame holding slot cards (ACP1 / ACP2 spec vocabulary).
//   - s101.Frame, probel.Frame, cerebrum.Frame, tsl.FrameV31Event mean
//     one wire-format unit per protocol codec.
//
// A Trame is at a higher level than either: it is the captured record
// OF a wire frame, with timestamp and direction. The file-level
// naming convention (`frames.jsonl`, per ADR-0020 Bucket 1) is kept;
// the in-code type avoids the overload.
//
// schema_version + dir + hex are required per ADR-0021; everything
// else is optional and forward-compat.
type Trame struct {
	SchemaVersion int       `json:"schema_version"`
	Timestamp     string    `json:"ts,omitempty"`
	Proto         string    `json:"proto,omitempty"`
	Transport     string    `json:"transport,omitempty"`
	Direction     Direction `json:"dir"`
	Hex           string    `json:"hex"` // lowercase hex, no spaces
	Peer          string    `json:"peer,omitempty"`
	Session       string    `json:"session,omitempty"`
	DhsVersion    string    `json:"dhs_version,omitempty"`
	Note          string    `json:"note,omitempty"`

	// Meta marks the ADR-0028 self-description record — line one of a
	// capture, carrying no wire bytes. ReadTrames skips meta records
	// (they are provenance, not traffic); ReadTramesMeta surfaces the
	// first one to callers that want it.
	Meta *MetaRecord `json:"meta,omitempty"`
}

// MetaRecord is the ADR-0028 capture self-description: the exact CLI
// invocation (credentials redacted by the writer), the binary that
// produced it, and the wire context — so an evidence file explains
// itself forever.
type MetaRecord struct {
	CLI           string `json:"cli"`
	BinaryVersion string `json:"binary_version,omitempty"`
	BinarySHA256  string `json:"binary_sha256,omitempty"`
	Proto         string `json:"proto,omitempty"`
	Target        string `json:"target,omitempty"`
	Verb          string `json:"verb,omitempty"`
	StartedUTC    string `json:"started_utc,omitempty"`
}
