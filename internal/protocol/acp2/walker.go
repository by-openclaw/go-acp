package acp2

import (
	"context"
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

// Walker performs a DFS walk of the ACP2 object tree starting from the root.
type Walker struct {
	session *Session
	logger  *slog.Logger
}

// NewWalker creates a walker that uses the given session for ACP2 requests.
func NewWalker(session *Session, logger *slog.Logger) *Walker {
	return &Walker{session: session, logger: logger}
}

// Walk performs a depth-first traversal of the ACP2 object tree on the
// given slot. Starts from obj-id 0 (root), recursively follows children
// (pid=14), and builds a flat list of protocol.Object.
func (w *Walker) Walk(ctx context.Context, slot int) (*WalkedTree, error) {
	tree := &WalkedTree{
		Slot:   slot,
		Labels: make(map[string]int),
	}

	w.logger.Debug("acp2: walker: starting DFS walk", "slot", slot)

	if err := w.walkObject(ctx, slot, 0, nil, tree); err != nil {
		return nil, fmt.Errorf("acp2 walk slot %d: %w", slot, err)
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
	obj, objType, numType, children := w.parseObjectProperties(msg.Properties, slot, objID, path)

	// Add to tree (skip pure node containers from the flat list — they
	// are structural only, not addressable objects with values).
	// Actually, node objects ARE added so users can see the tree structure.
	idx := len(tree.Objects)
	tree.Objects = append(tree.Objects, obj)
	tree.ObjTypes = append(tree.ObjTypes, objType)
	tree.NumTypes = append(tree.NumTypes, numType)
	if obj.Label != "" {
		tree.Labels[obj.Label] = idx
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
func (w *Walker) parseObjectProperties(props []Property, slot int, objID uint32, parentPath []string) (protocol.Object, ACP2ObjType, NumberType, []uint32) {
	obj := protocol.Object{
		Slot: slot,
		ID:   int(objID),
	}

	var objType ACP2ObjType
	var numType NumberType
	var children []uint32

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
			// Decode the value based on object type and number type.
			w.decodeValue(p, objType, numType, &obj)

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

	// Build Path from parent path + this object's label.
	if obj.Label != "" {
		obj.Path = append(parentPath, obj.Label)
	} else {
		obj.Path = append(parentPath, fmt.Sprintf("obj_%d", objID))
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

	return obj, objType, numType, children
}

// decodeValue decodes a pid=8 (value) property into the Object's Value field.
func (w *Walker) decodeValue(p *Property, objType ACP2ObjType, numType NumberType, obj *protocol.Object) {
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
			idx := p.Data[3] // u32 low byte
			ev := protocol.Value{
				Kind: protocol.KindEnum,
				Enum: idx,
				Uint: uint64(idx),
				Raw:  p.Data,
			}
			if int(idx) < len(obj.EnumItems) {
				ev.Str = obj.EnumItems[idx]
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
