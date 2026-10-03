package export

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"dhs/internal/consumer"
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
	// Unchanged counts the rows whose value the device already had: read
	// before any write and left alone, so applying the same file twice
	// writes nothing the second time. They are also listed in Skips with
	// the reason "unchanged".
	Unchanged int
	Skipped   int
	Failed    int
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

// walkNeeded switches on a per-slot pre-walk before any SetValue. No
// protocol needs it any more: ACP2 + EmberPlus have per-object meta
// fetch since v0.10.0 and ACP1 gained the same pattern in #421, so
// SetValue resolves type metadata per row via a single getObject on
// cache miss (~ms vs walking the slot). Kept as a package var rather
// than deleted so the walk-failure accounting stays exercised by a
// test and a future protocol can flip it without re-deriving it.
var walkNeeded = false

// Apply walks the snapshot and calls SetValue on every writable object
// whose persisted value differs from the live device value. Read-only
// objects are always skipped. The live tree is read from the plugin
// via Walk, not from the snapshot, so the comparison is against truth.
//
// dryRun=true logs what WOULD be written and returns without touching
// the device.
func Apply(ctx context.Context, plug consumer.Protocol, s *Snapshot, dryRun bool) (*ImportReport, error) {
	rep := &ImportReport{DryRun: dryRun}

	// Optional offline pre-flight check. Plugins that can validate a
	// (req, val) pair without a wire send implement ValueValidator;
	// see internal/consumer/value_validator.go. ACP2 does — uses a
	// single get_object to catch phantom obj-ids before SetValue, and
	// rejects enum values outside the options list.
	validator, _ := plug.(consumer.ValueValidator)
	var pending []write

	for _, dump := range s.Slots {
		if walkNeeded {
			if _, err := plug.Walk(ctx, dump.Slot); err != nil {
				rep.Failures = append(rep.Failures,
					fmt.Sprintf("slot %d walk failed: %v", dump.Slot, err))
				rep.Failed += len(dump.Objects)
				continue
			}
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
			if obj.Kind == consumer.KindUnknown ||
				obj.Kind == consumer.KindFrame {
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
			req := consumer.ValueRequest{Slot: dump.Slot}
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
			// Pre-flight Validate when the plugin supports it. Catches
			// phantom obj-id / enum-out-of-options / type-mismatch
			// before any SetValue is attempted — applies to both
			// dry-run and real apply.
			if validator != nil {
				if vErr := validator.ValidateValue(ctx, req, obj.Value); vErr != nil {
					reason := "validation_failed"
					if errors.Is(vErr, consumer.ErrObjectNotFound) {
						reason = "not_found"
					}
					rep.Skipped++
					rep.Skips = append(rep.Skips, skipFrom(dump.Slot, obj, reason))
					continue
				}
			}
			// Read before write: a row the device already holds is not
			// sent. That is what makes a values file something a play can
			// apply on every run — the second run changes nothing — and
			// what lets --check say how far the device is from the file.
			// A value that cannot be read is written as before: the write
			// is what the operator asked for, the read only spares it.
			if live, gerr := plug.GetValue(ctx, req); gerr == nil && sameValue(live, obj.Value) {
				rep.Unchanged++
				rep.Skips = append(rep.Skips, skipFrom(dump.Slot, obj, "unchanged"))
				continue
			}
			if dryRun {
				rep.Applied++
				continue
			}
			pending = append(pending, write{req, obj, dump.Slot})
		}
		writeAll(ctx, plug, pending, rep)
		pending = pending[:0]
	}
	return rep, nil
}

// write is one row that is to be sent.
type write struct {
	req  consumer.ValueRequest
	obj  consumer.Object
	slot int
}

// writeAll sends a slot's rows: in one batch where the plugin can take
// one, so fields that only make sense together reach the device
// together (a static address and its gateway in one document), and one
// by one otherwise — or when the batch was refused, so that the report
// still names the row the device said no to.
func writeAll(ctx context.Context, plug consumer.Protocol, rows []write, rep *ImportReport) {
	if len(rows) == 0 {
		return
	}
	if b, ok := plug.(consumer.BatchSetter); ok {
		reqs := make([]consumer.ValueRequest, len(rows))
		vals := make([]consumer.Value, len(rows))
		for i, w := range rows {
			reqs[i], vals[i] = w.req, w.obj.Value
		}
		if _, err := b.SetValues(ctx, reqs, vals); err == nil {
			rep.Applied += len(rows)
			return
		}
	}
	for _, w := range rows {
		if _, err := plug.SetValue(ctx, w.req, w.obj.Value); err != nil {
			rep.Failed++
			rep.Failures = append(rep.Failures,
				fmt.Sprintf("slot %d %s: %v", w.slot, w.obj.Label, err))
			continue
		}
		rep.Applied++
	}
}

// skipFrom builds a SkipRecord describing one row the importer chose
// not to attempt. Reason is a short one-word code ("read_only" /
// "unknown_kind" / "marker") so the CLI can group the report by cause.
func skipFrom(slot int, obj consumer.Object, reason string) SkipRecord {
	path := obj.Label
	if len(obj.Path) > 0 {
		path = strings.Join(obj.Path, ".")
	}
	kind := "unknown"
	if obj.Kind != consumer.KindUnknown {
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

// sameValue reports whether the device's value is the one the file
// asks for. Kinds that agree are compared on the field the kind
// selects; kinds that do not — a CSV row typed by its own column against
// what the plugin decoded — are compared as the text an operator reads.
func sameValue(live, want consumer.Value) bool {
	if live.Kind == want.Kind {
		switch live.Kind {
		case consumer.KindBool:
			return live.Bool == want.Bool
		case consumer.KindInt:
			return live.Int == want.Int
		case consumer.KindUint:
			return live.Uint == want.Uint
		case consumer.KindFloat:
			return live.Float == want.Float
		case consumer.KindEnum:
			return live.Enum == want.Enum
		case consumer.KindString:
			return live.Str == want.Str
		case consumer.KindIPAddr:
			return live.IPAddr == want.IPAddr
		}
	}
	return valueText(live) == valueText(want)
}

// valueText is the one rendering two differently typed values are
// compared through.
func valueText(v consumer.Value) string {
	switch v.Kind {
	case consumer.KindBool:
		return strconv.FormatBool(v.Bool)
	case consumer.KindInt:
		return strconv.FormatInt(v.Int, 10)
	case consumer.KindUint:
		return strconv.FormatUint(v.Uint, 10)
	case consumer.KindFloat:
		return strconv.FormatFloat(v.Float, 'g', -1, 64)
	case consumer.KindEnum:
		return strconv.Itoa(int(v.Enum))
	case consumer.KindString:
		return v.Str
	case consumer.KindIPAddr:
		return net.IP(v.IPAddr[:]).String()
	}
	return string(v.Raw)
}
