package cerebrumnb

import (
	"context"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"dhs/internal/cerebrum-nb/codec"
	"dhs/internal/consumer"
	"dhs/internal/export"
	"dhs/internal/wiretrace"
)

// Compile-time assertion: the cerebrum-nb Plugin satisfies
// consumer.Validator so `dhs consumer cerebrum-nb validate
// <frames.jsonl>` resolves at the CLI type-assert. Closes the
// cerebrum half of the #243 residual (D2 unit a, #700).
var _ consumer.Validator = (*Plugin)(nil)

// Validate decodes captured Cerebrum NB wire-trace records (Trames per
// ADR-0021 — each the full XML document of one ws text message, as
// --capture records them) through the stdlib codec, offline. A capture
// against a real Cerebrum becomes a committed decoder oracle.
//
// Invariants surfaced per document:
//   - cerebrum_case_normalized — the decoder case-folded a non-UPPERCASE
//     document (spec-vs-wire case deviation);
//   - nack <ERROR> (<code>) — refusals in the capture, so a dispute
//     review sees every server rejection at a glance.
//
// --out-tree aggregates the OBSERVED DEVICE OBJECTS (§5.4.3 VALUE rows;
// last value wins per DEVICE.SUB.OBJECT path) into a canonical snapshot
// tree.json — the DEVICE-domain (Tree/DM) view of a capture. Routing /
// category / salvo rows are counted but not tree'd: that state belongs
// to the Matrix-template CSVs (export/import), not the object tree.
// --out-params emits the same rows flat (.csv by extension, else JSON).
func (p *Plugin) Validate(ctx context.Context, trames []wiretrace.Trame, opts consumer.ValidateOpts) (*consumer.ValidateReport, error) {
	report := &consumer.ValidateReport{
		PerDirection: map[wiretrace.Direction]int{},
	}

	collect := opts.OutTree != "" || opts.OutParams != ""
	objects := map[string]consumer.Object{}
	var order []string

	record := func(devName, sub string, ov *codec.DeviceObjectValue) {
		if !collect || ov.Object == "" {
			return
		}
		key := devName + "." + sub + "." + ov.Object
		if _, seen := objects[key]; !seen {
			order = append(order, key)
		}
		segs := append([]string{devName, sub}, strings.Split(ov.Object, ".")...)
		obj := consumer.Object{
			ID:     len(order) - 1,
			Path:   segs,
			Label:  ov.Object,
			Access: deviceAccessBits(ov),
			Unit:   ov.Units,
			Meta: map[string]any{
				"data_type": ov.DataType,
				"available": ov.Available,
			},
		}
		if o, seen := objects[key]; seen {
			obj.ID = o.ID // first-seen ID stays stable
		}
		v := deviceValueToCanonical(ov)
		obj.Kind, obj.Value = v.Kind, v
		if len(ov.EnumList) > 0 {
			obj.EnumItems = ov.EnumList
		}
		objects[key] = obj
	}

	for i, t := range trames {
		if err := ctx.Err(); err != nil {
			return report, err
		}
		raw, err := hex.DecodeString(t.Hex)
		if err != nil {
			report.Errors = append(report.Errors, consumer.ValidateError{
				TrameIndex: i,
				Direction:  t.Direction,
				Err:        fmt.Sprintf("hex decode: %v", err),
			})
			continue
		}
		f, err := codec.Decode(raw)
		if err != nil {
			report.Errors = append(report.Errors, consumer.ValidateError{
				TrameIndex: i,
				Direction:  t.Direction,
				HexPrefix:  cerebrumShortHex(raw),
				Err:        fmt.Sprintf("cerebrum decode: %v", err),
			})
			continue
		}

		report.TramesProcessed++
		report.PerDirection[t.Direction]++

		if f.CaseChanged {
			report.Invariants = append(report.Invariants,
				fmt.Sprintf("trame %d: cerebrum_case_normalized", i))
		}
		if f.Kind == codec.KindNack && f.Nack != nil {
			report.Invariants = append(report.Invariants,
				fmt.Sprintf("trame %d: nack %s", i, f.Nack.Error()))
		}
		if d := f.Device; d != nil && d.Type == "VALUE" {
			for j := range d.ObjectValues {
				record(d.DeviceName, d.SubDevice, &d.ObjectValues[j])
			}
		}

		if opts.StopAt != "" && t.Note == opts.StopAt {
			report.StoppedAt = t.Note
			break
		}
	}

	if collect {
		ordered := make([]consumer.Object, 0, len(order))
		for _, key := range order {
			ordered = append(ordered, objects[key])
		}
		snap := &export.Snapshot{
			Device: export.DeviceInfo{
				Protocol: "cerebrum-nb",
			},
			Generator: "dhs validate --out-tree (cerebrum-nb)",
			CreatedAt: time.Now().UTC(),
			Slots: []export.SlotDump{{
				Slot:     0,
				Status:   consumer.SlotPresent.String(),
				WalkedAt: time.Now().UTC(),
				Objects:  ordered,
			}},
		}
		if opts.OutTree != "" {
			if err := writeCerebrumSnapshot(opts.OutTree, snap, true); err != nil {
				return report, fmt.Errorf("write out-tree: %w", err)
			}
		}
		if opts.OutParams != "" {
			if err := writeCerebrumSnapshot(opts.OutParams, snap, false); err != nil {
				return report, fmt.Errorf("write out-params: %w", err)
			}
		}
	}

	return report, nil
}

// writeCerebrumSnapshot writes the aggregated snapshot: forceJSON pins
// the canonical tree.json shape; otherwise the extension picks the
// format (.csv → CSV, anything else → JSON), mirroring the export verb.
func writeCerebrumSnapshot(path string, snap *export.Snapshot, forceJSON bool) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	if !forceJSON && strings.EqualFold(filepath.Ext(path), ".csv") {
		return export.WriteCSV(f, snap)
	}
	return export.WriteJSON(f, snap)
}

func cerebrumShortHex(b []byte) string {
	if len(b) > 16 {
		b = b[:16]
	}
	return hex.EncodeToString(b)
}
