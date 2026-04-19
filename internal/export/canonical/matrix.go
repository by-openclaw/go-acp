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

// MatrixLabel is one row of Matrix.Labels (pointer form).
type MatrixLabel struct {
	BasePath    string  `json:"basePath"`
	Description *string `json:"description"`
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
