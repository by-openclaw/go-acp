package codec

import "strings"

// DeviceType is the §3.1 enum: Router / SNMP / Device. The wire form
// is the canonical capitalised string — DO NOT lowercase.
type DeviceType string

const (
	DeviceTypeRouter DeviceType = "Router"
	DeviceTypeSNMP   DeviceType = "SNMP"
	DeviceTypeDevice DeviceType = "Device"
)

// LockKind is the §3.2 LOCK enum. Only PROTECT + RELEASE observed in
// spec examples; treat the type as open for forward-compat.
type LockKind string

const (
	LockProtect LockKind = "PROTECT"
	LockRelease LockKind = "RELEASE"
)

// ItemType is the §3.3 enum for category items (from the Skyline driver
// since spec §3.3 is image-only).
type ItemType string

const (
	ItemBlank    ItemType = "BLANK"
	ItemSrce     ItemType = "SRCE"
	ItemSource   ItemType = "SOURCE"
	ItemDest     ItemType = "DEST"
	ItemCategory ItemType = "CATEGORY"
	ItemSalvo    ItemType = "SALVO"
	ItemInherit  ItemType = "INHERIT"
	ItemText     ItemType = "TEXT"
	ItemFile     ItemType = "FILE"
	ItemCustom   ItemType = "CUSTOM"
)

// ----------------------------------------------------------------------
// §4.1 — Routing actions
// ----------------------------------------------------------------------

// RoutingAction is one row in keys.md "§4.1 Routing". TYPE selects
// which fields are required; the encoder only emits non-empty fields.
type RoutingAction struct {
	// TYPE: one of ROUTE / SRCE_LOCK / DEST_LOCK / LEVEL_MNE / SRCE_MNE
	// / DEST_MNE / SRCE_ASSOC / DEST_ASSOC / SRCE_ASSOC_IP /
	// DEST_ASSOC_IP / RM_SRCE_TAGS / RM_DEST_TAGS.
	Type string

	// Common addressing
	DeviceName string
	DeviceType DeviceType
	IPAddress  string

	// ROUTE
	SrceID        string
	SrceName      string
	SrceLevelID   string
	SrceLevelName string
	DestID        string
	DestName      string
	DestLevelID   string
	DestLevelName string
	UseTags       string

	// LOCK actions
	LevelID   string
	LevelName string
	Lock      LockKind
	Duration  string

	// MNEMONIC actions
	Mnemonic string
	AltMne   string
	OnDevice string

	// ASSOC actions
	LogicalSrceID    string
	LogicalDestID    string
	LogicalLevelID   string
	TargetDeviceName string
	TargetDeviceType DeviceType
	TargetLevelID    string
	TargetSrceID     string
	TargetDestID     string
	TargetSenderName string
	TargetReceiverName string
	SubDevice        string

	// RM_TAGS actions
	Tags string
}

// encodeAction satisfies ActionBody. Spec §4.1 attributes use
// UPPERCASE per the spec examples — we honour that for routing.
func (r *RoutingAction) encodeAction(b *strings.Builder) {
	a := AttrsBuilder{}.
		ForceAdd("TYPE", r.Type).
		Add("DEVICE_NAME", r.DeviceName).
		Add("DEVICE_TYPE", string(r.DeviceType)).
		Add("IP_ADDRESS", r.IPAddress).
		Add("SRCE_ID", r.SrceID).
		Add("SRCE_NAME", r.SrceName).
		Add("SRCE_LEVEL_ID", r.SrceLevelID).
		Add("SRCE_LEVEL_NAME", r.SrceLevelName).
		Add("DEST_ID", r.DestID).
		Add("DEST_NAME", r.DestName).
		Add("DEST_LEVEL_ID", r.DestLevelID).
		Add("DEST_LEVEL_NAME", r.DestLevelName).
		Add("USE_TAGS", r.UseTags).
		Add("LEVEL_ID", r.LevelID).
		Add("LEVEL_NAME", r.LevelName).
		Add("LOCK", string(r.Lock)).
		Add("DURATION", r.Duration).
		Add("MNEMONIC", r.Mnemonic).
		Add("ALT_MNE", r.AltMne).
		Add("ON_DEVICE", r.OnDevice).
		Add("LOGICAL_SRCE_ID", r.LogicalSrceID).
		Add("LOGICAL_DEST_ID", r.LogicalDestID).
		Add("LOGICAL_LEVEL_ID", r.LogicalLevelID).
		Add("TARGET_DEVICE_NAME", r.TargetDeviceName).
		Add("TARGET_DEVICE_TYPE", string(r.TargetDeviceType)).
		Add("TARGET_LEVEL_ID", r.TargetLevelID).
		Add("TARGET_SRCE_ID", r.TargetSrceID).
		Add("TARGET_DEST_ID", r.TargetDestID).
		Add("TARGET_SENDER_NAME", r.TargetSenderName).
		Add("TARGET_RECEIVER_NAME", r.TargetReceiverName).
		Add("SUB_DEVICE", r.SubDevice).
		Add("TAGS", r.Tags)
	emitElement(b, "routing", a, nil)
}

// ----------------------------------------------------------------------
// §4.2 — Category actions
// ----------------------------------------------------------------------

// CategoryAction wraps the §4.2 catalogue. The enclosing element is
// <category>; type selects MODIFY_ITEM / MODIFY_ALL / MODIFY_DESC /
// CREATE / DELETE / DELETE_ITEM.
type CategoryAction struct {
	Type        string
	Category    string
	Index       string
	ItemType    ItemType
	Value       string
	Name        string
	Label       string
	Inherits    string
	Description string
}

func (c *CategoryAction) encodeAction(b *strings.Builder) {
	a := AttrsBuilder{}.
		ForceAdd("type", c.Type).
		Add("category", c.Category).
		Add("index", c.Index).
		Add("item_type", string(c.ItemType)).
		Add("value", c.Value).
		Add("name", c.Name).
		Add("label", c.Label).
		Add("inherits", c.Inherits).
		Add("description", c.Description)
	emitElement(b, "category", a, nil)
}

// ----------------------------------------------------------------------
// §4.3 — Salvo actions
// ----------------------------------------------------------------------

// SalvoAction is the §4.3 catalogue: RUN / SAVE / RENAME / DESCRIPTION
// / DELETE.
type SalvoAction struct {
	Type        string
	Group       string
	Instance    string
	NewName     string
	Description string
}

func (s *SalvoAction) encodeAction(b *strings.Builder) {
	a := AttrsBuilder{}.
		ForceAdd("type", s.Type).
		Add("group", s.Group).
		Add("instance", s.Instance).
		Add("new_name", s.NewName).
		Add("description", s.Description)
	emitElement(b, "salvo", a, nil)
}

// ----------------------------------------------------------------------
// §4.4 — Device actions
// ----------------------------------------------------------------------

// DeviceAction is the §4.4 catalogue: SET_VALUE only (in 0.13).
type DeviceAction struct {
	Type       string
	DeviceName string
	SubDevice  string
	Object     string
	Value      string
}

func (d *DeviceAction) encodeAction(b *strings.Builder) {
	a := AttrsBuilder{}.
		ForceAdd("type", d.Type).
		Add("device_name", d.DeviceName).
		Add("sub_device", d.SubDevice).
		Add("object", d.Object).
		Add("value", d.Value)
	emitElement(b, "device", a, nil)
}
