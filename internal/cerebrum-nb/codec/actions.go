package codec

import (
	"strconv"
	"strings"
)

// DeviceType is the §3.1 enum: Router / SNMP / Device. The wire form
// is the canonical capitalised string — DO NOT lowercase.
//
// §3.1 (0v16 p9) also defines a sub-device colon syntax: a base type
// followed by ":N" addresses the Nth sub-module (e.g. "ROUTER:2" for
// the second matrix of an SW-P-08, "DEVICE:4" for the card in slot 4).
// Use ParseDeviceType / DeviceType.Base / DeviceType.Index to model
// base+index without losing the literal wire string.
type DeviceType string

const (
	DeviceTypeRouter DeviceType = "Router"
	DeviceTypeSNMP   DeviceType = "SNMP"
	DeviceTypeDevice DeviceType = "Device"
)

// Base returns the device type with any ":N" sub-device suffix removed.
// "ROUTER:2".Base() == "ROUTER"; "SNMP".Base() == "SNMP". §3.1 p9.
func (d DeviceType) Base() DeviceType {
	s := string(d)
	if i := strings.IndexByte(s, ':'); i >= 0 {
		return DeviceType(s[:i])
	}
	return d
}

// Index returns the sub-device index from a ":N" suffix and ok=true, or
// (0, false) when no suffix is present or N is not a valid integer.
// "DEVICE:4".Index() == (4, true); "DEVICE".Index() == (0, false).
// §3.1 p9.
func (d DeviceType) Index() (int, bool) {
	s := string(d)
	i := strings.IndexByte(s, ':')
	if i < 0 {
		return 0, false
	}
	n, err := strconv.Atoi(s[i+1:])
	if err != nil {
		return 0, false
	}
	return n, true
}

// MakeDeviceType joins a base type and a sub-device index into the
// colon wire form. index < 0 yields the bare base. §3.1 p9.
func MakeDeviceType(base DeviceType, index int) DeviceType {
	if index < 0 {
		return base
	}
	return DeviceType(string(base) + ":" + strconv.Itoa(index))
}

// LockKind is the §3.2 LOCK enum (0v16 p9). 0v16 defines five values;
// the pre-0v13 PROTECT / RELEASE strings the codec previously modelled
// were never in the spec table and are retained only as deprecated
// aliases so older callers keep compiling. The type is left open
// (string) for forward-compat against future spec additions.
type LockKind string

const (
	// §3.2 p9 canonical values.
	LockUnlocked      LockKind = "UNLOCKED"
	LockLocked        LockKind = "LOCKED"
	LockProtected     LockKind = "PROTECTED"
	LockLockedPath    LockKind = "LOCKED_PATH"
	LockProtectedPath LockKind = "PROTECTED_PATH"

	// Deprecated: not in the §3.2 table. The 4.1.2/4.1.3 worked
	// examples (p11-12) still show LOCK="RELEASE"/"PROTECT" verbs, so
	// these remain valid action arguments, but they are not LOCK_STATE
	// values a device reports back.
	LockProtect LockKind = "PROTECT"
	LockRelease LockKind = "RELEASE"
)

// ItemType is the §3.3 enum for category items (spec §3.3 is image-only;
// the values here are the canonical strings observed on the wire).
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
	emitElement(b, "ROUTING", a, nil)
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
		ForceAdd("TYPE", c.Type).
		Add("CATEGORY", c.Category).
		Add("INDEX", c.Index).
		Add("ITEM_TYPE", string(c.ItemType)).
		Add("VALUE", c.Value).
		Add("NAME", c.Name).
		Add("LABEL", c.Label).
		Add("INHERITS", c.Inherits).
		Add("DESCRIPTION", c.Description)
	emitElement(b, "CATEGORY", a, nil)
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
		ForceAdd("TYPE", s.Type).
		Add("GROUP", s.Group).
		Add("INSTANCE", s.Instance).
		Add("NEW_NAME", s.NewName).
		Add("DESCRIPTION", s.Description)
	emitElement(b, "SALVO", a, nil)
}

// ----------------------------------------------------------------------
// §4.4 — Device actions
// ----------------------------------------------------------------------

// DeviceAction is the §4.4 catalogue: SET_VALUE. 0v16 (§4.4.1 p19) adds
// the IP_ADDRESS alternative to DEVICE_NAME, plus the IS_ENUM and MODE
// attributes.
type DeviceAction struct {
	Type       string
	DeviceName string
	IPAddress  string // §4.4.1: DEVICE_NAME *or* IP_ADDRESS may address the device
	SubDevice  string
	Object     string
	Value      string
	IsEnum     bool   // §4.4.1: IS_ENUM="1" — VALUE is a string repr of an enum (0/omitted by default)
	Mode       string // §4.4.1: CSV MODE — SET | ADD_TAIL | INSERT_AT_HEAD | REMOVE (SET by default)
}

func (d *DeviceAction) encodeAction(b *strings.Builder) {
	a := AttrsBuilder{}.
		ForceAdd("TYPE", d.Type).
		Add("DEVICE_NAME", d.DeviceName).
		Add("IP_ADDRESS", d.IPAddress).
		Add("SUB_DEVICE", d.SubDevice).
		Add("OBJECT", d.Object).
		Add("VALUE", d.Value).
		Add("MODE", d.Mode)
	if d.IsEnum {
		a = a.Add("IS_ENUM", "1")
	}
	emitElement(b, "DEVICE", a, nil)
}

// ----------------------------------------------------------------------
// §4.5 — Device Configuration (0v16 pp20-23)
//
// CRUD over the Cerebrum device tree. Unlike §4.1-§4.4 these are NOT
// wrapped in <ACTION>; <DEVICE_CONFIGURATION> is itself a top-level
// command, so it has its own EncodeDeviceConfiguration entry point.
//
// The body shape under <CONFIGURATION> varies by DEVICE_TYPE
// (GENERIC / PANEL / ROUTER / SNMP). REMOVE carries no body (§4.5.2
// p23). The RX reply is <DEVICE_CONFIGURATION …><RESULT VALUE="…"/>
// </DEVICE_CONFIGURATION> — decoded as KindDeviceConfigResult.
// ----------------------------------------------------------------------

// DeviceConfigType is the §4.5 <DEVICE_CONFIGURATION TYPE=…> verb.
type DeviceConfigType string

const (
	DeviceConfigAdd    DeviceConfigType = "ADD"
	DeviceConfigModify DeviceConfigType = "MODIFY"
	DeviceConfigRemove DeviceConfigType = "REMOVE"
)

// ConfigDeviceType is the §4.5 DEVICE_TYPE selector. Distinct from the
// §3.1 DeviceType enum: §4.5 uses "GENERIC" where §3.1 uses "Device",
// and adds "PANEL". Keep the literal spec strings (p20-23).
type ConfigDeviceType string

const (
	ConfigDeviceGeneric ConfigDeviceType = "GENERIC"
	ConfigDevicePanel   ConfigDeviceType = "PANEL"
	ConfigDeviceRouter  ConfigDeviceType = "ROUTER"
	ConfigDeviceSNMP    ConfigDeviceType = "SNMP"
)

// ConnectionType is the §4.5 <PROTOCOL_CONFIGURATION CONNECTION_TYPE=…>
// enum (p20): ASYNC_HTTP / UDP / TCP / WEBSOCKET_SERVER.
type ConnectionType string

const (
	ConnAsyncHTTP        ConnectionType = "ASYNC_HTTP"
	ConnUDP              ConnectionType = "UDP"
	ConnTCP              ConnectionType = "TCP"
	ConnWebsocketServer  ConnectionType = "WEBSOCKET_SERVER"
)

// DeviceConfiguration is one §4.5 device CRUD command. Type selects the
// verb; DeviceType selects which body builder runs (Generic / Panel /
// Router / SNMP). For REMOVE, all body pointers are ignored (§4.5.2).
type DeviceConfiguration struct {
	Type       DeviceConfigType
	IPAddress  string
	DeviceName string // optional on ADD; required (with IP) on MODIFY
	DeviceType ConfigDeviceType

	// Exactly one of the following is honoured, chosen by DeviceType.
	Generic *GenericConfig // DEVICE_TYPE="GENERIC" (§4.5.1.1 p20)
	Panel   *PanelConfig   // DEVICE_TYPE="PANEL"   (§4.5.1.2 p21)
	Router  *RouterConfig  // DEVICE_TYPE="ROUTER"  (§4.5.1.3 p22)
	SNMP    *SNMPConfig    // DEVICE_TYPE="SNMP"    (§4.5.1.4 p23)
}

// ProtocolConfiguration is the §4.5.1.1 <PROTOCOL_CONFIGURATION> child
// shared by GENERIC + PANEL bodies (p20-21).
type ProtocolConfiguration struct {
	ConnectionType ConnectionType
	PortNumber     string
	Timeout        string // milliseconds
	PollPeriod     string // milliseconds
	Virtualise     string // "0" or "1"
	SecondaryIP    string // optional
}

func (p *ProtocolConfiguration) attrs() AttrsBuilder {
	return AttrsBuilder{}.
		Add("CONNECTION_TYPE", string(p.ConnectionType)).
		Add("PORT_NUMBER", p.PortNumber).
		Add("TIMEOUT", p.Timeout).
		Add("POLL_PERIOD", p.PollPeriod).
		Add("VIRTUALISE", p.Virtualise).
		Add("SECONDARY_IP", p.SecondaryIP)
}

// GenericConfig is the §4.5.1.1 GENERIC body (p20):
//
//	<CONFIGURATION DEVICE VERSION NAME>
//	  <PROTOCOL_CONFIGURATION …/>
//	  [<PROTOCOL_SPECIFIC>…</PROTOCOL_SPECIFIC>]
//	</CONFIGURATION>
type GenericConfig struct {
	Device  string
	Version string
	Name    string

	Protocol *ProtocolConfiguration

	// ProtocolSpecific, when non-nil, emits a <PROTOCOL_SPECIFIC>
	// child. The spec (p20) says its content is protocol-dependent and
	// not exhaustively documented; we carry raw inner XML so callers can
	// supply whatever the target protocol needs without the codec
	// guessing a schema. Empty string emits <PROTOCOL_SPECIFIC/>.
	ProtocolSpecific *string
}

func (g *GenericConfig) encodeBody(b *strings.Builder) {
	a := AttrsBuilder{}.
		Add("DEVICE", g.Device).
		Add("VERSION", g.Version).
		Add("NAME", g.Name)
	emitElement(b, "CONFIGURATION", a, func() {
		if g.Protocol != nil {
			emitElement(b, "PROTOCOL_CONFIGURATION", g.Protocol.attrs(), nil)
		}
		if g.ProtocolSpecific != nil {
			emitRawChild(b, "PROTOCOL_SPECIFIC", nil, *g.ProtocolSpecific)
		}
	})
}

// PanelConfig is the §4.5.1.2 PANEL body (p21). It reuses
// <PROTOCOL_CONFIGURATION> and adds a <PROTOCOL_SPECIFIC CPF PANEL_ID
// PANEL_TYPE> wrapper around a <PANEL_CONFIG> child.
type PanelConfig struct {
	Device  string
	Version string
	Name    string

	Protocol *ProtocolConfiguration

	CPF       string
	PanelID   string // optional
	PanelType string

	// PanelConfigInner is the raw inner XML of <PANEL_CONFIG>. The spec
	// example shows an empty <PANEL_CONFIG></PANEL_CONFIG>; content is
	// protocol-specific and undocumented (p21).
	PanelConfigInner string
}

func (p *PanelConfig) encodeBody(b *strings.Builder) {
	a := AttrsBuilder{}.
		Add("DEVICE", p.Device).
		Add("VERSION", p.Version).
		Add("NAME", p.Name)
	emitElement(b, "CONFIGURATION", a, func() {
		if p.Protocol != nil {
			emitElement(b, "PROTOCOL_CONFIGURATION", p.Protocol.attrs(), nil)
		}
		ps := AttrsBuilder{}.
			Add("CPF", p.CPF).
			Add("PANEL_ID", p.PanelID).
			Add("PANEL_TYPE", p.PanelType)
		emitElement(b, "PROTOCOL_SPECIFIC", ps, func() {
			emitRawChild(b, "PANEL_CONFIG", nil, p.PanelConfigInner)
		})
	})
}

// RouterConfig is the §4.5.1.3 ROUTER body (p22):
//
//	<CONFIGURATION DEVICE NAME TYPE SECONDARY_IP>
//	  <SERIAL [BAUD PARITY DATA_BITS STOP_BITS_IP ENABLE_DSOM]/>
//	  <ROUTER MAX_LEVEL MAX_SOURCE MAX_DEST IP_PORT MASTER_SERVER/>
//	</CONFIGURATION>
type RouterConfig struct {
	Device      string
	Name        string
	Type        string // numeric routing-device type id
	SecondaryIP string // optional

	Serial *SerialConfig // emits <SERIAL …/>; nil omits (spec shows <SERIAL/> in examples)
	Router *RouterParams // emits <ROUTER …/>
}

// SerialConfig is the §4.5.1.3 <SERIAL> child (p22). All attrs optional;
// the worked example uses the bare self-closing <SERIAL/> form.
type SerialConfig struct {
	Baud       string
	Parity     string
	DataBits   string
	StopBitsIP string
	EnableDSOM string
}

// RouterParams is the §4.5.1.3 <ROUTER> child (p22).
type RouterParams struct {
	MaxLevel     string
	MaxSource    string
	MaxDest      string
	IPPort       string
	MasterServer string
}

func (r *RouterConfig) encodeBody(b *strings.Builder) {
	a := AttrsBuilder{}.
		Add("DEVICE", r.Device).
		Add("NAME", r.Name).
		Add("TYPE", r.Type).
		Add("SECONDARY_IP", r.SecondaryIP)
	emitElement(b, "CONFIGURATION", a, func() {
		if r.Serial != nil {
			sa := AttrsBuilder{}.
				Add("BAUD", r.Serial.Baud).
				Add("PARITY", r.Serial.Parity).
				Add("DATA_BITS", r.Serial.DataBits).
				Add("STOP_BITS_IP", r.Serial.StopBitsIP).
				Add("ENABLE_DSOM", r.Serial.EnableDSOM)
			emitElement(b, "SERIAL", sa, nil)
		}
		if r.Router != nil {
			ra := AttrsBuilder{}.
				Add("MAX_LEVEL", r.Router.MaxLevel).
				Add("MAX_SOURCE", r.Router.MaxSource).
				Add("MAX_DEST", r.Router.MaxDest).
				Add("IP_PORT", r.Router.IPPort).
				Add("MASTER_SERVER", r.Router.MasterServer)
			emitElement(b, "ROUTER", ra, nil)
		}
	})
}

// SNMPConfig is the §4.5.1.4 SNMP body (p23): <CONFIGURATION NAME PORT/>.
type SNMPConfig struct {
	Name string
	Port string
}

func (s *SNMPConfig) encodeBody(b *strings.Builder) {
	a := AttrsBuilder{}.
		Add("NAME", s.Name).
		Add("PORT", s.Port)
	emitElement(b, "CONFIGURATION", a, nil)
}

// EncodeDeviceConfiguration builds the §4.5 top-level command:
//
//	<DEVICE_CONFIGURATION TYPE="ADD|MODIFY|REMOVE" IP_ADDRESS="…"
//	    [DEVICE_NAME="…"] DEVICE_TYPE="…">
//	  …body…
//	</DEVICE_CONFIGURATION>
//
// REMOVE and any config whose body pointer is nil emit the self-closing
// form (§4.5.2 p23 shows <DEVICE_CONFIGURATION …/> for REMOVE).
func EncodeDeviceConfiguration(mtid uint32, dc *DeviceConfiguration) []byte {
	var b strings.Builder
	a := AttrsBuilder{}.
		ForceAdd("TYPE", string(dc.Type)).
		Add("IP_ADDRESS", dc.IPAddress).
		Add("DEVICE_NAME", dc.DeviceName).
		Add("DEVICE_TYPE", string(dc.DeviceType)).
		ForceAdd("MTID", formatMTID(mtid))

	body := dc.bodyFunc(&b)
	emitElement(&b, "DEVICE_CONFIGURATION", a, body)
	return []byte(b.String())
}

// bodyFunc returns the body closure for the configured DeviceType, or
// nil when there is no body to emit (REMOVE, or a missing body struct).
func (dc *DeviceConfiguration) bodyFunc(b *strings.Builder) func() {
	if dc.Type == DeviceConfigRemove {
		return nil
	}
	switch {
	case dc.Generic != nil:
		return func() { dc.Generic.encodeBody(b) }
	case dc.Panel != nil:
		return func() { dc.Panel.encodeBody(b) }
	case dc.Router != nil:
		return func() { dc.Router.encodeBody(b) }
	case dc.SNMP != nil:
		return func() { dc.SNMP.encodeBody(b) }
	}
	return nil
}
