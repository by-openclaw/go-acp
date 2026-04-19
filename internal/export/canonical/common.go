package canonical

import (
	"encoding/json"
	"fmt"
)

// Access values — the four levels defined by Ember+ spec §4.1.1 and
// reused by ACP1/ACP2 (ACP1 access bits 0/1/2 collapse to these four).
const (
	AccessNone      = "none"
	AccessRead      = "read"
	AccessWrite     = "write"
	AccessReadWrite = "readWrite"
)

// Element is the union of every leaf/container type a canonical tree
// may contain. children[] on Node/Matrix/etc. is a []Element.
//
// Concrete implementations: *Node, *Parameter, *Matrix, *Function.
// (Templates do not appear inside children[] — they live at the
// top-level Export.Templates slice.)
type Element interface {
	// Kind returns the element type name ("node", "parameter",
	// "matrix", "function") — used by importers to dispatch and by
	// conformance tests to assert documented shape.
	Kind() string

	// Common returns a pointer to the element's embedded Header so
	// generic walkers can read oid / path / identifier without
	// switching on concrete type.
	Common() *Header
}

// Header carries the eight keys every canonical element begins with,
// in the exact order they must appear in the JSON output.
//
// Documented in docs/protocols/schema.md §2.
type Header struct {
	Number      int       `json:"number"`
	Identifier  string    `json:"identifier"`
	Path        string    `json:"path"`
	OID         string    `json:"oid"`
	Description *string   `json:"description"`
	IsOnline    bool      `json:"isOnline"`
	Access      string    `json:"access"`
	Children    []Element `json:"children"`
}

// Common returns itself so types embedding Header satisfy the Element
// interface's Common() method through promotion.
func (h *Header) Common() *Header { return h }

// unmarshalChildren dispatches each raw child to its concrete type
// using the same rules documented in docs/protocols/elements/*.md:
//
//   - has "arguments" key        → Function
//   - has "targets"   key        → Matrix
//   - has "type"      key        → Parameter
//   - otherwise                  → Node
//
// Unknown shapes return an error rather than being silently coerced —
// per feedback_no_workaround.md, divergences must become compliance
// events, not invisible data loss.
func unmarshalChildren(raws []json.RawMessage) ([]Element, error) {
	out := make([]Element, 0, len(raws))
	for i, r := range raws {
		e, err := UnmarshalElement(r)
		if err != nil {
			return nil, fmt.Errorf("child[%d]: %w", i, err)
		}
		out = append(out, e)
	}
	return out, nil
}

// UnmarshalElement parses a single raw JSON element into its concrete
// canonical type. Exposed for tests and importers.
func UnmarshalElement(raw json.RawMessage) (Element, error) {
	// Peek at the keys to dispatch.
	var probe map[string]json.RawMessage
	if err := json.Unmarshal(raw, &probe); err != nil {
		return nil, fmt.Errorf("peek: %w", err)
	}
	_, hasArguments := probe["arguments"]
	_, hasTargets := probe["targets"]
	_, hasType := probe["type"]

	switch {
	case hasArguments:
		f := &Function{}
		if err := json.Unmarshal(raw, f); err != nil {
			return nil, fmt.Errorf("function: %w", err)
		}
		return f, nil
	case hasTargets:
		m := &Matrix{}
		if err := json.Unmarshal(raw, m); err != nil {
			return nil, fmt.Errorf("matrix: %w", err)
		}
		return m, nil
	case hasType:
		p := &Parameter{}
		if err := json.Unmarshal(raw, p); err != nil {
			return nil, fmt.Errorf("parameter: %w", err)
		}
		return p, nil
	default:
		n := &Node{}
		if err := json.Unmarshal(raw, n); err != nil {
			return nil, fmt.Errorf("node: %w", err)
		}
		return n, nil
	}
}

// emptyChildren is the always-present value for a leaf element's
// children[] field. Using this (rather than nil) keeps the exporter's
// output as `"children": []` instead of `"children": null`.
var emptyChildren = []Element{}

// EmptyChildren returns the canonical empty children slice. Exporters
// that build leaves should assign this to Header.Children so JSON
// output shows `[]` not `null`.
func EmptyChildren() []Element { return emptyChildren }
