package acp1

import (
	"context"
	"fmt"
	"strings"

	"acp/internal/protocol"
	"acp/internal/protocol/compliance"
)

// walkerClient is the minimum contract the Walker needs from whatever
// client backs it. Both the UDP Client and the TCPClient satisfy this,
// so the Walker is transport-agnostic.
type walkerClient interface {
	Do(ctx context.Context, req *Message) (*Message, error)
}

// Walker walks the AxonNet object tree of one slot on one device.
//
// Workflow (per spec appendix "Recommended Practices" p. 34):
//  1. getObject(root, 0) → number of objects per group
//  2. For each group in {identity, control, status, alarm}:
//     for id in 0..count-1: getObject(group, id)
//  3. Convert each DecodedObject into a protocol-agnostic Object and
//     record its (group, label) pair in the label map so later calls
//     can address by label instead of the volatile object ID.
//
// The walker does NOT read File or Frame groups:
//   - File is out of scope for v1 (CLAUDE.md).
//   - Frame is only relevant on the rack controller (slot 0) and is
//     accessed via getValue(frame, 0), not getObject — handled by
//     GetDeviceInfo in the Plugin.
type Walker struct {
	client  walkerClient
	profile *compliance.Profile
}

// NewWalker wraps a client for object-tree traversal. Accepts any type
// implementing the minimum Do contract — UDP Client, TCPClient, or a
// test fake.
func NewWalker(client walkerClient) *Walker {
	return &Walker{client: client}
}

// SetProfile attaches a compliance profile so getObject-level
// deviations (error replies, short MDATA, unknown MCODE) can be
// counted as tolerance events. Safe to set more than once; nil
// disables event recording.
func (w *Walker) SetProfile(p *compliance.Profile) {
	w.profile = p
}

// SlotTree is the full decoded state of one slot's object tree plus a
// label→object lookup map the Plugin uses for label-addressed GetValue
// and SetValue calls.
type SlotTree struct {
	Slot     int
	BootMode uint8
	Objects  []protocol.Object
	// ACPTypes is parallel to Objects and holds the fine-grained ACP1
	// ObjectType for each entry. The public protocol.Object only carries
	// the widened ValueKind (Integer and Long both map to KindInt), so
	// the value codec needs this side channel to pick the correct wire
	// width at get/set time.
	ACPTypes []ObjectType
	// Labels maps group name → label string → index into Objects.
	// Labels are unique within a group per spec p. 34. Using an index
	// keeps Object slice the source of truth (no duplicated data).
	Labels map[string]map[string]int
}

// Lookup finds the object index for (group, label). Returns -1 if not
// found. Callers use this to translate a ValueRequest into a concrete
// ObjGroup / ObjID pair before issuing a GetValue / SetValue.
func (t *SlotTree) Lookup(group, label string) int {
	if t == nil {
		return -1
	}
	inner, ok := t.Labels[group]
	if !ok {
		return -1
	}
	idx, ok := inner[label]
	if !ok {
		return -1
	}
	return idx
}

// Walk performs the full object-tree traversal for one slot. Returns a
// SlotTree on success. On any transport / protocol error the partial
// results are discarded and the error is surfaced up.
func (w *Walker) Walk(ctx context.Context, slot int) (*SlotTree, error) {
	if slot < 0 || slot > 31 {
		return nil, fmt.Errorf("acp1: slot out of range: %d", slot)
	}

	// Step 1: read the root object to learn how many objects live in
	// each group on this slot.
	root, err := w.getObject(ctx, slot, GroupRoot, 0)
	if err != nil {
		return nil, fmt.Errorf("walk slot %d: root: %w", slot, err)
	}
	if root.Type != TypeRoot {
		return nil, fmt.Errorf("walk slot %d: root is type %d, want 0", slot, root.Type)
	}

	cap := int(root.NumIdentity) + int(root.NumControl) + int(root.NumStatus) + int(root.NumAlarm)
	tree := &SlotTree{
		Slot:     slot,
		BootMode: root.BootMode,
		Objects:  make([]protocol.Object, 0, cap),
		ACPTypes: make([]ObjectType, 0, cap),
		Labels:   map[string]map[string]int{},
	}

	// Step 2: walk each group. Order matches the spec group IDs and the
	// order the C# reference walker uses, for stable output in the CLI
	// tree view.
	walks := []struct {
		group ObjGroup
		count uint8
	}{
		{GroupIdentity, root.NumIdentity},
		{GroupControl, root.NumControl},
		{GroupStatus, root.NumStatus},
		{GroupAlarm, root.NumAlarm},
	}
	for _, g := range walks {
		for id := uint8(0); id < g.count; id++ {
			dec, err := w.getObject(ctx, slot, g.group, id)
			if err != nil {
				return nil, fmt.Errorf("walk slot %d %s[%d]: %w",
					slot, g.group, id, err)
			}
			obj := toProtocolObject(dec, slot, g.group, id)
			idx := len(tree.Objects)
			tree.Objects = append(tree.Objects, obj)
			tree.ACPTypes = append(tree.ACPTypes, dec.Type)
			// Record label → index. Spec p. 34 guarantees label uniqueness
			// within a group; we still tolerate duplicates (last one wins)
			// so a device firmware bug cannot crash the walker.
			if obj.Label != "" {
				gname := g.group.String()
				if tree.Labels[gname] == nil {
					tree.Labels[gname] = map[string]int{}
				}
				tree.Labels[gname][obj.Label] = idx
			}
		}
	}
	return tree, nil
}

// getObject is the one primitive the walker uses: send a getObject
// request for (slot, group, id) and return the typed DecodedObject.
// Handles both transport errors and protocol error replies.
func (w *Walker) getObject(ctx context.Context, slot int, group ObjGroup, id uint8) (*DecodedObject, error) {
	req := &Message{
		MType:    MTypeRequest,
		MAddr:    byte(slot),
		MCode:    byte(MethodGetObject),
		ObjGroup: group,
		ObjID:    id,
	}
	reply, err := w.client.Do(ctx, req)
	if err != nil {
		return nil, err
	}
	if reply.IsError() {
		// Record the class of error per spec p.11 + p.29. Transport
		// errors (MCODE<16) typically reflect device-internal failures
		// we can't diagnose from this side; object errors (MCODE>=16)
		// usually mean the walked tree went stale or the caller asked
		// for something that doesn't exist. Both are absorbed — the
		// caller receives the typed error — and counted for auditing.
		if w.profile != nil {
			if reply.MCode < 16 {
				w.profile.Note(TransportErrorReceived)
			} else {
				w.profile.Note(ObjectErrorReceived)
			}
		}
		return nil, reply.ErrCode()
	}
	return DecodeObject(reply.Value)
}

// toProtocolObject projects one DecodedObject into the protocol-agnostic
// Object type that the rest of the system consumes. Fields not applicable
// to the object's kind stay zero.
//
// Numeric constraints are stored as the widest matching Go type:
//   - Integer / Long / File → int64
//   - Byte / Enum / Alarm priority → uint64
//   - Float → float64
//   - IPAddr → uint64 (u32 widened)
func toProtocolObject(d *DecodedObject, slot int, group ObjGroup, id uint8) protocol.Object {
	o := protocol.Object{
		Slot:  slot,
		Group: group.String(),
		Path:  []string{group.String()},
		ID:    int(id),
		Label: d.Label,
		// Some firmware pads the unit string with leading/trailing
		// whitespace (e.g. " deg.C"). Trim so "%s%s" concatenation with a
		// formatted number always produces one clean space.
		Unit:   strings.TrimSpace(d.Unit),
		Access: d.Access,
	}
	switch d.Type {
	case TypeInteger, TypeLong:
		o.Kind = protocol.KindInt
		o.Min = d.MinInt
		o.Max = d.MaxInt
		o.Step = d.StepInt
		o.Def = d.DefInt
		o.Value = protocol.Value{Kind: protocol.KindInt, Int: d.IntVal}
	case TypeByte:
		o.Kind = protocol.KindUint
		o.Min = uint64(d.MinByte)
		o.Max = uint64(d.MaxByte)
		o.Step = uint64(d.StepByte)
		o.Def = uint64(d.DefByte)
		o.Value = protocol.Value{Kind: protocol.KindUint, Uint: uint64(d.ByteVal)}
	case TypeFloat:
		o.Kind = protocol.KindFloat
		o.Min = d.MinFloat
		o.Max = d.MaxFloat
		o.Step = d.StepFloat
		o.Def = d.DefFloat
		o.Value = protocol.Value{Kind: protocol.KindFloat, Float: d.FloatVal}
	case TypeIPAddr:
		o.Kind = protocol.KindIPAddr
		o.Min = d.MinUint
		o.Max = d.MaxUint
		o.Def = d.DefUint
		u := uint32(d.UintVal)
		o.Value = protocol.Value{
			Kind: protocol.KindIPAddr,
			Uint: d.UintVal,
			IPAddr: [4]byte{
				byte(u >> 24), byte(u >> 16), byte(u >> 8), byte(u),
			},
		}
	case TypeEnum:
		o.Kind = protocol.KindEnum
		o.EnumItems = d.EnumItems
		o.Def = uint64(d.DefByte)
		o.SubGroupMarker = d.IsSubGroupMarker()
		ev := protocol.Value{
			Kind: protocol.KindEnum,
			Enum: d.ByteVal,
			Uint: uint64(d.ByteVal),
		}
		if int(d.ByteVal) < len(d.EnumItems) {
			ev.Str = d.EnumItems[d.ByteVal]
		}
		o.Value = ev
	case TypeString:
		o.Kind = protocol.KindString
		o.MaxLen = int(d.MaxLen)
		o.SubGroupMarker = d.IsSubGroupMarker()
		o.Value = protocol.Value{Kind: protocol.KindString, Str: d.StrValue}
	case TypeAlarm:
		o.Kind = protocol.KindAlarm
		o.AlarmPriority = d.Priority
		o.AlarmTag = d.Tag
		o.AlarmOnMsg = d.EventOnMsg
		o.AlarmOffMsg = d.EventOffMsg
		// Surface the priority byte as a Uint so the CLI has a single
		// formatter path for "current value".
		o.Value = protocol.Value{Kind: protocol.KindUint, Uint: uint64(d.Priority)}
	case TypeFrame:
		o.Kind = protocol.KindFrame
		statuses := make([]protocol.SlotStatus, len(d.SlotStatus))
		for i, s := range d.SlotStatus {
			statuses[i] = protocol.SlotStatus(s)
		}
		o.Value = protocol.Value{Kind: protocol.KindFrame, SlotStatus: statuses}
	default:
		o.Kind = protocol.KindUnknown
	}
	return o
}
