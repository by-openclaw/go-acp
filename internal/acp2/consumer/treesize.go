package acp2

import (
	"unsafe"

	"dhs/internal/acp2/codec"
	"dhs/internal/consumer"
)

// Memory attribution for walked trees.
//
// A walked slot is the single largest thing this plugin holds: a Neuron
// BRIDGE slot is ~50k objects, and every object keeps its decoded value
// INCLUDING the raw wire bytes. Until now none of that reached the
// connector's memory counters, so `dhs metrics show` reported mem=0B
// while the process was actually holding hundreds of megabytes — the
// one number an operator sizing an LXC or a k3s pod needs most.
//
// These helpers estimate the retained bytes so Walk can publish it via
// metrics.Connector.SetTreeBytes. It is an estimate, deliberately: Go
// gives no cheap exact retained-size, and an estimate that tracks the
// real curve is worth far more than a zero. It counts each struct's own
// size plus the variable-length data hanging off it (string bodies,
// slice backing arrays, map entries), which is where the mass is.
//
// Deliberately NOT counted: Go's allocator size classes and map bucket
// slack, so the estimate reads slightly under true RSS. process_resident
// _memory_bytes from the Prometheus process collector remains the
// authority on total footprint; this number attributes the share of it
// that is tree.
const (
	// Header sizes on 64-bit: string = ptr+len, slice = ptr+len+cap.
	stringHeaderBytes = 16
	sliceHeaderBytes  = 24
	// mapEntryOverhead approximates a map bucket slot: key + value slot
	// plus tophash and pointer overhead, averaged.
	mapEntryOverhead = 48
)

// valueVariableBytes returns the heap a Value hangs off itself. The
// fixed part is already counted by the enclosing struct's Sizeof.
func valueVariableBytes(v *consumer.Value) uint64 {
	n := uint64(len(v.Raw))
	n += uint64(len(v.Str))
	var ss consumer.SlotStatus
	n += uint64(len(v.SlotStatus)) * uint64(unsafe.Sizeof(ss))
	return n
}

// objectBytes returns the estimated retained size of one object,
// including its own struct and everything it points at.
func objectBytes(o *consumer.Object) uint64 {
	n := uint64(unsafe.Sizeof(*o))
	n += uint64(len(o.Group)) + uint64(len(o.OID))
	n += uint64(len(o.Label)) + uint64(len(o.Unit))
	n += uint64(len(o.AlarmOnMsg)) + uint64(len(o.AlarmOffMsg))
	for _, p := range o.Path {
		n += stringHeaderBytes + uint64(len(p))
	}
	for _, e := range o.EnumItems {
		n += stringHeaderBytes + uint64(len(e))
	}
	// Meta values are `any`; their payloads are unknowable without
	// reflection, so count the entry slots only.
	n += uint64(len(o.Meta)) * mapEntryOverhead
	n += valueVariableBytes(&o.Value)
	return n
}

// estimateTreeBytes returns the estimated retained size of a walked
// tree: the objects plus the three parallel slices and the label index.
func estimateTreeBytes(t *WalkedTree) uint64 {
	if t == nil {
		return 0
	}
	n := uint64(unsafe.Sizeof(*t))
	for i := range t.Objects {
		n += objectBytes(&t.Objects[i])
	}
	n += uint64(len(t.ObjTypes)) * uint64(unsafe.Sizeof(codec.ACP2ObjType(0)))
	n += uint64(len(t.NumTypes)) * uint64(unsafe.Sizeof(codec.NumberType(0)))
	for _, m := range t.OptionsMaps {
		n += sliceHeaderBytes
		for _, label := range m {
			n += mapEntryOverhead + uint64(len(label))
		}
	}
	for k := range t.Labels {
		n += mapEntryOverhead + stringHeaderBytes + uint64(len(k))
	}
	return n
}

// Bytes returns the estimated total retained size of every tree the
// cache is holding. This is the number that answers "how much RAM is
// this consumer using for device state", which is what sizing a
// container asks.
func (c *walkedTreeCache) Bytes() uint64 {
	if c == nil {
		return 0
	}
	c.mu.Lock()
	defer c.mu.Unlock()

	var n uint64
	for _, el := range c.entries {
		// entries only ever hold *treeCacheEntry (see Put), so this
		// assertion cannot fail — a guarded form would be dead code
		// against the package's 100% coverage floor.
		n += estimateTreeBytes(el.Value.(*treeCacheEntry).tree)
	}
	return n
}
