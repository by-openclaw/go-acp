package prism

import (
	"context"
	"errors"
	"fmt"

	dhsc "dhs/internal/consumer"
	"dhs/internal/prism/codec"
)

// A walk ends when the instrument stops answering, not when it refuses a
// call: this many transport failures in a row mean it is gone.
const deadInARow = 5

type planned struct {
	e    codec.Entry
	t    codec.Target
	skip string
}

// Walk reads every GET-able call of the catalogue, one scope at a time.
func (p *Plugin) Walk(ctx context.Context, slot int) ([]dhsc.Object, error) {
	if slot != 0 {
		return nil, fmt.Errorf("prism: slot %d: an instrument has slot 0 only", slot)
	}
	var plan []planned
	for _, e := range p.cat.Entries() {
		plan = append(plan, planFor(e)...)
	}
	return p.readPlan(ctx, plan)
}

// WalkUnder reads one call, every scope of it unless the path names one.
func (p *Plugin) WalkUnder(ctx context.Context, scope string) ([]dhsc.Object, error) {
	e, t, err := p.resolve(dhsc.ValueRequest{Path: scope})
	if err != nil {
		return nil, err
	}
	if t.Scope != "" || t.Input != "" {
		return p.readPlan(ctx, []planned{{e: e, t: t}})
	}
	return p.readPlan(ctx, planFor(e))
}

func planFor(e codec.Entry) []planned {
	switch {
	case !e.Mode.CanGet():
		return []planned{{e: e, t: codec.Target{Name: e.Name}, skip: "POST only"}}
	case e.NoRead:
		return []planned{{e: e, t: codec.Target{Name: e.Name}, skip: "not read: an action, or the answer is no value"}}
	}
	var out []planned
	for _, t := range e.Targets() {
		out = append(out, planned{e: e, t: t})
	}
	return out
}

// readPlan records a call that fails in Meta["error"] and leaves its
// Value empty: a failure is never a value, and never stops the walk.
func (p *Plugin) readPlan(ctx context.Context, plan []planned) ([]dhsc.Object, error) {
	if _, err := p.session(); err != nil {
		return nil, err
	}
	out := make([]dhsc.Object, 0, len(plan))
	var read, failed, skipped, dead int
	for i, it := range plan {
		o := object(i, it)
		if it.skip != "" {
			o.Meta["skipped"] = it.skip
			skipped++
			out = append(out, o)
			continue
		}
		if err := ctx.Err(); err != nil {
			return out, err
		}
		reply, err := p.read(ctx, it.e, it.t)
		if err == nil {
			dead = 0
			read++
			o.Value = toValue(reply)
			out = append(out, o)
			continue
		}
		failed++
		o.Meta["error"] = err.Error()
		out = append(out, o)
		p.deps.Logger.Warn("prism: walk: call failed", "call", it.t.String(), "err", err)
		var te *dhsc.TransportError
		if !errors.As(err, &te) {
			dead = 0
			continue
		}
		if dead++; dead >= deadInARow {
			return out, fmt.Errorf("prism: walk stopped after %d of %d calls: %d transport failures in a row: %w", i+1, len(plan), dead, err)
		}
	}
	p.deps.Logger.Info("prism: walk done", "read", read, "failed", failed, "skipped", skipped)
	return out, nil
}

func object(id int, it planned) dhsc.Object {
	o := dhsc.Object{
		ID:    id,
		Path:  []string{it.t.Name},
		Label: it.t.Name,
		Kind:  kindOf(it.e),
		Meta:  map[string]any{"call": it.t.String(), "mode": it.e.Mode.String(), "datatype": string(it.e.Type)},
	}
	if s := scopeWord(it.t); s != "" {
		o.Group, o.Label = it.t.Name, s
		o.Path = append(o.Path, s)
	}
	if it.e.Mode.CanGet() {
		o.Access |= 0x01
	}
	if it.e.Mode.CanPost() {
		o.Access |= 0x02
	}
	o.EnumItems = it.e.Options
	return o
}

func scopeWord(t codec.Target) string {
	if t.Input != "" {
		return "input=" + t.Input
	}
	return t.Scope
}

func kindOf(e codec.Entry) dhsc.ValueKind {
	switch {
	case len(e.Options) > 0:
		return dhsc.KindEnum
	case e.Type == codec.TypeInts:
		return dhsc.KindInt
	case e.Type == codec.TypeFloats || e.Type == codec.TypeDoubles:
		return dhsc.KindFloat
	}
	return dhsc.KindString
}
