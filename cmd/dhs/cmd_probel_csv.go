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
	for _, s := range probelCSVSizes {
		r, rerr := p.AllSourceNames(cctx, mtx, level, s.nl)
		if rerr != nil {
			return fmt.Errorf("all-source-names size %d: %w", s.nl.Bytes(), rerr)
		}
		srcLabels[s.nl] = r.Names
		srcFirst = int(r.FirstSourceID)
	}
	srcPath := filepath.Join(*outDir, *prefix+"-src.csv")
	if err := writeProbelNameCSV(srcPath, "src_id", mtx, level, srcFirst, srcLabels); err != nil {
		return err
	}

	// Destinations — read at each width (matrix-scoped).
	dstLabels := map[codec.NameLength][]string{}
	dstFirst := 0
	for _, s := range probelCSVSizes {
		r, rerr := p.AllDestAssocNames(cctx, mtx, s.nl)
		if rerr != nil {
			return fmt.Errorf("all-dest-names size %d: %w", s.nl.Bytes(), rerr)
		}
		dstLabels[s.nl] = r.Names
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

	fmt.Printf("exported matrix=%d level=%d:\n  %s  (%d sources)\n  %s  (%d dests)\n  %s  (%d crosspoints)\n",
		mtx, level, srcPath, len(srcLabels[codec.NameLen16]), dstPath, len(dstLabels[codec.NameLen16]), xpPath, nXP)
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
		if l := sizeLabels[codec.NameLen16]; i < len(l) {
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
