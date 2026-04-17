package acp2

import (
	"context"
	"encoding/binary"
	"fmt"
	"log/slog"
	"strings"

	"acp/internal/protocol"
)

// WalkedTree is the decoded object tree for one slot, analogous to
// acp1.SlotTree but for the ACP2 hierarchical object model.
type WalkedTree struct {
	Slot    int
	Objects []protocol.Object
	// ObjTypes parallels Objects — the ACP2 object type for each entry.
	ObjTypes []ACP2ObjType
	// NumTypes parallels Objects — the NumberType for numeric objects.
	NumTypes []NumberType
	// OptionsMaps parallels Objects — wire-index→label map for enum/preset objects.
	OptionsMaps []map[uint32]string
	// Labels maps label → index into Objects for label-based lookup.
	Labels map[string]int
}

// Lookup finds the object index by label. Returns -1 if not found.
func (t *WalkedTree) Lookup(label string) int {
	if t == nil {
		return -1
	}
	idx, ok := t.Labels[label]
	if !ok {
		return -1
	}
	return idx
}

// WalkProgressFunc is called after each object is added to the tree during walk.
type WalkProgressFunc func(count int, obj *protocol.Object)

// Walker performs a DFS walk of the ACP2 object tree starting from the root.
type Walker struct {
	session    *Session
	logger     *slog.Logger
	OnProgress WalkProgressFunc
}

// NewWalker creates a walker that uses the given session for ACP2 requests.
func NewWalker(session *Session, logger *slog.Logger) *Walker {
	return &Walker{session: session, logger: logger}
}

// Walk performs a depth-first traversal of the ACP2 object tree on the
// given slot. The root obj-id varies by device firmware: the spec
// implies 0, but real devices (e.g. Axon SHPRM1 / CONVERT Hybrid)
// use obj-id 1 with label "ROOT_NODE_V2". We try 1 first, fall back
// to 0, so both conventions work.
func (w *Walker) Walk(ctx context.Context, slot int) (*WalkedTree, error) {
	tree := &WalkedTree{
		Slot:   slot,
		Labels: make(map[string]int),
	}

	w.logger.Debug("acp2: walker: starting DFS walk", "slot", slot)

	// Try obj-id 1 first (real devices), then obj-id 0 (spec default).
	rootErr := w.walkObject(ctx, slot, 1, nil, tree)
	if rootErr != nil {
		w.logger.Debug("acp2: walker: obj-id 1 failed, trying obj-id 0", "err", rootErr)
		tree.Objects = nil
		tree.Labels = make(map[string]int)
		rootErr = w.walkObject(ctx, slot, 0, nil, tree)
	}
	if rootErr != nil {
		return nil, fmt.Errorf("acp2 walk slot %d: %w", slot, rootErr)
	}

	w.logger.Debug("acp2: walker: walk complete",
		"slot", slot, "objects", len(tree.Objects))

	return tree, nil
}

// walkObject fetches one object via get_object and recursively walks its children.
// path tracks the label path from root to current node for protocol.Object.Path.
func (w *Walker) walkObject(ctx context.Context, slot int, objID uint32, path []string, tree *WalkedTree) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}

	w.logger.Debug("acp2: walker: get_object", "slot", slot, "obj_id", objID)

	msg, err := w.session.DoACP2(ctx, uint8(slot), &ACP2Message{
		Type:  ACP2TypeRequest,
		Func:  ACP2FuncGetObject,
		ObjID: objID,
		Idx:   0, // active index
	})
	if err != nil {
		return fmt.Errorf("get_object(%d): %w", objID, err)
	}

	// Parse properties from the reply.
	obj, objType, numType, optMap, children := w.parseObjectProperties(msg.Properties, slot, objID, path)

	// Add to tree (skip pure node containers from the flat list — they
	// are structural only, not addressable objects with values).
	// Actually, node objects ARE added so users can see the tree structure.
	idx := len(tree.Objects)
	tree.Objects = append(tree.Objects, obj)
	tree.ObjTypes = append(tree.ObjTypes, objType)
	tree.NumTypes = append(tree.NumTypes, numType)
	tree.OptionsMaps = append(tree.OptionsMaps, optMap)
	if obj.Label != "" {
		tree.Labels[obj.Label] = idx
	}
	if w.OnProgress != nil {
		w.OnProgress(idx+1, &tree.Objects[idx])
	}

	// Recurse into children.
	for _, childID := range children {
		childPath := make([]string, len(obj.Path))
		copy(childPath, obj.Path)
		if err := w.walkObject(ctx, slot, childID, childPath, tree); err != nil {
			// Log and continue on child errors — partial walk is better
			// than no walk.
			w.logger.Warn("acp2: walker: child error",
				"parent_id", objID, "child_id", childID, "err", err)
		}
	}

	return nil
}

// parseObjectProperties extracts a protocol.Object from ACP2 property headers.
func (w *Walker) parseObjectProperties(props []Property, slot int, objID uint32, parentPath []string) (protocol.Object, ACP2ObjType, NumberType, map[uint32]string, []uint32) {
	obj := protocol.Object{
		Slot: slot,
		ID:   int(objID),
	}

	var objType ACP2ObjType
	var numType NumberType
	var children []uint32
	var optionsMap map[uint32]string
	var valueProp *Property // deferred — options may come after value on the wire

	// First pass: collect metadata, options, constraints, children.
	// Value decode is deferred because pid=8 often arrives before pid=15
	// (options) in the get_object reply, and enum label resolution needs
	// the options map.
	for i := range props {
		p := &props[i]
		switch p.PID {
		case PIDObjectType:
			if len(p.Data) >= 4 {
				objType = ACP2ObjType(p.Data[3]) // u32, but only low byte matters
			} else {
				objType = ACP2ObjType(p.VType)
			}

		case PIDLabel:
			obj.Label = PropertyString(p)

		case PIDAccess:
			if len(p.Data) >= 4 {
				obj.Access = uint8(p.Data[3])
			} else {
				obj.Access = p.VType
			}

		case PIDNumberType:
			if len(p.Data) >= 4 {
				numType = NumberType(p.Data[3])
			} else {
				numType = NumberType(p.VType)
			}

		case PIDStringMaxLength:
			if v, err := PropertyU16(p); err == nil {
				obj.MaxLen = int(v)
			} else if len(p.Data) >= 4 {
				obj.MaxLen = int(p.Data[3])
			}

		case PIDValue:
			valueProp = p // defer until options are collected

		case PIDDefaultValue:
			w.decodeConstraint(p, objType, numType, &obj, "default")

		case PIDMinValue:
			w.decodeConstraint(p, objType, numType, &obj, "min")

		case PIDMaxValue:
			w.decodeConstraint(p, objType, numType, &obj, "max")

		case PIDStepSize:
			w.decodeConstraint(p, objType, numType, &obj, "step")

		case PIDUnit:
			obj.Unit = strings.TrimSpace(PropertyString(p))

		case PIDChildren:
			if ids, err := PropertyChildren(p); err == nil {
				children = ids
			}

		case PIDOptions:
			obj.EnumItems = PropertyOptions(p)
			optionsMap = PropertyOptionsMap(p)

		case PIDEventTag:
			if len(p.Data) >= 2 {
				obj.AlarmTag = p.Data[1]
			}

		case PIDEventPrio:
			if len(p.Data) >= 4 {
				obj.AlarmPriority = uint8(p.Data[3])
			}

		case PIDEventMessages:
			obj.AlarmOnMsg, obj.AlarmOffMsg = PropertyEventMessages(p)
		}
	}

	// Second pass: decode value now that options map is available.
	if valueProp != nil {
		w.decodeValue(valueProp, objType, numType, &obj, optionsMap)
	}

	// Resolve enum/preset default to label via optionsMap.
	if (objType == ObjTypeEnum || objType == ObjTypePreset) && obj.Def != nil && optionsMap != nil {
		if defIdx, ok := obj.Def.(uint64); ok {
			if label, found := optionsMap[uint32(defIdx)]; found {
				obj.Def = label
			}
		}
	}

	// Build Path from parent path + this object's label.
	if obj.Label != "" {
		obj.Path = append(parentPath, obj.Label)
	} else {
		obj.Path = append(parentPath, fmt.Sprintf("obj_%d", objID))
	}

	// Set Group from second path element (first child of root).
	// ACP2 path: [ROOT_NODE_V2, BOARD, Card Name] → group = "BOARD"
	if len(obj.Path) > 1 {
		obj.Group = obj.Path[1]
	}

	// Map ACP2 object type to protocol.ValueKind.
	switch objType {
	case ObjTypeNode:
		obj.Kind = protocol.KindRaw // containers have no scalar value
	case ObjTypeEnum:
		obj.Kind = protocol.KindEnum
	case ObjTypeNumber:
		obj.Kind = numberTypeToKind(numType)
	case ObjTypeIPv4:
		obj.Kind = protocol.KindIPAddr
	case ObjTypeString:
		obj.Kind = protocol.KindString
	case ObjTypePreset:
		obj.Kind = protocol.KindEnum // presets are enumeration-like
	}

	return obj, objType, numType, optionsMap, children
}

// decodeValue decodes a pid=8 (value) property into the Object's Value field.
func (w *Walker) decodeValue(p *Property, objType ACP2ObjType, numType NumberType, obj *protocol.Object, optMap map[uint32]string) {
	switch objType {
	case ObjTypeNumber:
		nt := NumberType(p.VType)
		if nt == 0 && numType != 0 {
			nt = numType
		}
		intV, uintV, floatV, err := DecodeNumericValue(nt, p.Data)
		if err != nil {
			w.logger.Debug("acp2: walker: decode numeric value", "err", err)
			obj.Value = protocol.Value{Kind: protocol.KindRaw, Raw: p.Data}
			return
		}
		switch numberTypeToKind(nt) {
		case protocol.KindInt:
			obj.Value = protocol.Value{Kind: protocol.KindInt, Int: intV, Raw: p.Data}
		case protocol.KindUint:
			obj.Value = protocol.Value{Kind: protocol.KindUint, Uint: uintV, Raw: p.Data}
		case protocol.KindFloat:
			obj.Value = protocol.Value{Kind: protocol.KindFloat, Float: floatV, Raw: p.Data}
		}

	case ObjTypeEnum, ObjTypePreset:
		if len(p.Data) >= 4 {
			fullIdx := binary.BigEndian.Uint32(p.Data[0:4])
			ev := protocol.Value{
				Kind: protocol.KindEnum,
				Enum: uint8(fullIdx),
				Uint: uint64(fullIdx),
				Raw:  p.Data,
			}
			if optMap != nil {
				if label, ok := optMap[fullIdx]; ok {
					ev.Str = label
				}
			}
			obj.Value = ev
		}

	case ObjTypeIPv4:
		if len(p.Data) >= 4 {
			obj.Value = protocol.Value{
				Kind: protocol.KindIPAddr,
				IPAddr: [4]byte{
					p.Data[0], p.Data[1], p.Data[2], p.Data[3],
				},
				Raw: p.Data,
			}
		}

	case ObjTypeString:
		obj.Value = protocol.Value{
			Kind: protocol.KindString,
			Str:  PropertyString(p),
			Raw:  p.Data,
		}

	default:
		if p.Data != nil {
			obj.Value = protocol.Value{Kind: protocol.KindRaw, Raw: p.Data}
		}
	}
}

// decodeConstraint decodes min/max/step/default properties.
func (w *Walker) decodeConstraint(p *Property, objType ACP2ObjType, numType NumberType, obj *protocol.Object, which string) {
	// Enum/preset: only default makes sense (u32 index).
	if (objType == ObjTypeEnum || objType == ObjTypePreset) && which == "default" {
		if len(p.Data) >= 4 {
			obj.Def = uint64(binary.BigEndian.Uint32(p.Data[0:4]))
		}
		return
	}

	if objType != ObjTypeNumber {
		return
	}
	nt := NumberType(p.VType)
	if nt == 0 && numType != 0 {
		nt = numType
	}
	intV, uintV, floatV, err := DecodeNumericValue(nt, p.Data)
	if err != nil {
		return
	}

	var val any
	switch numberTypeToKind(nt) {
	case protocol.KindInt:
		val = intV
	case protocol.KindUint:
		val = uintV
	case protocol.KindFloat:
		val = floatV
	default:
		return
	}

	switch which {
	case "min":
		obj.Min = val
	case "max":
		obj.Max = val
	case "step":
		obj.Step = val
	case "default":
		obj.Def = val
	}
}

// numberTypeToKind maps an ACP2 NumberType to a protocol.ValueKind.
func numberTypeToKind(nt NumberType) protocol.ValueKind {
	switch nt {
	case NumTypeS8, NumTypeS16, NumTypeS32, NumTypeS64:
		return protocol.KindInt
	case NumTypeU8, NumTypeU16, NumTypeU32, NumTypeU64:
		return protocol.KindUint
	case NumTypeFloat:
		return protocol.KindFloat
	case NumTypePreset:
		return protocol.KindEnum
	case NumTypeIPv4:
		return protocol.KindIPAddr
	case NumTypeString:
		return protocol.KindString
	default:
		return protocol.KindRaw
	}
}
