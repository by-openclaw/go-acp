package codec

import "strings"

// All TX element + attribute names emitted UPPERCASE per the
// wire-actual canonical form (verified against a live Cerebrum
// 2026-04-26).

// ----------------------------------------------------------------------
// §5.1 — Routing events / subscribes
// ----------------------------------------------------------------------

// RoutingChange is one row in keys.md "§5.1 Routing". On TX (inside
// <SUBSCRIBE>/<OBTAIN>/<UNSUBSCRIBE>) only the addressing attrs are
// honoured; on RX the server fills in current state.
type RoutingChange struct {
	// Type: ROUTE / SRCE_LOCK / DEST_LOCK / LEVEL_MNE / SRCE_MNE /
	// DEST_MNE / RM_SRCE_TAGS / RM_DEST_TAGS.
	Type string

	DeviceName string
	DeviceType DeviceType
	IPAddress  string // §1.7 alternative to DeviceName; required for the route-master sentinel ("0.0.0.0" + DEVICE_TYPE="ROUTER")

	// Addressing (use one of name / id pair per row in §5.1)
	SrceID    string
	SrceName  string
	DestID    string
	DestName  string
	LevelID   string
	LevelName string

	// MNE rows (§5.1.4 LEVEL_MNE / §5.1.5 SRCE_MNE / §5.1.6 DEST_MNE)
	// carry the actual name string in MNEMONIC and a slot index in
	// ALT_MNE. On TX (subscribe filter), Mnemonic + AltMne are
	// emitted as attributes per §4.1.4-§4.1.6.
	//
	// On RX, Cerebrum returns slot-keyed CHILD ELEMENTS instead — one
	// <mne mnemonic="..."/> for slot 0 (the primary), and one
	// <alt_mne_N mnemonic="..."/> per active alternate (1..N). Decoder
	// flattens these into Mnemonics[slot] = name. Slots whose child
	// carries `available="0"` are dropped. (Live wire 2026-04-30 against
	// Cerebrum at 10.41.64.95.)
	Mnemonic  string
	AltMne    string
	Mnemonics map[int]string

	// RouteSourceID / RouteSourceLevelID populate from the <route> child
	// of a TYPE=ROUTE routing_change RX row (cross-level routes carry a
	// source_level_id distinct from the row's level_id). RouteAvailable
	// reflects the child's `available` attribute.
	RouteSourceID      string
	RouteSourceLevelID string
	RouteAvailable     bool

	// Lock populates on RX for TYPE=SRCE_LOCK / DEST_LOCK (§5.1.2/§5.1.3
	// p24). SRCE_LOCK carries the detail in a <VALUE …/> child;
	// DEST_LOCK carries it in a <LOCK …/> child — the decoder accepts
	// both element names. The 0v16 LOCK_STATE enum is the §3.2 five-value
	// set (UNLOCKED / LOCKED / PROTECTED / LOCKED_PATH / PROTECTED_PATH).
	Lock *RoutingLock

	// OriginalMne / Associations / RMDefaultDevice populate on RX for
	// the mnemonic rows (§5.1.4-§5.1.6 pp25-26). OriginalMne is the
	// device's un-overridden mnemonic from the <MNE ORIGINAL_MNE=…/>
	// child. RMDefaultDevice is present only on LEVEL_MNE for Routemaster
	// levels. Associations carry per-level routing-master bindings.
	OriginalMne     string
	Associations    []RoutingAssociation
	RMDefaultDevice *RMDefaultDevice

	// Tags populates on RX for TYPE=RM_SRCE_TAGS / RM_DEST_TAGS
	// (§5.1.7/§5.1.8 p27). The <TAGS LIST="Tag1|Tag2"/> child uses the
	// pipe delimiter (§0.13 change-history p33), split into TagList.
	TagsAvailable bool
	TagList       []string
}

// RoutingLock is the lock detail of a SRCE_LOCK (<VALUE>) or DEST_LOCK
// (<LOCK>) routing_change RX row (§5.1.2/§5.1.3 p24).
type RoutingLock struct {
	Available  bool
	LockState  LockKind // §3.2 enum
	LockedBy   string
	UnlockTime string
	UnlockDate string
}

// RoutingAssociation is one <ASSOCIATION_N …/> row of a mnemonic
// routing_change RX (§5.1.5/§5.1.6 pp25-26). Index is the N suffix.
// SRCE_ID is present on source-mnemonic rows, DEST_ID on destination
// rows. The RM_* attributes are populated only for Routemaster sources.
type RoutingAssociation struct {
	Index        int
	SrceID       string
	DestID       string
	RMDeviceName string
	RMDeviceType DeviceType
	RMLevelID    string
	RMReversed   string
	RMTags       string
	RMIPUID      string
}

// RMDefaultDevice is the <RM_DEFAULT_DEVICE …/> child of a LEVEL_MNE
// routing_change RX — present only for Routemaster levels (§5.1.4 p25).
type RMDefaultDevice struct {
	DeviceName string
	DeviceType DeviceType
	LevelID    string
}

func (r *RoutingChange) encodeSubItem(b *strings.Builder) {
	a := AttrsBuilder{}.
		ForceAdd("TYPE", r.Type).
		Add("IP_ADDRESS", r.IPAddress).
		Add("DEVICE_NAME", r.DeviceName).
		Add("DEVICE_TYPE", string(r.DeviceType)).
		Add("SRCE_ID", r.SrceID).
		Add("SRCE_NAME", r.SrceName).
		Add("DEST_ID", r.DestID).
		Add("DEST_NAME", r.DestName).
		Add("LEVEL_ID", r.LevelID).
		Add("LEVEL_NAME", r.LevelName).
		Add("MNEMONIC", r.Mnemonic).
		Add("ALT_MNE", r.AltMne)
	emitElement(b, "ROUTING_CHANGE", a, nil)
}

// ----------------------------------------------------------------------
// §5.2 — Category events / subscribes
// ----------------------------------------------------------------------

// CategoryChange covers the §5.2 catalogue: CATEGORY_LIST / CATEGORY_DETAILS.
type CategoryChange struct {
	Type     string
	Category string

	// Categories is populated on RX for TYPE=CATEGORY_LIST. Live
	// Cerebrum returns one inner <CATEGORY LIST="A,B,C,..."/> with a
	// comma-separated list (verified 2026-04-27). We split into
	// individual entries here for ergonomic iteration.
	Categories []string

	// Details populate on RX for TYPE=CATEGORY_DETAILS (verified
	// 2026-04-27). The server emits `descsription` (sic) as the
	// description attribute; the decoder accepts both spellings.
	Details *CategoryDetailsInfo
}

// CategoryDetailsInfo is the <details> child of a CATEGORY_CHANGE
// TYPE=CATEGORY_DETAILS response.
type CategoryDetailsInfo struct {
	Label       string
	Available   bool
	Description string

	// Items is the parsed <items> sibling of <details> — positional
	// slots in the Cerebrum panel grid. Each slot has a TYPE
	// (CATEGORY / SOURCE / DESTINATION / BLANK) and a VALUE; BLANK
	// slots are dropped here, callers iterate Items by Index when
	// they need positional fidelity.
	//
	// Element names on the wire are <ITEM_N …/> with N starting at 1.
	// Live capture 2026-04-27 against real Cerebrum.
	Items []CategoryItem
}

// CategoryItem is one positional slot of a CATEGORY_DETAILS items grid.
type CategoryItem struct {
	Index int
	Type  string // "CATEGORY" | "SOURCE" | "DESTINATION" | "BLANK"
	Value string

	// Group is the optional GROUP attribute some item types carry
	// (§5.2.2 p28 note: "Items may also contain a GROUP attribute for
	// category types requiring group information" — e.g. SALVO items,
	// per the §3.3 ITEM_TYPE table). Empty when absent.
	Group string
}

func (c *CategoryChange) encodeSubItem(b *strings.Builder) {
	a := AttrsBuilder{}.
		ForceAdd("TYPE", c.Type).
		Add("CATEGORY", c.Category)
	emitElement(b, "CATEGORY_CHANGE", a, nil)
}

// ----------------------------------------------------------------------
// §5.3 — Salvo events / subscribes
// ----------------------------------------------------------------------

// SalvoChange covers the §5.3 catalogue: GROUP_LIST / INSTANCE_LIST /
// INSTANCE_DETAILS.
type SalvoChange struct {
	Type     string
	Group    string
	Instance string

	// Groups is populated on RX for TYPE=GROUP_LIST. Live Cerebrum
	// returns one inner <GROUPS LIST="..."/> with a comma-separated
	// list (verified 2026-04-27).
	Groups []string

	// Instances is populated on RX for TYPE=INSTANCE_LIST. Server
	// emits one <instances list="A,B,..."/> child (same CSV pattern
	// as GROUP_LIST; verified 2026-04-27).
	Instances []string

	// InstanceDetails populates on RX for TYPE=INSTANCE_DETAILS
	// (verified 2026-04-27). When Available is false the server
	// returns an empty <details available="0"/>.
	InstanceDetails *SalvoInstanceDetails
}

// SalvoInstanceDetails is the <DETAILS> child of a SALVO_CHANGE
// TYPE=INSTANCE_DETAILS response (§5.3.3 p28). 0v16 enriches it with
// DESCRIPTION / ACTIVE / DATE / TIME alongside AVAILABLE.
type SalvoInstanceDetails struct {
	Available   bool
	Description string
	Active      bool
	Date        string // e.g. "02/07/2017"
	Time        string // e.g. "17:33:22:12"
}

func (s *SalvoChange) encodeSubItem(b *strings.Builder) {
	a := AttrsBuilder{}.
		ForceAdd("TYPE", s.Type).
		Add("GROUP", s.Group).
		Add("INSTANCE", s.Instance)
	emitElement(b, "SALVO_CHANGE", a, nil)
}

// ----------------------------------------------------------------------
// §5.4 — Device events / subscribes
// ----------------------------------------------------------------------

// DeviceChange covers the §5.4 catalogue: LIST / DETAILS / VALUE.
type DeviceChange struct {
	Type       string
	IPAddress  string
	DeviceType DeviceType
	DeviceName string
	SubDevice  string
	Object     string

	// Devices is populated on RX for TYPE=LIST. Live Cerebrum nests
	// one <DEVICE IP="..."> per entry, each containing an
	// <INSTANCE DEVICE_TYPE="..."/> child (verified 2026-04-27 — the
	// outer attribute name is the short "IP", not the spec's
	// "IP_ADDRESS"). We accept both spellings.
	Devices []DeviceEntry

	// Details / Service / Connection / SubDevices populate on RX for
	// TYPE=DETAILS (verified 2026-04-27 against a Lawo Powercore peer
	// at 10.107.30.100). Spec keys.md §5.4 names only ip_address +
	// device_type as TX attrs; the RX shape carries vendor metadata
	// in nested elements.
	Details    *DeviceDetails
	Service    *DeviceService
	Connection *DeviceConnection
	SubDevices []DeviceEntry

	// ObjectValue populates on RX for TYPE=VALUE. When Available is
	// false the server reports the object as unknown / unsupported;
	// Value carries the live reading otherwise (verified shape on
	// 2026-04-27 against a real Cerebrum returning available="0").
	// For a single scalar object this is the first/only OBJECT_VALUE;
	// ObjectValues carries every OBJECT_VALUE the server emitted.
	ObjectValue *DeviceObjectValue

	// ObjectValues holds all <OBJECT_VALUE …/> children of a TYPE=VALUE
	// response. A scalar yields one entry; a group / table path (or a
	// wildcard table path such as Devices.[*]) yields several (§5.4.3
	// p29). ObjectValue == ObjectValues[0] when present.
	ObjectValues []DeviceObjectValue
}

// DeviceObjectValue is one <OBJECT_VALUE …/> child of a DEVICE_CHANGE
// TYPE=VALUE response (§5.4.3 p29). 0v13 captured only Available +
// Object; 0v16 carries the full descriptor.
type DeviceObjectValue struct {
	Object    string
	Value     string
	Available bool
	DataType  string // INTEGER / STRING / …
	Readable  bool
	Writable  bool
	Units     string
	Label     string
	Default   string
	Min       string // §5.4.3 range attrs (live 2026-08-16: MIN/MAX/STEP on FLOAT objects)
	Max       string
	Step      string
	EnumList  []string // ENUM_LIST="On,Off" split on comma
}

// DeviceEntry is one row of a TYPE=LIST DEVICE_CHANGE response (or one
// nested <sub_devices><device …/></sub_devices> entry under DETAILS).
//
// Per spec §3.1, Cerebrum classifies every device into one or more of
// 'Device' / 'Router' / 'SNMP'. The wire surfaces each class as a
// separate <INSTANCE DEVICE_TYPE="..."/> child of <DEVICE> (live wire
// 2026-04-27). DeviceTypes preserves every instance the server emitted,
// in order. DeviceType is kept for backwards compat — it returns the
// first non-empty class so existing callers that only want "primary
// class" still work.
type DeviceEntry struct {
	IPAddress   string
	DeviceType  DeviceType   // first instance — convenience accessor
	DeviceTypes []DeviceType // every <INSTANCE DEVICE_TYPE="..."/> emitted by the server
	DeviceName  string

	// Index / states populate for the positional SUB_DEVICES shape a live
	// NOC Cerebrum emits under DETAILS (2026-08-16, Neuron shelf
	// bm-n-nnshf-004): <SUB_DEVICES><DEVICE_1 TYPE="SHUFFLE-256"
	// PRIMARY_STATE="Connection Active" SECONDARY_STATE="..."/> — the
	// child is DEVICE_N (not <DEVICE>) and TYPE carries the sub-device
	// model, which lands in DeviceName. Index is N.
	Index          int
	PrimaryState   string
	SecondaryState string
}

// DeviceDetails is the <details> child of a DEVICE_CHANGE TYPE=DETAILS
// response. ip1/ip2 carry the primary / secondary control-network IPs
// (ST 2022-7 dual-network); VendorType is the device-model identifier
// distinct from the outer device_type enum (e.g. "Powercore").
type DeviceDetails struct {
	IP1, IP2   string
	Name       string
	VendorType string
}

// DeviceService is the <service> child — the data-plane interfaces
// the device uses for its essence streams.
type DeviceService struct {
	IP1, IP2 string
}

// DeviceConnection is the <connection> child — current health of the
// primary / secondary control-network connections, returned as
// human-readable strings (e.g. "Connection Active",
// "Connection Not Configured").
type DeviceConnection struct {
	PrimaryState   string
	SecondaryState string
}

func (d *DeviceChange) encodeSubItem(b *strings.Builder) {
	a := AttrsBuilder{}.
		ForceAdd("TYPE", d.Type).
		Add("IP_ADDRESS", d.IPAddress).
		Add("DEVICE_TYPE", string(d.DeviceType)).
		Add("DEVICE_NAME", d.DeviceName).
		Add("SUB_DEVICE", d.SubDevice).
		Add("OBJECT", d.Object)
	emitElement(b, "DEVICE_CHANGE", a, nil)
}

// ----------------------------------------------------------------------
// §5.5 — Datastore events / subscribes
// ----------------------------------------------------------------------

// DatastoreChange covers §5.5: subscription / obtain of a Cerebrum data
// store by relative path, plus the two RX shapes (the obtain reply with
// a <DATA> body, and the async change event with an <ELEMENT> body).
//
// PATH vs NAME ambiguity (§5.5.1 p30): the worked TX example uses
// PATH="…", but the attribute table immediately below names the field
// NAME. They cannot both be literal. Following the audit decision we
// EMIT PATH (matching the only concrete example) and ACCEPT BOTH on
// decode. See the report's flagged ambiguity.
type DatastoreChange struct {
	// Path is the relative store path. Used as the obtain/subscribe key
	// (TX) and echoed on the change event (RX, in the NAME attribute).
	Path string

	// Name is a deprecated alias for Path, kept so 0v13-era callers that
	// read DatastoreChange.Name keep compiling. The decoder mirrors Path
	// into it. New code should use Path.
	//
	// Deprecated: use Path.
	Name string

	// MTID, when non-empty on TX, is emitted as the optional §5.5.1
	// MTID attribute so the bare terminator reply can be correlated.
	MTID string

	// Type populates on RX. Obtain reply: "FILE". Change event:
	// "ATTRIBUTE" | "ELEMENT" | "FILE" (§5.5.2 p31).
	Type string

	// Available populates on the obtain reply (§5.5.1 p30) — whether the
	// store file was read successfully.
	Available bool

	// Data is the raw inner XML of the <DATA>…</DATA> body of an obtain
	// reply (§5.5.1 p30). Empty for the bare terminator and for change
	// events.
	Data string

	// Terminator is true for the bare <DATASTORE_CHANGE MTID="…"/> that
	// follows the <DATA> reply (§5.5.1 p30) — no TYPE, no body.
	Terminator bool

	// Element populates on a change event (§5.5.2 p31) from the single
	// <ELEMENT …/> child.
	Element *DatastoreElement
}

// DatastoreElement is the <ELEMENT …/> child of a §5.5.2 datastore
// change event (p31).
type DatastoreElement struct {
	Attribute  string
	Path       string
	Value      string
	Available  bool
	ChildCount string
}

// encodeSubItem emits the §5.5.1 obtain/subscribe form. Per the
// PATH/NAME resolution above we emit PATH (and optional MTID).
func (d *DatastoreChange) encodeSubItem(b *strings.Builder) {
	a := AttrsBuilder{}.
		Add("PATH", d.Path).
		Add("MTID", d.MTID)
	emitElement(b, "DATASTORE_CHANGE", a, nil)
}
