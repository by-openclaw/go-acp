package acp1

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"acp/internal/export/canonical"
	"acp/internal/protocol"
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

// buildGroupNode collects every Object in the tree belonging to the
// named group into a Node whose children are the group's Parameters.
// Returns nil when no objects fall into this group (keeps the slot's
// children[] clean of empty placeholders).
func buildGroupNode(slot int, slotOID, slotPath string, groupNumber int, groupName string, tree *SlotTree) *canonical.Node {
	if tree == nil {
		return nil
	}

	groupOID := slotOID + "." + strconv.Itoa(groupNumber)
	groupPath := slotPath + "." + groupName

	children := make([]canonical.Element, 0)
	for i, obj := range tree.Objects {
		if obj.Group != groupName {
			continue
		}
		acpType := ObjectType(0)
		if i < len(tree.ACPTypes) {
			acpType = tree.ACPTypes[i]
		}
		param := buildParameter(obj, acpType, groupOID, groupPath)
		if param != nil {
			children = append(children, param)
		}
	}

	if len(children) == 0 {
		return nil
	}

	// Deterministic output: sort by object ID ascending within each
	// group so the same walk produces byte-identical tree.json across
	// runs (important for doc-conformance / golden tests and diff
	// readability).
	sortByNumber(children)

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

// buildParameter maps a protocol.Object to a canonical.Parameter. Spec
// cross-refs are in per-kind switch below.
func buildParameter(obj protocol.Object, acpType ObjectType, parentOID, parentPath string) *canonical.Parameter {
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
	if obj.Kind == protocol.KindEnum && len(obj.EnumItems) > 0 {
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
	if obj.Kind == protocol.KindAlarm {
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

	// String MaxLen: exposed via format hint "maxLen=N" so the UI
	// can render an input width. Canonical schema doesn't have a
	// dedicated max-length field; format is the documented overflow.
	if obj.Kind == protocol.KindString && obj.MaxLen > 0 {
		hint := "maxLen=" + strconv.Itoa(obj.MaxLen)
		p.Format = &hint
	}

	return p
}

// kindToCanonicalType maps ValueKind + ACP1 ObjectType to the canonical
// parameter type string (docs/protocols/elements/parameter.md).
func kindToCanonicalType(k protocol.ValueKind, acpType ObjectType) string {
	switch k {
	case protocol.KindBool:
		return canonical.ParamBoolean
	case protocol.KindInt, protocol.KindUint:
		return canonical.ParamInteger
	case protocol.KindFloat:
		return canonical.ParamReal
	case protocol.KindEnum:
		return canonical.ParamEnum
	case protocol.KindString, protocol.KindIPAddr:
		return canonical.ParamString
	case protocol.KindAlarm:
		// ACP1 alarms are active-or-idle + text messages — boolean
		// with description carries the richest canonical shape.
		return canonical.ParamBoolean
	case protocol.KindRaw:
		return canonical.ParamOctets
	case protocol.KindFrame:
		// Frame status is a slot array; no Parameter mapping
		// materialises. Caller skips.
		return canonical.ParamOctets
	}
	// Fallback: File objects (acpType=TypeFile) and unknown kinds end
	// up here. File is a named resource — surface as string.
	if acpType == TypeFile {
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
	case 0:
		return canonical.AccessNone
	}
	return canonical.AccessRead
}

// valueToAny turns a protocol.Value into the right Go scalar for the
// canonical JSON value field. Kind dispatches to the typed union
// member; unknown / frame / raw kinds yield nil so the output shows
// `"value": null` rather than a typed zero.
func valueToAny(v protocol.Value) any {
	switch v.Kind {
	case protocol.KindBool:
		return v.Bool
	case protocol.KindInt:
		return v.Int
	case protocol.KindUint:
		return v.Uint
	case protocol.KindFloat:
		return v.Float
	case protocol.KindEnum:
		return int64(v.Enum)
	case protocol.KindString:
		return v.Str
	case protocol.KindIPAddr:
		return strconv.Itoa(int(v.IPAddr[0])) + "." +
			strconv.Itoa(int(v.IPAddr[1])) + "." +
			strconv.Itoa(int(v.IPAddr[2])) + "." +
			strconv.Itoa(int(v.IPAddr[3]))
	case protocol.KindAlarm:
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
