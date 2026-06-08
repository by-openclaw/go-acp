package acp1

import (
	"fmt"
	"strconv"
	"strings"
	"sync"

	"dhs/internal/acp1/codec"
	"dhs/internal/export"
	"dhs/internal/export/canonical"
)

// groupName -> ObjGroup constant. Mirrors buildSlotNode in
// internal/acp1/consumer/canonicalize.go. Canonical uses lowercase group
// identifiers; ACP1 wire uses the numeric group code.
var groupByName = map[string]codec.ObjGroup{
	"root":     codec.GroupRoot,
	"identity": codec.GroupIdentity,
	"control":  codec.GroupControl,
	"status":   codec.GroupStatus,
	"alarm":    codec.GroupAlarm,
	"file":     codec.GroupFile,
	"frame":    codec.GroupFrame,
}

// objectKey uniquely identifies an AxonNet object on the wire.
// slot fits in a byte (0-31 per spec); group+id likewise.
type objectKey struct {
	slot  uint8
	group codec.ObjGroup
	id    uint8
}

// entry holds one mutable AxonNet object: the canonical Parameter source
// + the derived ACP1 wire type (int16 / int32 / ipv4 / etc) + access bits.
// session.go reads and writes this; all access guarded by tree.mu.
type entry struct {
	key     objectKey
	param   *canonical.Parameter // points into the loaded canonical.Export
	acpType codec.ObjectType
	access  uint8 // ACP1 access bits 0/1/2
}

// tree is the in-memory index a provider serves. A canonical.Export is
// flattened at startup into a map keyed by (slot, group, id). The root
// device + slot-N + group placeholder Nodes are observed for their
// structural info (slot count) but not themselves served as objects —
// ACP1 exposes only Parameters.
type tree struct {
	mu      sync.RWMutex
	entries map[objectKey]*entry
	// slots holds per-slot counters needed to answer Root.getObject
	// (numIdentity/Control/Status/Alarm/File). Computed at load time.
	slots map[uint8]*slotCounts
}

// broadcastsEnabled implements the ACP1 Broadcasts gate per spec p.20:
// slot 0 / control / id 4 is an enum that controls whether the device
// emits spontaneous announces. When the field is absent (older trees
// without the gate object), the default is permissive (true) so legacy
// behaviour is preserved.
//
// The check is deliberately tolerant of enum encoding: any non-zero
// value-byte counts as "On" since the spec convention is items=["Off","On"]
// (index 0 = Off, index 1 = On). If the served tree carries a richer
// enum map with a labelled "On" entry, the lookup honours that too.
func (t *tree) broadcastsEnabled() bool {
	t.mu.RLock()
	defer t.mu.RUnlock()
	e, ok := t.entries[objectKey{slot: 0, group: codec.GroupControl, id: 4}]
	if !ok {
		return true // gate object not defined — permissive default
	}
	if e.param == nil {
		return true
	}
	// Enum map case: the labelled "On" entry's index is authoritative.
	if len(e.param.EnumMap) > 0 {
		var idx uint8
		switch v := e.param.Value.(type) {
		case int64:
			idx = uint8(v)
		case uint64:
			idx = uint8(v)
		case int:
			idx = uint8(v)
		case float64:
			idx = uint8(v)
		}
		for _, item := range e.param.EnumMap {
			if int64(idx) == item.Value {
				return strings.EqualFold(item.Key, "On")
			}
		}
		return false
	}
	// Bare numeric value with no map: convention 0=Off, anything else=On.
	switch v := e.param.Value.(type) {
	case int64:
		return v != 0
	case uint64:
		return v != 0
	case int:
		return v != 0
	case float64:
		return v != 0
	}
	return true
}

// numSlots returns the highest slot number that carries any entry,
// plus one (giving the count). Used by AN2 GetDeviceInfo replies.
func (t *tree) numSlots() int {
	t.mu.RLock()
	defer t.mu.RUnlock()
	max := -1
	for slot := range t.slots {
		if int(slot) > max {
			max = int(slot)
		}
	}
	return max + 1
}

// slotPresent reports whether the tree has any entry on the given slot.
// Used by AN2 GetSlotInfo to drive the per-slot present_flag.
func (t *tree) slotPresent(slot uint8) bool {
	t.mu.RLock()
	defer t.mu.RUnlock()
	_, ok := t.slots[slot]
	return ok
}

type slotCounts struct {
	numIdentity uint8
	numControl  uint8
	numStatus   uint8
	numAlarm    uint8
	numFile     uint8
}

// hasCard reports whether the slot carries any addressable card object
// (any non-frame group). A slot synthesised only to host the rack
// frame-status object has all-zero counts and is not itself a card.
func (c *slotCounts) hasCard() bool {
	if c == nil {
		return false
	}
	return c.numIdentity > 0 || c.numControl > 0 || c.numStatus > 0 ||
		c.numAlarm > 0 || c.numFile > 0
}

// newTree flattens a canonical.Export into the (slot, group, id) index.
// Shape expected (mirror of acp1.Canonicalize output):
//
//	Root Node                              "device"  oid="1"
//	 └── Slot Node   number=N              "slot-N"  oid="1.N+1"
//	      └── Group Node identifier="identity|control|..."
//	           └── Parameter number=objID  (leaf)
//
// Unknown group names are skipped with a warning; unknown parameter
// types fall through to Integer (most common) with a warning.
func newTree(exp *canonical.Export) (*tree, error) {
	if exp == nil || exp.Root == nil {
		return nil, fmt.Errorf("acp1 provider: empty canonical export")
	}
	root, ok := exp.Root.(*canonical.Node)
	if !ok {
		return nil, fmt.Errorf("acp1 provider: root must be Node, got %s", exp.Root.Kind())
	}

	t := &tree{
		entries: map[objectKey]*entry{},
		slots:   map[uint8]*slotCounts{},
	}

	for _, slotEl := range root.Common().Children {
		slotNode, ok := slotEl.(*canonical.Node)
		if !ok {
			continue
		}
		// Canonical slot Node: number=0 → slot 0. OID "1.1" encodes the
		// same (1-based) but we trust the Number field.
		slot := uint8(slotNode.Number)
		counts := &slotCounts{}
		t.slots[slot] = counts

		for _, groupEl := range slotNode.Children {
			groupNode, ok := groupEl.(*canonical.Node)
			if !ok {
				continue
			}
			grp, ok := groupByName[strings.ToLower(groupNode.Identifier)]
			if !ok {
				continue
			}

			for _, paramEl := range groupNode.Children {
				p, ok := paramEl.(*canonical.Parameter)
				if !ok {
					continue
				}
				id := uint8(p.Number)
				acpType, err := deriveACPType(p)
				if err != nil {
					return nil, fmt.Errorf("acp1 provider: %s (%s): %w",
						p.OID, p.Identifier, err)
				}
				e := &entry{
					key:     objectKey{slot: slot, group: grp, id: id},
					param:   p,
					acpType: acpType,
					access:  deriveAccess(p.Access),
				}
				t.entries[e.key] = e

				switch grp {
				case codec.GroupIdentity:
					if id >= counts.numIdentity {
						counts.numIdentity = id + 1
					}
				case codec.GroupControl:
					if id >= counts.numControl {
						counts.numControl = id + 1
					}
				case codec.GroupStatus:
					if id >= counts.numStatus {
						counts.numStatus = id + 1
					}
				case codec.GroupAlarm:
					if id >= counts.numAlarm {
						counts.numAlarm = id + 1
					}
				case codec.GroupFile:
					if id >= counts.numFile {
						counts.numFile = id + 1
					}
				}
			}
		}
	}

	return t, nil
}

// lookup returns the entry at the given (slot, group, id) under RLock.
func (t *tree) lookup(k objectKey) (*entry, bool) {
	t.mu.RLock()
	defer t.mu.RUnlock()
	e, ok := t.entries[k]
	return e, ok
}

// ReplaceSlot drops every existing entry for the target slot and
// installs the contents of the supplied DM-library snapshot in their
// place. Used by SlotLoad to model a hot-plug card swap: the wire
// identity for the slot becomes whatever the snapshot declares, so
// subsequent identity-probe replies reflect the new card.
//
// Slot 0 is the rack-controller card and a legitimate slot.load
// target (Cerebrum / VSM treat slot 0 as the frame's own identity).
// We preserve the frame-status object on (slot=0, group=frame,
// id=0) across the replace so the addressable slot count of the
// frame — set at tree.json load time — survives a controller
// hot-swap. The converter already strips frame/root group objects
// from the schema so they never reach this method.
//
// Errors:
//
//	snap==nil        → snapshotToEntries surfaces a clear error
//	conversion fails → propagated as-is so the admin verb caller can
//	                   refuse the load with the underlying type / range
//	                   complaint
func (t *tree) ReplaceSlot(slot uint8, snap *export.Snapshot) error {
	entries, counts, err := snapshotToEntries(slot, snap)
	if err != nil {
		return err
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	for k := range t.entries {
		if k.slot != slot {
			continue
		}
		if slot == 0 && k.group == codec.GroupFrame {
			// Preserve frame-status — frame slot-count belongs to the
			// starter tree, not to the swapped-in controller card.
			continue
		}
		delete(t.entries, k)
	}
	for _, e := range entries {
		t.entries[e.key] = e
	}
	t.slots[slot] = counts
	return nil
}

// ClearSlot drops every entry for the target slot. Used by SlotUnload
// after the cascade extract drives the slot back to no_card, so the
// served wire identity reverts to "no card present" for subsequent
// reads. Idempotent on already-empty slots.
//
// On slot 0 the frame-status object is preserved (same reason as
// ReplaceSlot): the frame keeps reporting per-slot status even when
// the controller card is logically extracted.
func (t *tree) ClearSlot(slot uint8) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	for k := range t.entries {
		if k.slot != slot {
			continue
		}
		if slot == 0 && k.group == codec.GroupFrame {
			continue
		}
		delete(t.entries, k)
	}
	if slot == 0 {
		// Reset counts to zero but keep the slot 0 entry so the
		// frame-status remains addressable.
		t.slots[0] = &slotCounts{}
	} else {
		delete(t.slots, slot)
	}
	return nil
}

// deriveACPType maps a canonical.Parameter to the concrete ACP1 wire
// type. ACP1 distinguishes int16/int32/uint8 and string/ipaddr/file at
// the wire level; canonical collapses those. We use Parameter.Format as
// the explicit ACP1 type hint and reject ambiguity with an error so the
// provider never emits a type it cannot represent.
//
//	integer  + no hint            -> TypeInteger (int16, Axon default)
//	integer  + "int32" | "long"   -> TypeLong
//	integer  + "uint8" | "byte"   -> TypeByte
//	real                          -> TypeFloat
//	enum                          -> TypeEnum
//	string   + no hint            -> TypeString
//	string   + "ipv4" | "ipaddr"  -> TypeIPAddr
//	string   + "file"             -> TypeFile
//	boolean  + "alarm"            -> TypeAlarm  (spec p.25)
//	octets   + "frame"            -> TypeFrame  (spec p.24; rack-controller only)
//	boolean without "alarm" hint  -> ERROR (ACP1 has no Boolean — use
//	                                 Enum with "Off,On" items for plain
//	                                 booleans, Alarm for alarm objects)
func deriveACPType(p *canonical.Parameter) (codec.ObjectType, error) {
	// Parameter.Format is a free-form comma-separated hint carrying
	// ACP1 type information AND other attributes (e.g. "maxLen=N" on
	// strings, "priority=N,tag=M" on alarms). Split and scan: first
	// matching type hint wins; non-type hints (maxLen/priority/tag) are
	// absorbed by later encoders.
	parts := formatParts(p.Format)
	typeHint, known := pickTypeHint(parts)
	if !known {
		return 0, fmt.Errorf("unrecognised format type-hint %q (valid bare tokens: int16|int32|uint8|ipv4|file|alarm|frame)", typeHint)
	}

	switch p.Type {
	case canonical.ParamReal:
		return codec.TypeFloat, nil
	case canonical.ParamEnum:
		return codec.TypeEnum, nil
	case canonical.ParamInteger:
		switch typeHint {
		case "", "int16":
			return codec.TypeInteger, nil
		case "int32", "long":
			return codec.TypeLong, nil
		case "uint8", "byte":
			return codec.TypeByte, nil
		}
		return 0, fmt.Errorf("integer: unknown type hint %q (want int16|int32|uint8)", typeHint)
	case canonical.ParamString:
		switch typeHint {
		case "", "string":
			return codec.TypeString, nil
		case "ipv4", "ipaddr":
			return codec.TypeIPAddr, nil
		case "file":
			return codec.TypeFile, nil
		}
		return 0, fmt.Errorf("string: unknown type hint %q (want ipv4|file or omit)", typeHint)
	case canonical.ParamBoolean:
		if typeHint != "alarm" {
			return 0, fmt.Errorf(
				"boolean has no ACP1 mapping — use enum with Off,On for plain booleans, " +
					"or set format=\"alarm\" with description=\"on: … / off: …\" for spec-p.25 Alarm objects")
		}
		return codec.TypeAlarm, nil
	case canonical.ParamOctets:
		if typeHint == "frame" {
			return codec.TypeFrame, nil
		}
		return 0, fmt.Errorf("octets: type hint %q unsupported in ACP1 (only \"frame\" is defined)", typeHint)
	}
	return 0, fmt.Errorf("unsupported canonical type %q for ACP1 provider", p.Type)
}

// formatParts splits a canonical Format string into lower-cased
// comma-trimmed tokens. An empty / nil Format yields an empty slice.
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

// pickTypeHint scans Format parts for the one that identifies an ACP1
// wire type. Tokens containing "=" are key-value attributes
// (maxLen=16, priority=2, tag=17) and are ignored here. A bare token
// that doesn't match a known type is treated as a typo by the caller.
//
// Returns:
//
//	(hint, true)   — a known type hint was found (or no bare tokens
//	                 present, which means "use the canonical default")
//	(badToken, false) — an unknown bare token was seen; caller should
//	                 surface this as a reject.
func pickTypeHint(parts []string) (string, bool) {
	known := map[string]struct{}{
		"int16": {}, "int32": {}, "long": {},
		"uint8": {}, "byte": {},
		"ipv4": {}, "ipaddr": {},
		"file":   {},
		"alarm":  {},
		"frame":  {},
		"string": {},
	}
	for _, p := range parts {
		if strings.ContainsRune(p, '=') {
			continue // key=value attribute, not a type hint
		}
		if _, ok := known[p]; ok {
			return p, true
		}
		return p, false
	}
	return "", true
}

// deriveAccess maps the canonical access string to the ACP1 access
// byte (spec p.20 bit 0 = read, bit 1 = write, bit 2 = setDef).
//
// Canonical's four-level string cannot distinguish setDef from write,
// but real Axon firmware (the "constructor" reference the provider
// mirrors) grants setDef on every writable object with a default. The
// provider follows that convention — write implies setDef — so tree.json
// round-tripped from a real device behaves the same way under the
// provider as it did under the device.
func deriveAccess(a string) uint8 {
	switch a {
	case canonical.AccessRead:
		return codec.AccessRead
	case canonical.AccessWrite:
		return codec.AccessWrite | codec.AccessSetDef
	case canonical.AccessReadWrite:
		return codec.AccessRead | codec.AccessWrite | codec.AccessSetDef
	}
	return 0
}

// parsePath converts a dotted canonical OID "1.N+1.group.id" back into a
// concrete (slot, group, id) key. Used by Provider.SetValue which
// receives arbitrary user-supplied paths.
//
// Accepted formats (both work):
//
//	"1.2.1.3"        four-component numeric OID
//	"slot-1.identity.label"  three-component identifier path (not supported yet)
//
// Returns a helpful error if the path is ill-formed or the components
// are out of range.
func parsePath(path string) (objectKey, error) {
	parts := strings.Split(path, ".")
	if len(parts) != 4 {
		return objectKey{}, fmt.Errorf("acp1 path: expected 4 components <1.slot+1.group.id>, got %q", path)
	}
	if parts[0] != "1" {
		return objectKey{}, fmt.Errorf("acp1 path: first component must be 1, got %q", parts[0])
	}
	slot1based, err := strconv.Atoi(parts[1])
	if err != nil || slot1based < 1 {
		return objectKey{}, fmt.Errorf("acp1 path: invalid slot component %q", parts[1])
	}
	grpNum, err := strconv.Atoi(parts[2])
	if err != nil || grpNum < 0 || grpNum > 6 {
		return objectKey{}, fmt.Errorf("acp1 path: invalid group component %q", parts[2])
	}
	id, err := strconv.Atoi(parts[3])
	if err != nil || id < 0 || id > 255 {
		return objectKey{}, fmt.Errorf("acp1 path: invalid id component %q", parts[3])
	}
	return objectKey{
		slot:  uint8(slot1based - 1),
		group: codec.ObjGroup(grpNum),
		id:    uint8(id),
	}, nil
}
