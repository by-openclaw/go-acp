package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
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

// cerebrumImportXpoint applies a crosspoint CSV (dest,srce,levels) as a stream
// of ROUTE actions — the northbound analogue of `probel-sw08p import --xpoint`.
// Multi-level rows expand to one ROUTE per level. `--check` is a pure offline
// dry-run: it prints exactly what would be sent and connects to nothing.
func cerebrumImportXpoint(_ context.Context, args []string) error {
	args = reorderFlagsFirst(args)
	fs := flag.NewFlagSet("cerebrum-nb import", flag.ContinueOnError)
	cf := newCerebrumFlags(fs)
	router := fs.String("router", "0.0.0.0", "router IP target (route-master sentinel 0.0.0.0) or a physical Router IP")
	deviceName := fs.String("device-name", "", "address by DEVICE_NAME instead of --router")
	csvPath := fs.String("csv", "", "crosspoint CSV to import (columns dest,srce,levels) — required")
	check := fs.Bool("check", false, "dry-run: print the ROUTE actions that would be sent; connect to nothing")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *csvPath == "" {
		return fmt.Errorf("cerebrum-nb import: --csv FILE is required (columns dest,srce,levels)")
	}
	data, err := os.ReadFile(*csvPath)
	if err != nil {
		return err
	}
	rows, err := parseCerebrumXpoint(data, *csvPath)
	if err != nil {
		return err
	}
	routes := expandCerebrumXpoint(rows)
	if len(routes) == 0 {
		return fmt.Errorf("cerebrum-nb import: %s has no crosspoints", *csvPath)
	}

	// --check: offline dry-run, no connection.
	if *check {
		for _, r := range routes {
			fmt.Printf("[would-route] dst=%s src=%s lvl=%s\n", r.Dest, r.Srce, r.Level)
		}
		fmt.Printf("cerebrum-nb import --check: %d crosspoint(s) across %d row(s) — nothing sent\n", len(routes), len(rows))
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
	if fails > 0 {
		return fmt.Errorf("%d/%d crosspoints failed", fails, len(routes))
	}
	fmt.Printf("cerebrum-nb import: applied %d crosspoint(s) from %s\n", len(routes), *csvPath)
	return nil
}

// cerebrumExportXpoint reads the router's current crosspoints by subscribing to
// the ROUTING_CHANGE snapshot (route-master sentinel, DEST/LEVEL wildcards),
// collapses them into multi-level rows, and writes the dest,srce,levels CSV that
// `import` reads back — the northbound analogue of `probel-sw08p export`. It
// stops on the WILDCARD_COMPLETE sentinel, or after the stream is quiet for
// --idle.
func cerebrumExportXpoint(ctx context.Context, args []string) error {
	args = reorderFlagsFirst(args)
	fs := flag.NewFlagSet("cerebrum-nb export", flag.ContinueOnError)
	cf := newCerebrumFlags(fs)
	router := fs.String("router", "0.0.0.0", "router IP target (route-master sentinel 0.0.0.0) or a physical Router IP")
	deviceType := fs.String("device-type", "ROUTER", "route-master device type")
	out := fs.String("out", "", "write the crosspoint CSV here (default: stdout)")
	idle := fs.Duration("idle", 3*time.Second, "stop collecting this long after the last snapshot frame if no WILDCARD_COMPLETE arrives")
	if err := fs.Parse(args); err != nil {
		return err
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
	tick := make(chan struct{}, 1)
	done := make(chan struct{})
	var once sync.Once

	sess.OnEvent(codec.KindRoutingChange, func(f *codec.Frame) {
		rc := f.Routing
		if rc == nil || rc.Type != "ROUTE" || rc.RouteSourceID == "" {
			return
		}
		mu.Lock()
		routes = append(routes, routeSpec{Dest: rc.DestID, Srce: rc.RouteSourceID, Level: rc.LevelID})
		mu.Unlock()
		select {
		case tick <- struct{}{}:
		default:
		}
	})
	sess.OnEvent(codec.KindWildcardComplete, func(*codec.Frame) {
		once.Do(func() { close(done) })
	})

	subCtx, subCancel := context.WithCancel(context.Background())
	defer subCancel()
	item := &codec.RoutingChange{Type: "ROUTE", IPAddress: *router, DeviceType: codec.DeviceType(*deviceType), DestID: "*", LevelID: "*"}
	if err := sess.Subscribe(subCtx, []codec.SubItem{item}); err != nil {
		return fmt.Errorf("cerebrum-nb export: subscribe failed: %w", err)
	}

	// Collect until WILDCARD_COMPLETE, or until the stream goes quiet for --idle.
	idleTimer := time.NewTimer(*idle)
	defer idleTimer.Stop()
collect:
	for {
		select {
		case <-done:
			break collect
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
	mu.Unlock()

	csv := formatCerebrumXpointCSV(collapseCerebrumRoutes(snap))
	if *out == "" {
		fmt.Print(csv)
		return nil
	}
	if err := os.WriteFile(*out, []byte(csv), 0o644); err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "cerebrum-nb export: wrote %d crosspoint row(s) to %s\n", strings.Count(csv, "\n")-1, *out)
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
