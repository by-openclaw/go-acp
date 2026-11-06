package main

// `dhs consumer ccm io-table <host | params.csv>` — the device's inputs
// and outputs as the web UI lists them, input 1 to the last, and every
// routable channel against the index the router addresses it by:
//
//	inputs.csv       one row per IP input: the fields a provisioning
//	                 sheet writes, the SDP read into columns, the first
//	                 and last matrix index of its channels
//	outputs.csv      the same per IP output
//	matrix-src.csv   every source channel by matrix index: kind, owner,
//	                 channel, both UUIDs, the router's labels
//	matrix-dst.csv   every destination channel, the same columns
//
// --names-src / --names-dst take the router's label files — the
// <prefix>-src.csv / -dst.csv every matrix export writes (probel-sw08p
// export, cerebrum-nb export, export --out-dir) — so the router's own
// labels sit beside CCM's names, and a channel CCM gives no index takes
// it from there by name.

import (
	"context"
	"encoding/csv"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	ccmc "dhs/internal/ccm/consumer"
	"dhs/internal/consumer"
	"dhs/internal/export"
)

func runCCMIOTable(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("consumer ccm io-table", flag.ContinueOnError)
	// The session settings every verb shares, at their defaults: none of
	// the ACP1 / Ember+ ones applies here, so they stay off this help.
	cf := addCommonFlags(flag.NewFlagSet(fs.Name(), flag.ContinueOnError))
	fs.DurationVar(&cf.timeout, "timeout", 8*time.Second, "per-request timeout against a device")
	out := fs.String("out", "", "output directory (default: snapshots/ccm/<host>/io-table/, ADR-0028)")
	namesSrc := fs.String("names-src", "", "the router's source label file (<prefix>-src.csv of a matrix export)")
	namesDst := fs.String("names-dst", "", "the router's destination label file (<prefix>-dst.csv)")
	matrix := fs.Int("matrix", 0, "with --names-*: the matrix_id whose rows are read")
	level := fs.Int("level", 0, "with --names-*: the level_id whose rows are read")
	if err := parseVerbFlags(fs, args); err != nil {
		return err
	}
	target := fs.Arg(0)
	if target == "" {
		return fmt.Errorf("usage: dhs consumer ccm io-table <host | params.csv> [--names-src FILE] [--names-dst FILE] [--out DIR]")
	}

	objs, key, err := ioTableObjects(ctx, target, cf)
	if err != nil {
		return err
	}
	src, err := readLabelFile(*namesSrc, *matrix, *level)
	if err != nil {
		return err
	}
	dst, err := readLabelFile(*namesDst, *matrix, *level)
	if err != nil {
		return err
	}
	tables := ccmc.BuildIOTables(objs, src, dst)

	if *out == "" {
		*out = filepath.Join(snapshotDir("ccm", key), "io-table")
	}
	if err := os.MkdirAll(*out, 0o755); err != nil {
		return fmt.Errorf("io-table: %w", err)
	}
	files := []struct {
		name string
		rows [][]string
	}{
		{"inputs.csv", tables.Inputs},
		{"outputs.csv", tables.Outputs},
		{"matrix-src.csv", matrixRecords(tables.Sources)},
		{"matrix-dst.csv", matrixRecords(tables.Destinations)},
	}
	for _, f := range files {
		if err := writeCSVFile(filepath.Join(*out, f.name), f.rows); err != nil {
			return err
		}
		fmt.Printf("  %-15s %6d row(s)\n", f.name, len(f.rows)-1)
	}
	fmt.Printf("io-table %s -> %s\n", target, *out)
	reportIndexGaps(os.Stdout, tables)
	return nil
}

// ioTableObjects reads the tree from a CSV export when the target is a
// file, from the device otherwise.
func ioTableObjects(ctx context.Context, target string, cf *commonFlags) ([]consumer.Object, string, error) {
	if st, err := os.Stat(target); err == nil && !st.IsDir() {
		f, err := os.Open(target)
		if err != nil {
			return nil, "", err
		}
		defer func() { _ = f.Close() }()
		snap, err := export.ReadCSV(f)
		if err != nil {
			return nil, "", fmt.Errorf("io-table: %s: %w", target, err)
		}
		var objs []consumer.Object
		for _, s := range snap.Slots {
			objs = append(objs, s.Objects...)
		}
		key := snap.Device.IP
		if key == "" {
			key = strings.TrimSuffix(filepath.Base(target), filepath.Ext(target))
		}
		return objs, deviceKeyFor("ccm", key), nil
	}

	cf.protocol = "ccm"
	plug, cleanup, err := connect(ctx, target, cf)
	if err != nil {
		return nil, "", err
	}
	defer cleanup()
	if r, ok := plug.(consumer.ReadScoper); ok {
		defer r.ReadScope(ctx)()
	}
	opCtx, cancel := withTimeout(ctx, cf.timeout)
	info, err := plug.GetDeviceInfo(opCtx)
	cancel()
	if err != nil {
		return nil, "", fmt.Errorf("io-table: device info: %w", err)
	}
	var objs []consumer.Object
	for s := 0; s < info.NumSlots; s++ {
		o, err := plug.Walk(ctx, s)
		if err != nil {
			return nil, "", fmt.Errorf("io-table: walk slot %d: %w", s, err)
		}
		objs = append(objs, o...)
	}
	if p, ok := plug.(*ccmc.Plugin); ok {
		for range ccmc.BuildIOTables(objs, nil, nil).NoIndex {
			p.ComplianceProfile().Note(ccmc.NoMatrixIndex)
		}
	}
	return objs, deviceKeyFor("ccm", target), nil
}

// readLabelFile reads a matrix label file: matrix_id, level_id, an id
// column, default_label, then one label_<N> column per width. Rows of
// other matrices and levels are left out. An empty path reads nothing.
func readLabelFile(path string, matrix, level int) (map[int]ccmc.MatrixLabel, error) {
	if path == "" {
		return nil, nil
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	return parseLabelCSV(f, matrix, level)
}

func parseLabelCSV(r io.Reader, matrix, level int) (map[int]ccmc.MatrixLabel, error) {
	cr := csv.NewReader(r)
	cr.FieldsPerRecord = -1
	head, err := cr.Read()
	if err != nil {
		return nil, fmt.Errorf("label file: %w", err)
	}
	col := map[string]int{}
	idCol := -1
	var labelCols []int
	for i, h := range head {
		h = strings.TrimSpace(strings.TrimPrefix(h, "Feff"))
		col[h] = i
		switch {
		case h == "matrix_id" || h == "level_id":
		case strings.HasSuffix(h, "_id") && idCol < 0:
			idCol = i
		case strings.HasPrefix(h, "label_"):
			labelCols = append(labelCols, i)
		}
	}
	if idCol < 0 {
		return nil, errors.New("label file: no id column (src_id / dst_id)")
	}
	field := func(rec []string, i int) string {
		if i >= 0 && i < len(rec) {
			return strings.TrimSpace(rec[i])
		}
		return ""
	}
	at := func(name string) int {
		if i, ok := col[name]; ok {
			return i
		}
		return -1
	}
	out := map[int]ccmc.MatrixLabel{}
	for {
		rec, err := cr.Read()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("label file: %w", err)
		}
		if m := field(rec, at("matrix_id")); m != "" && m != strconv.Itoa(matrix) {
			continue
		}
		if l := field(rec, at("level_id")); l != "" && l != strconv.Itoa(level) {
			continue
		}
		id, err := strconv.Atoi(field(rec, idCol))
		if err != nil {
			continue
		}
		l := ccmc.MatrixLabel{Default: field(rec, at("default_label"))}
		for _, c := range labelCols {
			l.Labels = append(l.Labels, field(rec, c))
		}
		out[id] = l
	}
	return out, nil
}

func matrixRecords(rows []ccmc.MatrixRow) [][]string {
	out := make([][]string, 0, len(rows)+1)
	out = append(out, ccmc.MatrixHeader)
	for _, r := range rows {
		out = append(out, r.Record())
	}
	return out
}

// writeCSVFile writes through a temporary file and renames it into place.
func writeCSVFile(path string, rows [][]string) error {
	tmp := path + ".tmp"
	f, err := os.Create(tmp)
	if err != nil {
		return err
	}
	w := csv.NewWriter(f)
	if err := w.WriteAll(rows); err != nil {
		_ = f.Close()
		return fmt.Errorf("io-table: %s: %w", path, err)
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// reportIndexGaps names the channels the device gave no matrix index,
// and how many of them the label file placed.
func reportIndexGaps(w io.Writer, t *ccmc.IOTables) {
	if len(t.NoIndex) == 0 {
		return
	}
	kinds := map[string]int{}
	var order []string
	for _, n := range t.NoIndex {
		k, _, _ := strings.Cut(n, ": ")
		if kinds[k] == 0 {
			order = append(order, k)
		}
		kinds[k]++
	}
	placed, unplaced := 0, 0
	for _, rows := range [][]ccmc.MatrixRow{t.Sources, t.Destinations} {
		for _, r := range rows {
			switch {
			case r.IndexFrom == "labels":
				placed++
			case r.Index < 0:
				unplaced++
			}
		}
	}
	parts := make([]string, 0, len(order))
	for _, k := range order {
		parts = append(parts, fmt.Sprintf("%s %d", k, kinds[k]))
	}
	_, _ = fmt.Fprintf(w, "deviation %s: %d channel(s) without matrixIndex (%s); %d placed from the label file, %d without an index\n",
		ccmc.NoMatrixIndex, len(t.NoIndex), strings.Join(parts, ", "), placed, unplaced)
}
