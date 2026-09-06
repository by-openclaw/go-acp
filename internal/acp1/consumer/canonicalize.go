package acp1

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"dhs/internal/acp1/codec"
	"dhs/internal/consumer"
	"dhs/internal/export/canonical"
)

// Canonicalize walks every cached SlotTree on this plugin and emits
// the device as a canonical Export per docs/protocols/schema.md. Shape:
//
//	device (Node, oid="1")
//	├── slot-0 (Node, number=0)
//	│   ├── identity (Node, number=1)
//	│   │   └── Parameter... (one per object in group)
//	│   ├── control  (Node, number=2)
//	│   ├── status   (Node, number=3)  (read-only)
//	│   ├── alarm    (Node, number=4)
//	│   └── file     (Node, number=5)
//	└── slot-N ...
//
// Only slots that are cached (already walked) appear; a fresh plugin
// with no Walk calls emits an empty-children device Node.
//
// ACP1's resolver / mode flags (templates / labels / gain) do not apply
// — the protocol has no `templateReference`, no `labels[]` SEQUENCE,
// no `parametersLocation`. Passing the CLI flags through is a no-op.
//
// Spec cross-references are in the per-type mapping helpers below.
func (p *Plugin) Canonicalize(ctx context.Context) (*canonical.Export, error) {
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("canonicalize canceled: %w", err)
	}

	p.mu.Lock()
	host := p.host
	trees := p.trees
	p.mu.Unlock()

	slotNodes := make([]canonical.Element, 0)
	if trees != nil {
		trees.mu.Lock()
		for slot, el := range trees.entries {
			entry := el.Value.(*cacheEntry)
			node := buildSlotNode(slot, entry.tree)
			slotNodes = append(slotNodes, node)
		}
		trees.mu.Unlock()
	}

	// Deterministic slot order — slot-0 first, then ascending.
	sortByNumber(slotNodes)

	identifier := "device"
	if host != "" {
		identifier = host
	}

	root := &canonical.Node{
		Header: canonical.Header{
			Number:     1,
			Identifier: identifier,
			Path:       identifier,
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

// buildSlotNode constructs the canonical Node for one slot. Children
// are the five AxonNet object groups (identity, control, status, alarm,
// file) each with their Parameters.
func buildSlotNode(slot int, tree *SlotTree) *canonical.Node {
	slotIdent := "slot-" + strconv.Itoa(slot)
	slotOID := "1." + strconv.Itoa(slot+1) // 1-based so slot-0 → 1.1
	slotPath := slotIdent

	groups := []struct {
		number int
		name   string
	}{
		{1, "identity"},
		{2, "control"},
		{3, "status"},
		{4, "alarm"},
		{5, "file"},
	}

	children := make([]canonical.Element, 0, len(groups))
	for _, g := range groups {
		groupNode := buildGroupNode(slot, slotOID, slotPath, g.number, g.name, tree)
		if groupNode != nil {
			children = append(children, groupNode)
		}
	}

	return &canonical.Node{
		Header: canonical.Header{
			Number:     slot,
			Identifier: slotIdent,
			Path:       slotPath,
			OID:        slotOID,
			IsOnline:   true,
			Access:     canonical.AccessRead,
			Children:   children,
		},
	}
}

// buildGroupNode collects every Object in the tree belonging to the named
// group into a Node. Sub-group markers (Synapse section headers like
// "DOWN CONV" / "TRANSPARENT" / "INSERTER" / "VIDEO PROC", flagged by
// codec.IsSubGroupMarker during the walk) become PARENT nodes: the objects
// that follow a marker — up to the next marker — nest as its children, so the
// canonical tree mirrors what a real controller (Cerebrum) shows. Objects
// before the first marker stay direct children of the group.
//
// This nesting is structural (export / API / UI) only. get/set/ensure resolve
// against the flat SlotTree by (group,id) or label, so control is unaffected
// by the hierarchy. Returns nil when no objects fall into this group.
func buildGroupNode(slot int, slotOID, slotPath string, groupNumber int, groupName string, tree *SlotTree) *canonical.Node {
	if tree == nil {
		return nil
	}

	groupOID := slotOID + "." + strconv.Itoa(groupNumber)
	groupPath := slotPath + "." + groupName

	// Collect this group's objects (+ their wire type) in object-id order so
	// markers precede the children that belong under them, deterministically.
	type groupObj struct {
		obj consumer.Object
		acp codec.ObjectType
	}
	items := make([]groupObj, 0)
	for i, obj := range tree.Objects {
		if obj.Group != groupName {
			continue
		}
		acpType := codec.ObjectType(0)
		if i < len(tree.ACPTypes) {
			acpType = tree.ACPTypes[i]
		}
		items = append(items, groupObj{obj, acpType})
	}
	if len(items) == 0 {
		return nil
	}
	sort.SliceStable(items, func(a, b int) bool { return items[a].obj.ID < items[b].obj.ID })

	children := make([]canonical.Element, 0)
	var current *canonical.Node // active sub-group parent, nil = top of group
	curOID, curPath := groupOID, groupPath
	for _, it := range items {
		if it.obj.SubGroupMarker {
			name := strings.TrimSpace(it.obj.Label)
			if name == "" {
				// NO_SUB_GROUP terminator (single-space enum marker): close
				// any open section back to group top-level and keep the marker
				// itself as a plain leaf rather than an empty parent.
				current = nil
				curOID, curPath = groupOID, groupPath
				if p := buildParameter(it.obj, it.acp, groupOID, groupPath); p != nil {
					children = append(children, p)
				}
				continue
			}
			mOID := groupOID + "." + strconv.Itoa(it.obj.ID)
			mPath := groupPath + "." + name
			current = &canonical.Node{
				Header: canonical.Header{
					Number:     it.obj.ID,
					Identifier: name,
					Path:       mPath,
					OID:        mOID,
					IsOnline:   true,
					Access:     canonical.AccessRead,
					Children:   make([]canonical.Element, 0),
				},
			}
			children = append(children, current)
			curOID, curPath = mOID, mPath
			// The marker is an object on the wire with an id of its own, so
			// it is also carried as this section's first leaf. Without it the
			// group's ids have a hole where every marker sits, and a walk of
			// a served copy stops there with "object instance does not
			// exist". Its own canonical type (enum or string, per
			// codec.IsSubGroupMarker) is what a provider rebuilds it from,
			// so nothing about the marker has to be guessed downstream.
			if p := buildParameter(it.obj, it.acp, mOID, mPath); p != nil {
				current.Children = append(current.Children, p)
			}
			continue
		}
		if current != nil {
			if p := buildParameter(it.obj, it.acp, curOID, curPath); p != nil {
				current.Children = append(current.Children, p)
			}
			continue
		}
		if p := buildParameter(it.obj, it.acp, groupOID, groupPath); p != nil {
			children = append(children, p)
		}
	}

	return &canonical.Node{
		Header: canonical.Header{
			Number:     groupNumber,
			Identifier: groupName,
			Path:       groupPath,
			OID:        groupOID,
			IsOnline:   true,
			Access:     canonical.AccessRead,
			Children:   children,
		},
	}
}

// buildParameter maps a consumer.Object to a canonical.Parameter. Spec
// cross-refs are in per-kind switch below.
func buildParameter(obj consumer.Object, acpType codec.ObjectType, parentOID, parentPath string) *canonical.Parameter {
	oid := parentOID + "." + strconv.Itoa(obj.ID)
	path := parentPath + "." + obj.Label
	// Use Label as identifier; fall back to "#<id>" when a device
	// leaves the label empty.
	ident := obj.Label
	if ident == "" {
		ident = "#" + strconv.Itoa(obj.ID)
	}

	p := &canonical.Parameter{
		Header: canonical.Header{
			Number:     obj.ID,
			Identifier: ident,
			Path:       path,
			OID:        oid,
			IsOnline:   true,
			Access:     accessString(obj.Access),
			Children:   canonical.EmptyChildren(),
		},
		Type: kindToCanonicalType(obj.Kind, acpType),
	}

	// Numeric-typed constraints. Only emit when non-zero on the wire.
	p.Value = valueToAny(obj.Value)
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

	// Enum: lift the item list into canonical EnumMap (key=label,
	// value=ordinal). Spec p.24 — comma-delimited item_list.
	if obj.Kind == consumer.KindEnum && len(obj.EnumItems) > 0 {
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

	// Alarm event messages (spec p.25). Carry as Parameter description
	// joined "on: <msg>\noff: <msg>" when present — no dedicated field
	// in the canonical shape; operators read via description.
	if obj.Kind == consumer.KindAlarm {
		parts := []string{}
		if obj.AlarmOnMsg != "" {
			parts = append(parts, "on: "+obj.AlarmOnMsg)
		}
		if obj.AlarmOffMsg != "" {
			parts = append(parts, "off: "+obj.AlarmOffMsg)
		}
		if len(parts) > 0 {
			desc := strings.Join(parts, " / ")
			p.Description = &desc
		}
	}

	// Format carries the ACP1 type through the canonical shape, plus any
	// attributes. Both halves matter: the type hint is what lets the
	// provider rebuild the exact ObjectType, and "maxLen=N" is what lets
	// a UI render an input width (the canonical schema has no dedicated
	// max-length field; format is the documented overflow).
	var hints []string
	if h := acp1TypeHint(obj.Kind, acpType); h != "" {
		hints = append(hints, h)
	}
	if obj.Kind == consumer.KindString && obj.MaxLen > 0 {
		hints = append(hints, "maxLen="+strconv.Itoa(obj.MaxLen))
	}
	if len(hints) > 0 {
		joined := strings.Join(hints, ",")
		p.Format = &joined
	}

	return p
}

// acp1TypeHint is the token the ACP1 provider reads back out of
// Parameter.Format to recover the exact ObjectType. Several canonical types
// are the image of more than one ACP1 type — integer covers Integer, Long
// and Byte; string covers String, IPAddr and File; boolean is only ever an
// Alarm — so without the hint the round trip through a canonical tree is
// lossy or, for alarm and frame, refused outright by the provider.
//
// Returns "" where the canonical type already determines the ACP1 type
// (Float, Enum) or where the hint would be the provider's own default
// (Integer), keeping the emitted format free of noise.
func acp1TypeHint(k consumer.ValueKind, acpType codec.ObjectType) string {
	switch k {
	case consumer.KindAlarm:
		return "alarm"
	case consumer.KindFrame:
		return "frame"
	case consumer.KindIPAddr:
		return "ipv4"
	}
	switch acpType {
	case codec.TypeFile:
		return "file"
	case codec.TypeLong:
		return "int32"
	case codec.TypeByte:
		return "uint8"
	}
	return ""
}

// kindToCanonicalType maps ValueKind + ACP1 ObjectType to the canonical
// parameter type string (docs/protocols/elements/parameter.md).
func kindToCanonicalType(k consumer.ValueKind, acpType codec.ObjectType) string {
	switch k {
	case consumer.KindBool:
		return canonical.ParamBoolean
	case consumer.KindInt, consumer.KindUint:
		return canonical.ParamInteger
	case consumer.KindFloat:
		return canonical.ParamReal
	case consumer.KindEnum:
		return canonical.ParamEnum
	case consumer.KindString, consumer.KindIPAddr:
		return canonical.ParamString
	case consumer.KindAlarm:
		// ACP1 alarms are active-or-idle + text messages — boolean
		// with description carries the richest canonical shape.
		return canonical.ParamBoolean
	case consumer.KindRaw:
		return canonical.ParamOctets
	case consumer.KindFrame:
		// Frame status is a slot array; no Parameter mapping
		// materialises. Caller skips.
		return canonical.ParamOctets
	}
	// Fallback: File objects (acpType=TypeFile) and unknown kinds end
	// up here. File is a named resource — surface as string.
	if acpType == codec.TypeFile {
		return canonical.ParamString
	}
	return canonical.ParamString
}

// accessString converts the ACP1 access byte (spec p.20 bit 0=R, bit
// 1=W, bit 2=setDef) to the canonical access string.
func accessString(a uint8) string {
	const (
		read  = 1 << 0
		write = 1 << 1
	)
	switch a & (read | write) {
	case read:
		return canonical.AccessRead
	case write:
		return canonical.AccessWrite
	case read | write:
		return canonical.AccessReadWrite
	default:
		// unreachable: a&(read|write) ∈ {0,1,2,3}; 1,2,3 cased above, so
		// only 0 (no R/W bits) reaches here.
		return canonical.AccessNone
	}
}

// valueToAny turns a consumer.Value into the right Go scalar for the
// canonical JSON value field. Kind dispatches to the typed union
// member; unknown / frame / raw kinds yield nil so the output shows
// `"value": null` rather than a typed zero.
func valueToAny(v consumer.Value) any {
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
		return int64(v.Enum)
	case consumer.KindString:
		return v.Str
	case consumer.KindIPAddr:
		return strconv.Itoa(int(v.IPAddr[0])) + "." +
			strconv.Itoa(int(v.IPAddr[1])) + "." +
			strconv.Itoa(int(v.IPAddr[2])) + "." +
			strconv.Itoa(int(v.IPAddr[3]))
	case consumer.KindAlarm:
		// Alarm carried as boolean active/idle; the message pair
		// lands in description.
		return v.Bool
	}
	return nil
}

// sortByNumber orders a slice of canonical elements by their header
// Number ascending. Used to produce deterministic slot / group / object
// ordering in the export.
func sortByNumber(els []canonical.Element) {
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
