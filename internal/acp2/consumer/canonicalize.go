package acp2

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"dhs/internal/acp2/codec"
	"dhs/internal/consumer"
	"dhs/internal/export/canonical"
)

// Canonicalize walks every cached WalkedTree on this plugin and emits
// the device as a canonical Export per docs/protocols/schema.md. Shape:
//
//	device (Node, oid="1")
//	├── slot-0 (Node, number=0, oid="1.1")
//	│   └── ROOT_NODE_V2 (Node)
//	│       ├── BOARD (Node)
//	│       │   ├── ACP Trace (Parameter, enum)
//	│       │   ├── Card Name (Parameter, string R--)
//	│       │   └── ...
//	│       ├── IDENTITY (Node)
//	│       │   └── User Label 1 (Parameter, string RW-)
//	│       └── PROCESSING VIDEO / REFERENCE / ...
//	└── slot-1 ...
//
// Only slots whose WalkedTree is cached (already walked) appear; a fresh
// plugin with no Walk calls emits an empty-children device Node.
//
// ACP2 has no Ember+ resolver surface — no templateReference, no matrix
// labels SEQUENCE, no parametersLocation. The CLI --templates / --labels
// / --gain flags are no-ops here.
//
// Spec cross-references are in the per-field mapping helpers below.
// ACP2 wire spec: acp2_protocol.pdf §§"Get_Object", "Object Types",
// "Property IDs". AN2 transport: an2_protocol.pdf.
func (p *Plugin) Canonicalize(ctx context.Context) (*canonical.Export, error) {
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("canonicalize canceled: %w", err)
	}

	p.mu.Lock()
	cache := p.trees
	p.mu.Unlock()

	slotNodes := make([]canonical.Element, 0)
	if cache != nil {
		cache.mu.RLock()
		for slot, el := range cache.entries {
			entry := el.Value.(*treeCacheEntry)
			if entry == nil || entry.tree == nil {
				continue
			}
			node := buildACP2SlotNode(slot, entry.tree)
			if node != nil {
				slotNodes = append(slotNodes, node)
			}
		}
		cache.mu.RUnlock()
	}

	// Deterministic slot order — slot-0 first, then ascending.
	sortACP2ByNumber(slotNodes)

	root := &canonical.Node{
		Header: canonical.Header{
			Number:     1,
			Identifier: "device",
			Path:       "device",
			OID:        "1",
			IsOnline:   true,
			Access:     canonical.AccessRead,
			Children:   slotNodes,
		},
	}
	if len(slotNodes) == 0 {
		root.Children = canonical.EmptyChildren()
	}

	return &canonical.Export{Root: root}, nil
}

// buildACP2SlotNode constructs the canonical Node for one slot. Children
// are the top-level group Nodes recovered from the walked tree's Path
// information — every object's Path is dot-separated from the slot root
// (e.g. "ROOT_NODE_V2.BOARD.Card Name"), so we split on '.' and build a
// nested dict keyed by path prefix.
func buildACP2SlotNode(slot int, tree *WalkedTree) *canonical.Node {
	slotIdent := "slot-" + strconv.Itoa(slot)
	slotOID := "1." + strconv.Itoa(slot+1) // 1-based so slot-0 → 1.1

	// nodeByPath accumulates container Nodes as the walker output is
	// replayed in DFS order. The slot itself anchors an empty prefix "".
	nodeByPath := map[string]*canonical.Node{
		"": {
			Header: canonical.Header{
				Number:     slot,
				Identifier: slotIdent,
				Path:       slotIdent,
				OID:        slotOID,
				IsOnline:   true,
				Access:     canonical.AccessRead,
				Children:   make([]canonical.Element, 0),
			},
		},
	}

	for i, obj := range tree.Objects {
		objType := codec.ObjTypeNode
		if i < len(tree.ObjTypes) {
			objType = tree.ObjTypes[i]
		}
		numType := codec.NumberType(0)
		if i < len(tree.NumTypes) {
			numType = tree.NumTypes[i]
		}
		placeACP2Object(slot, slotOID, slotIdent, obj, objType, numType, nodeByPath)
	}

	// Sort children recursively for deterministic output.
	sortChildrenDeep(nodeByPath[""])

	return nodeByPath[""]
}

// placeACP2Object inserts the given object into the right parent Node
// in nodeByPath, creating intermediate Node containers if the walker
// didn't also hand us one (defensive — in practice the walker covers
// every node it descends into).
//
// The object's Path is a dot-separated list from the slot root. The last
// segment is the element's identifier; everything before is the parent
// path. Node-type objects are attached as Nodes; everything else as
// Parameter.
func placeACP2Object(slot int, slotOID, slotIdent string, obj consumer.Object, objType codec.ACP2ObjType, numType codec.NumberType, nodeByPath map[string]*canonical.Node) {
	if len(obj.Path) == 0 {
		return
	}
	// obj.Path is []string from walker. Join for map key and canonical.
	fullPath := strings.Join(obj.Path, ".")
	parentKey := ""
	if len(obj.Path) > 1 {
		parentKey = strings.Join(obj.Path[:len(obj.Path)-1], ".")
	}

	// Ensure every intermediate path has an anchor Node. The walker emits
	// DFS, so parents appear before children, but a partial cache or a
	// renamed node could still leave a gap; fill it lazily here.
	ensureACP2Chain(slot, slotOID, slotIdent, obj.Path[:len(obj.Path)-1], nodeByPath)

	// unreachable: parentKey is always present here. When len(obj.Path)==1,
	// parentKey=="" which is the pre-seeded slot root; otherwise ensureACP2Chain
	// (called just above with obj.Path[:len-1]) materialises every prefix
	// including parentKey itself.
	parent := nodeByPath[parentKey]

	if objType == codec.ObjTypeNode {
		// Node container. If we've already seen this path (e.g. parent
		// was prematurely materialised by ensureACP2Chain), upgrade the
		// placeholder's metadata rather than re-adding.
		if existing, seen := nodeByPath[fullPath]; seen {
			existing.Number = obj.ID
			existing.Access = acp2AccessString(obj.Access)
			return
		}
		node := &canonical.Node{
			Header: canonical.Header{
				Number:     obj.ID,
				Identifier: acp2Identifier(obj),
				Path:       fullPath,
				OID:        slotOID + "." + strconv.Itoa(obj.ID),
				IsOnline:   true,
				Access:     acp2AccessString(obj.Access),
				Children:   make([]canonical.Element, 0),
			},
		}
		parent.Children = append(parent.Children, node)
		nodeByPath[fullPath] = node
		return
	}

	param := buildACP2Parameter(obj, objType, numType, slotOID, fullPath)
	if param != nil {
		parent.Children = append(parent.Children, param)
	}
}

// ensureACP2Chain makes sure every prefix of the given path has a
// placeholder Node in nodeByPath. Called before attaching a leaf whose
// walker-delivered parent chain may have gaps.
func ensureACP2Chain(slot int, slotOID, slotIdent string, segments []string, nodeByPath map[string]*canonical.Node) {
	for i := 1; i <= len(segments); i++ {
		key := strings.Join(segments[:i], ".")
		if _, ok := nodeByPath[key]; ok {
			continue
		}
		parentKey := ""
		if i > 1 {
			parentKey = strings.Join(segments[:i-1], ".")
		}
		placeholder := &canonical.Node{
			Header: canonical.Header{
				Number:     0,
				Identifier: segments[i-1],
				Path:       key,
				OID:        slotOID + ".0",
				IsOnline:   true,
				Access:     canonical.AccessRead,
				Children:   make([]canonical.Element, 0),
			},
		}
		nodeByPath[key] = placeholder
		// unreachable: parentKey is always present. For i==1 it is "" (the
		// pre-seeded slot root); for i>1 it is segments[:i-1], which was created
		// and stored in nodeByPath on the previous loop iteration.
		parent := nodeByPath[parentKey]
		parent.Children = append(parent.Children, placeholder)
	}
}

// buildACP2Parameter maps a consumer.Object (leaf) to a canonical.Parameter.
// Spec cross-refs for each property come from acp2_protocol.pdf.
func buildACP2Parameter(obj consumer.Object, objType codec.ACP2ObjType, numType codec.NumberType, slotOID, path string) *canonical.Parameter {
	oid := slotOID + "." + strconv.Itoa(obj.ID)

	p := &canonical.Parameter{
		Header: canonical.Header{
			Number:     obj.ID,
			Identifier: acp2Identifier(obj),
			Path:       path,
			OID:        oid,
			IsOnline:   true,
			Access:     acp2AccessString(obj.Access),
			Children:   canonical.EmptyChildren(),
		},
		Type: acp2KindToCanonicalType(obj.Kind, objType, numType),
	}

	p.Value = acp2ValueToAny(obj.Value)

	if obj.Min != nil {
		p.Minimum = obj.Min
	}
	if obj.Max != nil {
		p.Maximum = obj.Max
	}
	if obj.Step != nil {
		p.Step = obj.Step
	}
	if obj.Def != nil {
		p.Default = obj.Def
	}
	if obj.Unit != "" {
		u := obj.Unit
		p.Unit = &u
	}

	// Enum / preset (ACP2 object types 2 and "preset" per pid=5=9).
	// Spec acp2_protocol.pdf §"Property IDs" pid=15 delivers the options
	// list; the walker already exposes them as EnumItems + OptionsMap.
	if (objType == codec.ObjTypeEnum || objType == codec.ObjTypePreset) && len(obj.EnumItems) > 0 {
		entries := make([]canonical.EnumEntry, 0, len(obj.EnumItems))
		for i, item := range obj.EnumItems {
			entries = append(entries, canonical.EnumEntry{
				Key:   item,
				Value: int64(i),
			})
		}
		p.EnumMap = entries
		joined := strings.Join(obj.EnumItems, "\n")
		p.Enumeration = &joined
	}

	// String maxLength — pid=6 (spec §"Property IDs"). Expose via the
	// canonical format hint since the schema has no dedicated maxLen.
	if objType == codec.ObjTypeString && obj.MaxLen > 0 {
		hint := "maxLen=" + strconv.Itoa(obj.MaxLen)
		p.Format = &hint
	}

	return p
}

// acp2Identifier returns a stable identifier for an object. Falls back
// to "#<id>" when the device leaves the label empty (rare but allowed).
func acp2Identifier(obj consumer.Object) string {
	if obj.Label != "" {
		return obj.Label
	}
	return "#" + strconv.Itoa(obj.ID)
}

// acp2KindToCanonicalType picks the canonical parameter type string for
// an ACP2 leaf. The walker has already mapped ACP2ObjType → ValueKind
// (see walker.go parseObjectProperties), so we mostly dispatch on Kind;
// objType / numType disambiguate the special cases.
func acp2KindToCanonicalType(k consumer.ValueKind, objType codec.ACP2ObjType, numType codec.NumberType) string {
	switch k {
	case consumer.KindBool:
		return canonical.ParamBoolean
	case consumer.KindInt, consumer.KindUint:
		return canonical.ParamInteger
	case consumer.KindFloat:
		return canonical.ParamReal
	case consumer.KindEnum:
		return canonical.ParamEnum
	case consumer.KindString:
		return canonical.ParamString
	case consumer.KindIPAddr:
		return canonical.ParamString
	case consumer.KindRaw:
		return canonical.ParamOctets
	}
	// Unknown kind — fall back on objType / numType for leaf disambiguation.
	switch objType {
	case codec.ObjTypeString:
		return canonical.ParamString
	case codec.ObjTypeIPv4:
		return canonical.ParamString
	case codec.ObjTypeNumber:
		if numType == codec.NumTypeFloat {
			return canonical.ParamReal
		}
		return canonical.ParamInteger
	case codec.ObjTypeEnum, codec.ObjTypePreset:
		return canonical.ParamEnum
	}
	return canonical.ParamString
}

// acp2AccessString maps the ACP2 access byte (pid=3 — 1=R, 2=W, 3=RW,
// per acp2_protocol.pdf §"Property IDs") to the canonical access string.
func acp2AccessString(a uint8) string {
	const (
		read  = 1 << 0
		write = 1 << 1
	)
	// a & (read|write) masks to exactly {0,1,2,3}; the four cases below
	// are exhaustive, so no trailing return is reachable.
	switch a & (read | write) {
	case read:
		return canonical.AccessRead
	case write:
		return canonical.AccessWrite
	case read | write:
		return canonical.AccessReadWrite
	default: // 0 — no access bits set
		return canonical.AccessNone
	}
}

// acp2ValueToAny produces the right Go scalar for the canonical JSON
// `value` field. Matches ACP1's valueToAny signature so downstream code
// stays uniform.
func acp2ValueToAny(v consumer.Value) any {
	switch v.Kind {
	case consumer.KindBool:
		return v.Bool
	case consumer.KindInt:
		return v.Int
	case consumer.KindUint:
		return v.Uint
	case consumer.KindFloat:
		return v.Float
	case consumer.KindEnum:
		if v.Str != "" {
			return v.Str
		}
		return int64(v.Enum)
	case consumer.KindString:
		return v.Str
	case consumer.KindIPAddr:
		return strconv.Itoa(int(v.IPAddr[0])) + "." +
			strconv.Itoa(int(v.IPAddr[1])) + "." +
			strconv.Itoa(int(v.IPAddr[2])) + "." +
			strconv.Itoa(int(v.IPAddr[3]))
	}
	return nil
}

// sortACP2ByNumber orders a slice of canonical elements by Header.Number
// ascending. Insertion sort is fine — slot counts are small, group
// counts are small, and parameter counts within a group are a few
// hundred at worst.
func sortACP2ByNumber(els []canonical.Element) {
	for i := 1; i < len(els); i++ {
		for j := i; j > 0; j-- {
			if els[j].Common().Number < els[j-1].Common().Number {
				els[j], els[j-1] = els[j-1], els[j]
				continue
			}
			break
		}
	}
}

// sortChildrenDeep walks a Node tree and sorts every Children slice
// by Number. Deterministic output is non-negotiable for golden tests.
func sortChildrenDeep(n *canonical.Node) {
	if n == nil || len(n.Children) == 0 {
		return
	}
	sortACP2ByNumber(n.Children)
	for _, c := range n.Children {
		if child, ok := c.(*canonical.Node); ok {
			sortChildrenDeep(child)
		}
	}
}
