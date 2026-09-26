package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"dhs/internal/consumer"
)

// The matrix file-set for a connector whose WALK resolved the routing.
//
// Ember+ builds the set from a live matrix surface (MatrixSnapshot, see
// cmd_emberplus_xpoint.go). A REST device has no such surface: its
// crosspoints arrive as ordinary objects, and what each side of a
// crosspoint ADDRESSES was resolved during the walk and attached to the
// object as metadata. That is enough to write the same four files, in
// the same grammar, so an operator reads a Neuron's routing exactly
// like a Probel tally dump:
//
//	<prefix>-matrix.csv   the matrix entity (ADR-0023)
//	<prefix>-xpoint.csv   dest,srce,levels — the tally dump itself
//	<prefix>-dst.csv      dest -> the resource it is, its channel, its kind
//	<prefix>-src.csv      srce -> the same, for the feeding side
//
// Keeping the mapping in -dst.csv / -src.csv rather than on every
// crosspoint row is what makes it readable: a 17 728-crosspoint matrix
// has 17 728 tally rows and one mapping row per endpoint, instead of
// the same resource path repeated on every line.
//
// Nothing here is CCM-specific. Any connector that resolves its
// crosspoints gets the file set for free.

// linkedMatrix is one crosspoint map found in a walked tree.
type linkedMatrix struct {
	// path is the dotted path of the map itself
	// ("matrices.audio.state.main").
	path string
	// rows is one entry per destination, in the order they sort.
	rows []linkedXpoint
}

// linkedXpoint is one crosspoint and both of its resolved sides.
type linkedXpoint struct {
	dest, srce         string
	destRes, srceRes   string
	destChan, srceChan string
	destType, srceType string
}

// linkedMatrices groups every crosspoint object in a walked tree by the
// map it belongs to. A crosspoint object is one that carries a resolved
// side — that is the connector saying "this is a routing entry", and it
// costs no new interface to ask.
func linkedMatrices(objs []consumer.Object) []linkedMatrix {
	byMap := map[string][]linkedXpoint{}
	for _, o := range objs {
		if o.Meta == nil || len(o.Path) < 2 {
			continue
		}
		_, hasTarget := o.Meta[metaLinkTarget]
		_, hasSource := o.Meta[metaLinkSource]
		if !hasTarget && !hasSource {
			continue
		}
		mapPath := strings.Join(o.Path[:len(o.Path)-1], ".")
		byMap[mapPath] = append(byMap[mapPath], linkedXpoint{
			dest:     o.Path[len(o.Path)-1],
			srce:     o.Value.Str,
			destRes:  metaCell(o.Meta, metaLinkTarget),
			srceRes:  metaCell(o.Meta, metaLinkSource),
			destChan: metaCell(o.Meta, metaLinkTargetChan),
			srceChan: metaCell(o.Meta, metaLinkSourceChan),
			destType: metaCell(o.Meta, metaLinkTargetType),
			srceType: metaCell(o.Meta, metaLinkSourceType),
		})
	}
	out := make([]linkedMatrix, 0, len(byMap))
	for path, rows := range byMap {
		sort.SliceStable(rows, func(i, j int) bool {
			if c := cmpCerebrumID(rows[i].dest, rows[j].dest); c != 0 {
				return c < 0
			}
			return cmpCerebrumID(rows[i].srce, rows[j].srce) < 0
		})
		out = append(out, linkedMatrix{path: path, rows: rows})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].path < out[j].path })
	return out
}

// Meta keys a connector attaches to a resolved crosspoint. They match
// the consumer-side names (see internal/ccm/consumer/link.go), because
// one vocabulary for the same fact is the whole point.
const (
	metaLinkTarget     = "target"
	metaLinkTargetChan = "target_channel"
	metaLinkTargetType = "target_type"
	metaLinkSource     = "source"
	metaLinkSourceChan = "source_channel"
	metaLinkSourceType = "source_type"
)

// metaCell renders one metadata value as a CSV cell. A channel number is
// an int on the wire and a number in the file; a missing value is an
// empty cell, never "<nil>".
func metaCell(m map[string]any, key string) string {
	switch v := m[key].(type) {
	case nil:
		return ""
	case string:
		return v
	case int:
		return strconv.Itoa(v)
	case int64:
		return strconv.FormatInt(v, 10)
	case float64:
		if v == float64(int64(v)) {
			return strconv.FormatInt(int64(v), 10)
		}
		return strconv.FormatFloat(v, 'g', -1, 64)
	default:
		return fmt.Sprint(v)
	}
}

// runLinkedMatrixSetExport writes the file set for the device's
// matrices.
//
// Without --path it writes EVERY matrix, each into its own directory
// named after it — video, audio, ancillary, main, backup and current
// alike. A device with eleven matrices takes one command, not eleven,
// and an operator does not have to know what a box has before they can
// read what it is doing. Naming one with --path writes that one, flat,
// which is the shape the levelled protocols use and the shape
// `import --xpoint` reads.
func runLinkedMatrixSetExport(objs []consumer.Object, addr, outDir, prefix, proto, target string) error {
	found := linkedMatrices(objs)
	if len(found) == 0 {
		return fmt.Errorf("%w: no resolved crosspoints in the walked tree — this connector does not link its matrices",
			consumer.ErrValidationFailed)
	}
	if addr != "" {
		var chosen *linkedMatrix
		for i := range found {
			if strings.EqualFold(found[i].path, addr) {
				chosen = &found[i]
				break
			}
		}
		if chosen == nil {
			return fmt.Errorf("%w: matrix %q not found; this device has:\n  %s",
				consumer.ErrValidationFailed, addr, strings.Join(describeMatrices(found), "\n  "))
		}
		if err := writeLinkedMatrixSet(*chosen, outDir, prefix); err != nil {
			return err
		}
		return writePackMeta(outDir, proto, target)
	}

	for _, m := range found {
		dir := filepath.Join(outDir, matrixSlug(m.path))
		if err := writeLinkedMatrixSet(m, dir, prefix); err != nil {
			return err
		}
	}
	fmt.Fprintf(os.Stderr, "export: %d matrix file-set(s) under %s\n", len(found), outDir)
	return writePackMeta(outDir, proto, target)
}

// matrixSlug names the directory one matrix's files go in. The matrix
// path itself, so the directory says which matrix it holds and two
// exports of one device compare directory by directory.
func matrixSlug(path string) string {
	return strings.ReplaceAll(path, ".", "-")
}

// describeMatrices names every matrix and its size, for the error that
// follows a --path nobody on this device answers to.
func describeMatrices(found []linkedMatrix) []string {
	out := make([]string, 0, len(found))
	for _, m := range found {
		out = append(out, fmt.Sprintf("%s [%d crosspoints]", m.path, len(m.rows)))
	}
	return out
}

// writeLinkedMatrixSet writes the four files for one matrix.
func writeLinkedMatrixSet(m linkedMatrix, dir, prefix string) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	files := []struct{ name, content string }{
		{prefix + "-matrix.csv", formatMatrixDescCSV([]matrixDesc{linkedMatrixDesc(m)})},
		{prefix + "-xpoint.csv", formatLinkedXpointCSV(m.rows)},
		{prefix + "-dst.csv", formatLinkedEndpointCSV("dest", m.rows, true)},
		{prefix + "-src.csv", formatLinkedEndpointCSV("srce", m.rows, false)},
	}
	for _, f := range files {
		p := filepath.Join(dir, f.name)
		if err := os.WriteFile(p, []byte(f.content), 0o644); err != nil {
			return err
		}
		fmt.Fprintf(os.Stderr, "export: wrote %s (%d row(s))\n", p, strings.Count(f.content, "\n")-1)
	}
	return nil
}

// linkedMatrixDesc describes the matrix entity per ADR-0023. Each
// destination carries exactly one source on these devices — a route,
// not a mix — which is the 1toN behaviour.
func linkedMatrixDesc(m linkedMatrix) matrixDesc {
	srcs := map[string]bool{}
	for _, r := range m.rows {
		if r.srce != "" {
			srcs[r.srce] = true
		}
	}
	return matrixDesc{
		Matrix:               m.path,
		Behavior:             "1toN",
		Targets:              len(m.rows),
		Sources:              len(srcs),
		MaxConnectsPerTarget: 1,
		Label:                m.path[strings.LastIndex(m.path, ".")+1:],
	}
}

// formatLinkedXpointCSV writes the tally dump: one row per destination,
// in the same dest,srce,levels grammar the levelled protocols use.
// These matrices have no level axis, so the level is 0 throughout —
// stated rather than omitted, so the column means the same thing
// everywhere.
func formatLinkedXpointCSV(rows []linkedXpoint) string {
	var b strings.Builder
	b.WriteString("dest,srce,levels\n")
	for _, r := range rows {
		fmt.Fprintf(&b, "%s,%s,0\n", r.dest, r.srce)
	}
	return b.String()
}

// formatLinkedEndpointCSV writes one side's mapping: the key as the
// crosspoint map spells it, and the resource it addresses.
//
// One row per endpoint, not per crosspoint — a source feeding four
// hundred destinations is described once.
func formatLinkedEndpointCSV(keyCol string, rows []linkedXpoint, destSide bool) string {
	type entry struct{ res, ch, typ string }
	seen := map[string]entry{}
	var keys []string
	for _, r := range rows {
		key, e := r.srce, entry{r.srceRes, r.srceChan, r.srceType}
		if destSide {
			key, e = r.dest, entry{r.destRes, r.destChan, r.destType}
		}
		if key == "" {
			continue
		}
		if _, dup := seen[key]; dup {
			continue
		}
		seen[key] = e
		keys = append(keys, key)
	}
	sortCerebrumIDs(keys)
	var b strings.Builder
	b.WriteString(keyCol + ",resource,channel,type\n")
	for _, k := range keys {
		e := seen[k]
		fmt.Fprintf(&b, "%s,%s,%s,%s\n", k, e.res, e.ch, e.typ)
	}
	return b.String()
}

// ---------------------------------------------------------------------
// import (converge) leg — an export nobody can put back is a report,
// not a backup.
// ---------------------------------------------------------------------

// batchSetter is a connector that can write several values in one
// device operation. Converging a matrix on a REST device means changing
// many entries of ONE resource, and this API has no PATCH: without a
// batch it would be one whole-document PUT per crosspoint, each one
// shipping the entire map and each one able to undo the last.
type batchSetter interface {
	SetValues(ctx context.Context, reqs []consumer.ValueRequest, vals []consumer.Value) ([]consumer.Value, error)
}

// runLinkedXpointImport converges one matrix from a -xpoint.csv,
// against a connector whose crosspoints are ordinary writable objects.
func runLinkedXpointImport(ctx context.Context, plug consumer.Protocol, objs []consumer.Object,
	xpointPath, matrixPath string, check, jsonOut bool) error {

	data, err := os.ReadFile(xpointPath)
	if err != nil {
		return err
	}
	rows, err := parseCerebrumXpoint(data, xpointPath)
	if err != nil {
		return err
	}

	// Which matrix: the descriptor names it. Without one, the device
	// must have exactly one — anything else would be a guess about
	// where to write.
	addr := ""
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
			return fmt.Errorf("%w: %s: exactly one matrix row expected (got %d)",
				consumer.ErrValidationFailed, matrixPath, len(descs))
		}
		addr = descs[0].Matrix
	}

	found := linkedMatrices(objs)
	if len(found) == 0 {
		return fmt.Errorf("%w: no resolved crosspoints on this device — nothing to converge",
			consumer.ErrValidationFailed)
	}
	var live *linkedMatrix
	switch {
	case addr != "":
		for i := range found {
			if strings.EqualFold(found[i].path, addr) {
				live = &found[i]
			}
		}
		if live == nil {
			return fmt.Errorf("%w: %s names matrix %q, which this device does not have:\n  %s",
				consumer.ErrValidationFailed, matrixPath, addr, strings.Join(describeMatrices(found), "\n  "))
		}
	case len(found) == 1:
		live = &found[0]
	default:
		return fmt.Errorf("%w: this device has %d matrices — say which with --matrix <prefix>-matrix.csv:\n  %s",
			consumer.ErrValidationFailed, len(found), strings.Join(describeMatrices(found), "\n  "))
	}

	// What is routed now, by destination.
	current := make(map[string]string, len(live.rows))
	for _, r := range live.rows {
		current[r.dest] = r.srce
	}

	var (
		diffs []ensureDiff
		reqs  []consumer.ValueRequest
		vals  []consumer.Value
	)
	for _, want := range rows {
		from, known := current[want.Dest]
		if !known {
			return fmt.Errorf("%w: %s: this matrix has no destination %q",
				consumer.ErrValidationFailed, xpointPath, want.Dest)
		}
		if from == want.Srce {
			continue
		}
		diffs = append(diffs, ensureDiff{
			Field: fmt.Sprintf("xpoint.%s.%s", live.path, want.Dest),
			From:  from, To: want.Srce,
		})
		reqs = append(reqs, consumer.ValueRequest{Path: live.path + "." + want.Dest})
		vals = append(vals, consumer.Value{Kind: consumer.KindString, Str: want.Srce})
	}
	if diffs == nil {
		diffs = []ensureDiff{}
	}
	changed := len(diffs) > 0

	logw := os.Stdout
	if jsonOut {
		logw = os.Stderr
	}
	if check {
		for _, d := range diffs {
			_, _ = fmt.Fprintf(logw, "[would-xpoint] %s: %q -> %q\n", d.Field, d.From, d.To)
		}
		_, _ = fmt.Fprintf(logw, "import --check: would_change=%d of %d desired row(s) on %s — nothing sent\n",
			len(diffs), len(rows), live.path)
		if jsonOut {
			return emitEnsure(true, ensureResult{WouldChange: &changed, Diff: diffs})
		}
		return nil
	}

	if len(reqs) == 0 {
		_, _ = fmt.Fprintf(logw, "[xpoint] already converged — %s, nothing sent\n", live.path)
	} else if b, ok := plug.(batchSetter); ok {
		// One device operation for the whole matrix.
		if _, err := b.SetValues(ctx, reqs, vals); err != nil {
			return fmt.Errorf("xpoint %s: %w", live.path, err)
		}
		for _, d := range diffs {
			_, _ = fmt.Fprintf(logw, "[xpoint] OK %s: %q -> %q\n", d.Field, d.From, d.To)
		}
	} else {
		for i, req := range reqs {
			if _, err := plug.SetValue(ctx, req, vals[i]); err != nil {
				return fmt.Errorf("xpoint %s: %w", req.Path, err)
			}
			_, _ = fmt.Fprintf(logw, "[xpoint] OK %s: %q -> %q\n", diffs[i].Field, diffs[i].From, diffs[i].To)
		}
	}
	if jsonOut {
		return emitEnsure(true, ensureResult{Changed: &changed, Diff: diffs})
	}
	return nil
}
