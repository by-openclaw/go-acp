package acp2

import (
	"context"
	"encoding/binary"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"acp/internal/protocol"
	"acp/internal/transport"
)

// init registers the ACP2 plugin with the global protocol registry.
// Importing this package from cmd/ is enough to make "acp2" selectable
// by name via the --protocol flag.
func init() {
	protocol.Register(&Factory{})
}

// Factory builds ACP2 Plugin instances.
type Factory struct{}

func (f *Factory) Meta() protocol.ProtocolMeta {
	return protocol.ProtocolMeta{
		Name:        "acp2",
		DefaultPort: DefaultPort,
		Description: "Axon Control Protocol v2 (AN2/TCP)",
	}
}

func (f *Factory) New(logger *slog.Logger) protocol.Protocol {
	return &Plugin{logger: logger}
}

// Plugin is the ACP2 Protocol implementation. One instance handles one
// device. Internally it holds an AN2 Session for transport, a Walker for
// tree traversal, and per-slot caches of walked trees.
type Plugin struct {
	logger *slog.Logger

	mu      sync.Mutex
	session *Session
	walker  *Walker

	// trees caches the walked object tree per slot.
	trees map[int]*WalkedTree

	// Announce subscription tracking.
	subHandles map[subKey]int // subKey → session announce subscription ID

	// Optional traffic capture.
	recorder *transport.Recorder

	// Optional walk progress callback.
	walkProgress WalkProgressFunc
}

// subKey canonicalises a ValueRequest for map lookup.
type subKey struct {
	slot  int
	label string
	id    int
}

func reqToSubKey(req protocol.ValueRequest) subKey {
	return subKey{req.Slot, req.Label, req.ID}
}

// SetRecorder attaches a traffic recorder. Call before Connect.
func (p *Plugin) SetRecorder(rec *transport.Recorder) {
	p.mu.Lock()
	p.recorder = rec
	p.mu.Unlock()
}

// SetWalkProgress sets a callback invoked for each object during Walk.
// Allows the CLI to print objects as they're discovered (streaming output).
func (p *Plugin) SetWalkProgress(fn WalkProgressFunc) {
	p.mu.Lock()
	if p.walker != nil {
		p.walker.OnProgress = fn
	}
	p.walkProgress = fn
	p.mu.Unlock()
}

// Connect establishes the AN2/TCP connection and runs the full handshake:
// AN2 GetVersion, GetDeviceInfo, GetSlotInfo, EnableProtocolEvents, ACP2 GetVersion.
func (p *Plugin) Connect(ctx context.Context, ip string, port int) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.session != nil {
		return fmt.Errorf("acp2: already connected")
	}

	s := NewSession(p.logger)
	if p.recorder != nil {
		s.SetRecorder(p.recorder)
	}
	if err := s.Connect(ctx, ip, port); err != nil {
		return err
	}

	p.session = s
	p.walker = NewWalker(s, p.logger)
	p.walker.OnProgress = p.walkProgress
	p.trees = make(map[int]*WalkedTree)
	p.subHandles = make(map[subKey]int)
	return nil
}

// Disconnect tears down the AN2/TCP connection.
func (p *Plugin) Disconnect() error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.session == nil {
		return nil
	}
	err := p.session.Disconnect()
	p.session = nil
	p.walker = nil
	p.trees = nil
	p.subHandles = nil
	return err
}

// GetDeviceInfo returns device metadata from the AN2 handshake.
func (p *Plugin) GetDeviceInfo(ctx context.Context) (protocol.DeviceInfo, error) {
	p.mu.Lock()
	s := p.session
	p.mu.Unlock()
	if s == nil {
		return protocol.DeviceInfo{}, protocol.ErrNotConnected
	}

	return protocol.DeviceInfo{
		IP:              s.Host(),
		Port:            s.Port(),
		NumSlots:        s.NumSlots(),
		ProtocolVersion: int(s.ACP2Version()),
	}, nil
}

// GetSlotInfo returns the slot status as known from the AN2 handshake.
func (p *Plugin) GetSlotInfo(ctx context.Context, slot int) (protocol.SlotInfo, error) {
	p.mu.Lock()
	s := p.session
	p.mu.Unlock()
	if s == nil {
		return protocol.SlotInfo{}, protocol.ErrNotConnected
	}

	si := s.SlotInfoFromAN2(slot)

	// If the slot has been walked, add identity from the tree.
	p.mu.Lock()
	tree := p.trees[slot]
	p.mu.Unlock()
	if tree != nil {
		si.Identity = make(map[string]string)
		for _, obj := range tree.Objects {
			if obj.Kind == protocol.KindString && obj.Value.Str != "" {
				si.Identity[obj.Label] = obj.Value.Str
			}
		}
	}

	return si, nil
}

// Walk enumerates every object on the given slot using DFS traversal.
// Results are cached; subsequent calls return the cached tree.
func (p *Plugin) Walk(ctx context.Context, slot int) ([]protocol.Object, error) {
	p.mu.Lock()
	w := p.walker
	if tree, ok := p.trees[slot]; ok {
		p.mu.Unlock()
		return tree.Objects, nil
	}
	p.mu.Unlock()

	if w == nil {
		return nil, protocol.ErrNotConnected
	}

	tree, err := w.Walk(ctx, slot)
	if err != nil {
		return nil, err
	}

	p.mu.Lock()
	p.trees[slot] = tree
	p.mu.Unlock()

	return tree.Objects, nil
}

// GetValue reads one object value via ACP2 get_property(pid=8).
func (p *Plugin) GetValue(ctx context.Context, req protocol.ValueRequest) (protocol.Value, error) {
	p.mu.Lock()
	s := p.session
	tree := p.trees[req.Slot]
	p.mu.Unlock()
	if s == nil {
		return protocol.Value{}, protocol.ErrNotConnected
	}

	objID, objType, numType, _, err := p.resolveRequest(req, tree)
	if err != nil {
		return protocol.Value{}, err
	}

	// If no tree → type unknown. Fetch object metadata via get_object
	// to learn the type before reading the value (same pattern as ACP1).
	if objType == 0 && tree == nil {
		var miniTree *WalkedTree
		objType, numType, miniTree, err = p.fetchObjectMeta(ctx, s, uint8(req.Slot), objID)
		if err != nil {
			return protocol.Value{}, err
		}
		tree = miniTree
	}

	msg, err := s.DoACP2(ctx, uint8(req.Slot), &ACP2Message{
		Type:  ACP2TypeRequest,
		Func:  ACP2FuncGetProperty,
		PID:   PIDValue,
		ObjID: objID,
		Idx:   0, // active index
	})
	if err != nil {
		return protocol.Value{}, err
	}

	// Decode the value property from the reply.
	for i := range msg.Properties {
		prop := &msg.Properties[i]
		if prop.PID == PIDValue {
			return decodePropertyValue(prop, objType, numType, tree, objID)
		}
	}

	return protocol.Value{Kind: protocol.KindRaw, Raw: msg.Body}, nil
}

// SetValue writes one object value via ACP2 set_property(pid=8).
func (p *Plugin) SetValue(ctx context.Context, req protocol.ValueRequest, val protocol.Value) (protocol.Value, error) {
	p.mu.Lock()
	s := p.session
	tree := p.trees[req.Slot]
	p.mu.Unlock()
	if s == nil {
		return protocol.Value{}, protocol.ErrNotConnected
	}

	objID, objType, numType, obj, err := p.resolveRequest(req, tree)
	if err != nil {
		return protocol.Value{}, err
	}

	// Build the value property for the set request.
	prop, err := encodeSetProperty(objType, numType, obj, val)
	if err != nil {
		return protocol.Value{}, err
	}

	msg, err := s.DoACP2(ctx, uint8(req.Slot), &ACP2Message{
		Type:       ACP2TypeRequest,
		Func:       ACP2FuncSetProperty,
		PID:        PIDValue,
		ObjID:      objID,
		Idx:        0,
		Properties: []Property{prop},
	})
	if err != nil {
		return protocol.Value{}, err
	}

	// Decode the confirmed value from the reply.
	for i := range msg.Properties {
		rp := &msg.Properties[i]
		if rp.PID == PIDValue {
			return decodePropertyValue(rp, objType, numType, tree, objID)
		}
	}

	return protocol.Value{Kind: protocol.KindRaw, Raw: msg.Body}, nil
}

// Subscribe registers a callback for ACP2 announces matching req.
func (p *Plugin) Subscribe(req protocol.ValueRequest, fn protocol.EventFunc) error {
	p.mu.Lock()
	s := p.session
	p.mu.Unlock()
	if s == nil {
		return protocol.ErrNotConnected
	}

	slot := req.Slot
	wantID := req.ID
	wantLabel := req.Label

	id := s.SubscribeAnnounces(func(annSlot uint8, msg *ACP2Message) {
		if slot >= 0 && int(annSlot) != slot {
			return
		}
		if wantID >= 0 && int(msg.ObjID) != wantID {
			return
		}

		ev := protocol.Event{
			Slot:      int(annSlot),
			ID:        int(msg.ObjID),
			Timestamp: time.Now(),
		}

		// Try to resolve label and decode value from cached tree.
		p.mu.Lock()
		tree := p.trees[int(annSlot)]
		p.mu.Unlock()

		// Find object index in tree.
		treeIdx := -1
		if tree != nil {
			for ti, tobj := range tree.Objects {
				if tobj.ID == int(msg.ObjID) {
					treeIdx = ti
					ev.Label = tobj.Label
					break
				}
			}
		}

		if wantLabel != "" && ev.Label != wantLabel {
			return
		}

		// Decode value from announce properties.
		for i := range msg.Properties {
			prop := &msg.Properties[i]
			if prop.PID == PIDValue || prop.PID == msg.PID {
				if treeIdx >= 0 {
					val, derr := decodePropertyValue(prop, tree.ObjTypes[treeIdx], tree.NumTypes[treeIdx], tree, msg.ObjID)
					if derr == nil {
						ev.Value = val
					}
				}
				// Fallback: use vtype from announce property to decode
				// when tree doesn't contain this object.
				if ev.Value.Kind == protocol.KindUnknown && prop.Data != nil {
					nt := NumberType(prop.VType)
					if nt > 0 {
						val, derr := decodePropertyValue(prop, ObjTypeNumber, nt, nil, msg.ObjID)
						if derr == nil {
							ev.Value = val
						}
					}
					if ev.Value.Kind == protocol.KindUnknown {
						ev.Value = protocol.Value{Kind: protocol.KindRaw, Raw: prop.Data}
					}
				}
				break
			}
		}

		fn(ev)
	})

	p.mu.Lock()
	p.subHandles[reqToSubKey(req)] = id
	p.mu.Unlock()
	return nil
}

// Unsubscribe removes a previously registered Subscribe.
func (p *Plugin) Unsubscribe(req protocol.ValueRequest) error {
	p.mu.Lock()
	s := p.session
	id, ok := p.subHandles[reqToSubKey(req)]
	if ok {
		delete(p.subHandles, reqToSubKey(req))
	}
	p.mu.Unlock()

	if ok && s != nil {
		s.UnsubscribeAnnounces(id)
	}
	return nil
}

// ---- Internal helpers ----

// resolveRequest translates a ValueRequest into an ACP2 obj-id, object type,
// number type, and (optionally) the cached protocol.Object.
func (p *Plugin) resolveRequest(req protocol.ValueRequest, tree *WalkedTree) (uint32, ACP2ObjType, NumberType, *protocol.Object, error) {
	if req.Label != "" {
		if tree == nil {
			return 0, 0, 0, nil, fmt.Errorf("%w: no walked tree for slot %d",
				protocol.ErrUnknownLabel, req.Slot)
		}
		idx := tree.Lookup(req.Label)
		if idx < 0 {
			return 0, 0, 0, nil, fmt.Errorf("%w: label %q not found on slot %d",
				protocol.ErrUnknownLabel, req.Label, req.Slot)
		}
		obj := &tree.Objects[idx]
		return uint32(obj.ID), tree.ObjTypes[idx], tree.NumTypes[idx], obj, nil
	}

	// Address by explicit ID.
	objID := uint32(req.ID)
	if tree != nil {
		for i, obj := range tree.Objects {
			if obj.ID == req.ID {
				return objID, tree.ObjTypes[i], tree.NumTypes[i], &tree.Objects[i], nil
			}
		}
	}
	// No tree or not found — return with unknown type. The caller may
	// still work with raw bytes.
	return objID, 0, 0, nil, nil
}

// decodePropertyValue decodes a value Property into a protocol.Value.
func decodePropertyValue(p *Property, objType ACP2ObjType, numType NumberType, tree *WalkedTree, objID uint32) (protocol.Value, error) {
	switch objType {
	case ObjTypeNumber:
		nt := NumberType(p.VType)
		if nt == 0 && numType != 0 {
			nt = numType
		}
		intV, uintV, floatV, err := DecodeNumericValue(nt, p.Data)
		if err != nil {
			return protocol.Value{Kind: protocol.KindRaw, Raw: p.Data}, nil
		}
		switch numberTypeToKind(nt) {
		case protocol.KindInt:
			return protocol.Value{Kind: protocol.KindInt, Int: intV, Raw: p.Data}, nil
		case protocol.KindUint:
			return protocol.Value{Kind: protocol.KindUint, Uint: uintV, Raw: p.Data}, nil
		case protocol.KindFloat:
			return protocol.Value{Kind: protocol.KindFloat, Float: floatV, Raw: p.Data}, nil
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
			// Try to resolve enum name from tree's options map.
			if tree != nil {
				for i, obj := range tree.Objects {
					if obj.ID == int(objID) {
						if tree.OptionsMaps[i] != nil {
							if label, ok := tree.OptionsMaps[i][fullIdx]; ok {
								ev.Str = label
							}
						}
						break
					}
				}
			}
			return ev, nil
		}

	case ObjTypeIPv4:
		if len(p.Data) >= 4 {
			return protocol.Value{
				Kind: protocol.KindIPAddr,
				IPAddr: [4]byte{
					p.Data[0], p.Data[1], p.Data[2], p.Data[3],
				},
				Raw: p.Data,
			}, nil
		}

	case ObjTypeString:
		return protocol.Value{
			Kind: protocol.KindString,
			Str:  PropertyString(p),
			Raw:  p.Data,
		}, nil
	}

	return protocol.Value{Kind: protocol.KindRaw, Raw: p.Data}, nil
}

// encodeSetProperty builds the value Property for a set_property request.
func encodeSetProperty(objType ACP2ObjType, numType NumberType, obj *protocol.Object, val protocol.Value) (Property, error) {
	// If raw bytes are provided, use them directly.
	if len(val.Raw) > 0 && val.Str == "" && val.Int == 0 && val.Float == 0 && val.Uint == 0 {
		return MakeValueProperty(PIDValue, numType, val.Raw), nil
	}

	switch objType {
	case ObjTypeNumber:
		data, err := EncodeNumericValue(numType, val.Int, val.Uint, val.Float)
		if err != nil {
			return Property{}, err
		}
		return MakeValueProperty(PIDValue, numType, data), nil

	case ObjTypeEnum, ObjTypePreset:
		// Accept enum index from Enum field or parse from Str.
		enumIdx := val.Enum
		if val.Str != "" && obj != nil {
			for i, item := range obj.EnumItems {
				if item == val.Str {
					enumIdx = uint8(i)
					break
				}
			}
		}
		data, err := EncodeNumericValue(NumTypeU32, 0, uint64(enumIdx), 0)
		if err != nil {
			return Property{}, err
		}
		return MakeValueProperty(PIDValue, NumTypePreset, data), nil

	case ObjTypeIPv4:
		data := make([]byte, 4)
		copy(data, val.IPAddr[:])
		return MakeValueProperty(PIDValue, NumTypeIPv4, data), nil

	case ObjTypeString:
		return MakeStringProperty(PIDValue, val.Str), nil

	default:
		if len(val.Raw) > 0 {
			return MakeValueProperty(PIDValue, numType, val.Raw), nil
		}
		return Property{}, fmt.Errorf("acp2: cannot encode value for object type %d", objType)
	}
}

// fetchObjectMeta issues a single get_object to learn the ACP2 object type,
// number type, and options map for an obj-id. Builds a minimal single-object
// WalkedTree so decodePropertyValue can resolve enum labels.
// Used when no walked tree is available (same pattern as ACP1's findObject fallback).
func (p *Plugin) fetchObjectMeta(ctx context.Context, s *Session, slot uint8, objID uint32) (ACP2ObjType, NumberType, *WalkedTree, error) {
	msg, err := s.DoACP2(ctx, slot, &ACP2Message{
		Type:  ACP2TypeRequest,
		Func:  ACP2FuncGetObject,
		ObjID: objID,
		Idx:   0,
	})
	if err != nil {
		return 0, 0, nil, fmt.Errorf("get_object(%d): %w", objID, err)
	}

	var objType ACP2ObjType
	var numType NumberType
	var optMap map[uint32]string
	var label string
	for i := range msg.Properties {
		prop := &msg.Properties[i]
		switch prop.PID {
		case PIDObjectType:
			if len(prop.Data) >= 4 {
				objType = ACP2ObjType(prop.Data[3])
			} else {
				objType = ACP2ObjType(prop.VType)
			}
		case PIDNumberType:
			if len(prop.Data) >= 4 {
				numType = NumberType(prop.Data[3])
			} else {
				numType = NumberType(prop.VType)
			}
		case PIDOptions:
			optMap = PropertyOptionsMap(prop)
		case PIDLabel:
			label = PropertyString(prop)
		}
	}

	// Build a minimal single-object tree for decodePropertyValue.
	miniTree := &WalkedTree{
		Slot: int(slot),
		Objects: []protocol.Object{
			{Slot: int(slot), ID: int(objID), Label: label},
		},
		ObjTypes:    []ACP2ObjType{objType},
		NumTypes:    []NumberType{numType},
		OptionsMaps: []map[uint32]string{optMap},
		Labels:      map[string]int{},
	}
	if label != "" {
		miniTree.Labels[label] = 0
	}

	return objType, numType, miniTree, nil
}
