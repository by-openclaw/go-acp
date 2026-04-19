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
	Applied int
	Skipped int
	Failed  int
	// Filtered is the count of objects excluded before Apply by an
	// ImportFilter (e.g. --id / --label / --path flags). Different
	// from Skipped — filtered objects were never considered for apply;
	// skipped objects were considered and rejected by policy (read-only
	// / unknown-kind / marker). Populated by the caller after
	// ApplyFilter runs; Apply itself does not set this field.
	Filtered int
	DryRun   bool
	Failures []string
	// Skips lists every object that the importer deliberately did not
	// attempt, each with a one-word reason: "read_only", "container"
	// (node with no scalar value), "marker" (sub-group header), or
	// "unknown_kind" (compound type with no writer path). Populated so
	// dry-run can show the operator exactly what will not be applied
	// and why. A single line per skip, slot-qualified.
	Skips []SkipRecord
}

// SkipRecord is one rejected-at-client row. Small and printable.
type SkipRecord struct {
	Slot   int
	ID     int
	Label  string
	Path   string
	Kind   string
	Access string
	Reason string
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
	defer func() { _ = f.Close() }()

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
				rep.Skips = append(rep.Skips, skipFrom(dump.Slot, obj, "read_only"))
				continue
			}
			// Skip compound types that need dedicated paths rather
			// than a simple SetValue. Use obj.Kind (set from the
			// "kind" field in every format) rather than obj.Value.Kind
			// which YAML/CSV may leave as KindUnknown for some values.
			if obj.Kind == protocol.KindUnknown ||
				obj.Kind == protocol.KindFrame {
				rep.Skipped++
				rep.Skips = append(rep.Skips, skipFrom(dump.Slot, obj, "unknown_kind"))
				continue
			}
			// Also skip sub-group markers — they're section headers,
			// not real values.
			if obj.SubGroupMarker {
				rep.Skipped++
				rep.Skips = append(rep.Skips, skipFrom(dump.Slot, obj, "marker"))
				continue
			}

			// Per-protocol resolution — the plugins each accept a
			// different subset of ValueRequest fields:
			//   acp1      : (Group + ID) or (Group + Label). Labels
			//               are unique within a group.
			//   acp2      : ID (globally unique u32 obj-id). Labels
			//               collide across sub-nodes so are unsafe.
			//   emberplus : Path (dotted OID preferred) via numIndex.
			// Populating unused fields is harmless; what matters is
			// that *at least one* unique key is set. CSV round-trip
			// (issue #38) carries oid + path + id + label so every
			// protocol gets its unambiguous key back.
			req := protocol.ValueRequest{Slot: dump.Slot}
			switch s.Device.Protocol {
			case "acp1":
				req.Group = obj.Group
				if req.Group == "" && len(obj.Path) > 0 {
					req.Group = obj.Path[0]
				}
				req.ID = obj.ID
				req.Label = obj.Label
			case "acp2":
				req.ID = obj.ID
			case "emberplus":
				switch {
				case obj.OID != "":
					req.Path = obj.OID
				case len(obj.Path) > 0:
					req.Path = strings.Join(obj.Path, ".")
				default:
					req.Label = obj.Label
				}
			default:
				// Unknown protocol — set every field we have and hope
				// the plugin's resolver picks one.
				req.Group = obj.Group
				req.ID = obj.ID
				req.Label = obj.Label
				if obj.OID != "" {
					req.Path = obj.OID
				} else if len(obj.Path) > 0 {
					req.Path = strings.Join(obj.Path, ".")
				}
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

// skipFrom builds a SkipRecord describing one row the importer chose
// not to attempt. Reason is a short one-word code ("read_only" /
// "unknown_kind" / "marker") so the CLI can group the report by cause.
func skipFrom(slot int, obj protocol.Object, reason string) SkipRecord {
	path := obj.Label
	if len(obj.Path) > 0 {
		path = strings.Join(obj.Path, ".")
	}
	kind := "unknown"
	if obj.Kind != protocol.KindUnknown {
		kind = obj.Kind.String()
	}
	access := "R--"
	if obj.HasWrite() {
		access = "RW-"
	}
	return SkipRecord{
		Slot:   slot,
		ID:     obj.ID,
		Label:  obj.Label,
		Path:   path,
		Kind:   kind,
		Access: access,
		Reason: reason,
	}
}
