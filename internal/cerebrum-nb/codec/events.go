package codec

import "strings"

// ----------------------------------------------------------------------
// §5.1 — Routing events / subscribes
// ----------------------------------------------------------------------

// RoutingChange is one row in keys.md "§5.1 Routing". On TX (inside
// <subscribe>/<obtain>/<unsubscribe>) only the addressing attrs are
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
		ForceAdd("type", r.Type).
		Add("device_name", r.DeviceName).
		Add("device_type", string(r.DeviceType)).
		Add("srce_id", r.SrceID).
		Add("srce_name", r.SrceName).
		Add("dest_id", r.DestID).
		Add("dest_name", r.DestName).
		Add("level_id", r.LevelID).
		Add("level_name", r.LevelName)
	emitElement(b, "routing_change", a, nil)
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
		ForceAdd("type", c.Type).
		Add("category", c.Category)
	emitElement(b, "category_change", a, nil)
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
		ForceAdd("type", s.Type).
		Add("group", s.Group).
		Add("instance", s.Instance)
	emitElement(b, "salvo_change", a, nil)
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
		ForceAdd("type", d.Type).
		Add("ip_address", d.IPAddress).
		Add("device_type", string(d.DeviceType)).
		Add("device_name", d.DeviceName).
		Add("sub_device", d.SubDevice).
		Add("object", d.Object)
	emitElement(b, "device_change", a, nil)
}

// ----------------------------------------------------------------------
// §5.5 — Datastore events / subscribes
// ----------------------------------------------------------------------

// DatastoreChange covers §5.5: subscription/obtain by file path inside
// a Cerebrum data store. RX replies echo this element with a `type`
// attribute (e.g. ATTRIBUTE).
type DatastoreChange struct {
	Name string
	Type string // populated on RX, ignored on TX
}

func (d *DatastoreChange) encodeSubItem(b *strings.Builder) {
	a := AttrsBuilder{}.
		Add("name", d.Name)
	emitElement(b, "datastore_change", a, nil)
}
