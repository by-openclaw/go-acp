package main

// usage + replace — the source-side pair of the Matrix template
// (#714, owner-designed 2026-08-19):
//
//	usage    WHERE is a source assigned (reverse tally / source usage),
//	         optionally resolved through VIRTUAL resources — the
//	         correlation the NB API correctly leaves to the client
//	         (a dst's own crosspoint does not change when a virtual
//	         upstream is re-fed; owner-confirmed semantics).
//	replace  substitute source A with B on every LITERAL cell that
//	         carries A (ADR-0007: --check first, diff-only, run-twice
//	         = 0). Virtual chains inherit through their own feeds —
//	         replace never resolves behind the operator's back.
//
// Both are client-side compositions over the existing ROUTE obtain /
// action primitives — no new wire commands. Canonical shape: probel /
// ember matrices can implement the same two verbs identically.

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"dhs/internal/cerebrum-nb/codec"
)

// cerebrumUsageRow is one LITERAL assignment (srce feeds dest on
// levels), plus the virtual-chain resolution when requested.
type cerebrumUsageRow struct {
	Srce, Dest string
	Levels     []string
	Effective  string   // resolved physical source; "" = not resolved / marked
	Via        []string // virtual hops crossed, in chain order
	Mark       string   // "" | "loop" | "unfed"
}

// cerebrumDetectVirtuals returns the IDs that are VIRTUAL resources: a
// virtual registers on BOTH faces with the same ID and the same
// mnemonic (live staging 2026-08-18 — "Proc 1"/"Proc 2" as src 3/4 AND
// dst 3/4). ID equality alone is NOT enough: physical routers number
// srcs and dsts independently, so src 1 and dst 1 usually coexist
// without being virtual — the mnemonic must match too. Heuristic by
// necessity: the wire carries no virtual flag.
func cerebrumDetectVirtuals(src, dst []cerebrumMneRow) map[string]bool {
	srcName := map[string]string{}
	for _, r := range src {
		if r.ID != "" && r.Mnemonic != "" {
			srcName[r.ID] = r.Mnemonic
		}
	}
	out := map[string]bool{}
	for _, r := range dst {
		if r.ID == "" || r.Mnemonic == "" {
			continue
		}
		if n, ok := srcName[r.ID]; ok && n == r.Mnemonic {
			out[r.ID] = true
		}
	}
	return out
}

// cerebrumResolveSrc follows the virtual chain upstream from srce at
// one level: while srce is a virtual, the effective source is whatever
// feeds that virtual's DEST face on the same level. Loop-guarded;
// an unfed virtual ends the chain with mark "unfed".
func cerebrumResolveSrc(srce, level string, feed map[string]string, virtuals map[string]bool) (eff string, via []string, mark string) {
	cur := srce
	seen := map[string]bool{}
	for virtuals[cur] {
		if seen[cur] {
			return "", via, "loop"
		}
		seen[cur] = true
		via = append(via, cur)
		next, ok := feed[cur+"\x00"+level]
		if !ok {
			return "", via, "unfed"
		}
		cur = next
	}
	return cur, via, ""
}

// cerebrumBuildUsage turns the raw per-level route rows into
// source-keyed usage rows (levels collapsed per identical
// srce/dest/resolution), resolving virtual chains when resolve is set.
func cerebrumBuildUsage(routes []routeSpec, virtuals map[string]bool, resolve bool) []cerebrumUsageRow {
	feed := map[string]string{}
	for _, r := range routes {
		feed[r.Dest+"\x00"+r.Level] = r.Srce
	}
	type acc struct {
		row  cerebrumUsageRow
		seen map[string]bool
	}
	groups := map[string]*acc{}
	var order []string
	for _, r := range routes {
		if r.Dest == "" || r.Srce == "" || r.Level == "" {
			continue
		}
		eff, via, mark := "", []string(nil), ""
		if resolve {
			eff, via, mark = cerebrumResolveSrc(r.Srce, r.Level, feed, virtuals)
		}
		k := r.Srce + "\x00" + r.Dest + "\x00" + eff + "\x00" + strings.Join(via, ";") + "\x00" + mark
		g := groups[k]
		if g == nil {
			g = &acc{row: cerebrumUsageRow{Srce: r.Srce, Dest: r.Dest, Effective: eff, Via: via, Mark: mark}, seen: map[string]bool{}}
			groups[k] = g
			order = append(order, k)
		}
		if !g.seen[r.Level] {
			g.seen[r.Level] = true
			g.row.Levels = append(g.row.Levels, r.Level)
		}
	}
	out := make([]cerebrumUsageRow, 0, len(order))
	for _, k := range order {
		sortCerebrumIDs(groups[k].row.Levels)
		out = append(out, groups[k].row)
	}
	return out
}

// formatCerebrumUsageCSV renders rows source-keyed. The resolved
// columns appear only in resolve mode: effective_srce is the chain's
// physical end ("" on loop/unfed — always loud in ascii, silent-empty
// here so machine parsing stays simple), via lists the virtual hops.
// --names appends srce_label,dest_label (mnemonics — header-keyed,
// #751 G4: same columns as the probel/ember+ usage CSVs).
func formatCerebrumUsageCSV(rows []cerebrumUsageRow, resolve bool, srcNames, dstNames map[string]string) string {
	withNames := srcNames != nil || dstNames != nil
	var b strings.Builder
	b.WriteString("srce,dest,levels")
	if resolve {
		b.WriteString(",effective_srce,via")
	}
	if withNames {
		b.WriteString(",srce_label,dest_label")
	}
	b.WriteByte('\n')
	for _, r := range rows {
		b.WriteString(r.Srce)
		b.WriteByte(',')
		b.WriteString(r.Dest)
		b.WriteByte(',')
		b.WriteString(strings.Join(r.Levels, ";"))
		if resolve {
			b.WriteByte(',')
			b.WriteString(r.Effective)
			b.WriteByte(',')
			b.WriteString(strings.Join(r.Via, ";"))
		}
		if withNames {
			b.WriteByte(',')
			b.WriteString(srcNames[r.Srce])
			b.WriteByte(',')
			b.WriteString(dstNames[r.Dest])
		}
		b.WriteByte('\n')
	}
	return b.String()
}

// cerebrumUsageName renders "Label (id)" or just the id when no
// mnemonic is known.
func cerebrumUsageName(id string, names map[string]string) string {
	if n := names[id]; n != "" {
		return fmt.Sprintf("%s (%s)", n, id)
	}
	return id
}

// renderCerebrumUsageFanout prints the source-side tree: every dest
// the source feeds, recursing INTO virtual dests to show their
// subscribers (the "who follows this source" view). depth-guarded by
// the same loop set the resolver uses.
func renderCerebrumUsageFanout(w io.Writer, srce string, rows []cerebrumUsageRow, virtuals map[string]bool, srcNames, dstNames map[string]string, indent string, seen map[string]bool) {
	if seen[srce] {
		_, _ = fmt.Fprintf(w, "%s└── [loop!] %s\n", indent, cerebrumUsageName(srce, srcNames))
		return
	}
	seen[srce] = true
	defer delete(seen, srce)

	var children []cerebrumUsageRow
	for _, r := range rows {
		if r.Srce == srce {
			children = append(children, r)
		}
	}
	for i, r := range children {
		branch, next := "├──", "│   "
		if i == len(children)-1 {
			branch, next = "└──", "    "
		}
		label := cerebrumUsageName(r.Dest, dstNames)
		if virtuals[r.Dest] {
			_, _ = fmt.Fprintf(w, "%s%s %s  [virtual]  levels %s\n", indent, branch, label, strings.Join(r.Levels, ";"))
			renderCerebrumUsageFanout(w, r.Dest, rows, virtuals, srcNames, dstNames, indent+next, seen)
		} else {
			_, _ = fmt.Fprintf(w, "%s%s %s  levels %s\n", indent, branch, label, strings.Join(r.Levels, ";"))
		}
	}
}

// renderCerebrumUsageChain prints the dest-side view: per level-group,
// the chain upstream through virtuals to the effective source.
func renderCerebrumUsageChain(w io.Writer, dest string, rows []cerebrumUsageRow, srcNames, dstNames map[string]string) {
	var mine []cerebrumUsageRow
	for _, r := range rows {
		if r.Dest == dest {
			mine = append(mine, r)
		}
	}
	_, _ = fmt.Fprintf(w, "%s\n", cerebrumUsageName(dest, dstNames))
	for i, r := range mine {
		branch := "├──"
		if i == len(mine)-1 {
			branch = "└──"
		}
		chain := cerebrumUsageName(r.Srce, srcNames)
		if len(r.Via) > 0 {
			// The literal feed itself is the chain's first virtual hop
			// — its marker sits next to ITS name, then each further
			// hop upstream carries its own.
			chain += " [virtual]"
			for _, hop := range r.Via[1:] {
				chain += "  ← " + cerebrumUsageName(hop, srcNames) + " [virtual]"
			}
		}
		suffix := ""
		switch {
		case r.Mark != "":
			suffix = fmt.Sprintf("  [%s!]", r.Mark)
		case len(r.Via) > 0:
			suffix = fmt.Sprintf("  ← effective %s", cerebrumUsageName(r.Effective, srcNames))
		}
		_, _ = fmt.Fprintf(w, "%s levels %s : fed by %s%s\n", branch, strings.Join(r.Levels, ";"), chain, suffix)
	}
}

// cerebrumUsage drives `dhs consumer cerebrum-nb usage`.
func cerebrumUsage(_ context.Context, args []string) error {
	args = reorderFlagsFirst(args)
	fs := flag.NewFlagSet("cerebrum-nb usage", flag.ContinueOnError)
	cf := newCerebrumFlags(fs)
	router := fs.String("router", "0.0.0.0", "router target (route-master sentinel 0.0.0.0 or a physical Router IP)")
	srce := fs.String("srce", "", "filter: only this source's assignments (fan-out view in ascii)")
	dest := fs.String("dest", "", "filter: only this dest's feeds (upstream-chain view in ascii)")
	level := fs.String("level", "", "filter: one level only")
	resolveFlag := fs.Bool("resolve", false, "resolve VIRTUAL chains: add effective_srce + via columns (csv) / render the full chain (ascii)")
	withNames := fs.Bool("names", false, "csv: append srce_label,dest_label mnemonic columns (#751 G4 — same flag on every connector's usage; ascii always shows names)")
	format := fs.String("format", "csv", "output format: csv | ascii")
	out := fs.String("out", "", "csv output file (omitted = snapshots/<proto>/<router>/usage.csv per ADR-0028; \"-\" = stdout)")
	idle := fs.Duration("idle", 3*time.Second, "stop collecting this long after the last snapshot frame if no WILDCARD_COMPLETE arrives")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *format != "csv" && *format != "ascii" {
		return cerebrumValErr("usage", fmt.Sprintf("--format must be csv or ascii, got %q", *format))
	}
	if *srce != "" && *dest != "" {
		return cerebrumValErr("usage", "--srce and --dest are mutually exclusive (fan-out vs upstream-chain view)")
	}
	rest := fs.Args()
	if len(rest) < 1 {
		return cerebrumValErr("usage", "missing host[:port] argument")
	}
	host, portArg, err := splitHostPort(rest[0], cf.port)
	if err != nil {
		return err
	}
	cf.port = portArg
	cerebrumExpandAutoPaths(cf, "usage", host)

	p, err := cerebrumDial(cf, host, "usage")
	if err != nil {
		return err
	}
	defer func() { _ = p.Disconnect() }()

	st, err := cerebrumObtainState(context.Background(), p.Session(), *router, "ROUTER", *idle, cerebrumStateWant{
		Routes: true, RouteLevel: *level,
		SrcMne: true, DstMne: true,
		Verb: "usage",
	})
	if err != nil {
		return err
	}
	virtuals := cerebrumDetectVirtuals(st.Src, st.Dst)
	rows := cerebrumBuildUsage(st.Routes, virtuals, *resolveFlag)

	// Apply filters AFTER resolution (a --srce filter must not hide
	// the chains that resolve back to it).
	if *srce != "" || *dest != "" {
		filtered := rows[:0]
		for _, r := range rows {
			if (*srce != "" && r.Srce == *srce) || (*dest != "" && r.Dest == *dest) {
				filtered = append(filtered, r)
			}
		}
		rows = filtered
	}

	srcNames, dstNames := map[string]string{}, map[string]string{}
	for _, m := range dedupeCerebrumMnes(st.Src) {
		srcNames[m.ID] = m.Mnemonic
	}
	for _, m := range dedupeCerebrumMnes(st.Dst) {
		dstNames[m.ID] = m.Mnemonic
	}

	if *format == "ascii" {
		switch {
		case *dest != "":
			renderCerebrumUsageChain(os.Stdout, *dest, rows, srcNames, dstNames)
		case *srce != "":
			fmt.Printf("%s\n", cerebrumUsageName(*srce, srcNames))
			renderCerebrumUsageFanout(os.Stdout, *srce, cerebrumBuildUsage(st.Routes, virtuals, *resolveFlag), virtuals, srcNames, dstNames, "", map[string]bool{})
		default:
			// Full map: one fan-out block per feeding source, virtuals
			// marked; virtual subscribers appear nested under their
			// physical roots.
			seenTop := map[string]bool{}
			for _, r := range cerebrumBuildUsage(st.Routes, virtuals, *resolveFlag) {
				if seenTop[r.Srce] || virtuals[r.Srce] {
					continue
				}
				seenTop[r.Srce] = true
				fmt.Printf("%s\n", cerebrumUsageName(r.Srce, srcNames))
				renderCerebrumUsageFanout(os.Stdout, r.Srce, cerebrumBuildUsage(st.Routes, virtuals, *resolveFlag), virtuals, srcNames, dstNames, "", map[string]bool{})
			}
		}
		return nil
	}

	var csvSrcNames, csvDstNames map[string]string
	if *withNames {
		csvSrcNames, csvDstNames = srcNames, dstNames
	}
	csv := formatCerebrumUsageCSV(rows, *resolveFlag, csvSrcNames, csvDstNames)
	if *out == "-" {
		fmt.Print(csv)
		return nil
	}
	path := *out
	if path == "" {
		path = filepath.Join(snapshotDir("cerebrum-nb", *router), "usage.csv")
		fmt.Fprintf(os.Stderr, "cerebrum-nb usage: default snapshot file %s (ADR-0028)\n", path)
	}
	if dir := filepath.Dir(path); dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
	}
	if err := os.WriteFile(path, []byte(csv), 0o644); err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "cerebrum-nb usage: wrote %s (%d row(s))\n", path, len(rows))
	return nil
}

// cerebrumReplace drives `dhs consumer cerebrum-nb replace` — replace
// source A with B on every LITERAL cell carrying A (ADR-0007 ensure:
// --check dry-run, diff-only apply, run-twice = 0 because nothing
// carries A afterwards). Virtual subscribers inherit through their own
// chains, untouched — substitution never resolves behind the
// operator's back.
func cerebrumReplace(_ context.Context, args []string) error {
	args = reorderFlagsFirst(args)
	fs := flag.NewFlagSet("cerebrum-nb replace", flag.ContinueOnError)
	cf := newCerebrumFlags(fs)
	router := fs.String("router", "0.0.0.0", "router target (route-master sentinel 0.0.0.0 or a physical Router IP)")
	from := fs.String("srce", "", "source ID to replace (required)")
	with := fs.String("with", "", "source ID that takes over (required)")
	level := fs.String("level", "", "restrict to one level (omitted = every level where --srce is assigned)")
	check := fs.Bool("check", false, "dry-run (ADR-0007): list the cells that would change, send nothing")
	output := fs.String("output", "text", "output format: text | json (ADR-0002; json = {changed|would_change, diff[]})")
	idle := fs.Duration("idle", 3*time.Second, "snapshot idle fallback")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *from == "" || *with == "" {
		return cerebrumValErr("replace", "--srce and --with are required (replace source A with B)")
	}
	if *from == *with {
		return cerebrumValErr("replace", "--srce and --with are the same source — nothing to do")
	}
	jsonOut, oerr := resolveEnsureOutput(*output, false)
	if oerr != nil {
		return oerr
	}
	rest := fs.Args()
	if len(rest) < 1 {
		return cerebrumValErr("replace", "missing host[:port] argument")
	}
	host, portArg, err := splitHostPort(rest[0], cf.port)
	if err != nil {
		return err
	}
	cf.port = portArg
	cerebrumExpandAutoPaths(cf, "replace", host)

	p, err := cerebrumDial(cf, host, "replace")
	if err != nil {
		return err
	}
	defer func() { _ = p.Disconnect() }()
	sess := p.Session()
	logw := os.Stdout
	if jsonOut {
		logw = os.Stderr
	}

	st, err := cerebrumObtainState(context.Background(), sess, *router, "ROUTER", *idle, cerebrumStateWant{
		Routes: true, RouteLevel: *level, Verb: "replace",
	})
	if err != nil {
		return err
	}

	// The literal cells carrying --srce, in snapshot order.
	var cells []routeSpec
	for _, r := range st.Routes {
		if r.Srce == *from {
			cells = append(cells, r)
		}
	}
	changed := len(cells) > 0
	diffs := make([]ensureDiff, 0, len(cells))
	for _, c := range cells {
		diffs = append(diffs, ensureDiff{Field: fmt.Sprintf("route.%s.%s", c.Dest, c.Level), From: *from, To: *with})
	}

	if *check {
		for _, c := range cells {
			_, _ = fmt.Fprintf(logw, "[would-replace] dst=%s lvl=%s: src %s -> %s\n", c.Dest, c.Level, *from, *with)
		}
		_, _ = fmt.Fprintf(logw, "cerebrum-nb replace --check: would_change=%d cell(s) carrying src %s — nothing sent\n", len(cells), *from)
		if jsonOut {
			return emitEnsure(true, ensureResult{WouldChange: &changed, Diff: diffs})
		}
		return nil
	}

	fails := 0
	for _, c := range cells {
		body := &codec.RoutingAction{
			Type:       "ROUTE",
			IPAddress:  *router,
			DeviceType: codec.DeviceType("ROUTER"),
			DestID:     c.Dest,
			SrceID:     *with,
			LevelID:    c.Level,
		}
		if err := sess.Action(context.Background(), body); err != nil {
			_, _ = fmt.Fprintf(logw, "[replace] NACK dst=%s lvl=%s reason=%s\n", c.Dest, c.Level, err)
			fails++
			continue
		}
		_, _ = fmt.Fprintf(logw, "[replace] OK   dst=%s lvl=%s: src %s -> %s\n", c.Dest, c.Level, *from, *with)
	}
	if fails > 0 {
		return fmt.Errorf("%d/%d replace action(s) failed", fails, len(cells))
	}
	_, _ = fmt.Fprintf(logw, "cerebrum-nb replace: changed=%d cell(s); run again to verify 0 (nothing carries src %s any more)\n", len(cells), *from)
	if jsonOut {
		return emitEnsure(true, ensureResult{Changed: &changed, Diff: diffs})
	}
	return nil
}
