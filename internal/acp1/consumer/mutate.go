package acp1

import (
	"context"

	"dhs/internal/acp1/codec"
	"dhs/internal/consumer"
)

// SetIncValue, SetDecValue, and SetDefValue implement the three no-argument
// ACP1 mutating methods (spec §"Methods" p.28: setIncValue=2, setDecValue=3,
// setDefValue=4). Each sends the method for the resolved object and returns
// the device-confirmed new value. Unlike SetValue they carry no value bytes —
// the device computes value±step or the declared default and echoes the
// result. Supported only by the types in the spec method-support matrix
// (numeric types for inc/dec; numeric + enum + ipaddr for setDef); the device
// returns an "illegal method for type" error otherwise, which surfaces as a
// non-nil error here.
//
// Like GetValue, the confirmed reply is decoded against the cached walked
// tree, or a single getObject meta-fetch on a cold cache, falling back to a
// group-based decode if even that fails.

// SetIncValue increments the object's value by its declared step (clamped to
// max by the device).
func (p *Plugin) SetIncValue(ctx context.Context, req consumer.ValueRequest) (consumer.Value, error) {
	return p.mutateNoArg(ctx, req, codec.MethodSetIncValue)
}

// SetDecValue decrements the object's value by its declared step (clamped to
// min by the device).
func (p *Plugin) SetDecValue(ctx context.Context, req consumer.ValueRequest) (consumer.Value, error) {
	return p.mutateNoArg(ctx, req, codec.MethodSetDecValue)
}

// SetDefValue resets the object's value to its declared default.
func (p *Plugin) SetDefValue(ctx context.Context, req consumer.ValueRequest) (consumer.Value, error) {
	return p.mutateNoArg(ctx, req, codec.MethodSetDefValue)
}

func (p *Plugin) mutateNoArg(ctx context.Context, req consumer.ValueRequest, method codec.Method) (consumer.Value, error) {
	p.mu.Lock()
	c := p.client
	cache := p.trees
	p.mu.Unlock()
	if c == nil {
		return consumer.Value{}, consumer.ErrNotConnected
	}
	var tree *SlotTree
	if cache != nil {
		tree, _ = cache.Get(req.Slot)
	}

	group, id, err := resolve(req, tree)
	if err != nil {
		return consumer.Value{}, err
	}

	m := &codec.Message{
		MType:    codec.MTypeRequest,
		MAddr:    byte(req.Slot),
		MCode:    byte(method),
		ObjGroup: group,
		ObjID:    id,
	}
	reply, err := c.Do(ctx, m)
	if err != nil {
		return consumer.Value{}, err
	}
	if reply.IsError() {
		return consumer.Value{}, reply.ErrCode()
	}

	obj, acpType, found := findObject(tree, group, id)
	if !found {
		mObj, mType, mErr := p.fetchObjectMeta(ctx, req.Slot, group, id)
		if mErr != nil {
			return decodeByGroup(group, reply.Value)
		}
		obj, acpType = mObj, mType
	}
	return DecodeValueBytes(obj, acpType, reply.Value)
}
