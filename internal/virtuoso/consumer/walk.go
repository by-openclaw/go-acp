package virtuoso

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	dhsc "dhs/internal/consumer"
	"dhs/internal/virtuoso/codec"
)

// ErrSlotAbsent is a request addressed to a card that does not answer.
var ErrSlotAbsent = errors.New("virtuoso: no card answers in this slot")

// Object access bits.
const (
	accessRead      = 0x01
	accessReadWrite = 0x03
)

// Walk reads one module: its schema, then its data, one after the other.
// The result is kept for the session, so a later label lookup is not a
// second read of the device. It writes nothing.
func (p *Plugin) Walk(ctx context.Context, slot int) ([]dhsc.Object, error) {
	r, err := p.session()
	if err != nil {
		return nil, err
	}
	if !codec.ValidSlot(slot) {
		return nil, errSlotRange(slot)
	}
	p.walkMu.Lock()
	defer p.walkMu.Unlock()
	p.mu.Lock()
	tree, done := p.trees[slot]
	p.mu.Unlock()
	if done {
		return tree, nil
	}

	schema, err := p.schemaFor(ctx, r, slot)
	if err != nil {
		return nil, absent(slot, err)
	}
	env, err := r.do(ctx, "GET", codec.DataTarget(slot, nil))
	if err != nil {
		return nil, absent(slot, err)
	}
	flat, err := schema.Flatten(env.Data)
	if err != nil {
		return nil, err
	}
	objs := p.objects(slot, flat)

	p.mu.Lock()
	if p.trees != nil {
		p.trees[slot] = objs
	}
	p.mu.Unlock()
	return objs, nil
}

func absent(slot int, err error) error {
	if slot > 0 && noAnswer(err) {
		return fmt.Errorf("%w: slot %d (%s)", ErrSlotAbsent, slot, absentReason(err))
	}
	return err
}

// schemaFor fetches a module's schema once per session.
func (p *Plugin) schemaFor(ctx context.Context, r *rest, slot int) (*codec.Schema, error) {
	if s := p.loadedSchema(slot); s != nil {
		return s, nil
	}
	raw, err := r.schema(ctx, slot)
	if err != nil {
		return nil, err
	}
	s, err := codec.DecodeSchema(raw)
	if err != nil {
		return nil, err
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.schemas != nil {
		p.schemas[slot] = s
	}
	return s, nil
}

func (p *Plugin) loadedSchema(slot int) *codec.Schema {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.schemas[slot]
}

func (p *Plugin) objects(slot int, flat codec.Flat) []dhsc.Object {
	for range flat.Undeclared {
		p.ComplianceProfile().Note(DataNotInSchema)
	}
	for range flat.Unresolved {
		p.ComplianceProfile().Note(UnresolvedRef)
	}
	out := make([]dhsc.Object, 0, len(flat.Leaves))
	for i, l := range flat.Leaves {
		o := p.object(l)
		o.Slot, o.ID = slot, i
		out = append(out, o)
	}
	return out
}

func (p *Plugin) object(l codec.Leaf) dhsc.Object {
	last := len(l.Path) - 1
	o := dhsc.Object{
		Path:   l.Path,
		Group:  codec.JoinPath(l.Path[:last]),
		Label:  l.Path[last],
		Access: accessRead,
		Meta:   map[string]any{},
	}
	switch {
	case l.RPC:
		// Listed so the model is complete, and never invoked here.
		o.Kind, o.Access = dhsc.KindRaw, 0
		o.Meta["rpc"] = true
		if args := l.Node.Args(); len(args) > 0 {
			o.Meta["args"] = strings.Join(args, ",")
		}
		putText(o.Meta, "title", l.Node.Title)
		putText(o.Meta, "description", l.Node.Description)
		return o
	case l.Undeclared:
		o.Value = p.value(l.Value, nil)
		o.Kind = o.Value.Kind
		o.Meta["undeclared"] = true
		return o
	}
	fill(&o, l.Node)
	if l.Key {
		o.Access = accessRead
		o.Meta["key"] = true
	}
	if l.HasValue {
		o.Value = p.value(l.Value, l.Node)
	} else {
		o.Meta["noValue"] = true
	}
	return o
}

func fill(o *dhsc.Object, n *codec.Node) {
	o.Kind = kindOf(n)
	o.Unit = n.Unit
	if !n.ReadOnly {
		o.Access = accessReadWrite
	}
	o.Min, o.Max = number(n.Minimum), number(n.Maximum)
	if o.Kind == dhsc.KindFloat {
		o.Min, o.Max = float(o.Min), float(o.Max)
	}
	o.Def = defaultOf(n.Default)
	if isLabelled(n) {
		o.EnumItems = n.Labels
		vals := make([]string, len(n.Enum))
		for i, raw := range n.Enum {
			vals[i] = string(raw)
		}
		o.Meta["enumValues"] = strings.Join(vals, ",")
	}
	putText(o.Meta, "title", n.Title)
	putText(o.Meta, "description", n.Description)
	putText(o.Meta, "format", n.Format)
	putText(o.Meta, "pattern", n.Pattern)
	putText(o.Meta, "type", n.Type)
}

func putText(m map[string]any, k, v string) {
	if v != "" {
		m[k] = v
	}
}

// isLabelled: Value.Enum is one byte, so an enum over 256 entries stays a number.
func isLabelled(n *codec.Node) bool {
	return len(n.Enum) > 0 && len(n.Enum) == len(n.Labels) && len(n.Enum) <= 256
}

func kindOf(n *codec.Node) dhsc.ValueKind {
	switch {
	case isLabelled(n):
		return dhsc.KindEnum
	case n.Type == "boolean":
		return dhsc.KindBool
	case n.Type == "integer":
		if v, ok := number(n.Minimum).(int64); ok && v >= 0 {
			return dhsc.KindUint
		}
		return dhsc.KindInt
	case n.Type == "number":
		return dhsc.KindFloat
	}
	return dhsc.KindString
}

func number(n *json.Number) any {
	if n == nil {
		return nil
	}
	if i, err := n.Int64(); err == nil {
		return i
	}
	if f, err := n.Float64(); err == nil {
		return f
	}
	return nil
}

// float makes a limit of a number field a float64 even when the schema
// wrote it without a decimal point.
func float(v any) any {
	if i, ok := v.(int64); ok {
		return float64(i)
	}
	return v
}

func defaultOf(raw json.RawMessage) any {
	var v any
	if len(raw) == 0 || json.Unmarshal(raw, &v) != nil {
		return nil
	}
	return v
}
