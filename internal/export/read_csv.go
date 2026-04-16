package export

import (
	"encoding/csv"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	"acp/internal/protocol"
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

		// "group" column (new) or "path" column (legacy) — accept both.
		groupStr := col(row, idx, "group")
		if groupStr == "" {
			groupStr = col(row, idx, "path")
		}
		obj := protocol.Object{
			Slot:  slotNum,
			Group: groupStr,
			Path:  []string{groupStr},
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
func parseValueKind(s string) protocol.ValueKind {
	switch strings.ToLower(s) {
	case "bool":
		return protocol.KindBool
	case "int":
		return protocol.KindInt
	case "uint":
		return protocol.KindUint
	case "float":
		return protocol.KindFloat
	case "enum":
		return protocol.KindEnum
	case "string":
		return protocol.KindString
	case "ipaddr":
		return protocol.KindIPAddr
	case "alarm":
		return protocol.KindAlarm
	case "frame":
		return protocol.KindFrame
	case "raw":
		return protocol.KindRaw
	}
	return protocol.KindUnknown
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
func parseNum(s string, kind protocol.ValueKind) any {
	if s == "" {
		return nil
	}
	switch kind {
	case protocol.KindInt:
		n, _ := strconv.ParseInt(s, 10, 64)
		return n
	case protocol.KindUint:
		n, _ := strconv.ParseUint(s, 10, 64)
		return n
	case protocol.KindFloat:
		f, _ := strconv.ParseFloat(s, 64)
		return f
	}
	return nil
}

// parseCSVValue builds a protocol.Value from the CSV value + value_name
// columns.
func parseCSVValue(kind protocol.ValueKind, val, name string, items []string) protocol.Value {
	v := protocol.Value{Kind: kind}
	switch kind {
	case protocol.KindInt:
		v.Int, _ = strconv.ParseInt(val, 10, 64)
	case protocol.KindUint:
		v.Uint, _ = strconv.ParseUint(val, 10, 64)
	case protocol.KindFloat:
		v.Float, _ = strconv.ParseFloat(val, 64)
	case protocol.KindEnum:
		idx, _ := strconv.Atoi(val)
		v.Enum = uint8(idx)
		v.Str = name
	case protocol.KindString:
		v.Str = val
	case protocol.KindIPAddr:
		var a, b, c, d uint8
		fmt.Sscanf(val, "%d.%d.%d.%d", &a, &b, &c, &d)
		v.IPAddr = [4]byte{a, b, c, d}
	}
	return v
}
