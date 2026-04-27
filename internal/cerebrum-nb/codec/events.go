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

	// Addressing (use one of name / id pair per row in §5.1)
	SrceID    string
	SrceName  string
	DestID    string
	DestName  string
	LevelID   string
	LevelName string
}

func (r *RoutingChange) encodeSubItem(b *strings.Builder) {
	a := AttrsBuilder{}.
		ForceAdd("TYPE", r.Type).
		Add("DEVICE_NAME", r.DeviceName).
		Add("DEVICE_TYPE", string(r.DeviceType)).
		Add("SRCE_ID", r.SrceID).
		Add("SRCE_NAME", r.SrceName).
		Add("DEST_ID", r.DestID).
		Add("DEST_NAME", r.DestName).
		Add("LEVEL_ID", r.LevelID).
		Add("LEVEL_NAME", r.LevelName)
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

// SalvoInstanceDetails is the <details> child of a SALVO_CHANGE
// TYPE=INSTANCE_DETAILS response.
type SalvoInstanceDetails struct {
	Available bool
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
	ObjectValue *DeviceObjectValue
}

// DeviceObjectValue is the <object_value> child of a DEVICE_CHANGE
// TYPE=VALUE response.
type DeviceObjectValue struct {
	Available bool
	Object    string
}

// DeviceEntry is one row of a TYPE=LIST DEVICE_CHANGE response (or one
// nested <sub_devices><device …/></sub_devices> entry under DETAILS).
type DeviceEntry struct {
	IPAddress  string
	DeviceType DeviceType
	DeviceName string
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

// DatastoreChange covers §5.5: subscription/obtain by file path inside
// a Cerebrum data store. RX replies echo this element with a TYPE
// attribute (e.g. ATTRIBUTE).
type DatastoreChange struct {
	Name string
	Type string // populated on RX, ignored on TX
}

func (d *DatastoreChange) encodeSubItem(b *strings.Builder) {
	a := AttrsBuilder{}.
		Add("NAME", d.Name)
	emitElement(b, "DATASTORE_CHANGE", a, nil)
}
