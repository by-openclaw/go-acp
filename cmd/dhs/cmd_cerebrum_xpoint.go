package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
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
	check := fs.Bool("check", false, "dry-run: print the actions that would be sent; connect to nothing")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *csvPath != "" && *xpointPath != "" {
		return fmt.Errorf("cerebrum-nb import: --csv and --xpoint are the same input — pass one")
	}
	xp := *xpointPath
	if xp == "" {
		xp = *csvPath
	}
	if xp == "" && *srcPath == "" && *dstPath == "" && *lvlPath == "" {
		return fmt.Errorf("cerebrum-nb import: nothing to import (pass --xpoint and/or --src/--dst/--levels)")
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
	loadMne := func(path, keyCol string) ([]cerebrumMneRow, error) {
		if path == "" {
			return nil, nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}
		rows, err := parseCerebrumMneCSV(data, keyCol, path)
		if err != nil {
			return nil, err
		}
		if len(rows) == 0 {
			return nil, fmt.Errorf("cerebrum-nb import: %s has no mnemonics", path)
		}
		return rows, nil
	}
	srcRows, err := loadMne(*srcPath, "srce")
	if err != nil {
		return err
	}
	dstRows, err := loadMne(*dstPath, "dest")
	if err != nil {
		return err
	}
	lvlRows, err := loadMne(*lvlPath, "level")
	if err != nil {
		return err
	}

	// --check: offline dry-run, no connection.
	if *check {
		for _, r := range routes {
			fmt.Printf("[would-route]    dst=%s src=%s lvl=%s\n", r.Dest, r.Srce, r.Level)
		}
		for _, m := range srcRows {
			fmt.Printf("[would-srce-mne] src=%s mne=%q\n", m.ID, m.Mnemonic)
		}
		for _, m := range dstRows {
			fmt.Printf("[would-dest-mne] dst=%s mne=%q\n", m.ID, m.Mnemonic)
		}
		for _, m := range lvlRows {
			fmt.Printf("[would-level-mne] lvl=%s mne=%q\n", m.ID, m.Mnemonic)
		}
		fmt.Printf("cerebrum-nb import --check: %d crosspoint(s) across %d row(s), %d src-mne, %d dst-mne, %d level-mne — nothing sent\n",
			len(routes), xpRows, len(srcRows), len(dstRows), len(lvlRows))
		return nil
	}

	rest := fs.Args()
	if len(rest) < 1 {
		return fmt.Errorf("cerebrum-nb import: missing host[:port] argument (or pass --check for an offline dry-run)")
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

	fails := 0
	for _, r := range routes {
		body := &codec.RoutingAction{
			Type: "ROUTE", IPAddress: *router, DeviceName: *deviceName,
			DeviceType: codec.DeviceType("ROUTER"),
			DestID:     r.Dest, SrceID: r.Srce, LevelID: r.Level,
		}
		if err := sess.Action(context.Background(), body); err != nil {
			fmt.Printf("[route] NACK dst=%s src=%s lvl=%s reason=%s\n", r.Dest, r.Srce, r.Level, err)
			fails++
			continue
		}
		fmt.Printf("[route] OK   dst=%s src=%s lvl=%s\n", r.Dest, r.Srce, r.Level)
	}
	// Router (Routemaster) mnemonics are per-ID (0v16 §4.1.5/§4.1.6: the LEVEL
	// attr is for non-router devices only) — one action per row, no LEVEL_ID.
	// Level names themselves go via LEVEL_MNE with the level as the target.
	applyMne := func(kind string, rows []cerebrumMneRow) {
		for _, m := range rows {
			var srce, dest, lvl string
			switch kind {
			case "SRCE_MNE":
				srce = m.ID
			case "DEST_MNE":
				dest = m.ID
			case "LEVEL_MNE":
				lvl = m.ID
			}
			ctx, cancel := context.WithTimeout(context.Background(), cf.timeout)
			err := sess.SetMnemonic(ctx, kind, target, srce, dest, lvl, m.Mnemonic, "")
			cancel()
			if err != nil {
				fmt.Printf("[%s] NACK id=%s mne=%q reason=%s\n", strings.ToLower(kind), m.ID, m.Mnemonic, err)
				fails++
				continue
			}
			fmt.Printf("[%s] OK   id=%s mne=%q\n", strings.ToLower(kind), m.ID, m.Mnemonic)
		}
	}
	applyMne("SRCE_MNE", srcRows)
	applyMne("DEST_MNE", dstRows)
	applyMne("LEVEL_MNE", lvlRows)
	if fails > 0 {
		return fmt.Errorf("cerebrum-nb import: %d action(s) failed", fails)
	}
	fmt.Printf("cerebrum-nb import: applied %d crosspoint(s), %d src-mne, %d dst-mne, %d level-mne\n", len(routes), len(srcRows), len(dstRows), len(lvlRows))
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
		return fmt.Errorf("cerebrum-nb export: --out and --out-dir are mutually exclusive")
	}
	rest := fs.Args()
	if len(rest) < 1 {
		return fmt.Errorf("cerebrum-nb export: missing host[:port] argument")
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

	var mu sync.Mutex
	var routes []routeSpec
	var srcMne, dstMne, lvlMne []cerebrumMneRow
	crossLevel := 0
	completes := 0
	tick := make(chan struct{}, 1)
	kick := func() {
		select {
		case tick <- struct{}{}:
		default:
		}
	}

	sess.OnEvent(codec.KindRoutingChange, func(f *codec.Frame) {
		rc := f.Routing
		if rc == nil {
			return
		}
		mu.Lock()
		switch rc.Type {
		case "ROUTE":
			if rc.RouteSourceID == "" {
				break
			}
			if crossLevelRoute(rc.LevelID, rc.RouteSourceLevelID) {
				crossLevel++
				break
			}
			routes = append(routes, routeSpec{Dest: rc.DestID, Srce: rc.RouteSourceID, Level: rc.LevelID})
		case "SRCE_MNE":
			// Router mnemonics are per-ID; LEVEL_ID on the row is ignored
			// (0v16 §5.1.5) and dedupeCerebrumMnes drops per-level repeats.
			if m := primaryMnemonic(rc); m != "" && rc.SrceID != "" {
				srcMne = append(srcMne, cerebrumMneRow{ID: rc.SrceID, Mnemonic: m})
			}
		case "DEST_MNE":
			if m := primaryMnemonic(rc); m != "" && rc.DestID != "" {
				dstMne = append(dstMne, cerebrumMneRow{ID: rc.DestID, Mnemonic: m})
			}
		case "LEVEL_MNE":
			if m := primaryMnemonic(rc); m != "" && rc.LevelID != "" {
				lvlMne = append(lvlMne, cerebrumMneRow{ID: rc.LevelID, Mnemonic: m})
			}
		}
		mu.Unlock()
		kick()
	})
	sess.OnEvent(codec.KindWildcardComplete, func(*codec.Frame) {
		mu.Lock()
		completes++
		mu.Unlock()
		kick()
	})

	// One-shot OBTAIN per row (0v16 §2.4) — no standing subscription, nothing
	// to unsubscribe. One OBTAIN per item so a NACK on one cannot sink the
	// others (the live server is known to NACK some wildcard rows).
	type obtainItem struct {
		name string
		item codec.SubItem
	}
	routeLevel := *level
	if routeLevel == "" {
		routeLevel = "*"
	}
	plan := []obtainItem{
		{"ROUTE", &codec.RoutingChange{Type: "ROUTE", IPAddress: *router, DeviceType: codec.DeviceType(*deviceType), DestID: "*", LevelID: routeLevel}},
	}
	if trio {
		plan = append(plan,
			obtainItem{"SRCE_MNE", &codec.RoutingChange{Type: "SRCE_MNE", IPAddress: *router, DeviceType: codec.DeviceType(*deviceType), SrceID: "*"}},
			obtainItem{"DEST_MNE", &codec.RoutingChange{Type: "DEST_MNE", IPAddress: *router, DeviceType: codec.DeviceType(*deviceType), DestID: "*"}},
			obtainItem{"LEVEL_MNE", &codec.RoutingChange{Type: "LEVEL_MNE", IPAddress: *router, DeviceType: codec.DeviceType(*deviceType), LevelID: "*"}},
		)
	}
	obtCtx, obtCancel := context.WithCancel(context.Background())
	defer obtCancel()
	okSubs := 0
	for _, s := range plan {
		if err := sess.Obtain(obtCtx, []codec.SubItem{s.item}); err != nil {
			if s.name == "ROUTE" {
				return fmt.Errorf("cerebrum-nb export: ROUTE obtain failed: %w", err)
			}
			fmt.Fprintf(os.Stderr, "cerebrum-nb export: %s obtain refused (%v) — %s CSV will be empty\n", s.name, err, strings.ToLower(s.name))
			continue
		}
		okSubs++
	}

	// Collect until every granted subscription completed, or --idle of quiet.
	idleTimer := time.NewTimer(*idle)
	defer idleTimer.Stop()
collect:
	for {
		mu.Lock()
		doneAll := okSubs > 0 && completes >= okSubs
		mu.Unlock()
		if doneAll {
			break collect
		}
		select {
		case <-tick:
			if !idleTimer.Stop() {
				select {
				case <-idleTimer.C:
				default:
				}
			}
			idleTimer.Reset(*idle)
		case <-idleTimer.C:
			break collect
		case <-ctx.Done():
			break collect
		}
	}

	mu.Lock()
	snap := append([]routeSpec(nil), routes...)
	srcSnap := append([]cerebrumMneRow(nil), srcMne...)
	dstSnap := append([]cerebrumMneRow(nil), dstMne...)
	lvlSnap := append([]cerebrumMneRow(nil), lvlMne...)
	skipped := crossLevel
	mu.Unlock()

	if skipped > 0 {
		fmt.Fprintf(os.Stderr, "cerebrum-nb export: WARNING — %d cross-level route(s) skipped (dest,srce,levels CSV cannot represent SRCE_LEVEL != DEST_LEVEL yet)\n", skipped)
	}

	xpCSV := formatCerebrumXpointCSV(collapseCerebrumRoutes(snap))
	if !trio {
		if *out == "" {
			fmt.Print(xpCSV)
			return nil
		}
		if err := os.WriteFile(*out, []byte(xpCSV), 0o644); err != nil {
			return err
		}
		fmt.Fprintf(os.Stderr, "cerebrum-nb export: wrote %d crosspoint row(s) to %s\n", strings.Count(xpCSV, "\n")-1, *out)
		return nil
	}

	if err := os.MkdirAll(*outDir, 0o755); err != nil {
		return err
	}
	files := []struct {
		name, content string
	}{
		{*prefix + "-src.csv", formatCerebrumMneCSV("srce", dedupeCerebrumMnes(srcSnap))},
		{*prefix + "-dst.csv", formatCerebrumMneCSV("dest", dedupeCerebrumMnes(dstSnap))},
		{*prefix + "-level.csv", formatCerebrumMneCSV("level", dedupeCerebrumMnes(lvlSnap))},
		{*prefix + "-xpoint.csv", xpCSV},
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

// cerebrumDial builds a plugin from the common flags and connects (no LOGIN —
// see connectAndLogin). Shared by import/export; the host is already split out.
func cerebrumDial(cf *cerebrumFlags, host string) (*cerebrum.Plugin, error) {
	logLevel := slog.LevelInfo
	if cf.debug {
		logLevel = slog.LevelDebug
	}
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: logLevel}))
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
