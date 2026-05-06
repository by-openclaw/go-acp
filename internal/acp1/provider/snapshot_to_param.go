package acp1

import (
	"errors"
	"fmt"
	"strconv"
	"strings"

	"dhs/internal/acp1/codec"
	"dhs/internal/export"
	"dhs/internal/export/canonical"
	"dhs/internal/protocol"
)

// snapshotToEntries flattens a single-slot DM-library snapshot into
// tree entries keyed by (slot, group, id). Used by tree.ReplaceSlot
// when an admin slot.load swaps the served card for a slot.
//
// SlotDump matching mirrors consumer/seed.go findSlot semantics: prefer
// the entry whose Slot field equals the target, fall back to the only
// entry for single-slot products. Frame/Root group objects are dropped
// — those belong to slot 0 (rack controller) and are never replaced
// during a card hot-plug.
//
// The snapshot's protocol.Object carries Kind (widened) plus an
// optional Meta["acp1_type"] hint that disambiguates Integer vs Long
// vs Byte. When the hint is absent we infer from Kind plus min/max
// range: KindUint -> Byte, KindInt with min/max within int16 -> Integer,
// otherwise Long. This matches consumer/seed.go's kindToACPType.
func snapshotToEntries(slot uint8, snap *export.Snapshot) ([]*entry, *slotCounts, error) {
	if snap == nil {
		return nil, nil, errors.New("acp1 provider: snapshotToEntries: nil snapshot")
	}
	sd := findSlotDump(snap, int(slot))
	if sd == nil {
		return nil, nil, fmt.Errorf("acp1 provider: snapshotToEntries: snapshot has no slot %d", slot)
	}

	counts := &slotCounts{}
	out := make([]*entry, 0, len(sd.Objects))
	for _, obj := range sd.Objects {
		grp, ok := groupByName[strings.ToLower(obj.Group)]
		if !ok {
			// Unknown groups (Path-only objects from ACP2/Ember+ snapshots
			// that found their way here) are skipped. ACP1 has 7 fixed
			// groups; anything else cannot be served.
			continue
		}
		// Frame / Root objects belong to slot 0; never accept them via
		// slot.load (the rack-controller frame-status array is owned by
		// the producer's tree.json starter and untouched by hot-plug).
		if grp == codec.GroupFrame || grp == codec.GroupRoot {
			continue
		}

		acpType := acpTypeFromObject(obj)
		param, err := objectToParameter(obj, acpType)
		if err != nil {
			return nil, nil, fmt.Errorf("acp1 provider: snapshotToEntries: %s/%s: %w", obj.Group, obj.Label, err)
		}
		e := &entry{
			key:     objectKey{slot: slot, group: grp, id: uint8(obj.ID)},
			param:   param,
			acpType: acpType,
			access:  obj.Access,
		}
		out = append(out, e)

		// Counts feed Root.numIdentity/Control/... so getObject(Root) on
		// this slot reports accurate child counts.
		switch grp {
		case codec.GroupIdentity:
			if uint8(obj.ID)+1 > counts.numIdentity {
				counts.numIdentity = uint8(obj.ID) + 1
			}
		case codec.GroupControl:
			if uint8(obj.ID)+1 > counts.numControl {
				counts.numControl = uint8(obj.ID) + 1
			}
		case codec.GroupStatus:
			if uint8(obj.ID)+1 > counts.numStatus {
				counts.numStatus = uint8(obj.ID) + 1
			}
		case codec.GroupAlarm:
			if uint8(obj.ID)+1 > counts.numAlarm {
				counts.numAlarm = uint8(obj.ID) + 1
			}
		case codec.GroupFile:
			if uint8(obj.ID)+1 > counts.numFile {
				counts.numFile = uint8(obj.ID) + 1
			}
		}
	}
	return out, counts, nil
}

// findSlotDump locates the SlotDump matching the target slot. Mirrors
// consumer/seed.go's findSlot. Single-entry snapshots match unconditionally
// — the convention for single-slot products is "store under whatever
// slot the walk happened on".
func findSlotDump(snap *export.Snapshot, slot int) *export.SlotDump {
	for i := range snap.Slots {
		if snap.Slots[i].Slot == slot {
			return &snap.Slots[i]
		}
	}
	if len(snap.Slots) == 1 {
		return &snap.Slots[0]
	}
	return nil
}

// acpTypeFromObject derives the ACP1 wire type. Honours
// Meta["acp1_type"] when the walker persisted it (consumer/seed.go
// convention); otherwise infers from Kind plus min/max range.
func acpTypeFromObject(o protocol.Object) codec.ObjectType {
	if o.Meta != nil {
		if raw, ok := o.Meta["acp1_type"]; ok {
			switch v := raw.(type) {
			case float64:
				return codec.ObjectType(v)
			case int:
				return codec.ObjectType(v)
			case int64:
				return codec.ObjectType(v)
			case codec.ObjectType:
				return v
			}
		}
	}
	switch o.Kind {
	case protocol.KindUint:
		return codec.TypeByte
	case protocol.KindInt:
		if intExceedsI16(o.Min) || intExceedsI16(o.Max) || intExceedsI16(o.Def) {
			return codec.TypeLong
		}
		return codec.TypeInteger
	case protocol.KindFloat:
		return codec.TypeFloat
	case protocol.KindEnum:
		return codec.TypeEnum
	case protocol.KindIPAddr:
		return codec.TypeIPAddr
	case protocol.KindString:
		return codec.TypeString
	case protocol.KindAlarm, protocol.KindBool:
		return codec.TypeAlarm
	case protocol.KindFrame:
		return codec.TypeFrame
	}
	return codec.TypeInteger
}

// intExceedsI16 reports whether v is set and outside int16 range. nil
// (constraint absent) returns false so unconstrained values default to
// the narrower Integer wire type — matches the consumer's seed default
// when meta is absent.
func intExceedsI16(v any) bool {
	n, ok := anyAsInt64(v)
	if !ok {
		return false
	}
	return n < -32768 || n > 32767
}

func anyAsInt64(v any) (int64, bool) {
	switch x := v.(type) {
	case nil:
		return 0, false
	case int:
		return int64(x), true
	case int8:
		return int64(x), true
	case int16:
		return int64(x), true
	case int32:
		return int64(x), true
	case int64:
		return x, true
	case uint:
		return int64(x), true
	case uint8:
		return int64(x), true
	case uint16:
		return int64(x), true
	case uint32:
		return int64(x), true
	case uint64:
		return int64(x), true
	case float32:
		return int64(x), true
	case float64:
		return int64(x), true
	case string:
		n, err := strconv.ParseInt(x, 10, 64)
		if err == nil {
			return n, true
		}
	}
	return 0, false
}

// objectToParameter synthesises a *canonical.Parameter the encoder
// + session can read identically to a tree.json-loaded one. The pair
// (Type, Format) MUST round-trip through deriveACPType to acpType so
// the provider's encoder picks the right wire width.
func objectToParameter(o protocol.Object, acpType codec.ObjectType) (*canonical.Parameter, error) {
	ident := o.Label
	if ident == "" {
		ident = "#" + strconv.Itoa(o.ID)
	}
	p := &canonical.Parameter{
		Header: canonical.Header{
			Number:     o.ID,
			Identifier: ident,
			Path:       strings.Join(o.Path, "."),
			OID:        o.OID,
			IsOnline:   true,
			Access:     accessByteToString(o.Access),
			Children:   canonical.EmptyChildren(),
		},
		Type: canonicalTypeFor(acpType),
	}

	if hint := formatHintFor(o, acpType); hint != "" {
		p.Format = &hint
	}

	p.Value = valueAny(o, acpType)
	if o.Min != nil {
		p.Minimum = o.Min
	}
	if o.Max != nil {
		p.Maximum = o.Max
	}
	if o.Step != nil {
		p.Step = o.Step
	}
	if o.Def != nil {
		p.Default = o.Def
	}
	if o.Unit != "" {
		u := o.Unit
		p.Unit = &u
	}

	if o.Kind == protocol.KindEnum && len(o.EnumItems) > 0 {
		entries := make([]canonical.EnumEntry, 0, len(o.EnumItems))
		for i, item := range o.EnumItems {
			entries = append(entries, canonical.EnumEntry{
				Key:   item,
				Value: int64(i),
			})
		}
		p.EnumMap = entries
		joined := strings.Join(o.EnumItems, "\n")
		p.Enumeration = &joined
	}

	if o.Kind == protocol.KindAlarm {
		// The provider's encoder reads alarm priority/tag from a Format
		// hint suffix and the on/off messages from Description.
		// Schemas don't carry priority/tag separately — preserve
		// AlarmOnMsg / AlarmOffMsg via Description so the encoder
		// re-emits them.
		parts := []string{}
		if o.AlarmOnMsg != "" {
			parts = append(parts, "on: "+o.AlarmOnMsg)
		}
		if o.AlarmOffMsg != "" {
			parts = append(parts, "off: "+o.AlarmOffMsg)
		}
		if len(parts) > 0 {
			desc := strings.Join(parts, " / ")
			p.Description = &desc
		}
	}

	return p, nil
}

// accessByteToString converts the ACP1 access byte (bit 0=R, bit 1=W,
// bit 2=setDef per spec p.20) to the canonical access string.
// setDef bit alone has no canonical match; we fold it into write.
func accessByteToString(a uint8) string {
	const (
		readBit  = 1 << 0
		writeBit = 1 << 1
	)
	switch a & (readBit | writeBit) {
	case readBit:
		return canonical.AccessRead
	case writeBit:
		return canonical.AccessWrite
	case readBit | writeBit:
		return canonical.AccessReadWrite
	}
	return canonical.AccessNone
}

// canonicalTypeFor maps an ACP1 wire type to the canonical type string.
// The encoder pairs this with formatHintFor's hint to recover the wire
// type via deriveACPType.
func canonicalTypeFor(t codec.ObjectType) string {
	switch t {
	case codec.TypeInteger, codec.TypeLong, codec.TypeByte:
		return canonical.ParamInteger
	case codec.TypeFloat:
		return canonical.ParamReal
	case codec.TypeIPAddr, codec.TypeString, codec.TypeFile:
		return canonical.ParamString
	case codec.TypeEnum:
		return canonical.ParamEnum
	case codec.TypeAlarm:
		return canonical.ParamBoolean
	case codec.TypeFrame:
		return canonical.ParamOctets
	}
	return canonical.ParamString
}

// formatHintFor builds the Format string the provider's deriveACPType
// reads to disambiguate types canonical alone cannot distinguish:
//
//	TypeLong   "int32"   (canonical integer + hint)
//	TypeByte   "uint8"   (canonical integer + hint)
//	TypeIPAddr "ipv4"    (canonical string + hint)
//	TypeFile   "file"    (canonical string + hint)
//	TypeAlarm  "alarm"   (canonical boolean + hint)
//	TypeFrame  "frame"   (canonical octets + hint)
//
// String objects carrying MaxLen append a "maxLen=N" attribute the
// encoder reads via stringMaxLen. Multiple parts are comma-joined, e.g.
// "ipv4,maxLen=16".
func formatHintFor(o protocol.Object, acpType codec.ObjectType) string {
	parts := []string{}
	switch acpType {
	case codec.TypeLong:
		parts = append(parts, "int32")
	case codec.TypeByte:
		parts = append(parts, "uint8")
	case codec.TypeIPAddr:
		parts = append(parts, "ipv4")
	case codec.TypeFile:
		parts = append(parts, "file")
	case codec.TypeAlarm:
		parts = append(parts, "alarm")
	case codec.TypeFrame:
		parts = append(parts, "frame")
	}
	if acpType == codec.TypeString && o.MaxLen > 0 {
		parts = append(parts, "maxLen="+strconv.Itoa(o.MaxLen))
	}
	return strings.Join(parts, ",")
}

// valueAny renders a protocol.Value as the Go scalar the encoder
// expects under the matching canonical Type. For DM-library snapshots
// values are usually zero (schemas are value-less by design — the
// consumer's SeedFromDM clears them), but we still pass through what's
// present so a non-empty schema retains its values verbatim.
func valueAny(o protocol.Object, acpType codec.ObjectType) any {
	v := o.Value
	switch acpType {
	case codec.TypeInteger, codec.TypeLong:
		switch v.Kind {
		case protocol.KindInt:
			return v.Int
		case protocol.KindUint:
			return int64(v.Uint)
		}
		return int64(0)
	case codec.TypeByte:
		switch v.Kind {
		case protocol.KindUint:
			return int64(v.Uint)
		case protocol.KindInt:
			return v.Int
		}
		return int64(0)
	case codec.TypeFloat:
		if v.Kind == protocol.KindFloat {
			return v.Float
		}
		return float64(0)
	case codec.TypeEnum:
		if v.Kind == protocol.KindEnum {
			return int64(v.Enum)
		}
		return int64(0)
	case codec.TypeIPAddr:
		if v.Kind == protocol.KindIPAddr {
			return strconv.Itoa(int(v.IPAddr[0])) + "." +
				strconv.Itoa(int(v.IPAddr[1])) + "." +
				strconv.Itoa(int(v.IPAddr[2])) + "." +
				strconv.Itoa(int(v.IPAddr[3]))
		}
		return "0.0.0.0"
	case codec.TypeString, codec.TypeFile:
		if v.Kind == protocol.KindString {
			return v.Str
		}
		return ""
	case codec.TypeAlarm:
		if v.Kind == protocol.KindBool || v.Kind == protocol.KindAlarm {
			return v.Bool
		}
		return false
	}
	return nil
}
