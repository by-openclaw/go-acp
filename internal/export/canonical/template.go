package canonical

import (
	"encoding/json"
	"fmt"
)

// TemplateEntry is one row of Export.Templates (top-level templates[]).
// The embedded Template is any element type (Node/Parameter/Matrix/
// Function) with children populated.
//
// See docs/protocols/elements/template.md.
type TemplateEntry struct {
	Number      int     `json:"number"`
	OID         string  `json:"oid"`
	Identifier  string  `json:"identifier"`
	Description *string `json:"description"`
	Template    Element `json:"template"`
}

// UnmarshalJSON dispatches the embedded template to its concrete type
// via the same rules as children[] (see common.go unmarshalChildren).
func (t *TemplateEntry) UnmarshalJSON(data []byte) error {
	type alias struct {
		Number      int             `json:"number"`
		OID         string          `json:"oid"`
		Identifier  string          `json:"identifier"`
		Description *string         `json:"description"`
		Template    json.RawMessage `json:"template"`
	}
	var raw alias
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	el, err := UnmarshalElement(raw.Template)
	if err != nil {
		return fmt.Errorf("template %q: %w", raw.Identifier, err)
	}
	t.Number = raw.Number
	t.OID = raw.OID
	t.Identifier = raw.Identifier
	t.Description = raw.Description
	t.Template = el
	return nil
}

// Export is the top-level canonical-export shape. Matches the layout
// documented in docs/protocols/elements/template.md §Sample.
//
// Under --templates=inline (default) Templates is nil and the
// exporter omits the key entirely. Under separate/both it carries
// the collected templates.
type Export struct {
	Root      Element          `json:"root"`
	Templates []*TemplateEntry `json:"templates,omitempty"`
}

// UnmarshalJSON dispatches Root to the correct concrete Element type.
func (x *Export) UnmarshalJSON(data []byte) error {
	type alias struct {
		Root      json.RawMessage  `json:"root"`
		Templates []*TemplateEntry `json:"templates"`
	}
	var raw alias
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	if len(raw.Root) > 0 {
		el, err := UnmarshalElement(raw.Root)
		if err != nil {
			return fmt.Errorf("root: %w", err)
		}
		x.Root = el
	}
	x.Templates = raw.Templates
	return nil
}
