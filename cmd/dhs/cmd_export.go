package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"dhs/internal/export"
	"dhs/internal/consumer"
)

// runExport walks every present slot on the device and writes the
// snapshot to disk in json / yaml / csv form. Stream to stdout when
// --out is omitted. Format is derived from the --format flag first,
// the --out filename extension second, defaulting to json.
func runExport(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("export", flag.ExitOnError)
	fs.Usage = verbUsageFn(fs, helpExport) // #751 G5: -h = rich help + all flags
	cf := addCommonFlags(fs)
	format := fs.String("format", "", "output format: json | yaml | csv (default: json or from --out extension)")
	out := fs.String("out", "", "output file path (default: stdout)")
	slot := fs.Int("slot", -1, "export only this slot (-1 = all present slots)")
	pathFlag := fs.String("path", "", "filter objects by path prefix (e.g. BOARD, PSU/1); with --out-dir: the matrix to export (dotted path or numeric OID)")
	outDir := fs.String("out-dir", "", "canonical matrix file-set mode (#461): write <prefix>-matrix.csv / -xpoint.csv / -src.csv / -dst.csv for the matrix named by --path (same grammar as probel-sw08p / cerebrum-nb)")
	prefix := fs.String("prefix", "matrix", "file prefix used with --out-dir")
	host, rest, err := popHost(args)
	if err != nil {
		return fmt.Errorf("usage: dhs consumer <proto> export <host> [--format json|yaml|csv] [--out FILE | --out -] [--slot N] [--path SEG.SEG] [--out-dir DIR --prefix P]")
	}
	_ = parseVerbFlags(fs, rest)

	// ADR-0028 default home: --out omitted → snapshots/<proto>/<host>/
	// params.<fmt> (deterministic, never a cwd surprise). --out "-"
	// keeps the explicit stdout stream.
	stdoutOut := *out == "-"
	if stdoutOut {
		*out = ""
	}
	if !stdoutOut && *out == "" && *outDir == "" {
		ext := "json"
		switch *format {
		case "yaml", "yml":
			ext = "yaml"
		case "csv":
			ext = "csv"
		}
		*out = filepath.Join(snapshotDir(cf.protocol, host), "params."+ext)
		fmt.Fprintf(os.Stderr, "export: default snapshot file %s (ADR-0028)\n", *out)
	}

	// Format resolution: --format wins; otherwise guess from --out extension.
	fmtStr := *format
	if fmtStr == "" && *out != "" {
		ext := strings.ToLower(filepath.Ext(*out))
		switch ext {
		case ".yaml", ".yml":
			fmtStr = "yaml"
		case ".csv":
			fmtStr = "csv"
		default:
			fmtStr = "json"
		}
	}
	fmtEnum, err := export.ParseFormat(fmtStr)
	if err != nil {
		return err
	}

	plug, cleanup, err := connect(ctx, host, cf)
	if err != nil {
		return err
	}
	defer cleanup()

	opCtx, cancel := withTimeout(ctx, cf.timeout)
	defer cancel()

	// Build the snapshot: walk every present slot and copy its objects.
	info, err := plug.GetDeviceInfo(opCtx)
	if err != nil {
		return fmt.Errorf("device info: %w", err)
	}
	snap := &export.Snapshot{
		Device: export.DeviceInfo{
			IP:              info.IP,
			Port:            info.Port,
			Protocol:        cf.protocol,
			ProtocolVersion: info.ProtocolVersion,
			NumSlots:        info.NumSlots,
		},
		Generator: "acp " + version,
		CreatedAt: time.Now().UTC(),
	}
	// Canonical matrix file-set mode (#461): walk, then write the
	// -matrix/-xpoint/-src/-dst CSV set for the selected matrix instead
	// of a snapshot. Same verbs + grammar as the levelled protocols.
	if *outDir != "" {
		var all []consumer.Object
		for s := 0; s < info.NumSlots; s++ {
			objs, werr := plug.Walk(ctx, s)
			if werr != nil {
				continue
			}
			all = append(all, objs...)
		}
		return runMatrixSetExport(plug, all, *pathFlag, *outDir, *prefix, cf.protocol, host)
	}

	for s := 0; s < info.NumSlots; s++ {
		if *slot >= 0 && s != *slot {
			continue
		}
		si, serr := plug.GetSlotInfo(opCtx, s)
		if serr != nil {
			continue
		}
		if si.Status != consumer.SlotPresent {
			continue
		}
		objs, werr := plug.Walk(ctx, s)
		if werr != nil {
			fmt.Fprintf(os.Stderr, "warning: slot %d walk failed: %v\n", s, werr)
			continue
		}
		// Apply --path filter if set.
		if *pathFlag != "" {
			pathSegs := strings.Split(*pathFlag, ".")
			objs = filterByPath(objs, pathSegs)
		}
		snap.Slots = append(snap.Slots, export.SlotDump{
			Slot:     s,
			Status:   si.Status.String(),
			WalkedAt: time.Now().UTC(),
			Objects:  objs,
		})
	}

	// Pick the output writer: file or stdout.
	var w io.Writer = os.Stdout
	if *out != "" {
		if dir := filepath.Dir(*out); dir != "." {
			if err := os.MkdirAll(dir, 0o755); err != nil {
				return fmt.Errorf("create %s: %w", dir, err)
			}
		}
		f, ferr := os.Create(*out)
		if ferr != nil {
			return fmt.Errorf("create %s: %w", *out, ferr)
		}
		defer func() { _ = f.Close() }()
		w = f
	}

	switch fmtEnum {
	case export.FormatJSON:
		if err := export.WriteJSON(w, snap); err != nil {
			return err
		}
	case export.FormatYAML:
		if err := export.WriteYAML(w, snap); err != nil {
			return err
		}
	case export.FormatCSV:
		if err := export.WriteCSV(w, snap); err != nil {
			return err
		}
	}

	if *out != "" {
		fmt.Fprintf(os.Stderr, "exported %d slots to %s (%s)\n",
			len(snap.Slots), *out, fmtEnum)
	}
	return nil
}
