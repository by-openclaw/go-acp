package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"text/tabwriter"
	"time"

	"dhs/internal/snell-rollcall/codec/router"
	rollcall "dhs/internal/snell-rollcall/consumer"
)

// A RollCall router is not a device with a menu. Its routing, its names and its
// protects are control variables in a flat command space, so none of the
// generic verbs reach them: walking a router finds three menu lines and nothing
// to route with. These three verbs are that command space from the command
// line.

func helpRollcallRouter() {
	fmt.Println(`dhs consumer rollcall router <host> [--slot N] [--output text|json]

Find the routing interface and print what it publishes: the matrices, their
levels, and how many sources and destinations each level has.

The interface lives on one node of a controller and nothing in a device list
says which — on a Centra the matrices are their own nodes and neither serves
it — so without --slot every node is probed. A node that is not a router
refuses, which is how they are told apart.

Examples:
  dhs consumer rollcall router 10.6.250.105
  dhs consumer rollcall router 10.6.250.105 --slot 14 --output json`)
}

func helpRollcallRoute() {
	fmt.Println(`dhs consumer rollcall route <host> --matrix M --level L --dest D [--source S]
                                   [--slot N] [--protect-id ID] [--check]
                                   [--output text|json]

Read a crosspoint, or make one when --source is given.

Two things about this protocol are worth knowing before reading the output:

  * The reply to a route carries the crosspoint from BEFORE the change. What
    was actually routed arrives afterwards on the back channel, so this verb
    reads the destination again and prints that.

  * The tally follows tielines. Routing source 7 can read back as matrix 2
    source 1, because the number reported is the final upstream source rather
    than the one asked for. That is the router working.

IDEMPOTENCY (ADR-0007)

  A take reports "changed", and it is a measurement rather than a prediction:
  the destination is read before the take and again after it, and changed says
  whether those two differ. Nothing about it can be wrong, which matters here
  because of the tieline behaviour above — a crosspoint that already carries
  what was asked for reports the far end rather than the near one, and any
  rule that compared the request with the reading would call a converged route
  unconverged and take it again for ever.

  --check reads and sends nothing. It reports "satisfied": whether the
  destination already reads back as the source asked for. On a route within
  one matrix that is the whole answer. Across matrices it can be false while
  the route is already made, for the same reason — so --check across matrices
  is a question about the reading, not about the plant.

Examples:
  dhs consumer rollcall route 10.6.250.105 --matrix 1 --level 1 --dest 4
  dhs consumer rollcall route 10.6.250.105 --matrix 1 --level 1 --dest 4 --source 9
  dhs consumer rollcall route 10.6.250.105 --matrix 1 --level 1 --dest 4 --source 9 --check
  dhs consumer rollcall route 10.6.250.105 --matrix 1 --level 1 --dest 4 --output json`)
}

func helpRollcallTally() {
	fmt.Println(`dhs consumer rollcall tally <host> [--matrix M --level L] [--slot N] [--names]

Print the crosspoints of a level and then follow them live; Ctrl+C to stop.

The reading comes first because it has to: enabling the back channel does not
replay the current state on a real controller, so a panel that only subscribes
sees nothing until something moves.

With --matrix and --level it reads that level first. Without them it subscribes
to everything and prints only what changes.

Examples:
  dhs consumer rollcall tally 10.6.250.105 --matrix 1 --level 1
  dhs consumer rollcall tally 10.6.250.105 --matrix 1 --level 1 --names`)
}

// rollcallPlugin connects and returns the plugin as a RollCall one.
func rollcallPlugin(ctx context.Context, host string, cf *commonFlags) (*rollcall.Plugin, func(), error) {
	plug, cleanup, err := connect(ctx, host, cf)
	if err != nil {
		return nil, nil, err
	}
	rc, ok := plug.(*rollcall.Plugin)
	if !ok {
		cleanup()
		return nil, nil, fmt.Errorf("this verb is only supported for the rollcall protocol")
	}
	return rc, cleanup, nil
}

// findRouter locates the routing interface, on a named slot or by probing.
func findRouter(ctx context.Context, p *rollcall.Plugin, slot int) (*rollcall.RouterInterface, error) {
	if slot >= 0 {
		return p.RouterAt(ctx, slot)
	}
	return p.FindRouter(ctx)
}

func runRollcallRouter(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("router", flag.ExitOnError)
	fs.Usage = verbUsageFn(fs, helpRollcallRouter)
	cf := addCommonFlags(fs)
	slot := fs.Int("slot", -1, "node to read; without it every node is probed")
	output := fs.String("output", "text", "output format: text | json (ADR-0002)")

	host, rest, err := popHost(args)
	if err != nil {
		return fmt.Errorf("usage: dhs consumer rollcall router <host> [--slot N]")
	}
	_ = parseVerbFlags(fs, rest)

	p, cleanup, err := rollcallPlugin(ctx, host, cf)
	if err != nil {
		return err
	}
	defer cleanup()

	r, err := findRouter(ctx, p, *slot)
	if err != nil {
		return err
	}

	if strings.EqualFold(*output, "json") {
		return json.NewEncoder(os.Stdout).Encode(routerJSON(r))
	}

	fmt.Printf("router       %s\n", nameOrUnset(r.Name))
	fmt.Printf("slot         %d (%s)\n", r.Slot, r.Addr)
	fmt.Printf("interface    version %d\n", r.Version)
	fmt.Printf("salvos       %d\n", r.Salvos)
	fmt.Printf("devices      %d\n", r.Devices)
	fmt.Println()

	// Categories are how a panel narrows a plant down: a group matches a name
	// rather than owning a set, so what is printed is the match itself.
	for _, c := range r.CategoryList {
		kind := "any of"
		if c.Exclusive {
			kind = "one of"
		}
		fmt.Printf("category %d   %s (%s)\n", c.Number, nameOrUnset(c.Name), kind)
		for _, g := range c.Groups {
			fmt.Printf("             %-12s names starting %q at %d\n", g.Name, g.Search, g.Start)
		}
	}
	if len(r.CategoryList) > 0 {
		fmt.Println()
	}

	// The writer buffers, so a write cannot fail here; Flush is where an error
	// would surface, and that one is returned.
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	_, _ = fmt.Fprintln(w, "MATRIX\tLEVEL\tNAME\tSOURCES\tDESTINATIONS")
	for _, m := range r.Matrices {
		for _, lv := range m.Levels {
			_, _ = fmt.Fprintf(w, "%d %s\t%d\t%s\t%d\t%d\n",
				m.Number, m.Name, lv.Number, lv.Name, lv.Srcs.Count, lv.Dsts.Count)
		}
	}
	return w.Flush()
}

func runRollcallRoute(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("route", flag.ExitOnError)
	fs.Usage = verbUsageFn(fs, helpRollcallRoute)
	cf := addCommonFlags(fs)
	slot := fs.Int("slot", -1, "node to use; without it every node is probed")
	matrix := fs.Int("matrix", 1, "matrix number, counting from one")
	level := fs.Int("level", 1, "level number, counting from one")
	dest := fs.Int("dest", 0, "destination number, counting from one")
	source := fs.Int("source", 0, "source to route; omit to read what is routed")
	srcMatrix := fs.Int("source-matrix", 0, "matrix the source is on (default: the destination's)")
	srcLevel := fs.Int("source-level", 0, "level the source is on (default: the destination's)")
	protectID := fs.Int("protect-id", 0, "panel id for a temporary protected override")
	check := fs.Bool("check", false, "read only: report whether the destination already carries --source, and send nothing (ADR-0007)")
	output := fs.String("output", "text", "output format: text | json (ADR-0002)")

	host, rest, err := popHost(args)
	if err != nil {
		return fmt.Errorf("usage: dhs consumer rollcall route <host> --matrix M --level L --dest D [--source S]")
	}
	_ = parseVerbFlags(fs, rest)

	if *dest < 1 {
		return fmt.Errorf("--dest is required and counts from one")
	}
	if *check && *source < 1 {
		return fmt.Errorf("--check needs a --source to check against")
	}

	p, cleanup, err := rollcallPlugin(ctx, host, cf)
	if err != nil {
		return err
	}
	defer cleanup()

	r, err := findRouter(ctx, p, *slot)
	if err != nil {
		return err
	}

	m, l, d := uint32(*matrix), uint32(*level), uint32(*dest)

	var (
		xpt     rollcall.Crosspoint
		changed bool
		want    router.SourcePin
	)
	if *source > 0 {
		want = router.SourcePin{
			Matrix: uint8(orDefault(*srcMatrix, *matrix)),
			Level:  uint8(orDefault(*srcLevel, *level)),
			Source: uint16(*source),
		}
	}

	switch {
	case *check:
		// A dry run sends nothing, so all it can report is the reading.
		if xpt, err = p.Route(ctx, r, m, l, d); err != nil {
			return err
		}
	case *source > 0:
		// Read before, take, read after. Whether anything changed is then a
		// comparison of two readings rather than a guess about what a
		// controller will do with a request — which is the only form that
		// survives a route across a tieline, where what comes back is the far
		// end of the cable rather than the source that was asked for.
		var before rollcall.Crosspoint
		if before, err = p.Route(ctx, r, m, l, d); err != nil {
			return err
		}
		if xpt, err = takeRoute(ctx, p, r, m, l, d, want, uint16(*protectID)); err != nil {
			return err
		}
		changed = xpt.Source != before.Source
	default:
		if xpt, err = p.Route(ctx, r, m, l, d); err != nil {
			return err
		}
	}

	if strings.EqualFold(*output, "json") {
		out := map[string]any{
			"matrix":      m,
			"level":       l,
			"destination": d,
			"source": map[string]any{
				"matrix": xpt.Source.Matrix,
				"level":  xpt.Source.Level,
				"source": xpt.Source.Source,
			},
			"routed": xpt.Source.Source != 0,
		}
		if *check {
			out["satisfied"] = xpt.Source == want
			out["checked"] = true
		} else if *source > 0 {
			out["changed"] = changed
		}
		return json.NewEncoder(os.Stdout).Encode(out)
	}

	if *check {
		if xpt.Source == want {
			fmt.Printf("matrix %d level %d destination %d already reads %s\n", m, l, d, want)
			return nil
		}
		fmt.Printf("matrix %d level %d destination %d reads %s, would take %s\n",
			m, l, d, nameOrUnrouted(xpt.Source), want)
		return nil
	}
	if xpt.Source.Source == 0 {
		fmt.Printf("matrix %d level %d destination %d has nothing routed to it\n", m, l, d)
		return nil
	}
	if *source > 0 && !changed {
		fmt.Printf("matrix %d level %d destination %d <- %s (unchanged)\n", m, l, d, xpt.Source)
		return nil
	}
	fmt.Printf("matrix %d level %d destination %d <- %s\n", m, l, d, xpt.Source)
	return nil
}

// nameOrUnrouted spells a pin that names no source, so a dry run reads as a
// sentence rather than as a zero.
func nameOrUnrouted(p router.SourcePin) string {
	if p.Source == 0 {
		return "nothing"
	}
	return p.String()
}

func runRollcallTally(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("tally", flag.ExitOnError)
	fs.Usage = verbUsageFn(fs, helpRollcallTally)
	cf := addCommonFlags(fs)
	slot := fs.Int("slot", -1, "node to use; without it every node is probed")
	matrix := fs.Int("matrix", 0, "matrix to read first, counting from one")
	level := fs.Int("level", 0, "level to read first, counting from one")
	withNames := fs.Bool("names", false, "fetch the level's names and show them beside the numbers")

	host, rest, err := popHost(args)
	if err != nil {
		return fmt.Errorf("usage: dhs consumer rollcall tally <host> [--matrix M --level L]")
	}
	_ = parseVerbFlags(fs, rest)

	p, cleanup, err := rollcallPlugin(ctx, host, cf)
	if err != nil {
		return err
	}
	defer cleanup()

	r, err := findRouter(ctx, p, *slot)
	if err != nil {
		return err
	}

	var names rollcall.Names
	if *withNames && *matrix > 0 && *level > 0 {
		names, err = p.LevelNames(ctx, r, uint32(*matrix), uint32(*level), router.NameWidth32)
		if err != nil {
			// Names are a convenience. A tally without them is still a tally.
			fmt.Fprintf(os.Stderr, "warning: names unavailable: %v\n", err)
		}
	}

	show := func(x rollcall.Crosspoint) {
		line := fmt.Sprintf("m%d/l%d/d%-4d <- ", x.Dest.Matrix, x.Dest.Level, x.Dest.Source)
		if x.Source.Source == 0 {
			line += "(nothing)"
		} else {
			line += x.Source.String()
		}
		if len(names.Srcs) > 0 || len(names.Dsts) > 0 {
			line += fmt.Sprintf("   %s <- %s",
				nameOrBlank(names.Dst(uint32(x.Dest.Source))),
				nameOrBlank(names.Src(uint32(x.Source.Source))))
		}
		fmt.Println(line)
	}

	// The current state first: enabling the back channel does not replay it, so
	// a client that only subscribes sees nothing until something moves.
	if *matrix > 0 && *level > 0 {
		xpts, err := p.Routes(ctx, r, uint32(*matrix), uint32(*level))
		if err != nil {
			return err
		}
		for _, x := range xpts {
			show(x)
		}
		fmt.Println("--- following changes; Ctrl+C to stop")
	}

	if err := p.WatchRoutes(ctx, r, show); err != nil {
		return err
	}

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	select {
	case <-stop:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// takeRoute makes a route and returns what the router ended up with.
//
// Neither the reply nor an immediate read says that: the reply carries the
// crosspoint from before the change, and the controller applies asynchronously,
// so a read straight afterwards usually still shows the old source. What says
// it is the back-channel push, which is the mechanism a panel uses and the only
// one that tells the truth about tielines.
func takeRoute(ctx context.Context, p *rollcall.Plugin, r *rollcall.RouterInterface,
	m, l, d uint32, src router.SourcePin, protectID uint16) (rollcall.Crosspoint, error) {

	tally := make(chan rollcall.Crosspoint, 8)
	if err := p.WatchRoutes(ctx, r, func(x rollcall.Crosspoint) {
		if uint32(x.Dest.Matrix) == m && uint32(x.Dest.Level) == l && uint32(x.Dest.Source) == d {
			select {
			case tally <- x:
			default:
			}
		}
	}); err != nil {
		return rollcall.Crosspoint{}, err
	}

	if _, err := p.SetRoute(ctx, r, m, l, d, src, protectID); err != nil {
		return rollcall.Crosspoint{}, err
	}

	select {
	case x := <-tally:
		return x, nil
	case <-time.After(routeTallyWait):
		// The route may still have been made: a controller that reports no
		// tally is unusual rather than impossible, and the read is the best
		// answer left.
		return p.Route(ctx, r, m, l, d)
	case <-ctx.Done():
		return rollcall.Crosspoint{}, ctx.Err()
	}
}

// routeTallyWait is how long to wait for a controller to report a route it has
// accepted. It is a bound on a reply that has already been acknowledged, not a
// protocol timeout: something has gone wrong if it expires.
const routeTallyWait = 3 * time.Second

// routerJSON is the machine shape of a routing interface.
func routerJSON(r *rollcall.RouterInterface) map[string]any {
	matrices := make([]map[string]any, 0, len(r.Matrices))
	for _, m := range r.Matrices {
		levels := make([]map[string]any, 0, len(m.Levels))
		for _, lv := range m.Levels {
			levels = append(levels, map[string]any{
				"level":        lv.Number,
				"name":         lv.Name,
				"sources":      lv.Srcs.Count,
				"destinations": lv.Dsts.Count,
			})
		}
		matrices = append(matrices, map[string]any{
			"matrix":                   m.Number,
			"name":                     m.Name,
			"controller":               m.Controller,
			"source_associations":      m.SrcAssocs.Count,
			"destination_associations": m.DstAssocs.Count,
			"levels":                   levels,
		})
	}
	cats := make([]map[string]any, 0, len(r.CategoryList))
	for _, c := range r.CategoryList {
		groups := make([]map[string]any, 0, len(c.Groups))
		for _, g := range c.Groups {
			groups = append(groups, map[string]any{
				"group":  g.Number,
				"name":   g.Name,
				"search": g.Search,
				"start":  g.Start,
			})
		}
		cats = append(cats, map[string]any{
			"category":   c.Number,
			"name":       c.Name,
			"exclusive":  c.Exclusive,
			"sort_index": c.SortIndex,
			"groups":     groups,
		})
	}

	return map[string]any{
		"categories":        cats,
		"slot":              r.Slot,
		"address":           r.Addr.String(),
		"name":              r.Name,
		"interface_version": r.Version,
		"salvos":            r.Salvos,
		"devices":           r.Devices,
		"matrices":          matrices,
	}
}

func nameOrUnset(s string) string {
	if s == "" {
		return "(unset)"
	}
	return s
}

func nameOrBlank(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

// orDefault takes the first value when it was given, and the second otherwise.
func orDefault(given, fallback int) int {
	if given > 0 {
		return given
	}
	return fallback
}
