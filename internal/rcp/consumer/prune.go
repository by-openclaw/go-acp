package consumer

import (
	"context"
	"fmt"
	"sort"

	"dhs/internal/rcp/codec"
)

// prune removes from one collection every IO the plan does not name.
//
// With Prune the plan is the whole RouteMaster: what it names is made,
// what it does not name is removed. keep answers whether the plan names
// an IO, by its mnemonic and whether it is virtual.
//
// An IO without a mnemonic cannot be named in a plan, so it is never
// pruned; it is reported instead, for an operator to name or remove.
//
// IOs go from the highest id down. Deleting an IO moves every IO after
// it down by one, so going down keeps every id still to be deleted
// valid — and the collection is read again after each delete all the
// same, because that is the server's behaviour today, not a promise.
func (e *ensurer) prune(ctx context.Context, name string, col codec.Collection, keep func(mnemonic string, virtual bool) bool) error {
	have, err := e.load(ctx, col)
	if err != nil {
		return err
	}
	for {
		var extra []codec.IO
		for _, io := range have {
			switch {
			case io.Mnemonic.Original == "":
				// reported once, below
			case !keep(io.Mnemonic.Original, io.Virtual):
				extra = append(extra, io)
			}
		}
		if len(extra) == 0 {
			break
		}
		sort.Slice(extra, func(i, j int) bool { return extra[i].ID > extra[j].ID })
		if e.opts.Check {
			for _, io := range extra {
				e.note(name+" "+io.Mnemonic.Original, "delete", fmt.Sprintf("id %d, not in the plan", io.ID))
			}
			break
		}
		io := extra[0]
		target := name + " " + io.Mnemonic.Original
		e.note(target, "delete", fmt.Sprintf("id %d, not in the plan", io.ID))
		if have, err = e.remove(ctx, col, io, target); err != nil {
			return err
		}
	}
	for _, io := range have {
		if io.Mnemonic.Original == "" {
			e.report.Unnamed = append(e.report.Unnamed, fmt.Sprintf("%s id %d", name, io.ID))
		}
	}
	return nil
}

// pruneAll prunes the four collections against the plan.
func (e *ensurer) pruneAll(ctx context.Context, plan Plan) error {
	names := func(lists ...[]PlanIO) map[string]bool {
		m := map[string]bool{}
		for _, l := range lists {
			for _, io := range l {
				m[io.Mnemonic] = true
			}
		}
		return m
	}
	virtuals := names(plan.Virtuals)
	local := func(plain map[string]bool) func(string, bool) bool {
		return func(mnemonic string, virtual bool) bool {
			if virtual {
				return virtuals[mnemonic]
			}
			return plain[mnemonic]
		}
	}
	fed := func(plain map[string]bool) func(string, bool) bool {
		return func(mnemonic string, _ bool) bool { return plain[mnemonic] }
	}
	// Sources first: deleting a virtual source takes its destination with
	// it, and the destinations are read after that.
	steps := []struct {
		name string
		col  codec.Collection
		keep func(string, bool) bool
	}{
		{"sources", codec.Sources, local(names(plan.Sources))},
		{"destinations", codec.Destinations, local(names(plan.Destinations))},
		{"federation-sources", codec.FederationSources, fed(names(plan.FederationSources))},
		{"federation-destinations", codec.FederationDestinations, fed(names(plan.FederationDestinations))},
	}
	for _, s := range steps {
		if err := e.prune(ctx, s.name, s.col, s.keep); err != nil {
			return err
		}
	}
	return nil
}

// PlanOf reads the RouteMaster and returns it as a plan: the starting
// point for a plan that is then edited, and — applied with Prune to the
// server it came from — one that changes nothing.
//
// IOs without a mnemonic are left out (a plan finds IOs by mnemonic) and
// returned in unnamed.
func (c *Client) PlanOf(ctx context.Context, r Router) (plan Plan, unnamed []string, err error) {
	e := &ensurer{c: c}
	read := func(col codec.Collection) ([]codec.IO, error) { return e.load(ctx, col) }

	toPlan := func(col codec.Collection, io codec.IO) PlanIO {
		p := PlanIO{Mnemonic: io.Mnemonic.Original}
		if len(io.Mnemonic.Alternates) > 0 {
			p.Alternates = io.Mnemonic.Alternates
		}
		if io.TieLineInhibit {
			yes := true
			p.TieLineInhibit = &yes
		}
		for name, l := range io.Levels {
			var lu codec.LevelUpdate
			if !l.TagsInherited {
				tags := append([]string{}, l.Tags...)
				lu.Tags = &tags
			}
			if d := l.Device; d != nil && (d.IO != 0 || d.SenderReceiver != "") {
				du := codec.DeviceUpdate{}
				if d.SenderReceiver != "" {
					du.SenderReceiver = &d.SenderReceiver
				} else {
					du.Name, du.TypeID, du.DeviceLevel, du.IO = &d.Name, &d.TypeID, &d.DeviceLevel, &d.IO
				}
				if d.SubChannel != "" {
					du.SubChannel = &d.SubChannel
				}
				if d.Inhibit {
					du.Inhibit = &d.Inhibit
				}
				lu.Device = &du
			}
			if lu.Tags == nil && lu.Device == nil {
				continue
			}
			if p.Levels == nil {
				p.Levels = map[string]codec.LevelUpdate{}
			}
			p.Levels[name] = lu
		}
		return p
	}

	srcs, err := read(codec.Sources)
	if err != nil {
		return plan, nil, err
	}
	dsts, err := read(codec.Destinations)
	if err != nil {
		return plan, nil, err
	}
	fsrcs, err := read(codec.FederationSources)
	if err != nil {
		return plan, nil, err
	}
	fdsts, err := read(codec.FederationDestinations)
	if err != nil {
		return plan, nil, err
	}

	add := func(name string, col codec.Collection, ios []codec.IO, into *[]PlanIO) {
		for _, io := range ios {
			switch {
			case io.Mnemonic.Original == "":
				unnamed = append(unnamed, fmt.Sprintf("%s id %d", name, io.ID))
			case io.Virtual && col == codec.Sources:
				plan.Virtuals = append(plan.Virtuals, PlanIO{Mnemonic: io.Mnemonic.Original})
			case io.Virtual:
				// the other half of a virtual: named once, by its source
			default:
				*into = append(*into, toPlan(col, io))
			}
		}
	}
	add("sources", codec.Sources, srcs, &plan.Sources)
	add("destinations", codec.Destinations, dsts, &plan.Destinations)
	add("federation-sources", codec.FederationSources, fsrcs, &plan.FederationSources)
	add("federation-destinations", codec.FederationDestinations, fdsts, &plan.FederationDestinations)

	// Crosspoints: one route per destination when every level it is
	// routed on carries the same source, one per level otherwise.
	table, err := c.Routes(ctx, r, 0)
	if err != nil {
		return plan, unnamed, err
	}
	srcName := map[int64]string{}
	for _, s := range srcs {
		srcName[s.ID] = s.Mnemonic.Original
	}
	for _, d := range dsts {
		if d.Mnemonic.Original == "" {
			continue
		}
		rd := table[codec.DestKey(d.ID)]
		byLevel := map[int]string{}
		for key, l := range rd.Levels {
			if name := srcName[l.Source.ID]; name != "" {
				id, err := codec.KeyID(key)
				if err != nil {
					return plan, unnamed, err
				}
				byLevel[int(id)] = name
			}
		}
		levels := make([]int, 0, len(byLevel))
		same := true
		for lvl, name := range byLevel {
			levels = append(levels, lvl)
			same = same && name == byLevel[levels[0]]
		}
		sort.Ints(levels)
		if len(levels) == 0 {
			continue
		}
		if same {
			plan.Routes = append(plan.Routes, PlanRoute{Destination: d.Mnemonic.Original, Source: byLevel[levels[0]]})
			continue
		}
		for _, lvl := range levels {
			plan.Routes = append(plan.Routes, PlanRoute{Destination: d.Mnemonic.Original, Source: byLevel[lvl], Level: lvl})
		}
	}
	if r != RouteMaster {
		plan.Router = &PlanRouter{Device: r.Device, Index: r.Index}
	}
	return plan, unnamed, nil
}
