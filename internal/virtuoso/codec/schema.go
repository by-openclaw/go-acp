package codec

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
)

// Errors of a lookup in the schema.
var (
	ErrNotInSchema   = errors.New("virtuoso: path is not in the device schema")
	ErrUnresolvedRef = errors.New("virtuoso: schema reference does not resolve")
)

// maxRefChain bounds a chain of $ref nodes, which a broken schema could
// close into a loop.
const maxRefChain = 32

// Node is one node of the served schema: the keywords the guide lists
// ("Schema") plus allowInsert, autoKey and rpc.
type Node struct {
	Ref         string
	Type        string
	Title       string
	Description string
	ReadOnly    bool
	RPC         bool
	AllowInsert bool
	AutoKey     bool
	Unit        string
	Format      string
	Pattern     string
	Minimum     *json.Number
	Maximum     *json.Number
	Default     json.RawMessage
	Enum        []json.RawMessage
	Labels      []string
	Required    []string
	Properties  map[string]*Node
	// Item describes the children of a collection. The device serves a
	// collection as {"type":"object","additionalProperties":…}; the guide
	// describes {"type":"array","items":…}. Both land here.
	Item *Node
}

// UnmarshalJSON reads one node. "type", "required" and the collection
// keywords are read loosely: a field called "type" is an ordinary
// property of the device model, and the two collection spellings differ.
func (n *Node) UnmarshalJSON(b []byte) error {
	var w struct {
		Ref                  string            `json:"$ref"`
		Type                 json.RawMessage   `json:"type"`
		Title                string            `json:"title"`
		Description          string            `json:"description"`
		ReadOnly             bool              `json:"readOnly"`
		RPC                  bool              `json:"rpc"`
		AllowInsert          bool              `json:"allowInsert"`
		AutoKey              bool              `json:"autoKey"`
		Unit                 string            `json:"unit"`
		Format               string            `json:"format"`
		Pattern              string            `json:"pattern"`
		Minimum              *json.Number      `json:"minimum"`
		Maximum              *json.Number      `json:"maximum"`
		Default              json.RawMessage   `json:"default"`
		Enum                 []json.RawMessage `json:"enum"`
		Labels               []string          `json:"labels"`
		Required             json.RawMessage   `json:"required"`
		Properties           map[string]*Node  `json:"properties"`
		AdditionalProperties json.RawMessage   `json:"additionalProperties"`
		Items                json.RawMessage   `json:"items"`
	}
	if err := json.Unmarshal(b, &w); err != nil {
		return err
	}
	*n = Node{
		Ref: w.Ref, Title: w.Title, Description: w.Description, ReadOnly: w.ReadOnly,
		RPC: w.RPC, AllowInsert: w.AllowInsert, AutoKey: w.AutoKey, Unit: w.Unit,
		Format: w.Format, Pattern: w.Pattern, Minimum: w.Minimum, Maximum: w.Maximum,
		Default: w.Default, Enum: w.Enum, Labels: w.Labels, Properties: w.Properties,
	}
	_ = json.Unmarshal(w.Type, &n.Type)
	_ = json.Unmarshal(w.Required, &n.Required)
	for _, raw := range []json.RawMessage{w.AdditionalProperties, w.Items} {
		var item Node
		if len(raw) > 0 && json.Unmarshal(raw, &item) == nil && bytes.HasPrefix(bytes.TrimSpace(raw), []byte("{")) {
			n.Item = &item
			break
		}
	}
	return nil
}

// IsLeaf reports whether the node is a field: not a method, a collection
// or an object.
func (n *Node) IsLeaf() bool { return !n.RPC && n.Item == nil && len(n.Properties) == 0 }

// Args names an RPC's arguments. The property "result" describes what it
// returns ("RPC" in the guide), not an argument.
func (n *Node) Args() []string {
	out := make([]string, 0, len(n.Properties))
	for k := range n.Properties {
		if k != "result" {
			out = append(out, k)
		}
	}
	sort.Strings(out)
	return out
}

// Schema is the document served at schema.json.
type Schema struct {
	Properties  map[string]*Node `json:"properties"`
	Definitions map[string]*Node `json:"definitions"`
}

// DecodeSchema decodes a schema document.
func DecodeSchema(raw []byte) (*Schema, error) {
	var s Schema
	if err := json.Unmarshal(raw, &s); err != nil {
		return nil, fmt.Errorf("%w: schema: %v", ErrNotJSON, err)
	}
	if len(s.Properties) == 0 {
		return nil, errors.New("virtuoso: schema has no properties")
	}
	return &s, nil
}

// Resolve follows a node's $ref chain. The device serves
// "#/definitions/<name>"; the guide shows "#/<name>". ref is the last
// reference taken, "" for a node that had none.
func (s *Schema) Resolve(n *Node) (node *Node, ref string, ok bool) {
	for i := 0; n != nil && i < maxRefChain; i++ {
		if n.Ref == "" {
			return n, ref, true
		}
		ref = strings.TrimPrefix(strings.TrimPrefix(n.Ref, "#/"), "definitions/")
		n = s.Definitions[ref]
	}
	return nil, ref, false
}

// Target is what Find located.
type Target struct {
	Node *Node
	// Key is a key field of a collection item, which "Keys are not
	// possible to change by PATCH".
	Key bool
}

// Find locates path in the schema, taking any segment under a collection
// as one of its keys. A path through an RPC stops at the RPC.
func (s *Schema) Find(path []string) (Target, error) {
	cur := &Node{Properties: s.Properties}
	inItem, key := false, false
	for _, seg := range path {
		node, ref, ok := s.Resolve(cur)
		if !ok {
			return Target{}, fmt.Errorf("%w: %s", ErrUnresolvedRef, ref)
		}
		if node.RPC {
			return Target{Node: node}, nil
		}
		if node.Item != nil {
			cur, inItem, key = node.Item, true, false
			continue
		}
		child, ok := node.Properties[seg]
		if !ok {
			return Target{}, fmt.Errorf("%w: %s", ErrNotInSchema, JoinPath(path))
		}
		key = inItem && contains(node.Required, seg)
		cur, inItem = child, false
	}
	node, ref, ok := s.Resolve(cur)
	if !ok {
		return Target{}, fmt.Errorf("%w: %s", ErrUnresolvedRef, ref)
	}
	return Target{Node: node, Key: key}, nil
}

func contains(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

// Leaf is one entry of a flattened module: a field or a method.
type Leaf struct {
	Path     []string
	Node     *Node // the resolved schema node; nil on an undeclared value
	Value    any   // decoded with json.Number
	HasValue bool
	RPC      bool
	// Undeclared is a value the data document holds and the schema does not.
	Undeclared bool
	// Key is a key field of a collection item that the data repeats.
	Key bool
}

// Flat is the result of joining a schema with its data document.
type Flat struct {
	Leaves     []Leaf
	Undeclared int
	Unresolved int
}

// Flatten joins the schema with one data document: every field and RPC
// the schema declares, collections expanded by the keys the data holds.
// A field the schema declares and the data lacks is listed with no
// value; a value the data holds and the schema lacks is listed as
// Undeclared. A key field absent from a collection item is not listed:
// the guide says keys are not among the value fields, the path carries it.
func (s *Schema) Flatten(data json.RawMessage) (Flat, error) {
	doc := map[string]any{}
	if len(data) > 0 {
		dec := json.NewDecoder(bytes.NewReader(data))
		dec.UseNumber()
		if err := dec.Decode(&doc); err != nil {
			return Flat{}, fmt.Errorf("%w: data: %v", ErrNotJSON, err)
		}
	}
	w := &flattener{s: s, stack: map[string]bool{}}
	w.object(&Node{Properties: s.Properties}, doc, nil, false)
	return Flat{Leaves: w.out, Undeclared: w.undeclared, Unresolved: w.unresolved}, nil
}

type flattener struct {
	s          *Schema
	out        []Leaf
	stack      map[string]bool
	undeclared int
	unresolved int
}

func extend(path []string, seg string) []string {
	return append(path[:len(path):len(path)], seg)
}

func (w *flattener) stray(path []string, v any) {
	w.undeclared++
	w.out = append(w.out, Leaf{Path: path, Value: v, HasValue: true, Undeclared: true})
}

func (w *flattener) visit(n *Node, v any, has bool, path []string, inItem, key bool) {
	node, ref, ok := w.s.Resolve(n)
	if !ok {
		w.unresolved++
		return
	}
	if ref != "" {
		// A schema that refers to itself ends where the data ends.
		if w.stack[ref] && !has {
			return
		}
		if !w.stack[ref] {
			w.stack[ref] = true
			defer delete(w.stack, ref)
		}
	}
	m, isMap := v.(map[string]any)
	switch {
	case node.RPC:
		w.out = append(w.out, Leaf{Path: path, Node: node, RPC: true})
	case node.Item != nil:
		if has && !isMap {
			w.stray(path, v)
			return
		}
		for _, k := range sortedKeys(m) {
			w.visit(node.Item, m[k], true, extend(path, k), true, false)
		}
	case len(node.Properties) > 0:
		if has && !isMap {
			w.stray(path, v)
			return
		}
		w.object(node, m, path, inItem)
	default:
		w.out = append(w.out, Leaf{Path: path, Node: node, Value: v, HasValue: has, Key: key})
	}
}

func (w *flattener) object(node *Node, m map[string]any, path []string, inItem bool) {
	for _, k := range sortedKeys(node.Properties) {
		v, has := m[k]
		isKey := inItem && contains(node.Required, k)
		if isKey && !has {
			continue
		}
		w.visit(node.Properties[k], v, has, extend(path, k), false, isKey)
	}
	for _, k := range sortedKeys(m) {
		if _, declared := node.Properties[k]; !declared {
			w.stray(extend(path, k), m[k])
		}
	}
}

func sortedKeys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
