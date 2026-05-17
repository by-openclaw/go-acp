package export

import (
	"encoding/csv"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	"dhs/internal/consumer"
)

// ReadCSV parses a CSV snapshot produced by WriteCSV. Reconstructs a
// Snapshot from the header row + data rows. Lossy fields (slot_status
// arrays, preset depth) are recovered on a best-effort basis from the
// pipe-separated cell format.
func ReadCSV(r io.Reader) (*Snapshot, error) {
	cr := csv.NewReader(r)
	cr.FieldsPerRecord = -1 // tolerate ragged rows

	header, err := cr.Read()
	if err != nil {
		return nil, fmt.Errorf("csv: read header: %w", err)
	}
	idx := buildColumnIndex(header)

	snap := &Snapshot{CreatedAt: time.Now().UTC()}
	slotMap := map[string]*SlotDump{} // "ip:slot" → dump

	for {
		row, err := cr.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("csv: read row: %w", err)
		}

		ip := col(row, idx, "ip")
		proto := col(row, idx, "protocol")
		if snap.Device.IP == "" && ip != "" {
			snap.Device.IP = ip
			snap.Device.Protocol = proto
		}

		slotNum, _ := strconv.Atoi(col(row, idx, "slot"))
		key := fmt.Sprintf("%s:%d", ip, slotNum)
		dump, ok := slotMap[key]
		if !ok {
			dump = &SlotDump{
				Slot:     slotNum,
				Status:   col(row, idx, "status"),
				WalkedAt: time.Now().UTC(),
			}
			slotMap[key] = dump
		}

		// "path" column (current) or "group" column (legacy ≤0.2.0) —
		// accept both so CSVs from older exports still import cleanly.
		pathStr := col(row, idx, "path")
		if pathStr == "" {
			pathStr = col(row, idx, "group")
		}
		// Split path back into segments for ACP2 hierarchical import
		// (e.g. "ROOT_NODE_V2.PSU.1.Present"). Primary separator is "."
		// (matches Ember+ OID + memory rule feedback_path_separator);
		// "/" is accepted as a fallback so CSVs from earlier exports
		// (pre-#419) still import.
		var pathSegs []string
		switch {
		case strings.Contains(pathStr, "."):
			pathSegs = strings.Split(pathStr, ".")
		case strings.Contains(pathStr, "/"):
			pathSegs = strings.Split(pathStr, "/")
		case pathStr != "":
			pathSegs = []string{pathStr}
		}
		// ACP1 resolver still matches by (Group, ID) / (Group, Label).
		// For single-element paths (ACP1's flat group model) populate
		// Group so that resolver path keeps working; other plugins
		// ignore Group.
		group := ""
		if len(pathSegs) == 1 {
			group = pathSegs[0]
		}
		obj := consumer.Object{
			Slot:  slotNum,
			OID:   col(row, idx, "oid"),
			Group: group,
			Path:  pathSegs,
			Label: col(row, idx, "label"),
			Unit:  col(row, idx, "unit"),
		}
		if v := col(row, idx, "id"); v != "" {
			obj.ID, _ = strconv.Atoi(v)
		}
		obj.Kind = parseValueKind(col(row, idx, "kind"))
		obj.Access = parseAccess(col(row, idx, "access"))

		// Numeric constraints.
		obj.Min = parseNum(col(row, idx, "min"), obj.Kind)
		obj.Max = parseNum(col(row, idx, "max"), obj.Kind)
		obj.Step = parseNum(col(row, idx, "step"), obj.Kind)
		obj.Def = parseNum(col(row, idx, "default"), obj.Kind)

		// Enum items (pipe-separated).
		if items := col(row, idx, "enum_items"); items != "" {
			obj.EnumItems = strings.Split(items, "|")
		}

		// String max length.
		if v := col(row, idx, "max_len"); v != "" {
			obj.MaxLen, _ = strconv.Atoi(v)
		}

		// Alarm fields.
		if v := col(row, idx, "alarm_priority"); v != "" {
			n, _ := strconv.Atoi(v)
			obj.AlarmPriority = uint8(n)
		}
		if v := col(row, idx, "alarm_on"); v != "" {
			obj.AlarmOnMsg = v
		}
		if v := col(row, idx, "alarm_off"); v != "" {
			obj.AlarmOffMsg = v
		}

		// Value.
		obj.Value = parseCSVValue(obj.Kind, col(row, idx, "value"),
			col(row, idx, "value_name"), obj.EnumItems)

		dump.Objects = append(dump.Objects, obj)
	}

	// Flatten slotMap to the Snapshot.
	for _, d := range slotMap {
		snap.Slots = append(snap.Slots, *d)
	}
	return snap, nil
}

// buildColumnIndex maps column name → position for robust header lookup.
func buildColumnIndex(header []string) map[string]int {
	m := make(map[string]int, len(header))
	for i, h := range header {
		m[strings.TrimSpace(strings.ToLower(h))] = i
	}
	return m
}

// col safely extracts a column value from a row by name.
func col(row []string, idx map[string]int, name string) string {
	i, ok := idx[name]
	if !ok || i >= len(row) {
		return ""
	}
	return row[i]
}

// parseValueKind reverses kindName.
func parseValueKind(s string) consumer.ValueKind {
	switch strings.ToLower(s) {
	case "bool":
		return consumer.KindBool
	case "int":
		return consumer.KindInt
	case "uint":
		return consumer.KindUint
	case "float":
		return consumer.KindFloat
	case "enum":
		return consumer.KindEnum
	case "string":
		return consumer.KindString
	case "ipaddr":
		return consumer.KindIPAddr
	case "alarm":
		return consumer.KindAlarm
	case "frame":
		return consumer.KindFrame
	case "raw":
		return consumer.KindRaw
	}
	return consumer.KindUnknown
}

// parseAccess reverses accessStr.
func parseAccess(s string) uint8 {
	var a uint8
	if len(s) >= 1 && s[0] == 'R' {
		a |= 0x01
	}
	if len(s) >= 2 && s[1] == 'W' {
		a |= 0x02
	}
	if len(s) >= 3 && s[2] == 'D' {
		a |= 0x04
	}
	return a
}

// parseNum converts a numeric string to the right Go type based on kind.
func parseNum(s string, kind consumer.ValueKind) any {
	if s == "" {
		return nil
	}
	switch kind {
	case consumer.KindInt:
		n, _ := strconv.ParseInt(s, 10, 64)
		return n
	case consumer.KindUint:
		n, _ := strconv.ParseUint(s, 10, 64)
		return n
	case consumer.KindFloat:
		f, _ := strconv.ParseFloat(s, 64)
		return f
	}
	return nil
}

// parseCSVValue builds a consumer.Value from the CSV value + value_name
// columns.
//
// On a type-incompatible value (e.g. "not_a_number" in an int column),
// the returned Value carries Kind=KindUnknown so the downstream
// importer's Validate / unknown_kind skip path catches it. This is the
// signal that the CSV cell didn't parse — without it the silent
// zero-default would let the importer write garbage.
func parseCSVValue(kind consumer.ValueKind, val, name string, items []string) consumer.Value {
	v := consumer.Value{Kind: kind}
	switch kind {
	case consumer.KindInt:
		n, err := strconv.ParseInt(val, 10, 64)
		if err != nil && val != "" {
			return consumer.Value{Kind: consumer.KindUnknown}
		}
		v.Int = n
	case consumer.KindUint:
		n, err := strconv.ParseUint(val, 10, 64)
		if err != nil && val != "" {
			return consumer.Value{Kind: consumer.KindUnknown}
		}
		v.Uint = n
	case consumer.KindFloat:
		f, err := strconv.ParseFloat(val, 64)
		if err != nil && val != "" {
			return consumer.Value{Kind: consumer.KindUnknown}
		}
		v.Float = f
	case consumer.KindEnum:
		idx, err := strconv.Atoi(val)
		if err != nil && val != "" {
			return consumer.Value{Kind: consumer.KindUnknown}
		}
		v.Enum = uint8(idx)
		v.Str = name
	case consumer.KindString:
		v.Str = val
	case consumer.KindIPAddr:
		var a, b, c, d uint8
		n, err := fmt.Sscanf(val, "%d.%d.%d.%d", &a, &b, &c, &d)
		if (err != nil || n != 4) && val != "" {
			return consumer.Value{Kind: consumer.KindUnknown}
		}
		v.IPAddr = [4]byte{a, b, c, d}
	}
	return v
}
