package main

import (
	"context"
	"encoding/csv"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"dhs/internal/probel-sw08p/codec"
	probelproto "dhs/internal/probel-sw08p/consumer"
)

// probelCSVSizes is the fixed set of SW-P-08 label widths, one CSV column each.
var probelCSVSizes = []struct {
	col string
	nl  codec.NameLength
}{
	{"label_4", codec.NameLen4},
	{"label_8", codec.NameLen8},
	{"label_12", codec.NameLen12},
	{"label_16", codec.NameLen16},
}

// probelReadNameSizes are the widths the name-RESPONSE commands (cmd 106/107)
// actually return — SW-P-08 §3.3 supports 4/8/12 only. Width 16 is reserved for
// update-name (rx 117) + UMD, so it is NOT readable: the export leaves the
// label_16 column empty for the operator to fill before import (which DOES
// support 16 via update-name).
var probelReadNameSizes = []codec.NameLength{codec.NameLen4, codec.NameLen8, codec.NameLen12}

// runProbelExport writes the router config of one (matrix, level) as three CSV
// files — the recall set the operator edits and re-imports:
//
//	<prefix>-src.csv     matrix_id,level_id,src_id,default_label,label_4,label_8,label_12,label_16
//	<prefix>-dst.csv     matrix_id,level_id,dst_id,default_label,label_4,label_8,label_12,label_16
//	<prefix>-xpoint.csv  matrix_id,level_id,dst_id,src_id        (the crosspoint tally, dst <- src)
//
// Labels are read at all four SW-P-08 widths (one column each); default_label is
// the device's current label at the widest form (16). Source labels are
// (matrix, level)-scoped; dest labels are matrix-scoped (level_id is the run's
// level, for reference). Pass --srcs / --dsts to bound the collection on big
// matrices; otherwise it streams until idle.
func runProbelExport(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("probel-export", flag.ContinueOnError)
	matrix := fs.Int("matrix", 0, "matrix id (0-255)")
	outDir := fs.String("out", ".", "output directory for the three CSV files")
	prefix := fs.String("prefix", "sw08p", "CSV filename prefix")
	timeout := fs.Duration("timeout", 120*time.Second, "overall timeout")
	addr, flagArgs := popPositional(args)
	if addr == "" {
		return fmt.Errorf("missing <host:port>")
	}
	if err := fs.Parse(flagArgs); err != nil {
		return err
	}
	cctx, cancel := context.WithTimeout(ctx, *timeout)
	defer cancel()
	p, closer, err := dialProbel(cctx, addr)
	if err != nil {
		return err
	}
	defer closer()
	mtx, level := probelTarget(p, *matrix)

	if err := os.MkdirAll(*outDir, 0o755); err != nil {
		return fmt.Errorf("mkdir %s: %w", *outDir, err)
	}

	// Sources — read at each width.
	srcLabels := map[codec.NameLength][]string{}
	srcFirst := 0
	for _, nl := range probelReadNameSizes {
		r, rerr := p.AllSourceNames(cctx, mtx, level, nl)
		if rerr != nil {
			return fmt.Errorf("all-source-names size %d: %w", nl.Bytes(), rerr)
		}
		srcLabels[nl] = r.Names
		srcFirst = int(r.FirstSourceID)
	}
	srcPath := filepath.Join(*outDir, *prefix+"-src.csv")
	if err := writeProbelNameCSV(srcPath, "src_id", mtx, level, srcFirst, srcLabels); err != nil {
		return err
	}

	// Destinations — read at each width (matrix-scoped).
	dstLabels := map[codec.NameLength][]string{}
	dstFirst := 0
	for _, nl := range probelReadNameSizes {
		r, rerr := p.AllDestAssocNames(cctx, mtx, nl)
		if rerr != nil {
			return fmt.Errorf("all-dest-names size %d: %w", nl.Bytes(), rerr)
		}
		dstLabels[nl] = r.Names
		dstFirst = int(r.FirstDestAssociationID)
	}
	dstPath := filepath.Join(*outDir, *prefix+"-dst.csv")
	if err := writeProbelNameCSV(dstPath, "dst_id", mtx, level, dstFirst, dstLabels); err != nil {
		return err
	}

	// Crosspoints — the tally (dst <- src).
	res, rerr := p.CrosspointTallyDump(cctx, mtx, level)
	if rerr != nil {
		return fmt.Errorf("tally-dump: %w", rerr)
	}
	xpPath := filepath.Join(*outDir, *prefix+"-xpoint.csv")
	nXP, err := writeProbelXpointCSV(xpPath, mtx, level, res)
	if err != nil {
		return err
	}

	fmt.Printf("exported matrix=%d level=%d:\n  %s  (%d sources)\n  %s  (%d dests)\n  %s  (%d crosspoints)\n  (label_16 left empty — read cmds support 4/8/12 only; fill it for import)\n",
		mtx, level, srcPath, len(srcLabels[codec.NameLen12]), dstPath, len(dstLabels[codec.NameLen12]), xpPath, nXP)
	return nil
}

func trimProbelName(s string) string { return strings.TrimRight(s, "\x00 ") }

// writeProbelNameCSV emits a src/dst label CSV: id + default_label + one column
// per width. Row count is the max across widths (widths should agree).
func writeProbelNameCSV(path, idCol string, mtx, level uint8, first int, sizeLabels map[codec.NameLength][]string) error {
	f, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("create %s: %w", path, err)
	}
	defer func() { _ = f.Close() }()
	w := csv.NewWriter(f)
	defer w.Flush()
	if err := w.Write([]string{"matrix_id", "level_id", idCol, "default_label", "label_4", "label_8", "label_12", "label_16"}); err != nil {
		return err
	}
	n := 0
	for _, s := range probelCSVSizes {
		if len(sizeLabels[s.nl]) > n {
			n = len(sizeLabels[s.nl])
		}
	}
	for i := 0; i < n; i++ {
		def := ""
		if l := sizeLabels[codec.NameLen12]; i < len(l) { // widest READABLE width
			def = trimProbelName(l[i])
		}
		row := []string{strconv.Itoa(int(mtx)), strconv.Itoa(int(level)), strconv.Itoa(first + i), def}
		for _, s := range probelCSVSizes {
			v := ""
			if l := sizeLabels[s.nl]; i < len(l) {
				v = trimProbelName(l[i])
			}
			row = append(row, v)
		}
		if err := w.Write(row); err != nil {
			return err
		}
	}
	return w.Error()
}

// writeProbelXpointCSV emits the crosspoint tally as matrix,level,dst,src
// (dst <- src). Returns the number of crosspoints written.
func writeProbelXpointCSV(path string, mtx, level uint8, res probelproto.TallyDumpResult) (int, error) {
	f, err := os.Create(path)
	if err != nil {
		return 0, fmt.Errorf("create %s: %w", path, err)
	}
	defer func() { _ = f.Close() }()
	w := csv.NewWriter(f)
	defer w.Flush()
	if err := w.Write([]string{"matrix_id", "level_id", "dst_id", "src_id"}); err != nil {
		return 0, err
	}
	row := func(dst, src int) error {
		return w.Write([]string{strconv.Itoa(int(mtx)), strconv.Itoa(int(level)), strconv.Itoa(dst), strconv.Itoa(src)})
	}
	n := 0
	if res.IsWord {
		for i, s := range res.Word.SourceIDs {
			if err := row(int(res.Word.FirstDestinationID)+i, int(s)); err != nil {
				return n, err
			}
			n++
		}
	} else {
		for i, s := range res.Byte.SourceIDs {
			if err := row(int(res.Byte.FirstDestinationID)+i, int(s)); err != nil {
				return n, err
			}
			n++
		}
	}
	return n, w.Error()
}

// labelColumnForSize maps a SW-P-08 width (4|8|12|16) to its column index in the
// src/dst CSV (matrix_id,level_id,id,default_label,label_4,label_8,label_12,label_16).
func labelColumnForSize(size int) (int, error) {
	switch size {
	case 4:
		return 4, nil
	case 8:
		return 5, nil
	case 12:
		return 6, nil
	case 16:
		return 7, nil
	}
	return 0, fmt.Errorf("--size: expected 4|8|12|16, got %d", size)
}

// runProbelImport reads the CSVs written by `export` and applies them. Each file
// is selectable independently — import all or one at a time:
//
//	--src       src labels  (sw08p-src.csv)      via update-name
//	--dst       dst labels  (sw08p-dst.csv)      via update-name
//	--xpoints   crosspoints (sw08p-xpoint.csv)   via connect (RE-ROUTES the matrix)
//
// With no selector, it defaults to labels (src + dst) and skips crosspoints.
// --size picks which label_<N> column drives the labels. --dry-run previews
// without sending.
func runProbelImport(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("probel-import", flag.ContinueOnError)
	inDir := fs.String("in", ".", "directory containing the CSV files")
	prefix := fs.String("prefix", "sw08p", "CSV filename prefix")
	size := fs.Int("size", 8, "which label width column to import: 4 | 8 | 12 | 16")
	dryRun := fs.Bool("dry-run", false, "preview the would-send actions without sending")
	check := fs.Bool("check", false, "dry-run alias (ADR-0007): preview would_change, send nothing")
	output := fs.String("output", "text", "output format: text | json (ADR-0002; xpoints emit the ADR-0007 shape)")
	doSrc := fs.Bool("src", false, "import source labels (<prefix>-src.csv)")
	doDst := fs.Bool("dst", false, "import destination labels (<prefix>-dst.csv)")
	doXpoints := fs.Bool("xpoints", false, "import crosspoints (<prefix>-xpoint.csv; converges the matrix, idempotent)")
	timeout := fs.Duration("timeout", 300*time.Second, "overall timeout")
	addr, flagArgs := popPositional(args)
	if addr == "" {
		return fmt.Errorf("missing <host:port>")
	}
	if err := fs.Parse(flagArgs); err != nil {
		return err
	}
	// No selector → default to labels only (never auto-reroute crosspoints).
	if !*doSrc && !*doDst && !*doXpoints {
		*doSrc, *doDst = true, true
	}
	width, err := parseNameLen(strconv.Itoa(*size))
	if err != nil {
		return err
	}
	col, err := labelColumnForSize(*size)
	if err != nil {
		return err
	}
	jsonOut, oerr := resolveEnsureOutput(*output, false)
	if oerr != nil {
		return oerr
	}
	dry := *dryRun || *check

	cctx, cancel := context.WithTimeout(ctx, *timeout)
	defer cancel()
	p, closer, err := dialProbel(cctx, addr)
	if err != nil {
		return err
	}
	defer closer()

	if *doSrc {
		if err := applyProbelNameCSV(cctx, p, filepath.Join(*inDir, *prefix+"-src.csv"), "source", col, width, dry, jsonOut); err != nil {
			return err
		}
	}
	if *doDst {
		if err := applyProbelNameCSV(cctx, p, filepath.Join(*inDir, *prefix+"-dst.csv"), "dest-assoc", col, width, dry, jsonOut); err != nil {
			return err
		}
	}
	if *doXpoints {
		if err := applyProbelXpointCSV(cctx, p, filepath.Join(*inDir, *prefix+"-xpoint.csv"), dry, jsonOut); err != nil {
			return err
		}
	}
	return nil
}

// readProbelCSV reads a CSV (lenient on field count) and returns all records
// including the header row.
func readProbelCSV(path string) ([][]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", path, err)
	}
	defer func() { _ = f.Close() }()
	r := csv.NewReader(f)
	r.FieldsPerRecord = -1
	return r.ReadAll()
}

type labelItem struct {
	id       int
	label    string
	mtx, lvl uint8
}

// parseLabelItems reads the typed label rows (id from col 2, label from the
// width column), dropping short/non-integer rows.
func parseLabelItems(rows [][]string, col int) []labelItem {
	var items []labelItem
	for _, rec := range rows[1:] {
		if len(rec) <= col {
			continue
		}
		id, e1 := strconv.Atoi(strings.TrimSpace(rec[2]))
		mtx, e2 := strconv.Atoi(strings.TrimSpace(rec[0]))
		lvl, e3 := strconv.Atoi(strings.TrimSpace(rec[1]))
		if e1 != nil || e2 != nil || e3 != nil {
			continue
		}
		items = append(items, labelItem{id: id, label: rec[col], mtx: uint8(mtx), lvl: uint8(lvl)})
	}
	return items
}

// trimLabel strips the trailing space / NUL padding SW-P-08 applies to
// fixed-width name fields, so a read-back name compares equal to a CSV label.
func trimLabel(s string) string { return strings.TrimRight(s, " \x00") }

// applyProbelNameCSV converges one width column of labels to the CSV,
// idempotently (ADR-0007): it reads the current names back (widths 4/8/12) and
// sends update-name only for labels that differ, reporting the ADR-0007
// {changed|would_change, diff[]} shape. Width 16 has no read-back command, so
// those labels are always (re)sent and reported changed — a documented
// carve-out. check=true is a dry-run (no send).
func applyProbelNameCSV(ctx context.Context, p *probelproto.Plugin, path, typ string, col int, width codec.NameLength, check, jsonOut bool) error {
	rows, err := readProbelCSV(path)
	if err != nil {
		return err
	}
	if len(rows) < 2 {
		return nil
	}
	nameType, err := parseUpdateNameType(typ)
	if err != nil {
		return err
	}
	items := parseLabelItems(rows, col)

	// Read current names for the read-back diff. SW-P-08 read commands support
	// 4/8/12-char widths only; width 16 cannot be read back.
	readable := width.Bytes() <= 12
	isSource := typ == "source"
	current := map[[3]int]string{} // {mtx, lvl-or-0, id} -> trimmed name
	if readable {
		done := map[[2]uint8]bool{}
		for _, it := range items {
			k := [2]uint8{it.mtx, it.lvl}
			if !isSource {
				k[1] = 0 // dest names are matrix-scoped, not level-scoped
			}
			if done[k] {
				continue
			}
			done[k] = true
			if isSource {
				r, rerr := p.AllSourceNames(ctx, it.mtx, it.lvl, width)
				if rerr != nil {
					return fmt.Errorf("all-source-names matrix=%d level=%d: %w", it.mtx, it.lvl, rerr)
				}
				for i, n := range r.Names {
					current[[3]int{int(it.mtx), int(it.lvl), int(r.FirstSourceID) + i}] = trimLabel(n)
				}
			} else {
				r, rerr := p.AllDestAssocNames(ctx, it.mtx, width)
				if rerr != nil {
					return fmt.Errorf("all-dest-names matrix=%d: %w", it.mtx, rerr)
				}
				for i, n := range r.Names {
					current[[3]int{int(it.mtx), 0, int(r.FirstDestAssociationID) + i}] = trimLabel(n)
				}
			}
		}
	}

	// Diff: keep only labels that differ from the device.
	changedItems := make([]labelItem, 0, len(items))
	diffs := []ensureDiff{}
	for _, it := range items {
		key := [3]int{int(it.mtx), int(it.lvl), it.id}
		if !isSource {
			key[1] = 0
		}
		want := trimLabel(it.label)
		// An id absent from the read-back is unlabeled on the device (name ""),
		// so map-miss defaults to "". Skipping when cur == want (including both
		// empty) is what makes the whole table idempotent — otherwise the many
		// unlabeled slots (""→"") would re-send on every run.
		cur := current[key]
		if readable && cur == want {
			continue // already set (or both empty) — idempotent skip
		}
		changedItems = append(changedItems, it)
		field := fmt.Sprintf("src.%d.%d.%d", it.mtx, it.lvl, it.id)
		if !isSource {
			field = fmt.Sprintf("dst.%d.%d", it.mtx, it.id)
		}
		from := cur
		if !readable {
			from = "?" // width 16: no read-back
		}
		diffs = append(diffs, ensureDiff{Field: field, From: from, To: want})
	}

	// Apply changed labels, batched into contiguous same-(matrix,level) id runs
	// sized to the SW-P-08 DATA cap. Skipped entirely on --check.
	if !check {
		perFrame := 120 / int(width.Bytes())
		if perFrame < 1 {
			perFrame = 1
		}
		for i := 0; i < len(changedItems); {
			start := changedItems[i]
			names := []string{start.label}
			j := i + 1
			for j < len(changedItems) &&
				changedItems[j].mtx == start.mtx && changedItems[j].lvl == start.lvl &&
				changedItems[j].id == changedItems[j-1].id+1 && len(names) < perFrame {
				names = append(names, changedItems[j].label)
				j++
			}
			if err := p.UpdateNameRequest(ctx, codec.UpdateNameRequestParams{
				NameType: nameType, NameLength: width,
				MatrixID: start.mtx, LevelID: start.lvl,
				FirstID: uint16(start.id), Names: names,
			}); err != nil {
				return fmt.Errorf("update-name %s first=%d: %w", typ, start.id, err)
			}
			i = j
		}
	}

	changed := len(diffs) > 0
	if jsonOut {
		res := ensureResult{Diff: diffs}
		if check {
			res.WouldChange = &changed
		} else {
			res.Changed = &changed
		}
		return emitEnsure(true, res)
	}
	verb := "updated"
	if check {
		verb = "would update"
	}
	note := ""
	if !readable {
		note = " (width 16: no read-back — always sent)"
	}
	fmt.Printf("%s labels: %s %d of %d (width %d)%s from %s\n",
		typ, verb, len(diffs), len(items), width.Bytes(), note, filepath.Base(path))
	return nil
}

// applyProbelXpointCSV connects each dst <- src crosspoint from the CSV.
type xpointRow struct {
	mtx, lvl uint8
	dst, src uint16
}

// parseXpointRows reads the xpoint CSV (skipping the header) into typed rows,
// silently dropping short (<4 field) or non-integer rows — same leniency as the
// original importer.
func parseXpointRows(rows [][]string) []xpointRow {
	var out []xpointRow
	for _, rec := range rows[1:] {
		if len(rec) < 4 {
			continue
		}
		mtx, e1 := strconv.Atoi(strings.TrimSpace(rec[0]))
		lvl, e2 := strconv.Atoi(strings.TrimSpace(rec[1]))
		dst, e3 := strconv.Atoi(strings.TrimSpace(rec[2]))
		src, e4 := strconv.Atoi(strings.TrimSpace(rec[3]))
		if e1 != nil || e2 != nil || e3 != nil || e4 != nil {
			continue
		}
		out = append(out, xpointRow{uint8(mtx), uint8(lvl), uint16(dst), uint16(src)})
	}
	return out
}

// tallyToMap flattens a tally-dump result into dst→src. The dump carries a
// contiguous SourceIDs slice starting at FirstDestinationID.
func tallyToMap(res probelproto.TallyDumpResult) map[uint16]uint16 {
	m := map[uint16]uint16{}
	if res.IsWord {
		first := res.Word.FirstDestinationID
		for i, s := range res.Word.SourceIDs {
			m[first+uint16(i)] = s
		}
		return m
	}
	first := uint16(res.Byte.FirstDestinationID)
	for i, s := range res.Byte.SourceIDs {
		m[first+uint16(i)] = uint16(s)
	}
	return m
}

// xpointDiff decides, for one desired dst←src against the current tally,
// whether the crosspoint needs changing and renders the "from" value ("" when
// the destination is currently unrouted / absent from the tally).
func xpointDiff(current map[uint16]uint16, dst, src uint16) (changed bool, from string) {
	cur, ok := current[dst]
	if ok && cur == src {
		return false, "" // already routed as desired — idempotent skip
	}
	if ok {
		return true, strconv.Itoa(int(cur))
	}
	return true, ""
}

// applyProbelXpointCSV converges the matrix crosspoints to the CSV, idempotently
// (ADR-0007 / ADR-0023): it reads the live tally per (matrix,level) once, sends
// CrosspointConnect only for rows that differ, and reports the ADR-0007
// {changed|would_change, diff[]} shape. check=true is a dry-run (no send).
func applyProbelXpointCSV(ctx context.Context, p *probelproto.Plugin, path string, check, jsonOut bool) error {
	rows, err := readProbelCSV(path)
	if err != nil {
		return err
	}
	parsed := parseXpointRows(rows)

	// Read current tally once per (matrix,level) — never per row (scale).
	current := map[[2]uint8]map[uint16]uint16{}
	for _, r := range parsed {
		key := [2]uint8{r.mtx, r.lvl}
		if _, ok := current[key]; ok {
			continue
		}
		res, terr := p.CrosspointTallyDump(ctx, r.mtx, r.lvl)
		if terr != nil {
			return fmt.Errorf("tally-dump matrix=%d level=%d: %w", r.mtx, r.lvl, terr)
		}
		current[key] = tallyToMap(res)
	}

	diffs := []ensureDiff{}
	for _, r := range parsed {
		changed, from := xpointDiff(current[[2]uint8{r.mtx, r.lvl}], r.dst, r.src)
		if !changed {
			continue
		}
		diffs = append(diffs, ensureDiff{
			Field: fmt.Sprintf("%d.%d.%d", r.mtx, r.lvl, r.dst),
			From:  from,
			To:    strconv.Itoa(int(r.src)),
		})
		if !check {
			if _, err := p.CrosspointConnect(ctx, r.mtx, r.lvl, r.dst, r.src); err != nil {
				return fmt.Errorf("connect dst=%d src=%d: %w", r.dst, r.src, err)
			}
		}
	}

	changed := len(diffs) > 0
	if jsonOut {
		res := ensureResult{Diff: diffs}
		if check {
			res.WouldChange = &changed
		} else {
			res.Changed = &changed
		}
		return emitEnsure(true, res)
	}
	verb := "connected"
	if check {
		verb = "would connect"
	}
	fmt.Printf("crosspoints: %s %d of %d routes (%d already converged) from %s\n",
		verb, len(diffs), len(parsed), len(parsed)-len(diffs), filepath.Base(path))
	return nil
}
