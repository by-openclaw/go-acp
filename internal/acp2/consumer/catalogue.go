package acp2

// CommandKind groups ACP2 catalogue entries. ACP2 mixes AN2 transport
// constants (frame type, internal func IDs) with ACP2 application
// constants (message type, function IDs, object types, property IDs,
// error stat codes, number types). The CLI catalogue surfaces all
// kinds in parallel addressable as `<kind>:<id>`.
type CommandKind string

const (
	KindAN2Type     CommandKind = "an2-type"     // AN2 frame type field (request/reply/event/error/data)
	KindAN2Func     CommandKind = "an2-func"     // AN2 internal func ID (proto=0)
	KindACP2Type    CommandKind = "acp2-type"    // ACP2 message type byte
	KindACP2Func    CommandKind = "acp2-func"    // ACP2 application func ID
	KindObjType     CommandKind = "obj-type"     // ACP2 object type
	KindPid         CommandKind = "pid"          // ACP2 property ID
	KindNumberType  CommandKind = "number-type"  // ACP2 numeric vtype
	KindErrStat     CommandKind = "err-stat"     // ACP2 error stat code
)

// CatalogueEntry is the structured row used by the CLI catalogue
// renderers (`dhs list-commands acp2`, `dhs help-cmd acp2 <addr>`).
type CatalogueEntry struct {
	Kind    CommandKind
	ID      uint8
	Name    string
	SpecRef string
	Notes   string
}

// Address returns the canonical "<kind>:<id>" string used by
// `dhs help-cmd acp2 <addr>`.
func (c CatalogueEntry) Address() string {
	return string(c.Kind) + ":" + uint8ToDec(c.ID)
}

// Catalogue returns every ACP2 catalogue entry the consumer knows.
// Source of truth: acp2_protocol.pdf + an2_protocol.pdf, mirrored
// from the const blocks in this package's types.go and from
// internal/acp2/CLAUDE.md.
func Catalogue() []CatalogueEntry {
	return []CatalogueEntry{
		// AN2 frame types
		{Kind: KindAN2Type, ID: uint8(AN2TypeRequest), Name: "request", SpecRef: "AN2"},
		{Kind: KindAN2Type, ID: uint8(AN2TypeReply), Name: "reply", SpecRef: "AN2"},
		{Kind: KindAN2Type, ID: uint8(AN2TypeEvent), Name: "event", SpecRef: "AN2"},
		{Kind: KindAN2Type, ID: uint8(AN2TypeError), Name: "error", SpecRef: "AN2"},
		{Kind: KindAN2Type, ID: uint8(AN2TypeData), Name: "data", SpecRef: "AN2", Notes: "ACP1/ACP2 messages travel in data frames"},

		// AN2 internal funcs (proto=0)
		{Kind: KindAN2Func, ID: AN2FuncGetVersion, Name: "GetVersion", SpecRef: "AN2 §4"},
		{Kind: KindAN2Func, ID: AN2FuncGetDeviceInfo, Name: "GetDeviceInfo", SpecRef: "AN2 §4"},
		{Kind: KindAN2Func, ID: AN2FuncGetSlotInfo, Name: "GetSlotInfo", SpecRef: "AN2 §4"},
		{Kind: KindAN2Func, ID: AN2FuncEnableProtocolEvents, Name: "EnableProtocolEvents", SpecRef: "AN2 §4", Notes: "REQUIRED for ACP2 announces"},

		// ACP2 message types
		{Kind: KindACP2Type, ID: uint8(ACP2TypeRequest), Name: "request", SpecRef: "ACP2 §1"},
		{Kind: KindACP2Type, ID: uint8(ACP2TypeReply), Name: "reply", SpecRef: "ACP2 §1"},
		{Kind: KindACP2Type, ID: uint8(ACP2TypeAnnounce), Name: "announce", SpecRef: "ACP2 §1", Notes: "NOT 'event'"},
		{Kind: KindACP2Type, ID: uint8(ACP2TypeError), Name: "error", SpecRef: "ACP2 §1"},

		// ACP2 application funcs
		{Kind: KindACP2Func, ID: uint8(ACP2FuncGetVersion), Name: "get_version", SpecRef: "ACP2 §3"},
		{Kind: KindACP2Func, ID: uint8(ACP2FuncGetObject), Name: "get_object", SpecRef: "ACP2 §3", Notes: "all property headers for obj-id"},
		{Kind: KindACP2Func, ID: uint8(ACP2FuncGetProperty), Name: "get_property", SpecRef: "ACP2 §3"},
		{Kind: KindACP2Func, ID: uint8(ACP2FuncSetProperty), Name: "set_property", SpecRef: "ACP2 §3"},

		// Object types
		{Kind: KindObjType, ID: 0, Name: "node", SpecRef: "ACP2 §4", Notes: "container; children via pid 14"},
		{Kind: KindObjType, ID: 1, Name: "preset", SpecRef: "ACP2 §4", Notes: "value repeated per idx"},
		{Kind: KindObjType, ID: 2, Name: "enum", SpecRef: "ACP2 §4"},
		{Kind: KindObjType, ID: 3, Name: "number", SpecRef: "ACP2 §4"},
		{Kind: KindObjType, ID: 4, Name: "ipv4", SpecRef: "ACP2 §4"},
		{Kind: KindObjType, ID: 5, Name: "string", SpecRef: "ACP2 §4", Notes: "UTF-8"},

		// Property IDs
		{Kind: KindPid, ID: 1, Name: "object_type", SpecRef: "ACP2 §5"},
		{Kind: KindPid, ID: 2, Name: "label", SpecRef: "ACP2 §5", Notes: "0-terminated UTF-8"},
		{Kind: KindPid, ID: 3, Name: "access", SpecRef: "ACP2 §5", Notes: "1=r, 2=w, 3=rw"},
		{Kind: KindPid, ID: 4, Name: "announce_delay", SpecRef: "ACP2 §5", Notes: "u32 ms — NOT 'event_delay'"},
		{Kind: KindPid, ID: 5, Name: "number_type", SpecRef: "ACP2 §5"},
		{Kind: KindPid, ID: 6, Name: "string_max_length", SpecRef: "ACP2 §5", Notes: "u16"},
		{Kind: KindPid, ID: 7, Name: "preset_depth", SpecRef: "ACP2 §5", Notes: "valid idx list"},
		{Kind: KindPid, ID: 8, Name: "value", SpecRef: "ACP2 §5", Notes: "repeated per preset idx"},
		{Kind: KindPid, ID: 9, Name: "default_value", SpecRef: "ACP2 §5", Notes: "repeated per preset idx"},
		{Kind: KindPid, ID: 10, Name: "min_value", SpecRef: "ACP2 §5", Notes: "repeated per preset idx"},
		{Kind: KindPid, ID: 11, Name: "max_value", SpecRef: "ACP2 §5", Notes: "repeated per preset idx"},
		{Kind: KindPid, ID: 12, Name: "step_size", SpecRef: "ACP2 §5", Notes: "optional"},
		{Kind: KindPid, ID: 13, Name: "unit", SpecRef: "ACP2 §5", Notes: "optional, 0-terminated"},
		{Kind: KindPid, ID: 14, Name: "children", SpecRef: "ACP2 §5", Notes: "u32[] child obj-ids"},
		{Kind: KindPid, ID: 15, Name: "options", SpecRef: "ACP2 §5", Notes: "enum: 72 bytes per option"},
		{Kind: KindPid, ID: 16, Name: "event_tag", SpecRef: "ACP2 §5", Notes: "optional, u16"},
		{Kind: KindPid, ID: 17, Name: "event_prio", SpecRef: "ACP2 §5", Notes: "optional"},
		{Kind: KindPid, ID: 18, Name: "event_state", SpecRef: "ACP2 §5", Notes: "optional"},
		{Kind: KindPid, ID: 19, Name: "event_messages", SpecRef: "ACP2 §5", Notes: "optional, two strings"},
		{Kind: KindPid, ID: 20, Name: "preset_parent", SpecRef: "ACP2 §5", Notes: "optional, u32"},

		// Number types
		{Kind: KindNumberType, ID: 0, Name: "S8", SpecRef: "ACP2 §6"},
		{Kind: KindNumberType, ID: 1, Name: "S16", SpecRef: "ACP2 §6"},
		{Kind: KindNumberType, ID: 2, Name: "S32", SpecRef: "ACP2 §6"},
		{Kind: KindNumberType, ID: 3, Name: "S64", SpecRef: "ACP2 §6"},
		{Kind: KindNumberType, ID: 4, Name: "U8", SpecRef: "ACP2 §6"},
		{Kind: KindNumberType, ID: 5, Name: "U16", SpecRef: "ACP2 §6"},
		{Kind: KindNumberType, ID: 6, Name: "U32", SpecRef: "ACP2 §6"},
		{Kind: KindNumberType, ID: 7, Name: "U64", SpecRef: "ACP2 §6"},
		{Kind: KindNumberType, ID: 8, Name: "float", SpecRef: "ACP2 §6"},
		{Kind: KindNumberType, ID: 9, Name: "preset/enum", SpecRef: "ACP2 §6"},
		{Kind: KindNumberType, ID: 10, Name: "ipv4", SpecRef: "ACP2 §6"},
		{Kind: KindNumberType, ID: 11, Name: "string", SpecRef: "ACP2 §6"},

		// Error stat codes (ACP2 type=3)
		{Kind: KindErrStat, ID: 0, Name: "protocol-error", SpecRef: "ACP2 §7", Notes: "bad type/func/packet"},
		{Kind: KindErrStat, ID: 1, Name: "invalid-obj-id", SpecRef: "ACP2 §7"},
		{Kind: KindErrStat, ID: 2, Name: "invalid-idx", SpecRef: "ACP2 §7"},
		{Kind: KindErrStat, ID: 3, Name: "invalid-pid", SpecRef: "ACP2 §7", Notes: "or object lacks this property"},
		{Kind: KindErrStat, ID: 4, Name: "no-access", SpecRef: "ACP2 §7", Notes: "read-only, set attempted"},
		{Kind: KindErrStat, ID: 5, Name: "invalid-value", SpecRef: "ACP2 §7", Notes: "enum/preset out of options, etc."},
	}
}

// LookupCatalogue returns the entry matching `<kind>:<id>` (e.g.
// "pid:4", "acp2-func:1"). Returns false if either the kind is
// unknown or the id isn't in this codec's catalogue.
func LookupCatalogue(addr string) (CatalogueEntry, bool) {
	kind, id, ok := splitAddress(addr)
	if !ok {
		return CatalogueEntry{}, false
	}
	for _, e := range Catalogue() {
		if e.Kind == kind && e.ID == id {
			return e, true
		}
	}
	return CatalogueEntry{}, false
}

func splitAddress(s string) (CommandKind, uint8, bool) {
	for i := 0; i < len(s); i++ {
		if s[i] == ':' {
			k := CommandKind(s[:i])
			n, err := decToUint8(s[i+1:])
			if err {
				return "", 0, false
			}
			return k, n, true
		}
	}
	return "", 0, false
}

func uint8ToDec(n uint8) string {
	if n == 0 {
		return "0"
	}
	var buf [3]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0') + n%10
		n /= 10
	}
	return string(buf[i:])
}

func decToUint8(s string) (uint8, bool) {
	if s == "" || len(s) > 3 {
		return 0, true
	}
	var n uint16
	for _, c := range s {
		if c < '0' || c > '9' {
			return 0, true
		}
		n = n*10 + uint16(c-'0')
		if n > 255 {
			return 0, true
		}
	}
	return uint8(n), false
}
