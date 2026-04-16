package export

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"acp/internal/protocol"
)

// Ensure the readers satisfy a common shape.
var (
	_ func(io.Reader) (*Snapshot, error) = ReadJSON
	_ func(io.Reader) (*Snapshot, error) = ReadYAML
	_ func(io.Reader) (*Snapshot, error) = ReadCSV
)

// ImportReport collects per-object outcomes from Apply so callers can
// print a summary without guessing.
type ImportReport struct {
	Applied  int
	Skipped  int
	Failed   int
	DryRun   bool
	Failures []string
}

// LoadSnapshot reads a snapshot file from disk and returns the parsed
// Snapshot. Format is auto-detected from the file extension: .json →
// JSON, .yaml/.yml → YAML (currently not supported for import — users
// must re-export to JSON first), anything else → JSON with a warning.
//
// Keeping auto-detection here instead of in the CLI means the API
// server can reuse it directly.
func LoadSnapshot(path string) (*Snapshot, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", path, err)
	}
	defer f.Close()

	ext := strings.ToLower(filepath.Ext(path))
	switch ext {
	case ".yaml", ".yml":
		return ReadYAML(f)
	case ".csv":
		return ReadCSV(f)
	}
	// JSON is the default and recommended import format.
	return ReadJSON(f)
}

// Apply walks the snapshot and calls SetValue on every writable object
// whose persisted value differs from the live device value. Read-only
// objects are always skipped. The live tree is read from the plugin
// via Walk, not from the snapshot, so the comparison is against truth.
//
// dryRun=true logs what WOULD be written and returns without touching
// the device.
func Apply(ctx context.Context, plug protocol.Protocol, s *Snapshot, dryRun bool) (*ImportReport, error) {
	rep := &ImportReport{DryRun: dryRun}

	for _, dump := range s.Slots {
		// Make sure the plugin has a fresh tree for this slot so
		// SetValue can resolve labels and encode values.
		if _, err := plug.Walk(ctx, dump.Slot); err != nil {
			rep.Failures = append(rep.Failures,
				fmt.Sprintf("slot %d walk failed: %v", dump.Slot, err))
			rep.Failed += len(dump.Objects)
			continue
		}

		for _, obj := range dump.Objects {
			if !obj.HasWrite() {
				rep.Skipped++
				continue
			}
			// Skip compound types that need dedicated paths rather
			// than a simple SetValue. Use obj.Kind (set from the
			// "kind" field in every format) rather than obj.Value.Kind
			// which YAML/CSV may leave as KindUnknown for some values.
			if obj.Kind == protocol.KindUnknown ||
				obj.Kind == protocol.KindFrame {
				rep.Skipped++
				continue
			}
			// Also skip sub-group markers — they're section headers,
			// not real values.
			if obj.SubGroupMarker {
				rep.Skipped++
				continue
			}

			req := protocol.ValueRequest{
				Slot:  dump.Slot,
				Label: obj.Label,
			}
			if dryRun {
				rep.Applied++
				continue
			}
			if _, err := plug.SetValue(ctx, req, obj.Value); err != nil {
				rep.Failed++
				rep.Failures = append(rep.Failures,
					fmt.Sprintf("slot %d %s: %v", dump.Slot, obj.Label, err))
				continue
			}
			rep.Applied++
		}
	}
	return rep, nil
}
