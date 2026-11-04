package export

import (
	"fmt"
	"net"
	"strconv"
	"strings"
	"unicode/utf8"

	"dhs/internal/consumer"
)

// metaInvalid is the Object.Meta key a reader sets when a value cell
// cannot be imported as its kind says; the importer skips the row and
// reports it.
const metaInvalid = "import_invalid"

// cellProblem is why a value cell (CSV, or the flat JSON shape) cannot
// be imported as its kind, "" when it can. The import never guesses: a
// value that would need one is refused and named.
//
//	bool    true, false, 1 or 0 (any case); nothing else, no typo
//	number  must parse; an unsigned one is never negative
//	enum    the value is an item's (its id, or its position) and the
//	        name beside it is that item; a name alone must be an item
//	        exactly
//	ip      a dotted IPv4 address (the kind holds 4 bytes)
//	empty   only a string may be empty
func cellProblem(kind consumer.ValueKind, val, name string, items []string, ids []int64) string {
	if val == "" && kind != consumer.KindString && (kind != consumer.KindEnum || name == "") {
		return "no value"
	}
	switch kind {
	case consumer.KindBool:
		switch strings.ToLower(val) {
		case "true", "false", "1", "0":
			return ""
		}
		return fmt.Sprintf("%q is not a boolean (true, false, 1 or 0)", val)
	case consumer.KindInt:
		if _, err := strconv.ParseInt(val, 10, 64); err != nil {
			return fmt.Sprintf("%q is not a whole number", val)
		}
	case consumer.KindUint:
		if _, err := strconv.ParseUint(val, 10, 64); err != nil {
			return fmt.Sprintf("%q is not a whole number of 0 or more", val)
		}
	case consumer.KindFloat:
		if _, err := strconv.ParseFloat(val, 64); err != nil {
			return fmt.Sprintf("%q is not a number", val)
		}
	case consumer.KindEnum:
		return enumProblem(val, name, items, ids)
	case consumer.KindIPAddr:
		ip := net.ParseIP(val)
		switch {
		case ip == nil:
			return fmt.Sprintf("%q is not an IP address", val)
		case ip.To4() == nil:
			return fmt.Sprintf("%s is IPv6, the field holds an IPv4 address", val)
		case strings.Contains(val, ":"):
			return fmt.Sprintf("%s is written as IPv6, the field holds an IPv4 address", val)
		}
	}
	return ""
}

// enumProblem checks an enum cell: the value must be one the device
// gives an item (its id, or its position when it numbers none), and a
// name given beside it must be that item. A name alone must be an item
// exactly; a case-only difference is refused with the item it probably
// meant.
func enumProblem(val, name string, items []string, ids []int64) string {
	if val == "" {
		if _, ok := enumIndex(name, items); ok {
			return ""
		}
		return enumNameProblem(name, items)
	}
	id, err := strconv.ParseInt(val, 10, 64)
	if err != nil {
		return fmt.Sprintf("%q is not an enum value", val)
	}
	if len(items) == 0 {
		if id < 0 || id > 255 {
			return fmt.Sprintf("enum value %d is outside 0..255", id)
		}
		return ""
	}
	pos, ok := enumPos(id, items, ids)
	if !ok {
		return fmt.Sprintf("enum value %d is none of %s", id, strings.Join(enumCells(consumer.Object{EnumItems: items, EnumIDs: ids}), "|"))
	}
	if name != "" && items[pos] != name {
		if _, ok := enumIndex(name, items); !ok {
			return enumNameProblem(name, items)
		}
		return fmt.Sprintf("enum value %d is %q but the name says %q", id, items[pos], name)
	}
	return ""
}

// enumPos finds the item a value stands for: by id where the device
// numbers its items, by position otherwise.
func enumPos(id int64, items []string, ids []int64) (int, bool) {
	if len(ids) == len(items) && len(ids) > 0 {
		for i, v := range ids {
			if v == id {
				return i, true
			}
		}
		return 0, false
	}
	return int(id), id >= 0 && id < int64(len(items))
}

func enumNameProblem(name string, items []string) string {
	for _, it := range items {
		if strings.EqualFold(it, name) {
			return fmt.Sprintf("%q is not an item; %q differs only in case", name, it)
		}
	}
	return fmt.Sprintf("%q is not one of %s", name, strings.Join(items, "|"))
}

// enumIndex finds an item by its exact name.
func enumIndex(name string, items []string) (int, bool) {
	for i, it := range items {
		if it == name {
			return i, true
		}
	}
	return 0, false
}

// enumCells writes an enumeration as the files carry it: the names
// alone when the values are the positions, "id=name" when the device
// numbers its items (so the value a file holds can be checked against
// the item it names).
func enumCells(o consumer.Object) []string {
	if len(o.EnumIDs) != len(o.EnumItems) || len(o.EnumIDs) == 0 {
		return o.EnumItems
	}
	out := make([]string, len(o.EnumItems))
	for i, it := range o.EnumItems {
		out[i] = strconv.FormatInt(o.EnumIDs[i], 10) + "=" + it
	}
	return out
}

// enumValue is the value an export writes for an enum: the device's id
// of the item it names (an ACP2 option id does not fit the one-byte
// index), the index itself where the items are positions.
func enumValue(o consumer.Object) int64 {
	if len(o.EnumIDs) == len(o.EnumItems) && len(o.EnumIDs) > 0 {
		if pos, ok := enumIndex(o.Value.Str, o.EnumItems); ok {
			return o.EnumIDs[pos]
		}
	}
	return int64(o.Value.Enum)
}

// splitEnumCells reads enumCells back: ids only when every cell carries one.
func splitEnumCells(cells []string) ([]string, []int64) {
	names := make([]string, len(cells))
	ids := make([]int64, len(cells))
	for i, c := range cells {
		k, v, found := strings.Cut(c, "=")
		n, err := strconv.ParseInt(k, 10, 64)
		if !found || err != nil {
			return cells, nil
		}
		names[i], ids[i] = v, n
	}
	return names, ids
}

// valueProblem checks a parsed value against its row's definition, for
// every file format: an enum value that is one of the items, a string
// within max_len. (A cell that did not parse is marked by the reader.)
func valueProblem(obj consumer.Object) string {
	v := obj.Value
	switch {
	case v.Kind == consumer.KindEnum && len(obj.EnumItems) > 0 && v.Str != "":
		if _, ok := enumIndex(v.Str, obj.EnumItems); !ok {
			return enumNameProblem(v.Str, obj.EnumItems)
		}
	case v.Kind == consumer.KindEnum && len(obj.EnumItems) > 0:
		if _, ok := enumPos(int64(v.Enum), obj.EnumItems, obj.EnumIDs); !ok {
			return fmt.Sprintf("enum value %d is none of %s", v.Enum, strings.Join(enumCells(obj), "|"))
		}
	case v.Kind == consumer.KindString && obj.MaxLen > 0 && utf8.RuneCountInString(v.Str) > obj.MaxLen:
		return fmt.Sprintf("%d characters, max_len %d", utf8.RuneCountInString(v.Str), obj.MaxLen)
	}
	return ""
}

// ipClass names what an IPv4 address is, for the import's report.
func ipClass(a [4]byte) string {
	ip := net.IP(a[:])
	switch {
	case ip.IsUnspecified():
		return "ipv4 unspecified"
	case ip.Equal(net.IPv4bcast):
		return "ipv4 broadcast"
	case ip.IsLoopback():
		return "ipv4 loopback"
	case a[0] == 232:
		return "ipv4 multicast (SSM)"
	case ip.IsMulticast():
		return "ipv4 multicast"
	case ip.IsLinkLocalUnicast():
		return "ipv4 link-local"
	case ip.IsPrivate():
		return "ipv4 private unicast"
	}
	return "ipv4 public unicast"
}

// markCell records cellProblem's verdict on obj and, for an enum named
// without an index, sets the index the name stands for.
func markCell(obj *consumer.Object, val, name string) {
	if p := cellProblem(obj.Kind, val, name, obj.EnumItems, obj.EnumIDs); p != "" {
		if obj.Meta == nil {
			obj.Meta = map[string]any{}
		}
		obj.Meta[metaInvalid] = p
		return
	}
	if obj.Kind != consumer.KindEnum || len(obj.EnumItems) == 0 {
		return
	}
	// The write goes out by the item's name (an ACP2 id does not fit the
	// one-byte index); the index is the item's own value.
	pos, _ := enumIndex(name, obj.EnumItems)
	if val != "" {
		id, _ := strconv.ParseInt(val, 10, 64)
		pos, _ = enumPos(id, obj.EnumItems, obj.EnumIDs)
	}
	obj.Value.Str = obj.EnumItems[pos]
	obj.Value.Enum = uint8(pos)
	if len(obj.EnumIDs) == len(obj.EnumItems) {
		obj.Value.Enum = uint8(obj.EnumIDs[pos])
	}
}
