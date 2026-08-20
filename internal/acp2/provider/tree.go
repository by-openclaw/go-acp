package acp2

import (
	"fmt"
	"math"
	"strings"
	"sync"

	"dhs/internal/export/canonical"
	"dhs/internal/acp2/codec"
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
	objType  codec.ACP2ObjType  // node / number / enum / ipv4 / string / preset
	numType  codec.NumberType   // meaningful for codec.ObjTypeNumber, codec.ObjTypeEnum (u32 index)
	children []uint32           // pid=14 u32[] — direct child obj-ids
	node     *canonical.Node    // set when objType=node
	param    *canonical.Parameter // set for leaf types
	// presetDepth is the N of a preset child's idx list (pid 7). Zero
	// means "not a preset" — every other object type leaves this at 0.
	// Populated from the canonical `format` hint "depth=N" alongside
	// the bare "preset" token. Used to emit pid 7 and to size the
	// per-idx repetition of pids 8/9/10/11 in get_object replies.
	presetDepth uint32

	// presetIdxList holds the explicit u32 idx values for pid 7
	// (preset_depth) when the canonical fixture supplies them via
	// `idx=A,B,C` in Format. Spec example (acp2_protocol.docx
	// §"Preset depth", line 2613-2632) uses non-contiguous values
	// like {100, 200}. When this slice is empty, the encoder falls
	// back to contiguous 0..presetDepth-1 — the historical behaviour.
	presetIdxList []uint32
}

// tree is the obj-id indexed snapshot the provider serves. ACP2 obj-ids
// live in a per-slot namespace — two slots can both have obj-id=1 for
// their ROOT_NODE_V2 — so the index keys on (slot, obj-id).
type tree struct {
	mu      sync.RWMutex
	perSlot map[uint8]map[uint32]*entry
	slotN   uint8
	// labelDeviations counts entries whose wire label violates the
	// ACP2 §"Versions" charset — served VERBATIM (emulation fidelity,
	// see wireLabel) and surfaced here so newServer can log the count
	// as the observable spec-deviation event.
	labelDeviations int
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
	for _, index := range t.perSlot {
		for id, e := range index {
			if id == 0 && e.objID != 0 {
				continue // obj-id 0 alias of the slot root — counted once
			}
			if labelDeviatesSpec(e.label) {
				t.labelDeviations++
			}
		}
	}
	return t, nil
}

// flatten walks one element and all its descendants into the per-slot
// obj-id index. Assigns entries with their canonical Number as obj-id;
// each entry's `children` list is filled with the obj-ids of its
// direct children (sufficient to serve pid=14 without re-walking).
//
// Per spec acp2_protocol.docx §"Requirements" line 320-332: "Disabled
// parts of the menu are not visible to clients (not shown as children,
// options, etc)." Entries whose canonical Format carries a `disabled`
// token are skipped: never indexed, never appear in any pid=14
// children list, never reachable via get_object.
//
// Labels run through sanitizeLabel before being stored on the entry —
// per spec acp2_protocol.docx §"Versions" line 357: "Object labels
// only may contain out of a-z, A-Z, 0-9, ' ' and '-'." Any character
// outside that set is replaced with `-`; the unmodified label remains
// on the canonical element so downstream tooling sees the original.
func flatten(slot uint8, parent uint32, el canonical.Element, index map[uint32]*entry) error {
	switch x := el.(type) {
	case *canonical.Node:
		// Nodes use Format only on Parameters, not on the Node header,
		// so disabled-via-Format applies to leaves; Node-level
		// disabled is out of canonical schema today and would need a
		// separate field. Honour the same rule when a future schema
		// extension adds it.
		id := uint32(x.Number)
		e := &entry{
			objID:   id,
			slot:    slot,
			parent:  parent,
			label:   wireLabel(x.Identifier),
			access:  deriveAccess(x.Access),
			objType: codec.ObjTypeNode,
			node:    x,
		}
		index[id] = e
		for _, c := range x.Children {
			childID, err := elementID(c)
			if err != nil {
				return err
			}
			// Skip disabled children: drop from pid=14 list AND
			// skip recursive flatten so the obj-id is never
			// indexed.
			if isDisabledElement(c) {
				continue
			}
			e.children = append(e.children, childID)
			if err := flatten(slot, id, c, index); err != nil {
				return err
			}
		}
		return nil
	case *canonical.Parameter:
		// Belt-and-braces: even if the parent forgets to filter,
		// a disabled leaf still doesn't land in the index.
		if isDisabledParameter(x) {
			return nil
		}
		id := uint32(x.Number)
		objType, numType, err := deriveACP2Type(x)
		if err != nil {
			return fmt.Errorf("obj %d (%q): %w", id, x.Identifier, err)
		}
		e := &entry{
			objID:       id,
			slot:        slot,
			parent:      parent,
			label:       wireLabel(x.Identifier),
			access:      deriveAccess(x.Access),
			objType:     objType,
			numType:     numType,
			param:       x,
			presetDepth:   presetDepthHint(x),
			presetIdxList: presetIdxHint(x),
		}
		if objType == codec.ObjTypePreset && e.presetDepth == 0 {
			// Spec §5 requires at least one idx in pid 7 for preset children.
			// Default to depth=1 (single ACTIVE INDEX slot) when the
			// canonical format omits "depth=N".
			e.presetDepth = 1
		}
		index[id] = e
		return nil
	}
	return nil
}

// isDisabledElement returns true when the element should be hidden
// from clients per spec §"Requirements" disabled-menu rule. Today
// only Parameter entries can carry the `disabled` Format hint;
// Nodes always pass through.
func isDisabledElement(el canonical.Element) bool {
	if p, ok := el.(*canonical.Parameter); ok {
		return isDisabledParameter(p)
	}
	return false
}

// isDisabledParameter checks for the `disabled` bare token in
// Parameter.Format. Same convention as the type discriminator
// (`preset`, `ipv4`) — a flag without a value.
func isDisabledParameter(p *canonical.Parameter) bool {
	for _, kv := range formatParts(p.Format) {
		if kv == "disabled" {
			return true
		}
	}
	return false
}

// wireLabel returns the label to serve on the wire (pid=2) —
// VERBATIM, empty defaulting to "obj" (spec mandates non-empty).
//
// History: from 2026-05-08 (cd7af27e) to 2026-08-20 the provider
// REWROTE labels to the spec charset of acp2_protocol.docx §"Versions"
// line 357 ([A-Za-z0-9 \-]), turning the real Neuron's "ROOT_NODE_V2"
// into "ROOT-NODE-V2". That mutation broke emulation: Cerebrum's
// Neuron driver binds its default object model by exact label paths,
// so a renamed root orphaned the whole tree (wire-proven live
// 2026-08-20, staging capture vs a real-device walk of 10.44.72.18).
// Per the repo's spec-strict posture the deviation is the DEVICE's,
// and our job is to absorb and surface it — never to alter the data:
// the emulator must state what the emulated hardware states.
// labelDeviatesSpec reports the deviation so newTree can count + log
// it (the observable event), replacing the silent rewrite.
func wireLabel(label string) string {
	if label == "" {
		return "obj"
	}
	return label
}

// labelDeviatesSpec reports whether a label violates the ACP2
// §"Versions" charset ([A-Za-z0-9 \-]). Real EVS Neuron firmware
// does (underscores) — surfaced as a count at tree build, served
// verbatim.
func labelDeviatesSpec(label string) bool {
	for _, r := range label {
		if !isSpecLabelByte(r) {
			return true
		}
	}
	return false
}

// isSpecLabelByte reports whether r is in the ACP2 §"Versions"
// label charset: ASCII letters, digits, space, dash. Anything else
// (including non-ASCII, underscore, dot, etc.) is non-compliant.
func isSpecLabelByte(r rune) bool {
	switch {
	case r >= 'a' && r <= 'z':
		return true
	case r >= 'A' && r <= 'Z':
		return true
	case r >= '0' && r <= '9':
		return true
	case r == ' ' || r == '-':
		return true
	}
	return false
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
func deriveACP2Type(p *canonical.Parameter) (codec.ACP2ObjType, codec.NumberType, error) {
	parts := formatParts(p.Format)
	hint, known := pickTypeHint(parts)
	if !known {
		return 0, 0, fmt.Errorf("unrecognised format type-hint %q (valid: s8|s16|s32|s64|u8|u16|u32|u64|float|ipv4|preset)", hint)
	}

	// "preset" token → obj_type=1 regardless of the canonical Type. The
	// numeric wire type is picked from the same format hints as a plain
	// Number (s8..u64, float); absent → NumTypeS32.
	if hasPreset(parts) {
		switch hint {
		case "", "s32":
			return codec.ObjTypePreset, codec.NumTypeS32, nil
		case "s8":
			return codec.ObjTypePreset, codec.NumTypeS8, nil
		case "s16":
			return codec.ObjTypePreset, codec.NumTypeS16, nil
		case "s64":
			return codec.ObjTypePreset, codec.NumTypeS64, nil
		case "u8":
			return codec.ObjTypePreset, codec.NumTypeU8, nil
		case "u16":
			return codec.ObjTypePreset, codec.NumTypeU16, nil
		case "u32":
			return codec.ObjTypePreset, codec.NumTypeU32, nil
		case "u64":
			return codec.ObjTypePreset, codec.NumTypeU64, nil
		case "float":
			return codec.ObjTypePreset, codec.NumTypeFloat, nil
		}
		return 0, 0, fmt.Errorf("preset: unknown number type %q", hint)
	}

	switch p.Type {
	case canonical.ParamReal:
		return codec.ObjTypeNumber, codec.NumTypeFloat, nil
	case canonical.ParamEnum:
		return codec.ObjTypeEnum, codec.NumTypeU32, nil
	case canonical.ParamInteger:
		switch hint {
		case "", "s32":
			return codec.ObjTypeNumber, codec.NumTypeS32, nil
		case "s8":
			return codec.ObjTypeNumber, codec.NumTypeS8, nil
		case "s16":
			return codec.ObjTypeNumber, codec.NumTypeS16, nil
		case "s64":
			return codec.ObjTypeNumber, codec.NumTypeS64, nil
		case "u8":
			return codec.ObjTypeNumber, codec.NumTypeU8, nil
		case "u16":
			return codec.ObjTypeNumber, codec.NumTypeU16, nil
		case "u32":
			return codec.ObjTypeNumber, codec.NumTypeU32, nil
		case "u64":
			return codec.ObjTypeNumber, codec.NumTypeU64, nil
		case "float":
			return codec.ObjTypeNumber, codec.NumTypeFloat, nil
		}
		return 0, 0, fmt.Errorf("integer: unknown number type %q", hint)
	case canonical.ParamString:
		switch hint {
		case "", "string":
			return codec.ObjTypeString, codec.NumTypeString, nil
		case "ipv4", "ipaddr":
			return codec.ObjTypeIPv4, codec.NumTypeIPv4, nil
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
// wire type; tokens with "=" are attributes (maxLen=N, depth=N) and
// ignored. The bare token "preset" is also skipped here — it is an
// object-type modifier, orthogonal to the numeric wire type, handled
// in deriveACP2Type via hasPreset().
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
		if p == "preset" {
			continue
		}
		if _, ok := known[p]; ok {
			return p, true
		}
		return p, false
	}
	return "", true
}

// hasPreset reports whether the canonical format carries the bare
// "preset" token. A preset in ACP2 is obj_type=1 — an orthogonal
// modifier on a leaf parameter: pid 7 preset_depth lists valid idx
// values and pids 8/9/10/11 are repeated once per idx.
func hasPreset(parts []string) bool {
	for _, p := range parts {
		if p == "preset" {
			return true
		}
	}
	return false
}

// presetDepthHint extracts the "depth=N" attribute from
// Parameter.Format, returning 0 when absent. Used only for preset
// children; non-presets leave presetDepth at 0.
func presetDepthHint(p *canonical.Parameter) uint32 {
	for _, kv := range formatParts(p.Format) {
		if strings.HasPrefix(kv, "depth=") {
			var n int
			_, err := fmt.Sscanf(kv, "depth=%d", &n)
			if err == nil && n > 0 && n <= 65535 {
				return uint32(n)
			}
		}
	}
	return 0
}

// presetIdxHint extracts the "idx=A|B|C" attribute from
// Parameter.Format, returning the parsed u32 values. Used by the
// encoder to emit pid 7 (preset_depth) with the spec-allowed
// non-contiguous idx list (e.g. {100, 200} per acp2_protocol.docx
// §"Preset depth" line 2613-2632) instead of synthesising contiguous
// 0..N-1.
//
// The pipe `|` separates idx values rather than comma, because comma
// is the existing top-level separator between Format hints
// (`depth=2,maxLen=16,idx=100|200`). Returns nil when the hint is
// absent or unparseable; callers fall back to contiguous indices.
func presetIdxHint(p *canonical.Parameter) []uint32 {
	for _, kv := range formatParts(p.Format) {
		if !strings.HasPrefix(kv, "idx=") {
			continue
		}
		raw := strings.TrimPrefix(kv, "idx=")
		parts := strings.Split(raw, "|")
		out := make([]uint32, 0, len(parts))
		for _, p := range parts {
			p = strings.TrimSpace(p)
			if p == "" {
				continue
			}
			var n uint32
			if _, err := fmt.Sscanf(p, "%d", &n); err == nil {
				out = append(out, n)
			}
		}
		if len(out) > 0 {
			return out
		}
	}
	return nil
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
