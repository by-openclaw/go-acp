package hyperdeck

import (
	"strconv"
	"strings"
	"time"

	"acp/internal/protocol"
)

func timeZero() time.Time { return time.Time{} }

func atoi(s string) int {
	n, _ := strconv.Atoi(strings.TrimSpace(s))
	return n
}

func atoiLeading(s string) int {
	s = strings.TrimSpace(s)
	var b strings.Builder
	for _, r := range s {
		if r < '0' || r > '9' {
			break
		}
		b.WriteRune(r)
	}
	return atoi(b.String())
}

func slotParam(slot int) map[string]string {
	if slot <= 0 {
		return nil
	}
	return map[string]string{"slot id": strconv.Itoa(slot)}
}

func slotStatus(s string) protocol.SlotStatus {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "mounted":
		return protocol.SlotPresent
	case "mounting":
		return protocol.SlotPowerUp
	case "error":
		return protocol.SlotError
	case "empty":
		return protocol.SlotNoCard
	default:
		return protocol.SlotNoCard
	}
}

func parseBool(s string) bool {
	return strings.EqualFold(strings.TrimSpace(s), "true")
}

func requestPath(req protocol.ValueRequest) string {
	if req.Path != "" {
		return strings.ToLower(req.Path)
	}
	if req.Label != "" {
		return labelToPath(req.Label)
	}
	return ""
}

func labelToPath(label string) string {
	s := strings.ToLower(strings.TrimSpace(label))
	s = strings.ReplaceAll(s, " ", "_")
	return s
}

func addString(out *[]protocol.Object, cache map[string]protocol.Object, id int, path, label, value string, write bool) {
	addObject(out, cache, protocol.Object{
		Slot:   0,
		Path:   strings.Split(path, "."),
		OID:    path,
		ID:     id,
		Label:  label,
		Kind:   protocol.KindString,
		Access: access(write),
		Value:  stringValue(value),
	})
}

func addInt(out *[]protocol.Object, cache map[string]protocol.Object, id int, path, label string, value int64, write bool) {
	addObject(out, cache, protocol.Object{
		Slot:   0,
		Path:   strings.Split(path, "."),
		OID:    path,
		ID:     id,
		Label:  label,
		Kind:   protocol.KindInt,
		Access: access(write),
		Value:  intValue(value),
	})
}

func addBool(out *[]protocol.Object, cache map[string]protocol.Object, id int, path, label string, value bool, write bool) {
	addObject(out, cache, protocol.Object{
		Slot:   0,
		Path:   strings.Split(path, "."),
		OID:    path,
		ID:     id,
		Label:  label,
		Kind:   protocol.KindBool,
		Access: access(write),
		Value:  boolValue(value),
	})
}

func addObject(out *[]protocol.Object, cache map[string]protocol.Object, o protocol.Object) {
	*out = append(*out, o)
	cache[strings.ToLower(strings.Join(o.Path, "."))] = o
	cache[strings.ToLower(o.Label)] = o
}

func access(write bool) uint8 {
	if write {
		return 0x03
	}
	return 0x01
}

func stringValue(s string) protocol.Value {
	return protocol.Value{Kind: protocol.KindString, Str: s, Raw: []byte(s)}
}

func intValue(n int64) protocol.Value {
	return protocol.Value{Kind: protocol.KindInt, Int: n}
}

func boolValue(b bool) protocol.Value {
	raw := []byte("false")
	if b {
		raw = []byte("true")
	}
	return protocol.Value{Kind: protocol.KindBool, Bool: b, Raw: raw}
}

func parseValueBool(v protocol.Value) bool {
	if v.Kind == protocol.KindBool {
		return v.Bool
	}
	return parseBool(v.Str)
}

func boolString(v protocol.Value) string {
	if parseValueBool(v) {
		return "true"
	}
	return "false"
}

func valueFromParam(path string, params map[string]string) protocol.Value {
	key := path[strings.LastIndex(path, ".")+1:]
	key = strings.ReplaceAll(key, "_", " ")
	switch path {
	case "transport.speed", "slot.remaining_size":
		return intValue(int64(atoi(params[key])))
	default:
		return stringValue(params[key])
	}
}

func title(s string) string {
	parts := strings.Fields(s)
	for i := range parts {
		if len(parts[i]) > 0 {
			parts[i] = strings.ToUpper(parts[i][:1]) + parts[i][1:]
		}
	}
	return strings.Join(parts, " ")
}
