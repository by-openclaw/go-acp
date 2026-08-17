package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	cerebrum "dhs/internal/cerebrum-nb/consumer"
	"dhs/internal/consumer"
	"dhs/internal/export"
)

// cerebrumExportDevice is the DEVICE (Tree/DM) leg of `export` — acp2
// export parity: walk the device's object tree and write the full
// parameter snapshot as ONE file, json | yaml | csv picked by --format
// then the --out extension (default json), stdout when --out is
// omitted. One facet → one file; the multi-file --out-dir set stays
// the Matrix domain's shape (xpoint/src/dst/level/lock facets).
func cerebrumExportDevice(sess *cerebrum.Session, cf *cerebrumFlags, device string, byName bool, subDev string, starts []string, format, out string, maxReq int) error {
	// Format resolution: --format wins; otherwise the --out extension.
	fmtStr := format
	if fmtStr == "" && out != "" {
		switch strings.ToLower(filepath.Ext(out)) {
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

	rows, requests, err := cerebrumWalkSeeds(sess, cf.timeout, device, byName, subDev, starts, maxReq, "export")
	if err != nil {
		return err
	}

	// Device-agnostic object paths (same rows extract persists); the
	// snapshot header carries the device identity + sub-device slot.
	objs := make([]consumer.Object, 0, len(rows))
	for i := range rows {
		objs = append(objs, cerebrum.CanonicalDeviceObject("", "", &rows[i], i))
	}
	slot, _ := strconv.Atoi(subDev)
	snap := &export.Snapshot{
		Device: export.DeviceInfo{
			IP:       strings.TrimSpace(device),
			Protocol: "cerebrum-nb",
		},
		Generator: "dhs consumer cerebrum-nb export --device",
		CreatedAt: time.Now().UTC(),
		Slots: []export.SlotDump{{
			Slot:     slot,
			Status:   consumer.SlotPresent.String(),
			WalkedAt: time.Now().UTC(),
			Objects:  objs,
		}},
	}

	var w io.Writer = os.Stdout
	if out != "" {
		f, ferr := os.Create(out)
		if ferr != nil {
			return fmt.Errorf("create %s: %w", out, ferr)
		}
		defer func() { _ = f.Close() }()
		w = f
	}
	if err := writeCerebrumDeviceSnapshot(w, fmtEnum, snap); err != nil {
		return err
	}
	if out != "" {
		fmt.Fprintf(os.Stderr, "exported %d object(s) from %q sub-device %s in %d obtain(s) to %s (%s)\n",
			len(objs), device, subDev, requests, out, fmtEnum)
	}
	return nil
}

// writeCerebrumDeviceSnapshot dispatches on the parsed format — the
// same three writers the generic export verb uses, so a cerebrum
// device snapshot and an acp2 slot snapshot are the same file shape.
func writeCerebrumDeviceSnapshot(w io.Writer, fmtEnum export.Format, snap *export.Snapshot) error {
	switch fmtEnum {
	case export.FormatYAML:
		return export.WriteYAML(w, snap)
	case export.FormatCSV:
		return export.WriteCSV(w, snap)
	default:
		return export.WriteJSON(w, snap)
	}
}
