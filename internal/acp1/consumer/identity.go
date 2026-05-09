package acp1

import (
	"context"
	"fmt"
	"strconv"

	"dhs/internal/acp1/codec"
	"dhs/internal/protocol"
)

// SeedTreeFromCachedObjects rebuilds an in-memory SlotTree from a
// previously-walked + persisted snapshot and inserts it into the
// per-slot tree cache. Mirrors the ACP2 contract so cmd/dhs/cmd_watch.go
// can hot-load DM for both protocols via the same (probe + load + seed)
// pattern.
//
// Used by the watch CLI on startup (#363): probe per-slot identity,
// LoadByIdentity the matching DM, hand the objects to this method,
// then Subscribe. Without this the cold-start window between Subscribe
// firing and the background walk completing produces label-less rows.
//
// Re-seeding the same slot is safe — the cache Put replaces the prior
// entry. Existing labels map is rebuilt from o.Group + o.Label.
func (p *Plugin) SeedTreeFromCachedObjects(slot int, objs []protocol.Object) {
	p.mu.Lock()
	if p.trees == nil {
		p.trees = newSlotTreeCache(defaultCacheConfig().MaxSize, defaultCacheConfig().TTL)
	}
	cache := p.trees
	p.mu.Unlock()

	tree := &SlotTree{
		Slot:    slot,
		Objects: make([]protocol.Object, 0, len(objs)),
		Labels:  make(map[string]map[string]int, 8),
	}
	for i, o := range objs {
		tree.Objects = append(tree.Objects, o)
		if o.Group == "" || o.Label == "" {
			continue
		}
		g, ok := tree.Labels[o.Group]
		if !ok {
			g = make(map[string]int, len(objs)/4+1)
			tree.Labels[o.Group] = g
		}
		g[o.Label] = i
	}
	cache.Put(slot, tree)
}

// IdentityProbe returns the per-card MasterView identity string
// "<Model>@<HwRev>" for the given slot. Wraps GetIdentity (which
// itself fetches the (Model, SwRev, HwRev) triple via three fixed-pid
// getObject calls on group=1) and joins the two fields the DM cache
// keys on. Same shape as ACP2's IdentityProbe so cmd/dhs/cmd_watch.go
// treats both protocols identically (#363 — per-card DM, agnostic of
// IP / slot index).
//
// Returns "" with err when Model is empty (NAK on Card Label probe);
// HwRev empty is tolerated and produces "<Model>@" — the file just
// has an empty hw component.
func (p *Plugin) IdentityProbe(ctx context.Context, slot int) (string, error) {
	id, err := p.GetIdentity(ctx, slot)
	if err != nil {
		return "", fmt.Errorf("acp1: identity probe slot=%d: %w", slot, err)
	}
	if id.Model == "" {
		return "", fmt.Errorf("acp1: identity probe slot=%d: empty Model", slot)
	}
	return fmt.Sprintf("%s@%s", id.Model, id.HwRev), nil
}

// GetIdentity probes the ACP1 identity group (group=1) for the (Model,
// SwRev, HwRev) triple. Per spec p.20 the identity group has 8 fixed
// IDs; we read three:
//
//	id 0  Card Label             -> Model    (hard-required)
//	id 3  Card Software Revision -> SwRev    (soft-required)
//	id 4  Card Hardware Revision -> HwRev    (metadata only)
//
// Three deterministic getObject calls. NAK on the Card Label probe
// fires the IdentityNAK compliance event and returns
// protocol.ErrIdentityUnresolved. NAKs on SwRev / HwRev leave those
// fields empty but do not fail — partial fingerprints are still useful
// for DM-library partial-match lookup.
//
// Vendor strings come straight off the wire. Persistence callers must
// run them through internal/identity sanitisers before disk write.
func (p *Plugin) GetIdentity(ctx context.Context, slot int) (protocol.CardIdentity, error) {
	p.mu.Lock()
	c := p.client
	profile := p.profile
	p.mu.Unlock()
	if c == nil {
		return protocol.CardIdentity{}, protocol.ErrNotConnected
	}

	model, err := identityField(ctx, c, slot, 0)
	if err != nil {
		if profile != nil {
			profile.Note(IdentityNAK)
		}
		return protocol.CardIdentity{}, fmt.Errorf("%w: card label: %v", protocol.ErrIdentityUnresolved, err)
	}

	// SwRev + HwRev are best-effort. Empty on miss; the DM-library
	// resolver decides whether to keep looking with a partial key.
	swrev, _ := identityField(ctx, c, slot, 3)
	hwrev, _ := identityField(ctx, c, slot, 4)

	return protocol.CardIdentity{
		Model: model,
		SwRev: swrev,
		HwRev: hwrev,
	}, nil
}

// identityField issues one getObject for (group=identity, id) and
// extracts the value as a string regardless of underlying ACP1 type.
// Strings carry through verbatim; numerics format as decimal; floats
// use the shortest-round-trip representation.
func identityField(ctx context.Context, c clientIface, slot int, id byte) (string, error) {
	req := &codec.Message{
		MType:    codec.MTypeRequest,
		MAddr:    byte(slot),
		MCode:    byte(codec.MethodGetObject),
		ObjGroup: codec.GroupIdentity,
		ObjID:    id,
	}
	reply, err := c.Do(ctx, req)
	if err != nil {
		return "", err
	}
	if reply.IsError() {
		return "", reply.ErrCode()
	}
	d, err := codec.DecodeObject(reply.Value)
	if err != nil {
		return "", fmt.Errorf("decode identity: %w", err)
	}
	return identityValueAsString(d), nil
}

// identityValueAsString renders the "value" field of a DecodedObject as
// a Go string regardless of the underlying ACP1 object type. Identity
// objects are conventionally Strings, but real cards have shipped
// Software Revision as Integer or Long; the helper covers all numeric
// types so a non-string identity field still produces a usable
// fingerprint component.
func identityValueAsString(d *codec.DecodedObject) string {
	if d == nil {
		return ""
	}
	switch d.Type {
	case codec.TypeString:
		return d.StrValue
	case codec.TypeInteger, codec.TypeLong:
		return strconv.FormatInt(d.IntVal, 10)
	case codec.TypeByte, codec.TypeEnum:
		return strconv.FormatUint(uint64(d.ByteVal), 10)
	case codec.TypeIPAddr:
		return strconv.FormatUint(d.UintVal, 10)
	case codec.TypeFloat:
		return strconv.FormatFloat(d.FloatVal, 'f', -1, 64)
	}
	return ""
}
