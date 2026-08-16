package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"dhs/internal/cerebrum-nb/codec"
	cerebrum "dhs/internal/cerebrum-nb/consumer"
)

// cerebrumXpointRow is one line of a Cerebrum crosspoint CSV: a destination fed
// by a source across one OR MORE levels. Multi-level on a single row models the
// common "all-level take" (video + audio + ... follow the same source); a
// breakaway (a level fed by a different source) becomes its own row with its own
// source. This is the round-trip shape shared by `export` (snapshot -> rows) and
// `import` (rows -> ROUTE actions), and the same shape a salvo will reuse.
type cerebrumXpointRow struct {
	Dest   string
	Srce   string
	Levels []string
}

// parseCerebrumXpoint decodes a crosspoint CSV. The header (any order,
// case-insensitive) needs dest + srce + a level column, which may be either:
//
//	levels  - a ';'-separated list on one row (multi-level take), e.g. "1;2;3"
//	level   - a single level (legacy single-level form; still accepted)
//
// Blank lines and '#' comments are skipped. srcName is only for error messages.
func parseCerebrumXpoint(data []byte, srcName string) ([]cerebrumXpointRow, error) {
	lines := strings.Split(strings.ReplaceAll(string(data), "\r\n", "\n"), "\n")
	// Skip leading blank/comment lines to find the header.
	start := 0
	for start < len(lines) && (strings.TrimSpace(lines[start]) == "" || strings.HasPrefix(strings.TrimSpace(lines[start]), "#")) {
		start++
	}
	if start >= len(lines) {
		return nil, fmt.Errorf("%s: empty (no header)", srcName)
	}
	header := strings.Split(strings.ToLower(strings.TrimSpace(lines[start])), ",")
	idx := map[string]int{}
	for i, h := range header {
		idx[strings.TrimSpace(h)] = i
	}
	if _, ok := idx["dest"]; !ok {
		return nil, fmt.Errorf("%s: missing column \"dest\"", srcName)
	}
	if _, ok := idx["srce"]; !ok {
		return nil, fmt.Errorf("%s: missing column \"srce\"", srcName)
	}
	levelCol, multi := idx["levels"]
	if !multi {
		var ok bool
		levelCol, ok = idx["level"]
		if !ok {
			return nil, fmt.Errorf("%s: missing level column (need \"levels\" or \"level\")", srcName)
		}
	}

	var out []cerebrumXpointRow
	for n, line := range lines[start+1:] {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		f := strings.Split(line, ",")
		if len(f) < len(header) {
			return nil, fmt.Errorf("%s line %d: %d columns < header %d", srcName, start+n+2, len(f), len(header))
		}
		dest := strings.TrimSpace(f[idx["dest"]])
		srce := strings.TrimSpace(f[idx["srce"]])
		levels := splitLevelCell(f[levelCol])
		if dest == "" || srce == "" || len(levels) == 0 {
			return nil, fmt.Errorf("%s line %d: dest, srce and at least one level are required", srcName, start+n+2)
		}
		out = append(out, cerebrumXpointRow{Dest: dest, Srce: srce, Levels: levels})
	}
	return out, nil
}

// splitLevelCell splits a levels cell on ';' (also tolerating '|' and
// whitespace), trims, and drops empties - so "1; 2 ;3" -> ["1","2","3"].
func splitLevelCell(cell string) []string {
	raw := strings.FieldsFunc(cell, func(r rune) bool { return r == ';' || r == '|' || r == ' ' || r == '\t' })
	out := make([]string, 0, len(raw))
	for _, s := range raw {
		if s = strings.TrimSpace(s); s != "" {
			out = append(out, s)
		}
	}
	return out
}

// expandCerebrumXpoint flattens multi-level rows into one routeSpec per level,
// in row-then-level order - the list `import` sends as individual ROUTE actions.
func expandCerebrumXpoint(rows []cerebrumXpointRow) []routeSpec {
	var out []routeSpec
	for _, r := range rows {
		for _, lvl := range r.Levels {
			out = append(out, routeSpec{Dest: r.Dest, Srce: r.Srce, Level: lvl})
		}
	}
	return out
}

// collapseCerebrumRoutes is the inverse used by `export`: it groups a flat route
// list (one entry per dest/level as read from the snapshot) into multi-level
// rows. Levels feeding a dest from the SAME source coalesce onto one row; a
// level fed by a different source (breakaway) stays a separate row. Output is
// deterministically ordered (dest, then srce, numeric-aware) so exports diff
// cleanly and round-trip byte-stably.
func collapseCerebrumRoutes(routes []routeSpec) []cerebrumXpointRow {
	type acc struct {
		dest, srce string
		levels     []string
		seen       map[string]bool
	}
	groups := map[string]*acc{}
	var order []string
	for _, r := range routes {
		if r.Dest == "" || r.Srce == "" || r.Level == "" {
			continue
		}
		k := r.Dest + "\x00" + r.Srce
		g := groups[k]
		if g == nil {
			g = &acc{dest: r.Dest, srce: r.Srce, seen: map[string]bool{}}
			groups[k] = g
			order = append(order, k)
		}
		if !g.seen[r.Level] {
			g.seen[r.Level] = true
			g.levels = append(g.levels, r.Level)
		}
	}
	rows := make([]cerebrumXpointRow, 0, len(order))
	for _, k := range order {
		g := groups[k]
		sortCerebrumIDs(g.levels)
		rows = append(rows, cerebrumXpointRow{Dest: g.dest, Srce: g.srce, Levels: g.levels})
	}
	sort.SliceStable(rows, func(i, j int) bool {
		if c := cmpCerebrumID(rows[i].Dest, rows[j].Dest); c != 0 {
			return c < 0
		}
		return cmpCerebrumID(rows[i].Srce, rows[j].Srce) < 0
	})
	return rows
}

// formatCerebrumXpointCSV renders rows as a `dest,srce,levels` CSV with
// ';'-joined levels - the exact shape parseCerebrumXpoint reads back.
func formatCerebrumXpointCSV(rows []cerebrumXpointRow) string {
	var b strings.Builder
	b.WriteString("dest,srce,levels\n")
	for _, r := range rows {
		b.WriteString(r.Dest)
		b.WriteByte(',')
		b.WriteString(r.Srce)
		b.WriteByte(',')
		b.WriteString(strings.Join(r.Levels, ";"))
		b.WriteByte('\n')
	}
	return b.String()
}

// sortCerebrumIDs sorts ID strings numeric-first (so "2" < "10"), falling back
// to lexicographic when a value is non-numeric.
func sortCerebrumIDs(ids []string) {
	sort.SliceStable(ids, func(i, j int) bool { return cmpCerebrumID(ids[i], ids[j]) < 0 })
}

// cmpCerebrumID compares two ID strings numerically when both parse as integers,
// otherwise lexicographically. Returns -1/0/1.
func cmpCerebrumID(a, b string) int {
	ai, aerr := strconv.Atoi(strings.TrimSpace(a))
	bi, berr := strconv.Atoi(strings.TrimSpace(b))
	if aerr == nil && berr == nil {
		switch {
		case ai < bi:
			return -1
		case ai > bi:
			return 1
		default:
			return 0
		}
	}
	return strings.Compare(a, b)
}

// orDash renders an empty (out-of-scope) path as "-" in scope reports.
func orDash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

// cerebrumImportXpoint applies the probel-sw08p-style import trio: a
// crosspoint CSV (`--xpoint`, alias `--csv`; columns dest,srce,levels) as ROUTE
// actions, plus optional src/dst mnemonic CSVs (`--src`/`--dst`; columns
// srce|dest,levels,mnemonic) as SRCE_MNE/DEST_MNE actions. Multi-level rows
// expand to one action per level. `--check` is a pure offline dry-run: it
// prints exactly what would be sent and connects to nothing.
func cerebrumImportXpoint(_ context.Context, args []string) error {
	args = reorderFlagsFirst(args)
	fs := flag.NewFlagSet("cerebrum-nb import", flag.ContinueOnError)
	cf := newCerebrumFlags(fs)
	router := fs.String("router", "0.0.0.0", "router IP target (route-master sentinel 0.0.0.0) or a physical Router IP")
	deviceName := fs.String("device-name", "", "address by DEVICE_NAME instead of --router")
	csvPath := fs.String("csv", "", "alias for --xpoint (kept from the first release)")
	xpointPath := fs.String("xpoint", "", "crosspoint CSV to import (columns dest,srce,levels)")
	srcPath := fs.String("src", "", "source-mnemonic CSV to import (columns srce,mnemonic — router mnemonics are per-ID, not per-level, 0v16 §4.1.5)")
	dstPath := fs.String("dst", "", "dest-mnemonic CSV to import (columns dest,mnemonic — 0v16 §4.1.6)")
	lvlPath := fs.String("levels", "", "level-mnemonic CSV to import (columns level,mnemonic — LEVEL_MNE, 0v16 §4.1.4)")
	catSrcPath := fs.String("cat-src", "", "SRC category CSV (category,type,value — row order = slot order; builds/converges the navigation categories; DEST items rejected)")
	catDstPath := fs.String("cat-dst", "", "DST category CSV (same shape; SOURCE items rejected)")
	catMixedPath := fs.String("cat-mixed", "", "mixed category CSV (same shape; both kinds legal — export writes genuinely mixed-subtree categories here)")
	inDir := fs.String("in-dir", "", "directory holding an export set: <prefix>-xpoint.csv / -src.csv / -dst.csv / -level.csv / -cat-src.csv / -cat-dst.csv (the exact files `export --out-dir` wrote; files absent from the dir are simply out of scope)")
	prefix := fs.String("prefix", "cerebrum", "file prefix used with --in-dir (same default as export)")
	check := fs.Bool("check", false, "dry-run: read live state, report would_change per cell, send nothing")
	allowClear := fs.Bool("allow-clear", false, "an EMPTY cell in a managed label column CLEARS the live label (MNEMONIC=\"\" — clear form live-UNVERIFIED; always --check first)")
	output := fs.String("output", "text", "output format: text|json — json emits the ADR-0007 {changed|would_change, diff[]} shape on stdout (per-action detail goes to stderr)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	jsonOut, oerr := resolveEnsureOutput(*output, false)
	if oerr != nil {
		return oerr
	}
	// In JSON mode stdout carries only the ensure result (Ansible parses it);
	// per-change narration moves to stderr.
	logw := os.Stdout
	if jsonOut {
		logw = os.Stderr
	}
	if *csvPath != "" && *xpointPath != "" {
		return cerebrumValErr("import", "--csv and --xpoint are the same input — pass one")
	}
	// Categories are route-master-only (§5.2/§4.2 rows carry no device
	// addressing) — a physical-router import must not silently converge the
	// RM's categories. Explicit cat flags with a non-RM target are a caller
	// fault; --in-dir auto-resolved cat files are skipped with a warning
	// (the dir may simply hold a full RM export set).
	nonRMTarget := !cerebrumRouterIsRM(*router) || *deviceName != ""
	if nonRMTarget && (*catSrcPath != "" || *catDstPath != "" || *catMixedPath != "") {
		return cerebrumValErr("import", "--cat-src/--cat-dst/--cat-mixed are route-master-only (categories carry no device addressing) — drop them or import against the RM (no --router/--device-name)")
	}
	xp := *xpointPath
	if xp == "" {
		xp = *csvPath
	}
	if *inDir != "" {
		// Mirror export --out-dir: resolve every role not given explicitly to
		// <in-dir>/<prefix>-<role>.csv, keeping only files that exist —
		// missing files stay out of scope (partial-import semantics).
		resolve := func(target *string, suffix string) {
			if *target != "" {
				return
			}
			p := filepath.Join(*inDir, *prefix+suffix)
			if _, err := os.Stat(p); err == nil {
				*target = p
			}
		}
		resolve(&xp, "-xpoint.csv")
		resolve(srcPath, "-src.csv")
		resolve(dstPath, "-dst.csv")
		resolve(lvlPath, "-level.csv")
		if nonRMTarget {
			// The dir may hold a full RM export set — its cat files are out
			// of scope for a physical-router import, never an error.
			if _, err := os.Stat(filepath.Join(*inDir, *prefix+"-cat-src.csv")); err == nil {
				fmt.Fprintf(os.Stderr, "cerebrum-nb import: physical-router target — %s-cat-*.csv in %s skipped (categories are route-master-only)\n", *prefix, *inDir)
			}
		} else {
			resolve(catSrcPath, "-cat-src.csv")
			resolve(catDstPath, "-cat-dst.csv")
			resolve(catMixedPath, "-cat-mixed.csv")
		}
		fmt.Fprintf(os.Stderr, "cerebrum-nb import: --in-dir resolved xpoint=%s src=%s dst=%s levels=%s cat-src=%s cat-dst=%s cat-mixed=%s\n",
			orDash(xp), orDash(*srcPath), orDash(*dstPath), orDash(*lvlPath), orDash(*catSrcPath), orDash(*catDstPath), orDash(*catMixedPath))
	}
	if xp == "" && *srcPath == "" && *dstPath == "" && *lvlPath == "" && *catSrcPath == "" && *catDstPath == "" && *catMixedPath == "" {
		return cerebrumValErr("import", "nothing to import (pass --xpoint / --src / --dst / --levels / --cat-src / --cat-dst / --cat-mixed, or --in-dir DIR --prefix P)")
	}

	// Parse everything up front so a malformed file fails before any wire I/O.
	var routes []routeSpec
	var xpRows int
	if xp != "" {
		data, err := os.ReadFile(xp)
		if err != nil {
			return err
		}
		rows, err := parseCerebrumXpoint(data, xp)
		if err != nil {
			return err
		}
		if len(rows) == 0 {
			return fmt.Errorf("cerebrum-nb import: %s has no crosspoints", xp)
		}
		xpRows = len(rows)
		routes = expandCerebrumXpoint(rows)
	}
	loadMne := func(path, keyCol string) ([]cerebrumMneRow, []int, error) {
		if path == "" {
			return nil, nil, nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, nil, err
		}
		rows, slots, err := parseCerebrumMneCSV(data, keyCol, path)
		if err != nil {
			return nil, nil, err
		}
		if len(rows) == 0 {
			return nil, nil, fmt.Errorf("cerebrum-nb import: %s has no mnemonics", path)
		}
		return rows, slots, nil
	}
	srcRows, srcSlots, err := loadMne(*srcPath, "srce")
	if err != nil {
		return err
	}
	dstRows, dstSlots, err := loadMne(*dstPath, "dest")
	if err != nil {
		return err
	}
	lvlRows, lvlSlots, err := loadMne(*lvlPath, "level")
	if err != nil {
		return err
	}
	loadCat := func(path, kind string) ([]cerebrumCatDef, error) {
		if path == "" {
			return nil, nil
		}
		data, rerr := os.ReadFile(path)
		if rerr != nil {
			return nil, rerr
		}
		return parseCerebrumCatCSV(data, kind, path)
	}
	catSrcDefs, err := loadCat(*catSrcPath, "src")
	if err != nil {
		return err
	}
	catDstDefs, err := loadCat(*catDstPath, "dst")
	if err != nil {
		return err
	}
	catMixedDefs, err := loadCat(*catMixedPath, "mixed")
	if err != nil {
		return err
	}
	catDefs := append(append(append([]cerebrumCatDef{}, catSrcDefs...), catDstDefs...), catMixedDefs...)

	// Ensure semantics (ADR-0007): read live state, diff, converge only the
	// differences. --check reports would_change and sends nothing. Both need
	// the live state, so both need the host.
	rest := fs.Args()
	if len(rest) < 1 {
		return cerebrumValErr("import", "missing host[:port] argument (needed to read live state — ensure semantics)")
	}
	host, portArg, err := splitHostPort(rest[0], cf.port)
	if err != nil {
		return err
	}
	cf.port = portArg

	p, err := cerebrumDial(cf, host)
	if err != nil {
		return err
	}
	defer func() { _ = p.Disconnect() }()
	sess := p.Session()
	target := routeTargetFromFlags(*router, *deviceName)

	st := &cerebrumState{}
	if xp != "" || *srcPath != "" || *dstPath != "" || *lvlPath != "" {
		got, serr := cerebrumObtainState(context.Background(), sess, *router, "ROUTER", 15*time.Second, cerebrumStateWant{
			Routes: xp != "", SrcMne: *srcPath != "", DstMne: *dstPath != "", LvlMne: *lvlPath != "",
			StrictMne: true, Verb: "import",
		})
		if serr != nil {
			return serr
		}
		st = got
	}

	routeChanges := diffCerebrumRoutes(st.Routes, routes)
	srcChanges := diffCerebrumMnemonics("SRCE_MNE", st.Src, srcRows, srcSlots, *allowClear)
	dstChanges := diffCerebrumMnemonics("DEST_MNE", st.Dst, dstRows, dstSlots, *allowClear)
	lvlChanges := diffCerebrumMnemonics("LEVEL_MNE", st.Lvl, lvlRows, lvlSlots, *allowClear)
	mneChanges := append(append(srcChanges, dstChanges...), lvlChanges...)

	// Category leg (ensure): read the live §5.2 grids, diff per slot.
	var catChanges []cerebrumCatChange
	if len(catDefs) > 0 {
		catNames, catDetails, cerr := fetchCerebrumCategories(sess, cf.timeout)
		if cerr != nil {
			return cerr
		}
		exists := map[string]bool{}
		for _, n := range catNames {
			exists[n] = true
		}
		for _, def := range catDefs {
			var live *codec.CategoryDetailsInfo
			if exists[def.Name] {
				if d := catDetails[def.Name]; d != nil {
					live = d
				} else {
					live = &codec.CategoryDetailsInfo{}
				}
			}
			catChanges = append(catChanges, diffCerebrumCategory(def.Name, live, def.Items)...)
		}
	}
	total := len(routeChanges) + len(mneChanges) + len(catChanges)

	// ADR-0007 diff[] — always built (even empty), one entry per converging
	// cell: route.<dest>.<level> or <kind>.<id>.<slot>.
	diffs := make([]ensureDiff, 0, total)
	for _, c := range routeChanges {
		diffs = append(diffs, ensureDiff{Field: fmt.Sprintf("route.%s.%s", c.Dest, c.Level), From: c.From, To: c.To})
	}
	for _, c := range mneChanges {
		diffs = append(diffs, ensureDiff{Field: fmt.Sprintf("%s.%s.%d", strings.ToLower(c.Kind), c.ID, c.Slot), From: c.From, To: c.To})
	}
	for _, c := range catChanges {
		if c.Op == "CREATE" {
			diffs = append(diffs, ensureDiff{Field: "category." + c.Cat, From: "", To: "CREATE"})
			continue
		}
		diffs = append(diffs, ensureDiff{Field: fmt.Sprintf("category.%s.%d", c.Cat, c.Index), From: c.From, To: strings.TrimSpace(c.Type + " " + c.Value)})
	}

	if *check {
		for _, c := range routeChanges {
			_, _ = fmt.Fprintf(logw, "[would-route] dst=%s lvl=%s: %q -> src %s\n", c.Dest, c.Level, c.From, c.To)
		}
		for _, c := range mneChanges {
			_, _ = fmt.Fprintf(logw, "[would-%s] id=%s slot=%d: %q -> %q\n", strings.ToLower(c.Kind), c.ID, c.Slot, c.From, c.To)
		}
		for _, c := range catChanges {
			if c.Op == "CREATE" {
				_, _ = fmt.Fprintf(logw, "[would-category] create %s\n", c.Cat)
				continue
			}
			_, _ = fmt.Fprintf(logw, "[would-category] %s slot %d: %q -> %q\n", c.Cat, c.Index, c.From, strings.TrimSpace(c.Type+" "+c.Value))
		}
		_, _ = fmt.Fprintf(logw, "cerebrum-nb import --check: would_change=%d (%d route(s), %d label(s), %d category change(s)) of %d crosspoint(s)/%d label row(s)/%d categor(ies) desired — nothing sent\n",
			total, len(routeChanges), len(mneChanges), len(catChanges), len(routes), xpRows+len(srcRows)+len(dstRows)+len(lvlRows), len(catDefs))
		if jsonOut {
			wc := total > 0
			return emitEnsure(true, ensureResult{WouldChange: &wc, Diff: diffs})
		}
		return nil
	}

	fails := 0
	for _, c := range routeChanges {
		body := &codec.RoutingAction{
			Type: "ROUTE", IPAddress: *router, DeviceName: *deviceName,
			DeviceType: codec.DeviceType("ROUTER"),
			DestID:     c.Dest, SrceID: c.To, LevelID: c.Level,
		}
		if err := sess.Action(context.Background(), body); err != nil {
			_, _ = fmt.Fprintf(logw, "[route] NACK dst=%s src=%s lvl=%s reason=%s\n", c.Dest, c.To, c.Level, err)
			fails++
			continue
		}
		_, _ = fmt.Fprintf(logw, "[route] OK   dst=%s lvl=%s: %q -> src %s\n", c.Dest, c.Level, c.From, c.To)
	}
	// Router (Routemaster) mnemonics are per-ID (0v16 §4.1.5/§4.1.6: the LEVEL
	// attr is for non-router devices only); alternate sets address by slot
	// index (ALT_MNE=n). Level names go via LEVEL_MNE with the level as target.
	for _, c := range mneChanges {
		var srce, dest, lvl string
		switch c.Kind {
		case "SRCE_MNE":
			srce = c.ID
		case "DEST_MNE":
			dest = c.ID
		case "LEVEL_MNE":
			lvl = c.ID
		}
		actx, cancel := context.WithTimeout(context.Background(), cf.timeout)
		err := sess.SetMnemonic(actx, c.Kind, target, srce, dest, lvl, c.To, altSlotArg(c.Slot))
		cancel()
		if err != nil {
			_, _ = fmt.Fprintf(logw, "[%s] NACK id=%s slot=%d mne=%q reason=%s\n", strings.ToLower(c.Kind), c.ID, c.Slot, c.To, err)
			fails++
			continue
		}
		_, _ = fmt.Fprintf(logw, "[%s] OK   id=%s slot=%d: %q -> %q\n", strings.ToLower(c.Kind), c.ID, c.Slot, c.From, c.To)
	}
	for _, c := range catChanges {
		actx, cancel := context.WithTimeout(context.Background(), cf.timeout)
		var aerr error
		switch c.Op {
		case "CREATE":
			aerr = sess.Category(actx, &codec.CategoryAction{Type: "CREATE", Name: c.Cat})
		default: // MODIFY_ITEM (incl. BLANK clears)
			aerr = sess.Category(actx, &codec.CategoryAction{
				Type: "MODIFY_ITEM", Category: c.Cat,
				Index: strconv.Itoa(c.Index), ItemType: codec.ItemType(c.Type), Value: c.Value,
			})
		}
		cancel()
		if aerr != nil {
			_, _ = fmt.Fprintf(logw, "[category] NACK %s %s slot=%d reason=%s\n", c.Op, c.Cat, c.Index, aerr)
			fails++
			continue
		}
		if c.Op == "CREATE" {
			_, _ = fmt.Fprintf(logw, "[category] OK   CREATE %s\n", c.Cat)
		} else {
			_, _ = fmt.Fprintf(logw, "[category] OK   %s slot %d: %q -> %q\n", c.Cat, c.Index, c.From, strings.TrimSpace(c.Type+" "+c.Value))
		}
	}
	if fails > 0 {
		return fmt.Errorf("cerebrum-nb import: %d action(s) failed", fails)
	}
	_, _ = fmt.Fprintf(logw, "cerebrum-nb import: changed=%d (%d route(s), %d label(s), %d category change(s)); run again to verify 0\n", total, len(routeChanges), len(mneChanges), len(catChanges))
	if jsonOut {
		ch := total > 0
		return emitEnsure(true, ensureResult{Changed: &ch, Diff: diffs})
	}
	return nil
}

// cerebrumExportXpoint reads the router's current state via one-shot OBTAIN
// wildcard requests (0v16 §2.4 — same §5 rows as SUBSCRIBE but no standing
// subscription; §1.6 ends each wildcard with WILDCARD_COMPLETE) and writes
// CSVs that `import` reads back — the northbound analogue of
// `probel-sw08p export`.
//
// Two modes:
//
//	--out FILE            crosspoints only (first-release shape), stdout default
//	--out-dir D --prefix P probel-style set: P-src.csv + P-dst.csv +
//	                       P-level.csv + P-xpoint.csv (adds SRCE_MNE /
//	                       DEST_MNE / LEVEL_MNE OBTAINs)
//
// Router mnemonics are per-ID (0v16: LEVEL on *_MNE rows is "ignored unless
// the DEVICE_TYPE is DEVICE"), so the mnemonic CSVs carry no level column;
// levels get their own file via LEVEL_MNE. Cross-level routes
// (source_level_id != level_id on the RX row) cannot be represented in the
// dest,srce,levels CSV yet: they are counted and reported LOUDLY, never
// silently flattened. Collection stops when every granted OBTAIN delivered
// its WILDCARD_COMPLETE, or after the stream is quiet for --idle.
func cerebrumExportXpoint(ctx context.Context, args []string) error {
	args = reorderFlagsFirst(args)
	fs := flag.NewFlagSet("cerebrum-nb export", flag.ContinueOnError)
	cf := newCerebrumFlags(fs)
	router := fs.String("router", "0.0.0.0", "router IP target (route-master sentinel 0.0.0.0) or a physical Router IP — the matrix analogue of probel --matrix")
	deviceType := fs.String("device-type", "ROUTER", "route-master device type")
	level := fs.String("level", "", "restrict the crosspoint read to one level (probel-style per-level export); empty = all levels")
	out := fs.String("out", "", "crosspoints-only mode: write the xpoint CSV here (default: stdout)")
	outDir := fs.String("out-dir", "", "full-set mode: directory for <prefix>-src.csv / -dst.csv / -level.csv / -xpoint.csv")
	prefix := fs.String("prefix", "cerebrum", "full-set mode: file prefix")
	idle := fs.Duration("idle", 3*time.Second, "stop collecting this long after the last snapshot frame if no WILDCARD_COMPLETE arrives")
	if err := fs.Parse(args); err != nil {
		return err
	}
	trio := *outDir != ""
	if trio && *out != "" {
		return cerebrumValErr("export", "--out and --out-dir are mutually exclusive")
	}
	rest := fs.Args()
	if len(rest) < 1 {
		return cerebrumValErr("export", "missing host[:port] argument")
	}
	host, portArg, err := splitHostPort(rest[0], cf.port)
	if err != nil {
		return err
	}
	cf.port = portArg

	p, err := cerebrumDial(cf, host)
	if err != nil {
		return err
	}
	defer func() { _ = p.Disconnect() }()

	st, err := cerebrumObtainState(ctx, p.Session(), *router, *deviceType, *idle, cerebrumStateWant{
		Routes: true, RouteLevel: *level,
		SrcMne: trio, DstMne: trio, LvlMne: trio,
		Verb: "export",
	})
	if err != nil {
		return err
	}
	if st.CrossLevel > 0 {
		fmt.Fprintf(os.Stderr, "cerebrum-nb export: WARNING — %d cross-level route(s) (src level != dst level) not exported: shuffle representation not yet supported\n", st.CrossLevel)
	}
	snap, srcSnap, dstSnap, lvlSnap := st.Routes, st.Src, st.Dst, st.Lvl

	xpCSV := formatCerebrumXpointCSV(collapseCerebrumRoutes(snap))
	if !trio {
		if *out == "" {
			fmt.Print(xpCSV)
			return nil
		}
		if err := cerebrumWriteFile(*out, []byte(xpCSV)); err != nil {
			return err
		}
		fmt.Fprintf(os.Stderr, "cerebrum-nb export: wrote %d crosspoint row(s) to %s\n", strings.Count(xpCSV, "\n")-1, *out)
		return nil
	}

	if err := os.MkdirAll(*outDir, 0o755); err != nil {
		return err
	}
	srcD, dstD, lvlD := dedupeCerebrumMnes(srcSnap), dedupeCerebrumMnes(dstSnap), dedupeCerebrumMnes(lvlSnap)
	// Same alt_1..alt_N headers on all three files (plant-wide max), so an
	// empty column is fillable on any resource kind.
	nAlt := maxAltSlot(srcD)
	if n := maxAltSlot(dstD); n > nAlt {
		nAlt = n
	}
	if n := maxAltSlot(lvlD); n > nAlt {
		nAlt = n
	}
	files := []struct {
		name, content string
	}{
		{*prefix + "-src.csv", formatCerebrumMneCSVN("srce", srcD, nAlt)},
		{*prefix + "-dst.csv", formatCerebrumMneCSVN("dest", dstD, nAlt)},
		{*prefix + "-level.csv", formatCerebrumMneCSVN("level", lvlD, nAlt)},
		{*prefix + "-xpoint.csv", xpCSV},
	}
	// Category navigation files (§5.2 walk): SRC and DST kept in separate
	// files by owner rule; a category whose subtree carries both kinds is
	// written to both and warned about. Categories are ROUTE-MASTER-ONLY:
	// the §5.2 rows carry no device addressing at all, so a physical-router
	// export must skip them or it would silently write the RM's categories
	// into a router snapshot set.
	if cerebrumRouterIsRM(*router) {
		catNames, catDetails, cerr := fetchCerebrumCategories(p.Session(), cf.timeout)
		if cerr != nil {
			return cerr
		}
		srcPick, dstPick, mixed := classifyCerebrumCategories(catNames, catDetails)
		// Mixed-subtree categories get their OWN file so the src/dst files stay
		// pure (their parsers reject the other kind — round-trip must hold).
		mixedPick := map[string]bool{}
		for _, m := range mixed {
			mixedPick[m] = true
			delete(srcPick, m)
			delete(dstPick, m)
		}
		files = append(files,
			struct{ name, content string }{*prefix + "-cat-src.csv", formatCerebrumCatCSV(cerebrumCatDefsFromLive(catNames, srcPick, catDetails))},
			struct{ name, content string }{*prefix + "-cat-dst.csv", formatCerebrumCatCSV(cerebrumCatDefsFromLive(catNames, dstPick, catDetails))},
		)
		if len(mixed) > 0 {
			fmt.Fprintf(os.Stderr, "cerebrum-nb export: %d categor(ies) carry BOTH source and dest resources in their subtree — written to %s-cat-mixed.csv: %s\n", len(mixed), *prefix, strings.Join(mixed, ", "))
			files = append(files,
				struct{ name, content string }{*prefix + "-cat-mixed.csv", formatCerebrumCatCSV(cerebrumCatDefsFromLive(catNames, mixedPick, catDetails))},
			)
		}
	} else {
		fmt.Fprintf(os.Stderr, "cerebrum-nb export: --router %s is a physical router — category files skipped (categories are route-master-only; IDs in this set are the router's device-native numbering, import back with the same --router)\n", *router)
	}
	for _, f := range files {
		path := filepath.Join(*outDir, f.name)
		if err := os.WriteFile(path, []byte(f.content), 0o644); err != nil {
			return err
		}
		fmt.Fprintf(os.Stderr, "cerebrum-nb export: wrote %s (%d row(s))\n", path, strings.Count(f.content, "\n")-1)
	}
	return nil
}

// cerebrumRouterIsRM reports whether the --router target is the central
// route-master sentinel (§4.1: IP 0.0.0.0) as opposed to a physical
// ROUTER-class device. RM-only legs (categories) key off this.
func cerebrumRouterIsRM(router string) bool {
	return router == "" || router == "0.0.0.0"
}

// cerebrumDial builds a plugin from the common flags and connects (no LOGIN —
// see connectAndLogin). Shared by import/export; the host is already split out.
func cerebrumDial(cf *cerebrumFlags, host string) (*cerebrum.Plugin, error) {
	logger, _, lerr := cf.newLogger()
	if lerr != nil {
		return nil, lerr
	}
	p := cerebrum.NewPlugin(logger)
	p.Username = cf.user
	p.Password = cf.pass
	p.UseTLS = cf.tls
	p.InsecureSkipVerify = cf.insecure
	dialCtx, cancel := context.WithTimeout(context.Background(), cf.timeout)
	defer cancel()
	if err := p.Connect(dialCtx, host, cf.port); err != nil {
		return nil, err
	}
	return p, nil
}
