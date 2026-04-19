package canonical

import "encoding/json"

// Node is a container element. See docs/protocols/elements/node.md.
//
// JSON field order (locked by the doc):
//   number, identifier, path, oid, description, isOnline, access,
//   children, templateReference, schemaIdentifiers
type Node struct {
	Header
	TemplateReference *string `json:"templateReference"`
	SchemaIdentifiers *string `json:"schemaIdentifiers"`
}

// Kind implements Element.
func (*Node) Kind() string { return "node" }

// UnmarshalJSON decodes a Node including a correctly typed children[]
// slice. We cannot rely on Go's default unmarshalling because
// Header.Children is []Element (interface) and the concrete type of
// each child is decided by its key set.
func (n *Node) UnmarshalJSON(data []byte) error {
	type nodeOnHeader struct {
		Number            int               `json:"number"`
		Identifier        string            `json:"identifier"`
		Path              string            `json:"path"`
		OID               string            `json:"oid"`
		Description       *string           `json:"description"`
		IsOnline          bool              `json:"isOnline"`
		Access            string            `json:"access"`
		Children          []json.RawMessage `json:"children"`
		TemplateReference *string           `json:"templateReference"`
		SchemaIdentifiers *string           `json:"schemaIdentifiers"`
	}
	var raw nodeOnHeader
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	kids, err := unmarshalChildren(raw.Children)
	if err != nil {
		return err
	}
	n.Header = Header{
		Number:      raw.Number,
		Identifier:  raw.Identifier,
		Path:        raw.Path,
		OID:         raw.OID,
		Description: raw.Description,
		IsOnline:    raw.IsOnline,
		Access:      raw.Access,
		Children:    kids,
	}
	n.TemplateReference = raw.TemplateReference
	n.SchemaIdentifiers = raw.SchemaIdentifiers
	return nil
}
