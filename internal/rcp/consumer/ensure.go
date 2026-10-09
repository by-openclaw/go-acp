package consumer

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"dhs/internal/rcp/codec"
)

// PlanIO is one source or destination the plan wants. It is found by
// its mnemonic: the server allocates ids, so a plan cannot name one.
// Only the fields a plan states are compared and written.
type PlanIO struct {
	Mnemonic       string                       `json:"mnemonic"`
	Alternates     map[string]string            `json:"alternateMnemonics,omitempty"`
	TieLineInhibit *bool                        `json:"tieLineInhibit,omitempty"`
	Levels         map[string]codec.LevelUpdate `json:"levels,omitempty"`
}

// PlanRoute is one crosspoint the plan wants, by mnemonic. Level 0 is
// every level the destination exists on.
type PlanRoute struct {
	Destination string `json:"destination"`
	Source      string `json:"source"`
	Level       int    `json:"level,omitempty"`
}

// PlanRouter names the router the routes are made on.
type PlanRouter struct {
	Device string `json:"device"`
	Index  int    `json:"index"`
}

// Plan is the RouteMaster content to converge on. A virtual is one
// entry: the server makes its source and its destination together.
type Plan struct {
	Router                 *PlanRouter `json:"router,omitempty"`
	Sources                []PlanIO    `json:"sources,omitempty"`
	Destinations           []PlanIO    `json:"destinations,omitempty"`
	Virtuals               []PlanIO    `json:"virtuals,omitempty"`
	FederationSources      []PlanIO    `json:"federation-sources,omitempty"`
	FederationDestinations []PlanIO    `json:"federation-destinations,omitempty"`
	Routes                 []PlanRoute `json:"routes,omitempty"`
}

// EnsureOptions selects how a plan is applied.
type EnsureOptions struct {
	// Check reports what would change and writes nothing.
	Check bool
	// Absent removes everything the plan names instead of making it.
	Absent bool
	// FederationOptional reports a federation entry the server accepted
	// and did not create as pending instead of failing — a server with
	// no federation answers a federation create that way.
	FederationOptional bool
	// Prune makes the plan the whole RouteMaster: every named IO the
	// plan does not name is removed. Without it, an IO taken out of a
	// plan stays on the server.
	Prune bool
	// Settle bounds the wait for an accepted write to show. 0 → 5s.
	Settle time.Duration
}

// Change is one difference found, and what was done about it.
type Change struct {
	Target string // "sources DHS-SRC-0001"
	Action string // create | update | delete | take
	Detail string
}

// Report is the outcome of an Ensure.
type Report struct {
	Changes []Change
	// Pending are federation entries the server accepted and did not
	// create (FederationOptional).
	Pending []string
	// Unnamed are IOs without a mnemonic that a prune left alone: a plan
	// finds IOs by mnemonic and cannot name them.
	Unnamed []string
}

// Validate refuses a plan that cannot be applied as written.
func (p Plan) Validate() error {
	seen := map[string]bool{}
	check := func(col string, ios []PlanIO, c codec.Collection, virtual bool) error {
		for _, io := range ios {
			if strings.TrimSpace(io.Mnemonic) == "" {
				return fmt.Errorf("rcp plan: a %s entry has no mnemonic", col)
			}
			key := col + "\x00" + io.Mnemonic
			if seen[key] {
				return fmt.Errorf("rcp plan: %s %q is named twice", col, io.Mnemonic)
			}
			seen[key] = true
			if virtual && (len(io.Levels) > 0 || io.TieLineInhibit != nil) {
				return fmt.Errorf("rcp plan: virtual %q carries levels or tie line settings; a virtual has neither", io.Mnemonic)
			}
			u := codec.Update{Alternates: io.Alternates, TieLineInhibit: io.TieLineInhibit, Levels: io.Levels, Mnemonic: &io.Mnemonic}
			if err := u.Validate(c, false); err != nil {
				return fmt.Errorf("rcp plan: %s %q: %w", col, io.Mnemonic, err)
			}
		}
		return nil
	}
	for _, s := range []struct {
		name    string
		ios     []PlanIO
		col     codec.Collection
		virtual bool
	}{
		{"sources", p.Sources, codec.Sources, false},
		{"destinations", p.Destinations, codec.Destinations, false},
		{"virtuals", p.Virtuals, codec.Sources, true},
		{"federation-sources", p.FederationSources, codec.FederationSources, false},
		{"federation-destinations", p.FederationDestinations, codec.FederationDestinations, false},
	} {
		if err := check(s.name, s.ios, s.col, s.virtual); err != nil {
			return err
		}
	}
	for _, r := range p.Routes {
		if r.Destination == "" || r.Source == "" || r.Level < 0 {
			return fmt.Errorf("rcp plan: a route needs a destination, a source and a level of 0 or more")
		}
	}
	return nil
}

// ensurer carries one Ensure run.
type ensurer struct {
	c      *Client
	opts   EnsureOptions
	report Report
}

// Ensure converges the RouteMaster on the plan and reports every
// difference it found. Each write is read back; a write the server
// accepted and did not apply is an error, not a success.
func (c *Client) Ensure(ctx context.Context, plan Plan, opts EnsureOptions) (Report, error) {
	if err := plan.Validate(); err != nil {
		return Report{}, err
	}
	if opts.Prune && opts.Absent {
		return Report{}, errors.New("rcp: prune removes what a plan does not name, absent removes what it names — not both")
	}
	if opts.Settle <= 0 {
		opts.Settle = 5 * time.Second
	}
	e := &ensurer{c: c, opts: opts}

	steps := []struct {
		name    string
		col     codec.Collection
		ios     []PlanIO
		virtual bool
	}{
		{"sources", codec.Sources, plan.Sources, false},
		{"destinations", codec.Destinations, plan.Destinations, false},
		{"virtuals", codec.Sources, plan.Virtuals, true},
		{"federation-sources", codec.FederationSources, plan.FederationSources, false},
		{"federation-destinations", codec.FederationDestinations, plan.FederationDestinations, false},
	}
	for _, s := range steps {
		if len(s.ios) == 0 {
			continue
		}
		if err := e.collection(ctx, s.name, s.col, s.ios, s.virtual); err != nil {
			return e.report, err
		}
	}
	if opts.Prune {
		if err := e.pruneAll(ctx, plan); err != nil {
			return e.report, err
		}
	}
	if !opts.Absent && len(plan.Routes) > 0 {
		r := RouteMaster
		if plan.Router != nil && plan.Router.Device != "" {
			r = Router{Device: plan.Router.Device, Index: plan.Router.Index}
		}
		if err := e.routes(ctx, r, plan.Routes); err != nil {
			return e.report, err
		}
	}
	return e.report, nil
}

func (e *ensurer) note(target, action, detail string) {
	e.report.Changes = append(e.report.Changes, Change{Target: target, Action: action, Detail: detail})
}

// load reads every IO of a collection.
//
// One GET per IO: the API has no bulk read of the collection. At the
// size of a real plant that is the cost of an ensure, and the /batch
// endpoint is where it comes down when it is needed.
func (e *ensurer) load(ctx context.Context, col codec.Collection) ([]codec.IO, error) {
	ids, err := e.c.List(ctx, col)
	if err != nil {
		return nil, err
	}
	out := make([]codec.IO, 0, len(ids))
	for _, id := range ids {
		io, err := e.c.Get(ctx, col, id)
		if err != nil {
			return nil, err
		}
		out = append(out, io)
	}
	return out, nil
}

// find returns the IO carrying a mnemonic, among the virtual or the
// non-virtual ones.
func find(ios []codec.IO, mnemonic string, virtual bool) (codec.IO, bool) {
	for _, io := range ios {
		if io.Mnemonic.Original == mnemonic && io.Virtual == virtual {
			return io, true
		}
	}
	return codec.IO{}, false
}

func (e *ensurer) collection(ctx context.Context, name string, col codec.Collection, want []PlanIO, virtual bool) error {
	have, err := e.load(ctx, col)
	if err != nil {
		return err
	}
	for _, w := range want {
		target := name + " " + w.Mnemonic
		cur, found := find(have, w.Mnemonic, virtual)

		if e.opts.Absent {
			if !found {
				continue
			}
			e.note(target, "delete", fmt.Sprintf("id %d", cur.ID))
			if e.opts.Check {
				continue
			}
			if have, err = e.remove(ctx, col, cur, target); err != nil {
				return err
			}
			continue
		}

		if !found {
			e.note(target, "create", "")
			if e.opts.Check {
				continue
			}
			id, err := e.create(ctx, col, w, virtual)
			if errors.Is(err, errNotCreated) && col.IsFederation() && e.opts.FederationOptional {
				e.report.Pending = append(e.report.Pending, target)
				e.report.Changes = e.report.Changes[:len(e.report.Changes)-1]
				continue
			}
			if err != nil {
				return fmt.Errorf("%s: %w", target, err)
			}
			if cur, err = e.c.Get(ctx, col, id); err != nil {
				return err
			}
			// A new IO carries its mnemonic and nothing else: the rest of
			// what the plan states is applied as an update, below.
			if _, diffs := diffIO(col, w, cur); len(diffs) == 0 {
				continue
			}
			if err := e.update(ctx, col, w, cur, target); err != nil {
				return err
			}
			continue
		}

		_, diffs := diffIO(col, w, cur)
		if len(diffs) == 0 {
			continue
		}
		e.note(target, "update", strings.Join(diffs, ", "))
		if e.opts.Check {
			continue
		}
		if err := e.update(ctx, col, w, cur, target); err != nil {
			return err
		}
	}
	return nil
}

// errNotCreated: the server accepted a create and no new id appeared.
var errNotCreated = errors.New("the server accepted the create and created nothing")

// settle calls probe until it reports done or the settle time is spent.
func (e *ensurer) settle(ctx context.Context, probe func() (bool, error)) (bool, error) {
	deadline := time.Now().Add(e.opts.Settle)
	for {
		done, err := probe()
		if err != nil || done {
			return done, err
		}
		if time.Now().After(deadline) {
			return false, nil
		}
		select {
		case <-ctx.Done():
			return false, ctx.Err()
		case <-time.After(200 * time.Millisecond):
		}
	}
}

// create makes one IO and returns the id the server gave it.
func (e *ensurer) create(ctx context.Context, col codec.Collection, w PlanIO, virtual bool) (string, error) {
	before, err := e.c.List(ctx, col)
	if err != nil {
		return "", err
	}
	had := make(map[string]bool, len(before))
	for _, id := range before {
		had[id] = true
	}
	one, yes := 1, true
	body := codec.Update{Count: &one, Mnemonic: &w.Mnemonic}
	if virtual {
		// A virtual is created bare, then named.
		body = codec.Update{Count: &one, Virtual: &yes}
	}
	if err := e.c.Create(ctx, col, body); err != nil {
		return "", err
	}
	var id string
	ok, err := e.settle(ctx, func() (bool, error) {
		after, err := e.c.List(ctx, col)
		if err != nil {
			return false, err
		}
		for _, a := range after {
			if !had[a] {
				id = a
				return true, nil
			}
		}
		return false, nil
	})
	if err != nil {
		return "", err
	}
	if !ok {
		return "", errNotCreated
	}
	if virtual {
		if err := e.c.Update(ctx, col, id, codec.Update{Mnemonic: &w.Mnemonic}); err != nil {
			return "", err
		}
	}
	named, err := e.settle(ctx, func() (bool, error) {
		io, err := e.c.Get(ctx, col, id)
		return err == nil && io.Mnemonic.Original == w.Mnemonic, err
	})
	if err != nil {
		return "", err
	}
	if !named {
		return "", fmt.Errorf("id %s was created but does not carry the mnemonic", id)
	}
	return id, nil
}

// update writes the differences and reads the IO back until none is left.
func (e *ensurer) update(ctx context.Context, col codec.Collection, w PlanIO, cur codec.IO, target string) error {
	u, _ := diffIO(col, w, cur)
	id := fmt.Sprint(cur.ID)
	if err := e.c.Update(ctx, col, id, u); err != nil {
		return fmt.Errorf("%s: %w", target, err)
	}
	var left []string
	ok, err := e.settle(ctx, func() (bool, error) {
		now, err := e.c.Get(ctx, col, id)
		if err != nil {
			return false, err
		}
		_, left = diffIO(col, w, now)
		return len(left) == 0, nil
	})
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("%s: the server accepted the update and did not apply: %s", target, strings.Join(left, ", "))
	}
	return nil
}

// remove deletes one IO, waits for it to be gone, and returns the
// collection as it then is.
//
// Gone is judged by mnemonic, and the collection is read again, because
// Cerebrum 2.5.3 renumbers: deleting an IO moves every IO after it down
// by one, so the deleted id is at once the id of its successor, and
// every id read before the delete is stale.
func (e *ensurer) remove(ctx context.Context, col codec.Collection, io codec.IO, target string) ([]codec.IO, error) {
	if err := e.c.Delete(ctx, col, fmt.Sprint(io.ID)); err != nil {
		return nil, fmt.Errorf("%s: %w", target, err)
	}
	var now []codec.IO
	gone, err := e.settle(ctx, func() (bool, error) {
		var err error
		if now, err = e.load(ctx, col); err != nil {
			return false, err
		}
		_, still := find(now, io.Mnemonic.Original, io.Virtual)
		return !still, nil
	})
	if err != nil {
		return nil, err
	}
	if !gone {
		return nil, fmt.Errorf("%s: the server accepted the delete and the IO is still there", target)
	}
	return now, nil
}

// diffIO compares what the plan states with what the IO is, and returns
// the update that closes the gap with one description per difference.
func diffIO(col codec.Collection, w PlanIO, cur codec.IO) (codec.Update, []string) {
	var u codec.Update
	var diffs []string
	if w.Mnemonic != cur.Mnemonic.Original {
		u.Mnemonic = &w.Mnemonic
		diffs = append(diffs, "mnemonic")
	}
	for name, v := range w.Alternates {
		if cur.Mnemonic.Alternates[name] != v {
			u.Alternates = w.Alternates
			diffs = append(diffs, "alternateMnemonics")
			break
		}
	}
	if w.TieLineInhibit != nil && *w.TieLineInhibit != cur.TieLineInhibit {
		u.TieLineInhibit = w.TieLineInhibit
		diffs = append(diffs, "tieLineInhibit")
	}
	names := make([]string, 0, len(w.Levels))
	for name := range w.Levels {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		want := w.Levels[name]
		have, exists := cur.Levels[name]
		if levelDiffers(col, want, have, exists) {
			if u.Levels == nil {
				u.Levels = map[string]codec.LevelUpdate{}
			}
			u.Levels[name] = want
			diffs = append(diffs, name)
		}
	}
	return u, diffs
}

// levelDiffers reports whether a level of an IO is not what the plan
// states for it.
func levelDiffers(col codec.Collection, want codec.LevelUpdate, have codec.IOLevel, exists bool) bool {
	dev := codec.Device{}
	if have.Device != nil {
		dev = *have.Device
	}
	if want.Clear != nil && *want.Clear {
		// A cleared level may still be listed, carrying the level's own
		// device and no IO: that is cleared.
		return exists && (dev.IO != 0 || dev.SenderReceiver != "" || (len(have.Tags) > 0 && !have.TagsInherited))
	}
	if want.TagsInherit != nil && *want.TagsInherit && exists && !have.TagsInherited {
		return true
	}
	if want.Tags != nil && (!exists || have.TagsInherited || !sameTags(*want.Tags, have.Tags)) {
		return true
	}
	d := want.Device
	if d == nil {
		return false
	}
	switch {
	case d.Name != nil && *d.Name != dev.Name,
		d.TypeID != nil && *d.TypeID != dev.TypeID,
		d.DeviceLevel != nil && *d.DeviceLevel != dev.DeviceLevel,
		d.IO != nil && *d.IO != dev.IO,
		d.SenderReceiver != nil && *d.SenderReceiver != dev.SenderReceiver,
		d.SubChannel != nil && *d.SubChannel != dev.SubChannel,
		d.Inhibit != nil && *d.Inhibit != dev.Inhibit,
		!col.IsDestination() && d.Ignore != nil && *d.Ignore != dev.Ignore,
		col.IsDestination() && d.Disconnect != nil && *d.Disconnect != dev.Disconnect:
		return true
	}
	return false
}

// sameTags compares two tag lists as sets: tags are matched one by one,
// and their order is not something the server keeps.
func sameTags(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	set := make(map[string]int, len(a))
	for _, t := range a {
		set[t]++
	}
	for _, t := range b {
		if set[t] == 0 {
			return false
		}
		set[t]--
	}
	return true
}

// routes makes the crosspoints of the plan.
func (e *ensurer) routes(ctx context.Context, r Router, want []PlanRoute) error {
	srcs, err := e.load(ctx, codec.Sources)
	if err != nil {
		return err
	}
	dsts, err := e.load(ctx, codec.Destinations)
	if err != nil {
		return err
	}
	byName := func(ios []codec.IO, mnemonic string) (int64, bool) {
		for _, io := range ios {
			if io.Mnemonic.Original == mnemonic {
				return io.ID, true
			}
		}
		return 0, false
	}
	known := make(map[int64]bool, len(srcs))
	for _, s := range srcs {
		known[s.ID] = true
	}

	for _, w := range want {
		target := fmt.Sprintf("route %s <- %s", w.Destination, w.Source)
		if w.Level != 0 {
			target += fmt.Sprintf(" level %d", w.Level)
		}
		dst, okD := byName(dsts, w.Destination)
		src, okS := byName(srcs, w.Source)
		if !okD || !okS {
			if e.opts.Check {
				// In a check, an IO the plan would create does not exist yet.
				e.note(target, "take", "")
				continue
			}
			return fmt.Errorf("%s: no such %s", target, map[bool]string{true: "source", false: "destination"}[okD])
		}
		routed := func() (bool, error) {
			table, err := e.c.Routes(ctx, r, dst)
			if err != nil {
				return false, err
			}
			return routedTo(table[codec.DestKey(dst)], src, w.Level, known), nil
		}
		ok, err := routed()
		if err != nil {
			return err
		}
		if ok {
			continue
		}
		e.note(target, "take", "")
		if e.opts.Check {
			continue
		}
		if err := e.c.Take(ctx, r, dst, src, w.Level); err != nil {
			return fmt.Errorf("%s: %w", target, err)
		}
		if ok, err = e.settle(ctx, routed); err != nil {
			return err
		}
		if !ok {
			return fmt.Errorf("%s: the server accepted the take and the route does not show", target)
		}
	}
	return nil
}

// routedTo reports whether a destination is routed to src. On one level
// that is that level's source. On every level (0) it is: at least one
// level routed to src, and no level routed to another source of the
// RouteMaster — a level the destination does not exist on reads 0 or a
// reserved id, which is not a source.
func routedTo(d codec.RouteDest, src int64, level int, known map[int64]bool) bool {
	if level != 0 {
		return d.Levels[codec.LevelKey(level)].Source.ID == src
	}
	hit := false
	for _, l := range d.Levels {
		switch id := l.Source.ID; {
		case id == src:
			hit = true
		case known[id]:
			return false
		}
	}
	return hit
}
