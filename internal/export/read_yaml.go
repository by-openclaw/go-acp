package export

import (
	"bufio"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	"acp/internal/protocol"
)

// ReadYAML parses a YAML snapshot produced by WriteYAML. This is NOT a
// general-purpose YAML parser — it's a targeted line-by-line state
// machine that handles exactly the shape our emitter produces:
//
//   - 2-space indent levels
//   - "key: value" pairs with optional quoting
//   - "- item" list entries
//   - Flow sequences "[a, b, c]"
//
// Files produced by other YAML emitters may or may not parse. For
// guaranteed round-trip, use JSON.
func ReadYAML(r io.Reader) (*Snapshot, error) {
	scanner := bufio.NewScanner(r)
	snap := &Snapshot{CreatedAt: time.Now().UTC()}

	var (
		section  string // "device", "slots"
		curSlot  *SlotDump
		curObj   *protocol.Object
		curGroup string // current group name (identity, control, ...)
	)

	for scanner.Scan() {
		line := scanner.Text()
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}

		indent := countIndent(line)
		key, val := splitKV(trimmed)

		// Top-level keys (indent 0).
		if indent == 0 {
			switch key {
			case "device":
				section = "device"
			case "slots":
				section = "slots"
			case "generator":
				snap.Generator = unquote(val)
			case "created_at":
				snap.CreatedAt, _ = time.Parse("2006-01-02T15:04:05Z", unquote(val))
			}
			continue
		}

		// Device fields (indent 2).
		if section == "device" && indent == 2 {
			switch key {
			case "ip":
				snap.Device.IP = unquote(val)
			case "port":
				snap.Device.Port, _ = strconv.Atoi(val)
			case "protocol":
				snap.Device.Protocol = unquote(val)
			case "protocol_version":
				snap.Device.ProtocolVersion, _ = strconv.Atoi(val)
			case "num_slots":
				snap.Device.NumSlots, _ = strconv.Atoi(val)
			}
			continue
		}

		// Slot list entry (indent 2, starts with "- ").
		if section == "slots" && indent == 2 && strings.HasPrefix(trimmed, "- ") {
			if curSlot != nil {
				flushObj(&curSlot.Objects, curObj)
				curObj = nil
				snap.Slots = append(snap.Slots, *curSlot)
			}
			curSlot = &SlotDump{}
			curGroup = ""
			inner := strings.TrimPrefix(trimmed, "- ")
			k, v := splitKV(inner)
			if k == "slot" {
				curSlot.Slot, _ = strconv.Atoi(v)
			}
			continue
		}

		// Slot-level keys (indent 4). These can be metadata fields
		// (status, walked_at) OR group names (identity, control, ...).
		if section == "slots" && indent == 4 && curSlot != nil {
			switch key {
			case "status":
				curSlot.Status = unquote(val)
			case "walked_at":
				curSlot.WalkedAt, _ = time.Parse("2006-01-02T15:04:05Z", unquote(val))
			case "objects":
				// Legacy flat-list format — treat as ungrouped.
				curGroup = ""
			default:
				// Any other indent-4 key with an empty val (ends with ":")
				// is a group name: identity, control, status, alarm, etc.
				if val == "" || val == ":" {
					curGroup = key
				} else {
					curGroup = key
				}
			}
			continue
		}

		// Object list entry (indent 6, starts with "- ").
		if section == "slots" && indent == 6 && strings.HasPrefix(trimmed, "- ") {
			flushObj(&curSlot.Objects, curObj)
			curObj = &protocol.Object{
				Slot:  curSlot.Slot,
				Group: curGroup,
				Path:  []string{curGroup},
			}
			inner := strings.TrimPrefix(trimmed, "- ")
			k, v := splitKV(inner)
			applyObjectField(curObj, k, v)
			continue
		}

		// Object fields (indent 8).
		if section == "slots" && indent == 8 && curObj != nil {
			applyObjectField(curObj, key, val)
			continue
		}
	}

	if curSlot != nil {
		flushObj(&curSlot.Objects, curObj)
		snap.Slots = append(snap.Slots, *curSlot)
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("yaml scan: %w", err)
	}
	return snap, nil
}

// applyObjectField sets one key:value pair on a protocol.Object.
func applyObjectField(o *protocol.Object, key, val string) {
	switch key {
	case "label":
		o.Label = unquote(val)
	case "path":
		o.Path = parseFlowSeq(val)
		if len(o.Path) > 0 {
			o.Group = o.Path[0]
		}
	case "id":
		o.ID, _ = strconv.Atoi(val)
	case "kind":
		o.Kind = parseValueKind(unquote(val))
	case "access":
		o.Access = parseAccess(unquote(val))
	case "unit":
		o.Unit = unquote(val)
	case "min":
		o.Min = parseNum(val, o.Kind)
	case "max":
		o.Max = parseNum(val, o.Kind)
	case "step":
		o.Step = parseNum(val, o.Kind)
	case "default":
		o.Def = parseNum(val, o.Kind)
	case "enum_items":
		o.EnumItems = parseFlowSeq(val)
	case "max_len":
		o.MaxLen, _ = strconv.Atoi(val)
	case "alarm_priority":
		n, _ := strconv.Atoi(val)
		o.AlarmPriority = uint8(n)
	case "alarm_tag":
		// Hex "0x01" or decimal "1"
		v := strings.TrimPrefix(val, "0x")
		n, _ := strconv.ParseUint(v, 16, 8)
		o.AlarmTag = uint8(n)
	case "alarm_on":
		o.AlarmOnMsg = unquote(val)
	case "alarm_off":
		o.AlarmOffMsg = unquote(val)
	case "sub_group_marker":
		o.SubGroupMarker = (val == "true")
	case "value":
		applyValue(o, val)
	case "value_name":
		o.Value.Str = unquote(val)
	}
}

// applyValue sets the Object's Value field from a YAML scalar.
func applyValue(o *protocol.Object, val string) {
	o.Value.Kind = o.Kind
	s := unquote(val)
	switch o.Kind {
	case protocol.KindInt:
		o.Value.Int, _ = strconv.ParseInt(s, 10, 64)
	case protocol.KindUint:
		o.Value.Uint, _ = strconv.ParseUint(s, 10, 64)
	case protocol.KindFloat:
		o.Value.Float, _ = strconv.ParseFloat(s, 64)
	case protocol.KindEnum:
		n, _ := strconv.Atoi(s)
		o.Value.Enum = uint8(n)
	case protocol.KindString:
		o.Value.Str = s
	case protocol.KindIPAddr:
		var a, b, c, d uint8
		_, _ = fmt.Sscanf(s, "%d.%d.%d.%d", &a, &b, &c, &d)
		o.Value.IPAddr = [4]byte{a, b, c, d}
	}
}

// flushObj appends a completed object to the slice if non-nil.
func flushObj(objs *[]protocol.Object, o *protocol.Object) {
	if o != nil {
		*objs = append(*objs, *o)
	}
}

// countIndent counts leading spaces.
func countIndent(line string) int {
	n := 0
	for _, r := range line {
		if r == ' ' {
			n++
		} else {
			break
		}
	}
	return n
}

// splitKV splits "key: value" into (key, value). Value may be empty.
func splitKV(s string) (string, string) {
	i := strings.Index(s, ":")
	if i < 0 {
		return s, ""
	}
	key := s[:i]
	val := strings.TrimSpace(s[i+1:])
	return key, val
}

// unquote strips surrounding double-quotes from a YAML/JSON string.
func unquote(s string) string {
	if len(s) >= 2 && s[0] == '"' && s[len(s)-1] == '"' {
		// Use strconv.Unquote for proper escape handling.
		if u, err := strconv.Unquote(s); err == nil {
			return u
		}
		return s[1 : len(s)-1]
	}
	return s
}

// parseFlowSeq parses a YAML flow sequence "[a, b, c]" into a string
// slice. Each element is unquoted individually.
func parseFlowSeq(s string) []string {
	s = strings.TrimSpace(s)
	if !strings.HasPrefix(s, "[") || !strings.HasSuffix(s, "]") {
		if s == "" {
			return nil
		}
		return []string{unquote(s)}
	}
	inner := s[1 : len(s)-1]
	if inner == "" {
		return nil
	}
	parts := strings.Split(inner, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		out = append(out, unquote(strings.TrimSpace(p)))
	}
	return out
}
