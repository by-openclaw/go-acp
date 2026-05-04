package acp1

import (
	"context"
	"fmt"
	"strconv"

	"dhs/internal/acp1/codec"
	"dhs/internal/protocol"
)

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
