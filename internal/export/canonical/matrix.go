package canonical

import "encoding/json"

// Matrix type values — see docs/protocols/elements/matrix.md §Classification axes.
const (
	MatrixOneToN   = "oneToN"
	MatrixOneToOne = "oneToOne"
	MatrixNToN     = "nToN"

	ModeLinear    = "linear"
	ModeNonLinear = "nonLinear"

	ConnOpAbsolute   = "absolute"
	ConnOpConnect    = "connect"
	ConnOpDisconnect = "disconnect"

	ConnDispTally    = "tally"
	ConnDispModified = "modified"
	ConnDispPending  = "pending"
	ConnDispLocked   = "locked"
)

// MatrixLabel is one row of Matrix.Labels (pointer form). A matrix can
// expose multiple levels (Video / Audio / UMD etc.); each level gets one
// MatrixLabel entry. Absence of a level in the slice = that level is not
// exposed by this device.
//
// Name encoding fields (NameSize / MultiLine / PadChar / KeepPadding)
// control how source, destination, and association labels for THIS level
// are packed onto the wire and unpacked off it. Zero-values map to
// sensible defaults at the schema→codec boundary (see probel provider
// tree.go), so a minimal tree fixture does not have to state them.
type MatrixLabel struct {
	BasePath    string  `json:"basePath"`
	Description *string `json:"description"`

	// NameSize in bytes (4 / 8 / 12 / 16 per SW-P-08 §5.1; other values
	// legal but rare). 0 = "don't override; handlers fall back to the
	// NameLength value carried in each request frame".
	NameSize uint8 `json:"nameSize,omitempty"`

	// MultiLine marks names that encode a two-line display as
	// "<line1>\r\n<line2>" inside a fixed-width field (Calrec + some
	// Imagine hardware). Wire codec preserves CR/LF verbatim; UI
	// layer decides whether to render as two rows.
	MultiLine bool `json:"multiLine,omitempty"`

	// PadChar is the byte used to right-pad short names on encode and
	// strip on decode. nil = use the codec default (ASCII space, 0x20).
	// A non-nil pointer lets operators/config distinguish "use default"
	// from "use NUL (0x00)" or any other explicit byte.
	PadChar *uint8 `json:"padChar,omitempty"`

	// KeepPadding, when true, skips the trim step on decode — callers
	// get the raw fixed-width bytes as a string. Default (false) trims
	// trailing pad + NUL bytes for display-friendly output.
	KeepPadding bool `json:"keepPadding,omitempty"`
}

// EffectiveNameSize returns NameSize if explicitly set, otherwise the
// fallback (typically 8 for SW-P-08). Keeps the schema→codec adapter
// in one place.
func (ml MatrixLabel) EffectiveNameSize(fallback uint8) uint8 {
	if ml.NameSize > 0 {
		return ml.NameSize
	}
	return fallback
}

// EffectivePadChar returns the configured PadChar, falling back to
// codec.DefaultPadChar (ASCII space) when the pointer is nil.
func (ml MatrixLabel) EffectivePadChar() uint8 {
	if ml.PadChar != nil {
		return *ml.PadChar
	}
	return 0x20 // codec.DefaultPadChar — duplicated to avoid import cycle
}

// MatrixTarget / MatrixSource represent declared target / source
// indices. Kept separate types for readability even though the shape
// is identical.
type MatrixTarget struct {
	Number int64 `json:"number"`
}

type MatrixSource struct {
	Number int64 `json:"number"`
}

// MatrixConnection is one row of Matrix.Connections.
type MatrixConnection struct {
	Target      int64   `json:"target"`
	Sources     []int64 `json:"sources"`
	Operation   string  `json:"operation"`
	Disposition string  `json:"disposition"`
	Locked      bool    `json:"locked"`
}

// Matrix is a crosspoint grid. See docs/protocols/elements/matrix.md.
type Matrix struct {
	Header

	Type string `json:"type"`
	Mode string `json:"mode"`

	TargetCount              int64  `json:"targetCount"`
	SourceCount              int64  `json:"sourceCount"`
	MaximumTotalConnects     *int64 `json:"maximumTotalConnects"`
	MaximumConnectsPerTarget *int64 `json:"maximumConnectsPerTarget"`

	ParametersLocation  *string `json:"parametersLocation"`
	GainParameterNumber *int64  `json:"gainParameterNumber"`

	Labels      []MatrixLabel      `json:"labels"`
	Targets     []MatrixTarget     `json:"targets"`
	Sources     []MatrixSource     `json:"sources"`
	Connections []MatrixConnection `json:"connections"`

	// The inline resolution maps. Exporters set these explicitly per
	// --labels / --gain mode. nil marshals to null (pointer-only mode);
	// an empty map marshals to {} (inline mode with no entries found).
	//
	// TargetLabels / SourceLabels are two-level maps keyed by the
	// provider's labels[i].description (e.g. "Primary", "Secondary").
	// The inner map is number (as decimal string) → label string.
	// A matrix with a single "Primary" level looks like:
	//   {"Primary": {"0": "OUT 1", "1": "OUT 2"}}
	// The outer key preserves spec §5.1.1's multi-level contract even
	// when only one level is present. Levels that fail to resolve are
	// omitted entirely — no empty inner maps allocated.
	TargetLabels     map[string]map[string]string `json:"targetLabels"`
	SourceLabels     map[string]map[string]string `json:"sourceLabels"`
	TargetParams     map[string]map[string]any    `json:"targetParams"`
	SourceParams     map[string]map[string]any    `json:"sourceParams"`
	ConnectionParams map[string]map[string]any    `json:"connectionParams"`
}

// Kind implements Element.
func (*Matrix) Kind() string { return "matrix" }

// UnmarshalJSON handles the children[] interface dispatch.
func (m *Matrix) UnmarshalJSON(data []byte) error {
	type alias struct {
		Number                   int                          `json:"number"`
		Identifier               string                       `json:"identifier"`
		Path                     string                       `json:"path"`
		OID                      string                       `json:"oid"`
		Description              *string                      `json:"description"`
		IsOnline                 bool                         `json:"isOnline"`
		Access                   string                       `json:"access"`
		Children                 []json.RawMessage            `json:"children"`
		Type                     string                       `json:"type"`
		Mode                     string                       `json:"mode"`
		TargetCount              int64                        `json:"targetCount"`
		SourceCount              int64                        `json:"sourceCount"`
		MaximumTotalConnects     *int64                       `json:"maximumTotalConnects"`
		MaximumConnectsPerTarget *int64                       `json:"maximumConnectsPerTarget"`
		ParametersLocation       *string                      `json:"parametersLocation"`
		GainParameterNumber      *int64                       `json:"gainParameterNumber"`
		Labels                   []MatrixLabel                `json:"labels"`
		Targets                  []MatrixTarget               `json:"targets"`
		Sources                  []MatrixSource               `json:"sources"`
		Connections              []MatrixConnection           `json:"connections"`
		TargetLabels             map[string]map[string]string `json:"targetLabels"`
		SourceLabels             map[string]map[string]string `json:"sourceLabels"`
		TargetParams             map[string]map[string]any    `json:"targetParams"`
		SourceParams             map[string]map[string]any    `json:"sourceParams"`
		ConnectionParams         map[string]map[string]any    `json:"connectionParams"`
	}
	var raw alias
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	kids, err := unmarshalChildren(raw.Children)
	if err != nil {
		return err
	}
	m.Header = Header{
		Number:      raw.Number,
		Identifier:  raw.Identifier,
		Path:        raw.Path,
		OID:         raw.OID,
		Description: raw.Description,
		IsOnline:    raw.IsOnline,
		Access:      raw.Access,
		Children:    kids,
	}
	m.Type = raw.Type
	m.Mode = raw.Mode
	m.TargetCount = raw.TargetCount
	m.SourceCount = raw.SourceCount
	m.MaximumTotalConnects = raw.MaximumTotalConnects
	m.MaximumConnectsPerTarget = raw.MaximumConnectsPerTarget
	m.ParametersLocation = raw.ParametersLocation
	m.GainParameterNumber = raw.GainParameterNumber
	m.Labels = raw.Labels
	m.Targets = raw.Targets
	m.Sources = raw.Sources
	m.Connections = raw.Connections
	m.TargetLabels = raw.TargetLabels
	m.SourceLabels = raw.SourceLabels
	m.TargetParams = raw.TargetParams
	m.SourceParams = raw.SourceParams
	m.ConnectionParams = raw.ConnectionParams
	return nil
}
