package acp2

import (
	"fmt"
	"math"
	"strings"
	"sync"

	"acp/internal/export/canonical"
	iacp2 "acp/internal/acp2/consumer"
)

// entry is one object in the served tree. Holds the canonical source
// plus the derived ACP2 wire type, children obj-id list, access bits,
// and (for Number objects) the concrete NumberType. Read under
// tree.mu.RLock; mutation via set_property takes the write lock.
type entry struct {
	objID    uint32
	slot     uint8
	parent   uint32             // 0 for the slot root (ROOT_NODE_V2)
	label    string
	access   uint8              // bit 0 read, bit 1 write (spec pid=3)
	objType  iacp2.ACP2ObjType  // node / number / enum / ipv4 / string / preset
	numType  iacp2.NumberType   // meaningful for ObjTypeNumber, ObjTypeEnum (u32 index)
	children []uint32           // pid=14 u32[] — direct child obj-ids
	node     *canonical.Node    // set when objType=node
	param    *canonical.Parameter // set for leaf types
}

// tree is the obj-id indexed snapshot the provider serves. ACP2 obj-ids
// live in a per-slot namespace — two slots can both have obj-id=1 for
// their ROOT_NODE_V2 — so the index keys on (slot, obj-id).
type tree struct {
	mu      sync.RWMutex
	perSlot map[uint8]map[uint32]*entry
	slotN   uint8
}

func emptyTree() *tree {
	return &tree{perSlot: map[uint8]map[uint32]*entry{}, slotN: 1}
}

// newTree walks a canonical.Export and builds the per-slot obj-id
// index. Expected shape (same as acp2 consumer's Canonicalize output):
//
//	device (Node, number=1, oid="1")
//	├── slot-N  (Node, number=N)
//	│   └── ROOT_NODE_V2 (Node, number=1) — ACP2 obj-id 1 on slot N
//	│       └── ...
//
// The slot Node itself is a canonical-only abstraction; ACP2 reaches it
// via the AN2 frame's slot field, not via an obj-id. Objects inside the
// slot subtree use their canonical Header.Number as the ACP2 obj-id.
//
// deriveACP2Type (in this file) maps canonical type + Format hint to
// the ACP2 wire type + NumberType combination. Ambiguous shapes reject
// at load time rather than silently guessing.
func newTree(exp *canonical.Export) (*tree, error) {
	if exp == nil || exp.Root == nil {
		return nil, fmt.Errorf("acp2 provider: empty canonical export")
	}
	root, ok := exp.Root.(*canonical.Node)
	if !ok {
		return nil, fmt.Errorf("acp2 provider: root must be Node, got %s", exp.Root.Kind())
	}

	t := &tree{perSlot: map[uint8]map[uint32]*entry{}}
	var maxSlot uint8 = 1

	for _, slotEl := range root.Children {
		slotNode, ok := slotEl.(*canonical.Node)
		if !ok {
			continue
		}
		slot := uint8(slotNode.Number)
		if slot > maxSlot {
			maxSlot = slot
		}
		index := map[uint32]*entry{}
		for _, childEl := range slotNode.Children {
			if err := flatten(slot, 0, childEl, index); err != nil {
				return nil, fmt.Errorf("slot %d: %w", slot, err)
			}
		}
		// The consumer's walker starts at obj-id=0 per spec §"Walk
		// does DFS from obj-id 1 (or 0)". Real Axon devices expose the
		// absolute root at obj-id=0; canonical trees use Number=1 for
		// the ROOT_NODE_V2 (because canonicalize writes what the walker
		// got back). Bridge both by aliasing 0 to the first top-level
		// child of this slot.
		if len(index) > 0 {
			if _, has0 := index[0]; !has0 {
				// Pick the obj-id whose parent==0 with the lowest
				// Number — that's the slot's root node.
				var rootID uint32 = math.MaxUint32
				for id, e := range index {
					if e.parent == 0 && id < rootID {
						rootID = id
					}
				}
				if rootID != math.MaxUint32 {
					index[0] = index[rootID]
				}
			}
			t.perSlot[slot] = index
		}
	}
	t.slotN = maxSlot
	return t, nil
}

// flatten walks one element and all its descendants into the per-slot
// obj-id index. Assigns entries with their canonical Number as obj-id;
// each entry's `children` list is filled with the obj-ids of its
// direct children (sufficient to serve pid=14 without re-walking).
func flatten(slot uint8, parent uint32, el canonical.Element, index map[uint32]*entry) error {
	switch x := el.(type) {
	case *canonical.Node:
		id := uint32(x.Number)
		e := &entry{
			objID:   id,
			slot:    slot,
			parent:  parent,
			label:   x.Identifier,
			access:  deriveAccess(x.Access),
			objType: iacp2.ObjTypeNode,
			node:    x,
		}
		index[id] = e
		for _, c := range x.Children {
			childID, err := elementID(c)
			if err != nil {
				return err
			}
			e.children = append(e.children, childID)
			if err := flatten(slot, id, c, index); err != nil {
				return err
			}
		}
		return nil
	case *canonical.Parameter:
		id := uint32(x.Number)
		objType, numType, err := deriveACP2Type(x)
		if err != nil {
			return fmt.Errorf("obj %d (%q): %w", id, x.Identifier, err)
		}
		e := &entry{
			objID:   id,
			slot:    slot,
			parent:  parent,
			label:   x.Identifier,
			access:  deriveAccess(x.Access),
			objType: objType,
			numType: numType,
			param:   x,
		}
		index[id] = e
		return nil
	}
	return nil
}

// elementID returns the Header.Number of any canonical element.
func elementID(el canonical.Element) (uint32, error) {
	switch x := el.(type) {
	case *canonical.Node:
		return uint32(x.Number), nil
	case *canonical.Parameter:
		return uint32(x.Number), nil
	}
	return 0, fmt.Errorf("element kind %s has no Number field", el.Kind())
}

// lookup returns the entry at (slot, obj-id) under RLock.
func (t *tree) lookup(slot uint8, id uint32) (*entry, bool) {
	t.mu.RLock()
	defer t.mu.RUnlock()
	ids, ok := t.perSlot[slot]
	if !ok {
		return nil, false
	}
	e, ok := ids[id]
	return e, ok
}

// count returns the total number of indexed entries (used only in logs).
func (t *tree) count() int {
	t.mu.RLock()
	defer t.mu.RUnlock()
	n := 0
	for _, ids := range t.perSlot {
		n += len(ids)
	}
	return n
}

// deriveAccess maps the canonical access string to the ACP2 access bits
// (spec pid=3: bit 0 = read, bit 1 = write). ACP2 has no setDef bit so
// the mapping is 1:1.
func deriveAccess(a string) uint8 {
	switch a {
	case canonical.AccessRead:
		return 0x01
	case canonical.AccessWrite:
		return 0x02
	case canonical.AccessReadWrite:
		return 0x03
	}
	return 0
}

// deriveACP2Type picks the ACP2 wire type + NumberType from a canonical
// Parameter. Uses Parameter.Format as the disambiguator (ACP2's
// 10 number_type variants collapse into canonical's 4-5 Param types).
//
//	integer  + "s8"|"s16"|"s32"|"s64"|"u8"|"u16"|"u32"|"u64"  -> Number + exact numType
//	integer  + no hint                                         -> Number + NumTypeS32 (Axon default)
//	real                                                       -> Number + NumTypeFloat
//	enum                                                       -> Enum  + NumTypeU32 (index)
//	string   + "ipv4"                                          -> IPv4  + NumTypeIPv4
//	string   + no hint | maxLen=N                              -> String + NumTypeString
//	boolean                                                    -> REJECT (ACP2 has no bool; use enum Off,On)
func deriveACP2Type(p *canonical.Parameter) (iacp2.ACP2ObjType, iacp2.NumberType, error) {
	parts := formatParts(p.Format)
	hint, known := pickTypeHint(parts)
	if !known {
		return 0, 0, fmt.Errorf("unrecognised format type-hint %q (valid: s8|s16|s32|s64|u8|u16|u32|u64|float|ipv4)", hint)
	}

	switch p.Type {
	case canonical.ParamReal:
		return iacp2.ObjTypeNumber, iacp2.NumTypeFloat, nil
	case canonical.ParamEnum:
		return iacp2.ObjTypeEnum, iacp2.NumTypeU32, nil
	case canonical.ParamInteger:
		switch hint {
		case "", "s32":
			return iacp2.ObjTypeNumber, iacp2.NumTypeS32, nil
		case "s8":
			return iacp2.ObjTypeNumber, iacp2.NumTypeS8, nil
		case "s16":
			return iacp2.ObjTypeNumber, iacp2.NumTypeS16, nil
		case "s64":
			return iacp2.ObjTypeNumber, iacp2.NumTypeS64, nil
		case "u8":
			return iacp2.ObjTypeNumber, iacp2.NumTypeU8, nil
		case "u16":
			return iacp2.ObjTypeNumber, iacp2.NumTypeU16, nil
		case "u32":
			return iacp2.ObjTypeNumber, iacp2.NumTypeU32, nil
		case "u64":
			return iacp2.ObjTypeNumber, iacp2.NumTypeU64, nil
		case "float":
			return iacp2.ObjTypeNumber, iacp2.NumTypeFloat, nil
		}
		return 0, 0, fmt.Errorf("integer: unknown number type %q", hint)
	case canonical.ParamString:
		switch hint {
		case "", "string":
			return iacp2.ObjTypeString, iacp2.NumTypeString, nil
		case "ipv4", "ipaddr":
			return iacp2.ObjTypeIPv4, iacp2.NumTypeIPv4, nil
		}
		return 0, 0, fmt.Errorf("string: unknown type hint %q (want ipv4 or omit)", hint)
	case canonical.ParamBoolean:
		return 0, 0, fmt.Errorf("boolean has no ACP2 mapping — use enum with Off,On for plain booleans")
	}
	return 0, 0, fmt.Errorf("unsupported canonical type %q for ACP2 provider", p.Type)
}

// formatParts splits the Parameter.Format string into lower-cased
// comma-trimmed tokens. Mirror of the ACP1 provider's helper —
// "maxLen=N" / "priority=2" style key=value pairs coexist with bare
// type-hint tokens.
func formatParts(f *string) []string {
	if f == nil || *f == "" {
		return nil
	}
	parts := strings.Split(*f, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(strings.ToLower(p))
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

// pickTypeHint scans for the one bare token that identifies an ACP2
// wire type; tokens with "=" are attributes (maxLen=N) and ignored.
// Returns (hint, true) on hit or no bare token; (badToken, false) on
// a typo.
func pickTypeHint(parts []string) (string, bool) {
	known := map[string]struct{}{
		"s8": {}, "s16": {}, "s32": {}, "s64": {},
		"u8": {}, "u16": {}, "u32": {}, "u64": {},
		"float":  {},
		"ipv4":   {},
		"ipaddr": {},
		"string": {},
	}
	for _, p := range parts {
		if strings.ContainsRune(p, '=') {
			continue
		}
		if _, ok := known[p]; ok {
			return p, true
		}
		return p, false
	}
	return "", true
}

// maxLenHint extracts the "maxLen=N" attribute from Parameter.Format,
// returning 0 when absent. Used to populate pid=6 (string_max_length)
// on String object replies.
func maxLenHint(p *canonical.Parameter) uint16 {
	for _, kv := range formatParts(p.Format) {
		if strings.HasPrefix(kv, "maxlen=") {
			var n int
			_, err := fmt.Sscanf(kv, "maxlen=%d", &n)
			if err == nil && n > 0 && n <= 65535 {
				return uint16(n)
			}
		}
	}
	return 0
}
