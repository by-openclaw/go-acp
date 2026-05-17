package export

import (
	"encoding/csv"
	"fmt"
	"io"
	"strconv"
	"strings"

	"dhs/internal/consumer"
)

// csvHeader is the fixed column order every row follows. Adding a
// column is a breaking change for any consumer that parses this file
// by position — prefer parsing by header name.
//
// oid + path + id + label together make the CSV a lossless round-trip
// carrier: oid disambiguates Ember+ elements whose labels collide
// across sub-trees; path records the dotted tree location; id is the
// numeric handle ACP1/ACP2 importers match by. Readers fall back to
// the legacy `group` column when present (pre-#38 exports).
var csvHeader = []string{
	"ip", "protocol", "slot", "oid", "path",
	"id", "label", "kind", "access",
	"value", "value_name",
	"unit", "min", "max", "step", "default",
	"enum_items",
	"max_len",
	"alarm_priority", "alarm_tag", "alarm_on", "alarm_off",
	"slot_status",
}

// WriteCSV emits a Snapshot as one-row-per-object CSV with a header
// row. Nested fields (enum items, slot status) use `|` as the
// intra-cell separator — chosen to avoid conflict with the CSV `,`
// delimiter and European Excel `;`.
//
// CSV still has shape limitations vs JSON:
//   - Preset depth indices (ACP2) are not represented — only the
//     active-index row is emitted.
//   - Hierarchical paths are joined with `/`; reader splits them back.
//   - Raw byte values are hex-encoded.
//
// Writable-object round-trip is lossless: every scalar field the
// importer needs (oid, path, id, label, kind, value, access) survives
// a JSON → CSV → JSON → import --dry-run with zero diff (issue #38).
func WriteCSV(w io.Writer, s *Snapshot) error {
	cw := csv.NewWriter(w)
	defer cw.Flush()

	if err := cw.Write(csvHeader); err != nil {
		return fmt.Errorf("csv header: %w", err)
	}

	for _, slot := range s.Slots {
		// Group objects by path so the CSV is sorted by group —
		// identity block, then control block, then status, then alarm.
		// Mirrors the YAML grouped structure and makes the spreadsheet
		// easier to navigate.
		groups := groupByPath(slot.Objects)
		for _, gname := range groups.order {
			_ = gname
			for _, o := range groups.items[gname] {
				// Skip container nodes (kind=raw) — they have no
				// value and exist only as tree structure in ACP2.
				if o.Kind == consumer.KindRaw {
					continue
				}
				row := buildCSVRow(s.Device, slot, o)
				if err := cw.Write(row); err != nil {
					return fmt.Errorf("csv row slot=%d id=%d: %w", slot.Slot, o.ID, err)
				}
			}
		}
	}
	cw.Flush()
	return cw.Error()
}

// buildCSVRow maps one consumer.Object to a flat []string matching
// csvHeader. Missing fields become empty strings.
func buildCSVRow(dev DeviceInfo, slot SlotDump, o consumer.Object) []string {
	row := make([]string, len(csvHeader))
	// ip, protocol, slot, oid, path
	row[0] = dev.IP
	row[1] = dev.Protocol
	row[2] = strconv.Itoa(slot.Slot)
	row[3] = o.OID
	row[4] = joinPath(o)
	// id, label, kind, access
	row[5] = strconv.Itoa(o.ID)
	row[6] = o.Label
	row[7] = kindName(o.Kind)
	row[8] = accessStr(o.Access)
	// value, value_name — the "edit me" columns
	row[9], row[10] = valueAndName(o)
	// unit, min, max, step, default — metadata
	row[11] = o.Unit
	row[12] = numStr(o.Min)
	row[13] = numStr(o.Max)
	row[14] = numStr(o.Step)
	row[15] = numStr(o.Def)
	// enum_items
	row[16] = strings.Join(o.EnumItems, "|")
	// max_len
	if o.MaxLen > 0 {
		row[17] = strconv.Itoa(o.MaxLen)
	}
	// alarm
	if o.Kind == consumer.KindAlarm {
		row[18] = strconv.Itoa(int(o.AlarmPriority))
		row[19] = fmt.Sprintf("0x%02X", o.AlarmTag)
		row[20] = o.AlarmOnMsg
		row[21] = o.AlarmOffMsg
	}
	// slot_status
	row[22] = slotStatusPipe(o.Value.SlotStatus)
	return row
}

// joinPath renders the object's Path slice as a dot-separated string —
// same convention as Ember+ OID, the importer's req.Path resolver, and
// watch event paths (see feedback_path_separator). For ACP1 (single-
// level) this is usually just "control" / "status" / "identity" /
// "alarm" / "frame". Falls back to the legacy Group field when Path is
// empty.
func joinPath(o consumer.Object) string {
	if len(o.Path) > 0 {
		return strings.Join(o.Path, ".")
	}
	return o.Group
}

// valueAndName renders the object's current value into the CSV's
// (value, value_name) column pair. value_name is the human-readable
// form for enums; empty otherwise.
func valueAndName(o consumer.Object) (string, string) {
	v := o.Value
	switch v.Kind {
	case consumer.KindInt:
		return strconv.FormatInt(v.Int, 10), ""
	case consumer.KindUint:
		return strconv.FormatUint(v.Uint, 10), ""
	case consumer.KindFloat:
		return strconv.FormatFloat(v.Float, 'g', -1, 64), ""
	case consumer.KindEnum:
		return strconv.Itoa(int(v.Enum)), v.Str
	case consumer.KindString:
		return v.Str, ""
	case consumer.KindIPAddr:
		return fmt.Sprintf("%d.%d.%d.%d",
			v.IPAddr[0], v.IPAddr[1], v.IPAddr[2], v.IPAddr[3]), ""
	case consumer.KindFrame:
		return "", ""
	}
	return "", ""
}

// slotStatusPipe renders a SlotStatus slice as a pipe-separated string
// of human-readable names: "present|error|boot_mode|...".
func slotStatusPipe(statuses []consumer.SlotStatus) string {
	if len(statuses) == 0 {
		return ""
	}
	parts := make([]string, len(statuses))
	for i, s := range statuses {
		parts[i] = s.String()
	}
	return strings.Join(parts, "|")
}

// numStr converts any (int64/uint64/float64) to a display string.
// Nil and unknown types become empty.
func numStr(v any) string {
	switch n := v.(type) {
	case int64:
		return strconv.FormatInt(n, 10)
	case uint64:
		return strconv.FormatUint(n, 10)
	case float64:
		return strconv.FormatFloat(n, 'g', -1, 64)
	case nil:
		return ""
	default:
		return fmt.Sprintf("%v", n)
	}
}

// kindName mirrors the CLI helper — duplicated here to keep the export
// package self-contained (no import from cmd/acp).
func kindName(k consumer.ValueKind) string {
	switch k {
	case consumer.KindBool:
		return "bool"
	case consumer.KindInt:
		return "int"
	case consumer.KindUint:
		return "uint"
	case consumer.KindFloat:
		return "float"
	case consumer.KindEnum:
		return "enum"
	case consumer.KindString:
		return "string"
	case consumer.KindIPAddr:
		return "ipaddr"
	case consumer.KindAlarm:
		return "alarm"
	case consumer.KindFrame:
		return "frame"
	case consumer.KindRaw:
		return "raw"
	default:
		return "unknown"
	}
}

// accessStr mirrors the CLI helper — same reason as kindName.
func accessStr(a uint8) string {
	r, w, d := "-", "-", "-"
	if a&0x01 != 0 {
		r = "R"
	}
	if a&0x02 != 0 {
		w = "W"
	}
	if a&0x04 != 0 {
		d = "D"
	}
	return r + w + d
}
