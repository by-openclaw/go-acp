package acp2

import (
	"context"
	"encoding/binary"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"sync"
	"time"

	"dhs/internal/protocol"
	"dhs/internal/protocol/compliance"
	"dhs/internal/transport"
	"dhs/internal/acp2/codec"
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
		DefaultPort: codec.DefaultPort,
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

	// trees caches the walked object tree per slot (LRU + TTL).
	trees *walkedTreeCache

	// Announce subscription tracking.
	subHandles map[subKey]int // subKey → session announce subscription ID

	// Optional traffic capture.
	recorder *transport.Recorder

	// Optional walk progress callback.
	walkProgress WalkProgressFunc

	// profile aggregates wire-tolerance events observed during this
	// session. See compliance_events.go for the catalog. Nil until
	// Connect fires; callers read via ComplianceProfile().
	profile *compliance.Profile
}

// SeedTreeFromCachedObjects rebuilds an in-memory WalkedTree from a
// previously-walked + persisted snapshot and inserts it into the
// per-slot tree cache. After this returns, the next Subscribe-fed
// announce can decode types and labels immediately — no need to wait
// for a fresh Walk to populate the cache.
//
// Used by the watch CLI on startup (cmd_watch.go #323): load the disk
// snapshot, hand it to the plugin, then Subscribe. Without this, the
// first set of announces render as `raw(N)` until the background walk
// finishes (sometimes 30+ s on a 50 000-object Neuron tree).
//
// The snapshot's objects MUST carry acp2.objType + acp2.numType in
// Meta (set by walker.go on a normal walk). Objects without those
// keys are kept in the tree for label resolution but their value
// decoding falls back to KindRaw — same as today.
func (p *Plugin) SeedTreeFromCachedObjects(slot int, objs []protocol.Object) {
	p.mu.Lock()
	if p.trees == nil {
		p.trees = newWalkedTreeCache(32, 10*time.Minute)
	}
	cache := p.trees
	p.mu.Unlock()

	tree := &WalkedTree{
		Slot:        slot,
		Objects:     make([]protocol.Object, 0, len(objs)),
		ObjTypes:    make([]codec.ACP2ObjType, 0, len(objs)),
		NumTypes:    make([]codec.NumberType, 0, len(objs)),
		OptionsMaps: make([]map[uint32]string, 0, len(objs)),
		Labels:      make(map[string]int, len(objs)),
	}
	for i, o := range objs {
		// Fill ObjType / NumType from Meta. Default to ObjTypeNode /
		// NumTypeS32 when absent — Nodes don't decode values, and
		// the Subscribe path falls back to vtype-from-wire when
		// ObjType is unset.
		var ot codec.ACP2ObjType
		var nt codec.NumberType
		if o.Meta != nil {
			if v, ok := o.Meta["acp2.objType"]; ok {
				ot = codec.ACP2ObjType(metaToUint8(v))
			}
			if v, ok := o.Meta["acp2.numType"]; ok {
				nt = codec.NumberType(metaToUint8(v))
			}
		}
		var optMap map[uint32]string
		if o.Meta != nil {
			if v, ok := o.Meta["acp2.optionsMap"]; ok {
				optMap = decodeStringlyOptionsMap(v)
			}
		}
		tree.Objects = append(tree.Objects, o)
		tree.ObjTypes = append(tree.ObjTypes, ot)
		tree.NumTypes = append(tree.NumTypes, nt)
		tree.OptionsMaps = append(tree.OptionsMaps, optMap)
		if o.Label != "" {
			tree.Labels[o.Label] = i
		}
	}
	cache.Put(slot, tree)
}

// metaToUint8 coerces a Meta value into uint8. Tolerates the
// JSON-decoded float64 that arrives when a snapshot round-trips
// through encoding/json plus the in-memory uint8 the live walker
// stashes. Returns 0 on unrecognised types.
func metaToUint8(v any) uint8 {
	switch x := v.(type) {
	case uint8:
		return x
	case int:
		return uint8(x)
	case int64:
		return uint8(x)
	case float64:
		return uint8(x)
	}
	return 0
}

// decodeStringlyOptionsMap turns a JSON-decoded `map[string]any`
// (where keys are stringified u32 indices and values are labels)
// back into the in-memory `map[uint32]string` shape WalkedTree
// expects. Tolerates the live-walker shape (already correct) too.
func decodeStringlyOptionsMap(v any) map[uint32]string {
	switch x := v.(type) {
	case map[uint32]string:
		return x
	case map[string]string:
		out := make(map[uint32]string, len(x))
		for k, lbl := range x {
			var idx uint32
			_, _ = fmt.Sscanf(k, "%d", &idx)
			out[idx] = lbl
		}
		return out
	case map[string]any:
		out := make(map[uint32]string, len(x))
		for k, lbl := range x {
			var idx uint32
			_, _ = fmt.Sscanf(k, "%d", &idx)
			if s, ok := lbl.(string); ok {
				out[idx] = s
			}
		}
		return out
	}
	return nil
}

// ComplianceProfile returns the session-scoped compliance profile.
// Returns nil if Connect hasn't been called yet. Safe to call from
// any goroutine.
func (p *Plugin) ComplianceProfile() *compliance.Profile {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.profile
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
	p.trees = newWalkedTreeCache(32, 10*time.Minute)
	p.subHandles = make(map[subKey]int)
	p.profile = &compliance.Profile{}
	s.SetProfile(p.profile)
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
	if p.trees != nil {
		p.trees.Clear()
	}
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
	tree, _ := p.trees.Get(slot)
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
	if tree, ok := p.trees.Get(slot); ok {
		return tree.Objects, nil
	}

	p.mu.Lock()
	w := p.walker
	p.mu.Unlock()
	if w == nil {
		return nil, protocol.ErrNotConnected
	}

	tree, err := w.Walk(ctx, slot)
	if err != nil {
		return nil, err
	}

	p.trees.Put(slot, tree)
	return tree.Objects, nil
}

// IdentityProbe walks slot 0 (sub-second on any Axon device) and
// derives a stable device identity = "<CardName>@<HardwareVersion>"
// by looking up obj labels in the ROOT_NODE_V2.BOARD subtree.
// Returns "" with err when either label is missing or the slot 0
// walk fails — caller should fall back to a fresh full walk.
//
// Why labels and not obj-ids: ACP2 obj-ids vary per product, but
// the labels "Card Name" and "Hardware Version" are constant across
// the Axon catalogue (verified against real Neuron 10.41.40.4 +
// tree-fresh.json).
func (p *Plugin) IdentityProbe(ctx context.Context) (string, error) {
	objs, err := p.Walk(ctx, 0)
	if err != nil {
		return "", fmt.Errorf("acp2: identity probe walk(slot=0): %w", err)
	}
	cardName := ""
	hwVersion := ""
	for _, o := range objs {
		switch o.Label {
		case "Card Name":
			if o.Value.Kind == protocol.KindString {
				cardName = o.Value.Str
			}
		case "Hardware Version":
			if o.Value.Kind == protocol.KindString {
				hwVersion = o.Value.Str
			}
		}
	}
	if cardName == "" || hwVersion == "" {
		return "", fmt.Errorf("acp2: identity probe — missing Card Name (%q) or Hardware Version (%q) on slot 0",
			cardName, hwVersion)
	}
	return fmt.Sprintf("%s@%s", cardName, hwVersion), nil
}

// GetValue reads one object value via ACP2 get_property(pid=8).
func (p *Plugin) GetValue(ctx context.Context, req protocol.ValueRequest) (protocol.Value, error) {
	p.mu.Lock()
	s := p.session
	p.mu.Unlock()
	if s == nil {
		return protocol.Value{}, protocol.ErrNotConnected
	}
	tree, _ := p.trees.Get(req.Slot)

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

	// req.PID overrides pid=8 (value); req.Idx overrides idx=0 (active).
	// Zero defaults preserve historical behaviour.
	targetPID := uint8(codec.PIDValue)
	if req.PID > 0 {
		targetPID = uint8(req.PID)
	}
	targetIdx := uint32(req.Idx)

	msg, err := s.DoACP2(ctx, uint8(req.Slot), &codec.ACP2Message{
		Type:  codec.ACP2TypeRequest,
		Func:  codec.ACP2FuncGetProperty,
		PID:   targetPID,
		ObjID: objID,
		Idx:   targetIdx,
	})
	if err != nil {
		return protocol.Value{}, err
	}

	// Decode the targeted property from the reply. Fall back to the raw
	// body when the reply does not carry targetPID (e.g. probes that
	// asked for an unusual pid).
	for i := range msg.Properties {
		prop := &msg.Properties[i]
		if prop.PID == targetPID {
			if targetPID == codec.PIDValue {
				return decodePropertyValue(prop, objType, numType, tree, objID)
			}
			return protocol.Value{Kind: protocol.KindRaw, Raw: prop.Data}, nil
		}
	}

	return protocol.Value{Kind: protocol.KindRaw, Raw: msg.Body}, nil
}

// SetValue writes one object value via ACP2 set_property(pid=8).
func (p *Plugin) SetValue(ctx context.Context, req protocol.ValueRequest, val protocol.Value) (protocol.Value, error) {
	p.mu.Lock()
	s := p.session
	p.mu.Unlock()
	if s == nil {
		return protocol.Value{}, protocol.ErrNotConnected
	}
	tree, _ := p.trees.Get(req.Slot)

	objID, objType, numType, obj, err := p.resolveRequest(req, tree)
	if err != nil {
		return protocol.Value{}, err
	}

	// If no tree → type unknown. Fetch object metadata via get_object
	// (same pattern as GetValue).
	if objType == 0 && tree == nil {
		var miniTree *WalkedTree
		objType, numType, miniTree, err = p.fetchObjectMeta(ctx, s, uint8(req.Slot), objID)
		if err != nil {
			return protocol.Value{}, err
		}
		tree = miniTree
		if len(miniTree.Objects) > 0 {
			obj = &miniTree.Objects[0]
		}
	}

	// Build reverse enum map (label → wire index) for enum resolution.
	var reverseEnum map[string]uint32
	if tree != nil && obj != nil {
		for i, tobj := range tree.Objects {
			if tobj.ID == obj.ID && tree.OptionsMaps[i] != nil {
				reverseEnum = make(map[string]uint32, len(tree.OptionsMaps[i]))
				for wireIdx, label := range tree.OptionsMaps[i] {
					reverseEnum[label] = wireIdx
				}
				break
			}
		}
	}

	// Build the value property for the set request.
	prop, err := encodeSetProperty(objType, numType, obj, val, reverseEnum)
	if err != nil {
		return protocol.Value{}, err
	}

	msg, err := s.DoACP2(ctx, uint8(req.Slot), &codec.ACP2Message{
		Type:       codec.ACP2TypeRequest,
		Func:       codec.ACP2FuncSetProperty,
		PID:        codec.PIDValue,
		ObjID:      objID,
		Idx:        0,
		Properties: []codec.Property{prop},
	})
	if err != nil {
		return protocol.Value{}, err
	}

	// Decode the confirmed value from the reply.
	for i := range msg.Properties {
		rp := &msg.Properties[i]
		if rp.PID == codec.PIDValue {
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

	id := s.SubscribeAnnounces(func(annSlot uint8, msg *codec.ACP2Message) {
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
		tree, _ := p.trees.Get(int(annSlot))

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
			if prop.PID == codec.PIDValue || prop.PID == msg.PID {
				if treeIdx >= 0 {
					val, derr := decodePropertyValue(prop, tree.ObjTypes[treeIdx], tree.NumTypes[treeIdx], tree, msg.ObjID)
					if derr == nil {
						ev.Value = val
					}
				}
				// Fallback when the tree didn't have this obj (slow walk
				// finishes after announce arrives, or watch started cold).
				// The property header's vtype byte (spec §5.2.2) tells us
				// the wire shape; map it to the corresponding ACP2 ObjType
				// so Enum / IPv4 / String announces decode typed instead
				// of falling through to KindRaw.
				if ev.Value.Kind == protocol.KindUnknown && prop.Data != nil {
					nt := codec.NumberType(prop.VType)
					ot := objTypeFromVType(nt)
					val, derr := decodePropertyValue(prop, ot, nt, nil, msg.ObjID)
					if derr == nil {
						ev.Value = val
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
func (p *Plugin) resolveRequest(req protocol.ValueRequest, tree *WalkedTree) (uint32, codec.ACP2ObjType, codec.NumberType, *protocol.Object, error) {
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

// objTypeFromVType maps an ACP2 §5.2.2 number-type byte (the data byte
// of pid 8/9 value properties) to the matching object type. Used by
// the announce fallback path when the cached tree doesn't have the
// announced obj — the vtype byte alone tells us how to decode the
// value body.
//
//	| vtype | ACP2 NumberType | ACP2 ObjType   |
//	|-------|-----------------|----------------|
//	| 0..8  | s8..float       | ObjTypeNumber  |
//	| 9     | preset/enum     | ObjTypeEnum    |
//	| 10    | ipv4            | ObjTypeIPv4    |
//	| 11    | string          | ObjTypeString  |
func objTypeFromVType(nt codec.NumberType) codec.ACP2ObjType {
	switch nt {
	case codec.NumTypePreset:
		return codec.ObjTypeEnum
	case codec.NumTypeIPv4:
		return codec.ObjTypeIPv4
	case codec.NumTypeString:
		return codec.ObjTypeString
	default:
		return codec.ObjTypeNumber
	}
}

// decodePropertyValue decodes a value Property into a protocol.Value.
func decodePropertyValue(p *codec.Property, objType codec.ACP2ObjType, numType codec.NumberType, tree *WalkedTree, objID uint32) (protocol.Value, error) {
	switch objType {
	case codec.ObjTypeNumber:
		nt := codec.NumberType(p.VType)
		if nt == 0 && numType != 0 {
			nt = numType
		}
		intV, uintV, floatV, err := codec.DecodeNumericValue(nt, p.Data)
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

	case codec.ObjTypeEnum, codec.ObjTypePreset:
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

	case codec.ObjTypeIPv4:
		if len(p.Data) >= 4 {
			return protocol.Value{
				Kind: protocol.KindIPAddr,
				IPAddr: [4]byte{
					p.Data[0], p.Data[1], p.Data[2], p.Data[3],
				},
				Raw: p.Data,
			}, nil
		}

	case codec.ObjTypeString:
		return protocol.Value{
			Kind: protocol.KindString,
			Str:  codec.PropertyString(p),
			Raw:  p.Data,
		}, nil
	}

	return protocol.Value{Kind: protocol.KindRaw, Raw: p.Data}, nil
}

// encodeSetProperty builds the value Property for a set_property request.
func encodeSetProperty(objType codec.ACP2ObjType, numType codec.NumberType, obj *protocol.Object, val protocol.Value, reverseEnum map[string]uint32) (codec.Property, error) {
	// If raw bytes are provided, use them directly.
	if len(val.Raw) > 0 && val.Str == "" && val.Int == 0 && val.Float == 0 && val.Uint == 0 {
		return codec.MakeValueProperty(codec.PIDValue, numType, val.Raw), nil
	}

	switch objType {
	case codec.ObjTypeNumber:
		data, err := codec.EncodeNumericValue(numType, val.Int, val.Uint, val.Float)
		if err != nil {
			return codec.Property{}, err
		}
		return codec.MakeValueProperty(codec.PIDValue, numType, data), nil

	case codec.ObjTypeEnum, codec.ObjTypePreset:
		// Accept enum index from Enum field or resolve label via reverse map.
		enumIdx := uint32(val.Enum)
		if val.Str != "" && reverseEnum != nil {
			if wireIdx, ok := reverseEnum[val.Str]; ok {
				enumIdx = wireIdx
			}
		}
		data, err := codec.EncodeNumericValue(codec.NumTypeU32, 0, uint64(enumIdx), 0)
		if err != nil {
			return codec.Property{}, err
		}
		return codec.MakeValueProperty(codec.PIDValue, codec.NumTypePreset, data), nil

	case codec.ObjTypeIPv4:
		ip := val.IPAddr
		// Parse from string if IPAddr is zero (CLI passes --value "x.x.x.x" as Str).
		if ip == [4]byte{} && val.Str != "" {
			parts := strings.Split(val.Str, ".")
			if len(parts) == 4 {
				for i, p := range parts {
					v, err := strconv.Atoi(p)
					if err == nil && v >= 0 && v <= 255 {
						ip[i] = byte(v)
					}
				}
			}
		}
		data := make([]byte, 4)
		copy(data, ip[:])
		return codec.MakeValueProperty(codec.PIDValue, codec.NumTypeIPv4, data), nil

	case codec.ObjTypeString:
		return codec.MakeStringProperty(codec.PIDValue, val.Str), nil

	default:
		if len(val.Raw) > 0 {
			return codec.MakeValueProperty(codec.PIDValue, numType, val.Raw), nil
		}
		return codec.Property{}, fmt.Errorf("acp2: cannot encode value for object type %d", objType)
	}
}

// fetchObjectMeta issues a single get_object to learn the ACP2 object type,
// number type, and options map for an obj-id. Builds a minimal single-object
// WalkedTree so decodePropertyValue can resolve enum labels.
// Used when no walked tree is available (same pattern as ACP1's findObject fallback).
func (p *Plugin) fetchObjectMeta(ctx context.Context, s *Session, slot uint8, objID uint32) (codec.ACP2ObjType, codec.NumberType, *WalkedTree, error) {
	msg, err := s.DoACP2(ctx, slot, &codec.ACP2Message{
		Type:  codec.ACP2TypeRequest,
		Func:  codec.ACP2FuncGetObject,
		ObjID: objID,
		Idx:   0,
	})
	if err != nil {
		return 0, 0, nil, fmt.Errorf("get_object(%d): %w", objID, err)
	}

	var objType codec.ACP2ObjType
	var numType codec.NumberType
	var optMap map[uint32]string
	var label string
	for i := range msg.Properties {
		prop := &msg.Properties[i]
		switch prop.PID {
		case codec.PIDObjectType:
			if len(prop.Data) >= 4 {
				objType = codec.ACP2ObjType(prop.Data[3])
			} else {
				objType = codec.ACP2ObjType(prop.VType)
			}
		case codec.PIDNumberType:
			if len(prop.Data) >= 4 {
				numType = codec.NumberType(prop.Data[3])
			} else {
				numType = codec.NumberType(prop.VType)
			}
		case codec.PIDOptions:
			optMap = codec.PropertyOptionsMap(prop)
		case codec.PIDLabel:
			label = codec.PropertyString(prop)
		}
	}

	// Build a minimal single-object tree for decodePropertyValue.
	miniTree := &WalkedTree{
		Slot: int(slot),
		Objects: []protocol.Object{
			{Slot: int(slot), ID: int(objID), Label: label},
		},
		ObjTypes:    []codec.ACP2ObjType{objType},
		NumTypes:    []codec.NumberType{numType},
		OptionsMaps: []map[uint32]string{optMap},
		Labels:      map[string]int{},
	}
	if label != "" {
		miniTree.Labels[label] = 0
	}

	return objType, numType, miniTree, nil
}
