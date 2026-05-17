package acp1

import (
	"errors"
	"fmt"

	"dhs/internal/acp1/codec"
	"dhs/internal/export"
	"dhs/internal/consumer"
)

// SeedFromDM pre-populates the in-memory slot-tree cache from a DM-library
// snapshot so the announce decoder has type info on the first frame —
// no blocking walk required at Connect time.
//
// The seed is value-less by design (schemas don't carry live values).
// First successful announce / GetValue / Walk on a seeded slot
// overwrites the type entries with wire-confirmed shapes.
//
// Hot-swap is the caller's responsibility (watch hot-plug enrichment
// re-seeds when GetIdentity returns a different fingerprint).
//
// ACPTypes is populated via best-effort mapping from consumer.ValueKind.
// Kind is a widened type (KindInt covers Integer + Long), so the wire
// width may be wrong for set/get round-trip until the real walk fires.
// This is acceptable: seeded values arrive only via announces (decoded
// from the ACP1 type byte) and the first GetValue refreshes the entry.
func (p *Plugin) SeedFromDM(slot int, snap *export.Snapshot) error {
	if snap == nil {
		return errors.New("acp1: SeedFromDM: nil snapshot")
	}
	if len(snap.Slots) == 0 {
		return errors.New("acp1: SeedFromDM: snapshot has no slots")
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.trees == nil {
		return consumer.ErrNotConnected
	}

	sd := findSlot(snap, slot)
	if sd == nil {
		return fmt.Errorf("acp1: SeedFromDM: snapshot has no slot %d", slot)
	}

	objs := make([]consumer.Object, len(sd.Objects))
	acpTypes := make([]codec.ObjectType, len(sd.Objects))
	labels := map[string]map[string]int{}
	for i, o := range sd.Objects {
		objs[i] = o
		objs[i].Value = consumer.Value{} // schemas are value-less
		acpTypes[i] = kindToACPType(o.Kind, o.Meta)
		if o.Group != "" && o.Label != "" {
			if labels[o.Group] == nil {
				labels[o.Group] = map[string]int{}
			}
			labels[o.Group][o.Label] = i
		}
	}

	tree := &SlotTree{
		Slot:     slot,
		Objects:  objs,
		ACPTypes: acpTypes,
		Labels:   labels,
	}
	p.trees.Put(slot, tree)
	return nil
}

// findSlot returns the SlotDump matching slot or nil. By convention a
// single-slot snapshot may have Slot=0 (legacy) or Slot=N (per-slot
// dump); we accept exact match first, single-entry fallback second.
func findSlot(snap *export.Snapshot, slot int) *export.SlotDump {
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

// kindToACPType maps the widened ValueKind back to a concrete ACP1
// ObjectType. Lossy: KindInt could come from Integer (i16) or Long
// (i32); we default to Integer and document the limitation.
//
// Plugins MAY persist the original ACPType under Object.Meta["acp1_type"]
// during walk; when present, the meta override wins.
func kindToACPType(k consumer.ValueKind, meta map[string]any) codec.ObjectType {
	if meta != nil {
		if raw, ok := meta["acp1_type"]; ok {
			switch v := raw.(type) {
			case float64: // JSON numbers decode as float64
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
	switch k {
	case consumer.KindBool, consumer.KindEnum:
		return codec.TypeEnum
	case consumer.KindInt:
		return codec.TypeInteger
	case consumer.KindUint:
		return codec.TypeByte
	case consumer.KindFloat:
		return codec.TypeFloat
	case consumer.KindString:
		return codec.TypeString
	case consumer.KindIPAddr:
		return codec.TypeIPAddr
	case consumer.KindAlarm:
		return codec.TypeAlarm
	case consumer.KindFrame:
		return codec.TypeFrame
	}
	return codec.ObjectType(0)
}
