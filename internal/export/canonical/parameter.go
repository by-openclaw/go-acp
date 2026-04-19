package canonical

import "encoding/json"

// Parameter value types — see docs/protocols/elements/parameter.md §Value types.
const (
	ParamInteger = "integer"
	ParamReal    = "real"
	ParamString  = "string"
	ParamBoolean = "boolean"
	ParamEnum    = "enum"
	ParamOctets  = "octets"
	ParamTrigger = "trigger"
)

// EnumEntry is one row of Parameter.EnumMap. Key is the label shown to
// the user; Value is the numeric identifier carried on the wire; Masked
// indicates a non-selectable option (smh `~` prefix, stripped).
type EnumEntry struct {
	Key    string `json:"key"`
	Value  int64  `json:"value"`
	Masked bool   `json:"masked,omitempty"`
}

// StreamDescriptor is emitted when a Parameter participates in a
// CollectionAggregate stream (one binary blob carries multiple
// parameters at fixed offsets). See docs/protocols/elements/stream.md.
type StreamDescriptor struct {
	Format int `json:"format"`
	Offset int `json:"offset"`
}

// Parameter is a leaf value. See docs/protocols/elements/parameter.md.
//
// JSON field order (locked by the doc): common header, then
//   type, value, default, minimum, maximum, step, unit, format,
//   factor, formula, enumeration, enumMap, streamIdentifier,
//   streamDescriptor, templateReference, schemaIdentifiers.
type Parameter struct {
	Header

	Type              string            `json:"type"`
	Value             any               `json:"value"`
	Default           any               `json:"default"`
	Minimum           any               `json:"minimum"`
	Maximum           any               `json:"maximum"`
	Step              any               `json:"step"`
	Unit              *string           `json:"unit"`
	Format            *string           `json:"format"`
	Factor            *int64            `json:"factor"`
	Formula           *string           `json:"formula"`
	Enumeration       *string           `json:"enumeration"`
	EnumMap           []EnumEntry       `json:"enumMap"`
	StreamIdentifier  *int64            `json:"streamIdentifier"`
	StreamDescriptor  *StreamDescriptor `json:"streamDescriptor"`
	TemplateReference *string           `json:"templateReference"`
	SchemaIdentifiers *string           `json:"schemaIdentifiers"`
}

// Kind implements Element.
func (*Parameter) Kind() string { return "parameter" }

// UnmarshalJSON mirrors the Node pattern: decode children[] as raw
// messages, then dispatch each to the correct concrete type. For a
// well-formed Parameter children[] is empty, but we honour the
// possibility that a provider nests children under a Parameter (e.g.
// per-parameter sub-parameters).
func (p *Parameter) UnmarshalJSON(data []byte) error {
	type alias struct {
		Number            int               `json:"number"`
		Identifier        string            `json:"identifier"`
		Path              string            `json:"path"`
		OID               string            `json:"oid"`
		Description       *string           `json:"description"`
		IsOnline          bool              `json:"isOnline"`
		Access            string            `json:"access"`
		Children          []json.RawMessage `json:"children"`
		Type              string            `json:"type"`
		Value             json.RawMessage   `json:"value"`
		Default           json.RawMessage   `json:"default"`
		Minimum           json.RawMessage   `json:"minimum"`
		Maximum           json.RawMessage   `json:"maximum"`
		Step              json.RawMessage   `json:"step"`
		Unit              *string           `json:"unit"`
		Format            *string           `json:"format"`
		Factor            *int64            `json:"factor"`
		Formula           *string           `json:"formula"`
		Enumeration       *string           `json:"enumeration"`
		EnumMap           []EnumEntry       `json:"enumMap"`
		StreamIdentifier  *int64            `json:"streamIdentifier"`
		StreamDescriptor  *StreamDescriptor `json:"streamDescriptor"`
		TemplateReference *string           `json:"templateReference"`
		SchemaIdentifiers *string           `json:"schemaIdentifiers"`
	}
	var raw alias
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	kids, err := unmarshalChildren(raw.Children)
	if err != nil {
		return err
	}
	p.Header = Header{
		Number:      raw.Number,
		Identifier:  raw.Identifier,
		Path:        raw.Path,
		OID:         raw.OID,
		Description: raw.Description,
		IsOnline:    raw.IsOnline,
		Access:      raw.Access,
		Children:    kids,
	}
	p.Type = raw.Type
	p.Value = decodeAny(raw.Value)
	p.Default = decodeAny(raw.Default)
	p.Minimum = decodeAny(raw.Minimum)
	p.Maximum = decodeAny(raw.Maximum)
	p.Step = decodeAny(raw.Step)
	p.Unit = raw.Unit
	p.Format = raw.Format
	p.Factor = raw.Factor
	p.Formula = raw.Formula
	p.Enumeration = raw.Enumeration
	p.EnumMap = raw.EnumMap
	p.StreamIdentifier = raw.StreamIdentifier
	p.StreamDescriptor = raw.StreamDescriptor
	p.TemplateReference = raw.TemplateReference
	p.SchemaIdentifiers = raw.SchemaIdentifiers
	return nil
}

// decodeAny turns a raw JSON value into the natural Go type so the
// conformance test can compare values without needing to know Type
// up-front. Preserves nil/null as (any)(nil).
func decodeAny(raw json.RawMessage) any {
	if len(raw) == 0 {
		return nil
	}
	var v any
	_ = json.Unmarshal(raw, &v)
	return v
}
