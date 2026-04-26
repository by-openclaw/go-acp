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
