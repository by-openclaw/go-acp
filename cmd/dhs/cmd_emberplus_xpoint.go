package main

// Canonical matrix crosspoint export/import for tree protocols whose
// plugin exposes a matrix snapshot + connect surface (Ember+ today).
// Same verbs, same CSV grammar, same ADR-0007 ensure semantics as
// probel-sw08p and cerebrum-nb (#461, ADR-0002/0023):
//
//	export <host> --path <matrix> --out-dir DIR [--prefix P]
//	    <prefix>-matrix.csv   ADR-0023 descriptor (one row per matrix)
//	    <prefix>-xpoint.csv   dest,srce,levels  (levels fixed "0" — no
//	                          levels in tree matrices; grammar parity)
//	    <prefix>-src.csv      source labels, one column per label group
//	    <prefix>-dst.csv      target labels, one column per label group
//	import <host> --xpoint FILE [--matrix FILE] [--check] [--output json]
//	    converge per target honoring the ADR-0023 behavior:
//	      1to1 / 1toN  absolute takes (never toggles)
//	      NtoM         connect the missing, EXPLICITLY disconnect the
//	                   surplus (the file is authoritative per listed
//	                   target; unlisted targets stay untouched)
//
// The wire operations (MatrixConnect / glow ConnOp*) are never exposed
// on the CLI — owner rule: protocol commands stay internal.

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"dhs/internal/consumer"
	"dhs/internal/emberplus/codec/matrix"
)

// matrixXpointConn is the plugin surface the canonical crosspoint legs
// need. Ember+ implements it; future tree protocols opt in by doing the
// same — the generic code never names a protocol.
type matrixXpointConn interface {
	MatrixSnapshot(matrixPath string) []matrix.TargetState
	MatrixConnect(ctx context.Context, matrixPath string, target int32, sources []int32, operation int64) error
}

// glow ConnectionOperation values (spec p.89) — internal only.
const (
	xpOpAbsolute   int64 = 0
	xpOpConnect    int64 = 1
	xpOpDisconnect int64 = 2
)

// matrixDesc is the ADR-0023 matrix entity as carried by -matrix.csv.
type matrixDesc struct {
	Matrix               string // dotted identifier path in the tree
	Behavior             string // 1to1 | 1toN | NtoM | dynamic (ADR-0023)
	Targets              int
	Sources              int
	MaxConnectsPerTarget int // 0 = unlimited / not stated
	MaxTotalConnects     int // 0 = unlimited / not stated
	Label                string
}

// behaviorFromGlowName maps the wire-level matrix type name (walk meta
// "type": oneToN / oneToOne / nToN) onto the ADR-0023 behavior enum, so
// protocol vocabulary never leaks into the files.
func behaviorFromGlowName(name string) string {
	switch strings.ToLower(name) {
	case "onetoone":
		return "1to1"
	case "oneton":
		return "1toN"
	case "nton":
		return "NtoM"
	}
	return "dynamic"
}

// ---------------------------------------------------------------------
// -matrix.csv (ADR-0023 descriptor)
// ---------------------------------------------------------------------

func formatMatrixDescCSV(descs []matrixDesc) string {
	var b strings.Builder
	b.WriteString("matrix,behavior,targets,sources,max_connects_per_target,max_total_connects,label\n")
	for _, d := range descs {
		fmt.Fprintf(&b, "%s,%s,%d,%d,%d,%d,%s\n",
			d.Matrix, d.Behavior, d.Targets, d.Sources,
			d.MaxConnectsPerTarget, d.MaxTotalConnects, d.Label)
	}
	return b.String()
}

func parseMatrixDescCSV(data []byte, srcName string) ([]matrixDesc, error) {
	lines := strings.Split(strings.ReplaceAll(string(data), "\r\n", "\n"), "\n")
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
	for _, col := range []string{"matrix", "behavior"} {
		if _, ok := idx[col]; !ok {
			return nil, fmt.Errorf("%s: missing column %q", srcName, col)
		}
	}
	atoi := func(f []string, col string) int {
		i, ok := idx[col]
		if !ok || i >= len(f) {
			return 0
		}
		n, _ := strconv.Atoi(strings.TrimSpace(f[i]))
		return n
	}
	var out []matrixDesc
	for n, line := range lines[start+1:] {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		f := strings.Split(line, ",")
		if len(f) < 2 {
			return nil, fmt.Errorf("%s line %d: need at least matrix,behavior", srcName, start+n+2)
		}
		d := matrixDesc{
			Matrix:               strings.TrimSpace(f[idx["matrix"]]),
			Behavior:             strings.TrimSpace(f[idx["behavior"]]),
			Targets:              atoi(f, "targets"),
			Sources:              atoi(f, "sources"),
			MaxConnectsPerTarget: atoi(f, "max_connects_per_target"),
			MaxTotalConnects:     atoi(f, "max_total_connects"),
		}
		if i, ok := idx["label"]; ok && i < len(f) {
			d.Label = strings.TrimSpace(f[i])
		}
		switch d.Behavior {
		case "1to1", "1toN", "NtoM", "dynamic":
		default:
			return nil, fmt.Errorf("%s line %d: behavior %q (want 1to1|1toN|NtoM|dynamic per ADR-0023)", srcName, start+n+2, d.Behavior)
		}
		if d.Matrix == "" {
			return nil, fmt.Errorf("%s line %d: empty matrix path", srcName, start+n+2)
		}
		out = append(out, d)
	}
	return out, nil
}

// ---------------------------------------------------------------------
// walk-side helpers (descriptor + labels from the canonical objects)
// ---------------------------------------------------------------------

// findMatrixObject locates the walked matrix object addressed by a
// dotted identifier path OR a numeric OID (same dual addressing as
// every glow verb). A matrix object is recognized by its walk meta
// ("element" == "matrix").
func findMatrixObject(objs []consumer.Object, addr string) (consumer.Object, string, bool) {
	for _, o := range objs {
		if o.Meta == nil || o.Meta["element"] != "matrix" {
			continue
		}
		dotted := strings.Join(o.Path, ".")
		if addr == "" || strings.EqualFold(dotted, addr) || o.OID == addr {
			return o, dotted, true
		}
	}
	return consumer.Object{}, "", false
}

// listMatrixPaths names every matrix in the walked tree — the UX for a
// missing/ambiguous --path.
func listMatrixPaths(objs []consumer.Object) []string {
	var out []string
	for _, o := range objs {
		if o.Meta != nil && o.Meta["element"] == "matrix" {
			out = append(out, strings.Join(o.Path, ".")+" [oid="+o.OID+"]")
		}
	}
	sort.Strings(out)
	return out
}

func metaInt(m map[string]any, key string) int {
	switch v := m[key].(type) {
	case int:
		return v
	case int32:
		return int(v)
	case int64:
		return int(v)
	case float64:
		return int(v)
	}
	return 0
}

func metaString(m map[string]any, key string) string {
	if s, ok := m[key].(string); ok {
		return s
	}
	return ""
}

// descFromObject builds the ADR-0023 descriptor from a walked matrix
// object's meta.
func descFromObject(o consumer.Object, dotted string) matrixDesc {
	return matrixDesc{
		Matrix:               dotted,
		Behavior:             behaviorFromGlowName(metaString(o.Meta, "type")),
		Targets:              metaInt(o.Meta, "targetCount"),
		Sources:              metaInt(o.Meta, "sourceCount"),
		MaxConnectsPerTarget: metaInt(o.Meta, "maximumConnectsPerTarget"),
		MaxTotalConnects:     metaInt(o.Meta, "maximumTotalConnects"),
		Label:                o.Label,
	}
}

// labelGroups extracts the multi-level label sets from walk meta
// ("targetLabels" / "sourceLabels": group → index → label). Returns the
// sorted group names and the per-group maps.
func labelGroups(o consumer.Object, key string) ([]string, map[string]map[string]string) {
	raw, ok := o.Meta[key].(map[string]map[string]string)
	if !ok || len(raw) == 0 {
		return nil, nil
	}
	groups := make([]string, 0, len(raw))
	for g := range raw {
		groups = append(groups, g)
	}
	sort.Strings(groups)
	return groups, raw
}

// formatMatrixLabelCSV renders one label CSV: keyCol ("dest"/"srce"),
// one column per label group (the "many levels" of a tree matrix).
// Rows sort numerically by index; indices come from the union of all
// groups so a hole in one group cannot drop a row.
func formatMatrixLabelCSV(keyCol string, groups []string, byGroup map[string]map[string]string) string {
	seen := map[int]bool{}
	var ids []int
	for _, g := range groups {
		for idx := range byGroup[g] {
			if n, err := strconv.Atoi(idx); err == nil && !seen[n] {
				seen[n] = true
				ids = append(ids, n)
			}
		}
	}
	sort.Ints(ids)
	var b strings.Builder
	b.WriteString(keyCol)
	for _, g := range groups {
		b.WriteByte(',')
		b.WriteString(g)
	}
	b.WriteByte('\n')
	for _, id := range ids {
		b.WriteString(strconv.Itoa(id))
		for _, g := range groups {
			b.WriteByte(',')
			b.WriteString(byGroup[g][strconv.Itoa(id)])
		}
		b.WriteByte('\n')
	}
	return b.String()
}

// ---------------------------------------------------------------------
// export leg
// ---------------------------------------------------------------------

// runMatrixSetExport writes the canonical matrix file set for ONE
// matrix selected by --path/--oid (or the only matrix in the tree),
// plus the pack meta.json (#738).
func runMatrixSetExport(plug consumer.Protocol, objs []consumer.Object, addr, outDir, prefix, proto, target string) error {
	mc, ok := plug.(matrixXpointConn)
	if !ok {
		return fmt.Errorf("%w: this protocol has no matrix surface — --out-dir matrix export unsupported", consumer.ErrValidationFailed)
	}
	obj, dotted, found := findMatrixObject(objs, addr)
	if !found {
		avail := listMatrixPaths(objs)
		if len(avail) == 0 {
			return fmt.Errorf("%w: no matrix in the walked tree", consumer.ErrValidationFailed)
		}
		return fmt.Errorf("%w: matrix %q not found; available:\n  %s", consumer.ErrValidationFailed, addr, strings.Join(avail, "\n  "))
	}
	desc := descFromObject(obj, dotted)

	// Crosspoints from the live snapshot: one row per (target, source),
	// levels fixed "0" (grammar parity with the levelled protocols).
	var rows []cerebrumXpointRow
	for _, ts := range mc.MatrixSnapshot(dotted) {
		for _, src := range ts.Sources {
			rows = append(rows, cerebrumXpointRow{
				Dest:   strconv.Itoa(int(ts.Target)),
				Srce:   strconv.Itoa(int(src)),
				Levels: []string{"0"},
			})
		}
	}
	sort.SliceStable(rows, func(i, j int) bool {
		if c := cmpCerebrumID(rows[i].Dest, rows[j].Dest); c != 0 {
			return c < 0
		}
		return cmpCerebrumID(rows[i].Srce, rows[j].Srce) < 0
	})

	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return err
	}
	files := []struct{ name, content string }{
		{prefix + "-matrix.csv", formatMatrixDescCSV([]matrixDesc{desc})},
		{prefix + "-xpoint.csv", formatCerebrumXpointCSV(rows)},
	}
	if groups, byGroup := labelGroups(obj, "sourceLabels"); groups != nil {
		files = append(files, struct{ name, content string }{prefix + "-src.csv", formatMatrixLabelCSV("srce", groups, byGroup)})
	}
	if groups, byGroup := labelGroups(obj, "targetLabels"); groups != nil {
		files = append(files, struct{ name, content string }{prefix + "-dst.csv", formatMatrixLabelCSV("dest", groups, byGroup)})
	}
	for _, f := range files {
		p := filepath.Join(outDir, f.name)
		if err := os.WriteFile(p, []byte(f.content), 0o644); err != nil {
			return err
		}
		fmt.Fprintf(os.Stderr, "export: wrote %s (%d row(s))\n", p, strings.Count(f.content, "\n")-1)
	}
	return writePackMeta(outDir, proto, target)
}

// ---------------------------------------------------------------------
// import (converge) leg
// ---------------------------------------------------------------------

// xpointChange is one converging crosspoint action.
type xpointChange struct {
	Target  int32
	Sources []int32
	Op      int64  // xpOp*
	From    string // display: current source set
	To      string // display: desired source set
}

// diffMatrixXpoints computes the converge actions for desired rows
// against the live snapshot, honoring the ADR-0023 behavior.
func diffMatrixXpoints(desc matrixDesc, live []matrix.TargetState, rows []cerebrumXpointRow) ([]xpointChange, error) {
	desired := map[int32][]int32{}
	var order []int32
	for _, r := range rows {
		if len(r.Levels) != 1 || r.Levels[0] != "0" {
			return nil, fmt.Errorf("%w: dest %s: levels must be \"0\" for a tree matrix (no levels — ADR-0023)", consumer.ErrValidationFailed, r.Dest)
		}
		t, err := strconv.Atoi(r.Dest)
		if err != nil {
			return nil, fmt.Errorf("%w: dest %q is not a target index", consumer.ErrValidationFailed, r.Dest)
		}
		s, err := strconv.Atoi(r.Srce)
		if err != nil {
			return nil, fmt.Errorf("%w: srce %q is not a source index", consumer.ErrValidationFailed, r.Srce)
		}
		tt := int32(t)
		if _, seen := desired[tt]; !seen {
			order = append(order, tt)
		}
		desired[tt] = append(desired[tt], int32(s))
	}

	// Behavior-level validation before anything is sent (exit 2).
	for t, srcs := range desired {
		switch desc.Behavior {
		case "1to1", "1toN":
			if len(srcs) > 1 {
				return nil, fmt.Errorf("%w: dest %d has %d sources but behavior %s allows one", consumer.ErrValidationFailed, t, len(srcs), desc.Behavior)
			}
		case "NtoM":
			if desc.MaxConnectsPerTarget > 0 && len(srcs) > desc.MaxConnectsPerTarget {
				return nil, fmt.Errorf("%w: dest %d has %d sources > max_connects_per_target %d", consumer.ErrValidationFailed, t, len(srcs), desc.MaxConnectsPerTarget)
			}
		}
	}

	cur := map[int32][]int32{}
	for _, ts := range live {
		cur[ts.Target] = ts.Sources
	}

	joinSrcs := func(s []int32) string {
		parts := make([]string, len(s))
		for i, v := range s {
			parts[i] = strconv.Itoa(int(v))
		}
		return strings.Join(parts, ";")
	}

	var out []xpointChange
	for _, t := range order {
		want := dedupInt32(desired[t])
		have := dedupInt32(cur[t])
		switch desc.Behavior {
		case "NtoM":
			add := subtractInt32(want, have)
			del := subtractInt32(have, want)
			if len(add) > 0 {
				out = append(out, xpointChange{Target: t, Sources: add, Op: xpOpConnect, From: joinSrcs(have), To: joinSrcs(want)})
			}
			if len(del) > 0 {
				out = append(out, xpointChange{Target: t, Sources: del, Op: xpOpDisconnect, From: joinSrcs(have), To: joinSrcs(want)})
			}
		default: // 1to1 / 1toN / dynamic → absolute take when different
			if !equalInt32Sets(want, have) {
				out = append(out, xpointChange{Target: t, Sources: want, Op: xpOpAbsolute, From: joinSrcs(have), To: joinSrcs(want)})
			}
		}
	}
	return out, nil
}

func dedupInt32(in []int32) []int32 {
	seen := map[int32]bool{}
	out := make([]int32, 0, len(in))
	for _, v := range in {
		if !seen[v] {
			seen[v] = true
			out = append(out, v)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

func subtractInt32(a, b []int32) []int32 {
	inB := map[int32]bool{}
	for _, v := range b {
		inB[v] = true
	}
	var out []int32
	for _, v := range a {
		if !inB[v] {
			out = append(out, v)
		}
	}
	return out
}

func equalInt32Sets(a, b []int32) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// runMatrixXpointImport converges one matrix to the xpoint CSV per the
// ADR-0007 contract. Matrix identity + behavior come from -matrix.csv
// when given, else from the live walked descriptor; a behavior mismatch
// between file and live is a validation error.
func runMatrixXpointImport(ctx context.Context, plug consumer.Protocol, objs []consumer.Object, xpointPath, matrixPath string, check, jsonOut bool) error {
	mc, ok := plug.(matrixXpointConn)
	if !ok {
		return fmt.Errorf("%w: this protocol has no matrix surface — --xpoint import unsupported", consumer.ErrValidationFailed)
	}

	data, err := os.ReadFile(xpointPath)
	if err != nil {
		return err
	}
	rows, err := parseCerebrumXpoint(data, xpointPath)
	if err != nil {
		return err
	}

	var fileDesc *matrixDesc
	if matrixPath != "" {
		md, merr := os.ReadFile(matrixPath)
		if merr != nil {
			return merr
		}
		descs, perr := parseMatrixDescCSV(md, matrixPath)
		if perr != nil {
			return perr
		}
		if len(descs) != 1 {
			return fmt.Errorf("%w: %s: exactly one matrix row expected (got %d)", consumer.ErrValidationFailed, matrixPath, len(descs))
		}
		fileDesc = &descs[0]
	}

	addr := ""
	if fileDesc != nil {
		addr = fileDesc.Matrix
	}
	obj, dotted, found := findMatrixObject(objs, addr)
	if !found {
		return fmt.Errorf("%w: matrix %q not found in the walked tree; available:\n  %s", consumer.ErrValidationFailed, addr, strings.Join(listMatrixPaths(objs), "\n  "))
	}
	liveDesc := descFromObject(obj, dotted)
	desc := liveDesc
	if fileDesc != nil {
		if fileDesc.Behavior != liveDesc.Behavior {
			return fmt.Errorf("%w: %s says behavior %s but the live matrix is %s — refusing to converge with the wrong rule", consumer.ErrValidationFailed, matrixPath, fileDesc.Behavior, liveDesc.Behavior)
		}
		desc = *fileDesc
		desc.Matrix = dotted
	}

	changes, err := diffMatrixXpoints(desc, mc.MatrixSnapshot(dotted), rows)
	if err != nil {
		return err
	}

	logw := os.Stdout
	if jsonOut {
		logw = os.Stderr
	}
	diffs := make([]ensureDiff, 0, len(changes))
	for _, c := range changes {
		diffs = append(diffs, ensureDiff{Field: fmt.Sprintf("xpoint.%s.%d", dotted, c.Target), From: c.From, To: c.To})
	}
	changed := len(changes) > 0

	if check {
		for _, c := range changes {
			_, _ = fmt.Fprintf(logw, "[would-xpoint] %s target=%d: %q -> %q\n", dotted, c.Target, c.From, c.To)
		}
		_, _ = fmt.Fprintf(logw, "import --check: would_change=%d of %d desired row(s) on %s (%s) — nothing sent\n", len(changes), len(rows), dotted, desc.Behavior)
		if jsonOut {
			return emitEnsure(true, ensureResult{WouldChange: &changed, Diff: diffs})
		}
		return nil
	}
	for _, c := range changes {
		if err := mc.MatrixConnect(ctx, dotted, c.Target, c.Sources, c.Op); err != nil {
			return fmt.Errorf("xpoint %s target %d: %w", dotted, c.Target, err)
		}
		_, _ = fmt.Fprintf(logw, "[xpoint] OK %s target=%d: %q -> %q\n", dotted, c.Target, c.From, c.To)
	}
	if len(changes) == 0 {
		_, _ = fmt.Fprintf(logw, "[xpoint] already converged — %s (%s), nothing sent\n", dotted, desc.Behavior)
	}
	if jsonOut {
		return emitEnsure(true, ensureResult{Changed: &changed, Diff: diffs})
	}
	return nil
}
