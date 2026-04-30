package acp1

// CommandKind groups ACP1 catalogue entries. ACP1 has no single "byte
// command" namespace like Probel — its wire protocol mixes message
// types (MType), method IDs (MCODE), object groups, object types, and
// error codes. The CLI catalogue surfaces all four as parallel kinds
// addressable as `<kind>:<id>` (e.g. `method:0`, `objgroup:2`).
type CommandKind string

const (
	KindMessageType CommandKind = "msgtype"
	KindMethod      CommandKind = "method"
	KindObjGroup    CommandKind = "objgroup"
	KindObjType     CommandKind = "objtype"
	KindXportErr    CommandKind = "xport-err"
	KindObjErr      CommandKind = "obj-err"
)

// CatalogueEntry is the structured row used by the CLI catalogue
// renderers (`dhs list-commands acp1`, `dhs help-cmd acp1 <addr>`).
type CatalogueEntry struct {
	Kind    CommandKind
	ID      uint8
	Name    string
	SpecRef string
	Notes   string
}

// Address returns the canonical "<kind>:<id>" string used by
// `dhs help-cmd acp1 <addr>`.
func (c CatalogueEntry) Address() string {
	return string(c.Kind) + ":" + uint8ToDec(c.ID)
}

// Catalogue returns every ACP1 catalogue entry the consumer knows.
// Order: MType → Methods → Object groups → Object types → ACP errors
// → AxonNet errors. Used by the CLI catalogue helpers.
//
// Source of truth: AXON-ACP_v1_4.pdf, mirrored from the const blocks
// in this package's types.go.
func Catalogue() []CatalogueEntry {
	out := []CatalogueEntry{
		// Message types (MType field, 1 byte at header offset 5).
		{Kind: KindMessageType, ID: uint8(MTypeAnnounce), Name: "Announce", SpecRef: "p. 8", Notes: "MTID=0; broadcast"},
		{Kind: KindMessageType, ID: uint8(MTypeRequest), Name: "Request", SpecRef: "p. 8", Notes: "client → server"},
		{Kind: KindMessageType, ID: uint8(MTypeReply), Name: "Reply", SpecRef: "p. 8", Notes: "server → client"},
		{Kind: KindMessageType, ID: uint8(MTypeError), Name: "Error", SpecRef: "p. 8", Notes: "MCODE = error code"},

		// Methods (MCODE byte when MType < 3). Spec §"Methods" p. 28.
		{Kind: KindMethod, ID: uint8(MethodGetValue), Name: "getValue", SpecRef: "p. 28"},
		{Kind: KindMethod, ID: uint8(MethodSetValue), Name: "setValue", SpecRef: "p. 28"},
		{Kind: KindMethod, ID: uint8(MethodSetIncValue), Name: "setIncValue", SpecRef: "p. 28"},
		{Kind: KindMethod, ID: uint8(MethodSetDecValue), Name: "setDecValue", SpecRef: "p. 28"},
		{Kind: KindMethod, ID: uint8(MethodSetDefValue), Name: "setDefValue", SpecRef: "p. 28"},
		{Kind: KindMethod, ID: uint8(MethodGetObject), Name: "getObject", SpecRef: "p. 28", Notes: "returns all properties in sequence"},

		// Object groups. Spec §"Object Groups" p. 17.
		{Kind: KindObjGroup, ID: uint8(GroupRoot), Name: "root", SpecRef: "p. 17", Notes: "1 object, card counts per group"},
		{Kind: KindObjGroup, ID: uint8(GroupIdentity), Name: "identity", SpecRef: "p. 17"},
		{Kind: KindObjGroup, ID: uint8(GroupControl), Name: "control", SpecRef: "p. 17", Notes: "writable parameters"},
		{Kind: KindObjGroup, ID: uint8(GroupStatus), Name: "status", SpecRef: "p. 17", Notes: "read-only"},
		{Kind: KindObjGroup, ID: uint8(GroupAlarm), Name: "alarm", SpecRef: "p. 17"},
		{Kind: KindObjGroup, ID: uint8(GroupFile), Name: "file", SpecRef: "p. 17", Notes: "firmware + param table"},
		{Kind: KindObjGroup, ID: uint8(GroupFrame), Name: "frame", SpecRef: "p. 17", Notes: "rack controller only"},

		// Object types. Spec §"Object details" p. 19 + per-type tables p. 21–27.
		{Kind: KindObjType, ID: uint8(TypeRoot), Name: "ROOT", SpecRef: "p. 21", Notes: "9 props"},
		{Kind: KindObjType, ID: uint8(TypeInteger), Name: "INTEGER", SpecRef: "p. 22", Notes: "10 props (i16)"},
		{Kind: KindObjType, ID: uint8(TypeIPAddr), Name: "IPADDR", SpecRef: "p. 22", Notes: "10 props (u32)"},
		{Kind: KindObjType, ID: uint8(TypeFloat), Name: "FLOAT", SpecRef: "p. 23", Notes: "10 props (float32)"},
		{Kind: KindObjType, ID: uint8(TypeEnum), Name: "ENUMERATED", SpecRef: "p. 24", Notes: "8 props"},
		{Kind: KindObjType, ID: uint8(TypeString), Name: "STRING", SpecRef: "p. 25", Notes: "6 props"},
		{Kind: KindObjType, ID: uint8(TypeFrame), Name: "FRAME STATUS", SpecRef: "p. 25", Notes: "4 props, read-only"},
		{Kind: KindObjType, ID: uint8(TypeAlarm), Name: "ALARM", SpecRef: "p. 26", Notes: "8 props"},
		{Kind: KindObjType, ID: uint8(TypeFile), Name: "FILE", SpecRef: "p. 26", Notes: "5 props; engineer-mode only"},
		{Kind: KindObjType, ID: uint8(TypeLong), Name: "LONG", SpecRef: "p. 27", Notes: "10 props (i32)"},
		{Kind: KindObjType, ID: uint8(TypeByte), Name: "BYTE", SpecRef: "p. 27", Notes: "10 props (u8)"},

		// ACP transport errors (MType=3, MCODE < 16). Spec §"ACP Header" p. 11.
		{Kind: KindXportErr, ID: uint8(TErrUndefined), Name: "undefined", SpecRef: "p. 11"},
		{Kind: KindXportErr, ID: uint8(TErrInternalBusComm), Name: "internal-bus-comm", SpecRef: "p. 11"},
		{Kind: KindXportErr, ID: uint8(TErrInternalBusTimeout), Name: "internal-bus-timeout", SpecRef: "p. 11"},
		{Kind: KindXportErr, ID: uint8(TErrTransactionTimeout), Name: "transaction-timeout", SpecRef: "p. 11"},
		{Kind: KindXportErr, ID: uint8(TErrOutOfResources), Name: "out-of-resources", SpecRef: "p. 11"},

		// AxonNet object errors (MType=3, MCODE >= 16). Spec §"AxonNet error codes" p. 29.
		{Kind: KindObjErr, ID: uint8(OErrGroupNoExist), Name: "object-group-not-exist", SpecRef: "p. 29"},
		{Kind: KindObjErr, ID: uint8(OErrInstanceNoExist), Name: "object-instance-not-exist", SpecRef: "p. 29"},
		{Kind: KindObjErr, ID: uint8(OErrPropertyNoExist), Name: "object-property-not-exist", SpecRef: "p. 29"},
		{Kind: KindObjErr, ID: uint8(OErrNoWriteAccess), Name: "no-write-access", SpecRef: "p. 29"},
		{Kind: KindObjErr, ID: uint8(OErrNoReadAccess), Name: "no-read-access", SpecRef: "p. 29"},
		{Kind: KindObjErr, ID: uint8(OErrNoSetDefAccess), Name: "no-setDefault-access", SpecRef: "p. 29"},
		{Kind: KindObjErr, ID: uint8(OErrTypeNoExist), Name: "object-type-not-exist", SpecRef: "p. 29"},
		{Kind: KindObjErr, ID: uint8(OErrIllegalMethod), Name: "illegal-method", SpecRef: "p. 29"},
		{Kind: KindObjErr, ID: uint8(OErrIllegalForType), Name: "illegal-method-for-type", SpecRef: "p. 29"},
		{Kind: KindObjErr, ID: uint8(OErrFile), Name: "file-error", SpecRef: "p. 29"},
		{Kind: KindObjErr, ID: uint8(OErrSPFConstraint), Name: "SPF-constraint-violation", SpecRef: "p. 29"},
		{Kind: KindObjErr, ID: uint8(OErrSPFBufferFull), Name: "SPF-buffer-full", SpecRef: "p. 29", Notes: "retry"},
	}
	return out
}

// LookupCatalogue returns the entry matching `<kind>:<id>` (e.g.
// "method:0"). Returns false if either the kind is unknown or the id
// isn't in this codec's catalogue.
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

// splitAddress parses "<kind>:<id>" into its components.
func splitAddress(s string) (CommandKind, uint8, bool) {
	for i := 0; i < len(s); i++ {
		if s[i] == ':' {
			k := CommandKind(s[:i])
			rest := s[i+1:]
			n, err := decToUint8(rest)
			if err {
				return "", 0, false
			}
			return k, n, true
		}
	}
	return "", 0, false
}

// uint8ToDec / decToUint8 are tiny stdlib-free converters used by the
// catalogue helpers — keeps consumer/types.go independent of strconv
// imports it doesn't already pull in.
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
