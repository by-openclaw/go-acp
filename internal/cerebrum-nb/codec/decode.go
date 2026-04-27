package codec

import (
	"fmt"
	"strconv"
	"strings"
)

// FrameKind tags the shape of a decoded RX message.
type FrameKind int

const (
	KindUnknown FrameKind = iota

	// Direct replies to TX commands (§2 + §1.4)
	KindLoginReply
	KindPollReply
	KindAck
	KindNack
	KindBusy

	// Async events (§5)
	KindRoutingChange
	KindCategoryChange
	KindSalvoChange
	KindDeviceChange
	KindDatastoreChange
)

func (k FrameKind) String() string {
	switch k {
	case KindLoginReply:
		return "login_reply"
	case KindPollReply:
		return "poll_reply"
	case KindAck:
		return "ack"
	case KindNack:
		return "nack"
	case KindBusy:
		return "busy"
	case KindRoutingChange:
		return "routing_change"
	case KindCategoryChange:
		return "category_change"
	case KindSalvoChange:
		return "salvo_change"
	case KindDeviceChange:
		return "device_change"
	case KindDatastoreChange:
		return "datastore_change"
	}
	return "unknown"
}

// Frame is one decoded RX message. Kind indicates which typed body
// (if any) is populated; Root always carries the raw AST.
type Frame struct {
	Kind        FrameKind
	MTID        string
	Root        *Element
	CaseChanged bool

	LoginReply *LoginReply
	PollReply  *PollReply
	Nack       *NackError

	Routing   *RoutingChange
	Category  *CategoryChange
	Salvo     *SalvoChange
	Device    *DeviceChange
	Datastore *DatastoreChange
}

// LoginReply is the body of <login_reply mtid="…" api_ver="…"/>.
type LoginReply struct {
	MTID   string
	APIVer string
}

// PollReply is the body of <poll_reply mtid="…" CONNECTED_SERVER_ACTIVE
// PRIMARY_SERVER_STATE SECONDARY_SERVER_STATE/>.
type PollReply struct {
	MTID                  string
	ConnectedServerActive bool
	PrimaryServerState    bool
	SecondaryServerState  bool
}

// Decode parses one XML document and returns a typed Frame. Errors
// only on transport-level XML failures; a `<nack>` is a successful
// decode with Kind=KindNack and the NackError carried in Frame.Nack.
func Decode(data []byte) (*Frame, error) {
	root, err := ParseElement(data)
	if err != nil {
		return nil, fmt.Errorf("cerebrum-nb: decode: %w", err)
	}
	f := &Frame{
		Root:        root,
		MTID:        root.Attr("mtid"),
		CaseChanged: root.CaseChanged,
	}
	switch root.Name {
	case "login_reply":
		f.Kind = KindLoginReply
		f.LoginReply = &LoginReply{
			MTID:   root.Attr("mtid"),
			APIVer: root.Attr("api_ver"),
		}
	case "poll_reply":
		f.Kind = KindPollReply
		f.PollReply = &PollReply{
			MTID:                  root.Attr("mtid"),
			ConnectedServerActive: parseBoolFlag(root.Attr("connected_server_active")),
			PrimaryServerState:    parseBoolFlag(root.Attr("primary_server_state")),
			SecondaryServerState:  parseBoolFlag(root.Attr("secondary_server_state")),
		}
	case "ack":
		f.Kind = KindAck
	case "nack":
		f.Kind = KindNack
		f.Nack = parseNack(root)
	case "busy":
		f.Kind = KindBusy
	case "routing_change":
		f.Kind = KindRoutingChange
		f.Routing = parseRoutingChange(root)
	case "category_change":
		f.Kind = KindCategoryChange
		f.Category = parseCategoryChange(root)
	case "salvo_change":
		f.Kind = KindSalvoChange
		f.Salvo = parseSalvoChange(root)
	case "device_change":
		f.Kind = KindDeviceChange
		f.Device = parseDeviceChange(root)
	case "datastore_change":
		f.Kind = KindDatastoreChange
		f.Datastore = parseDatastoreChange(root)
	default:
		f.Kind = KindUnknown
	}
	return f, nil
}

// parseBoolFlag treats "1" / "true" / "yes" as true; everything else
// (including empty + "0") as false. Spec §1.4 uses "1"/"0" only; the
// looser parse tolerates impl variation.
func parseBoolFlag(s string) bool {
	switch s {
	case "1", "true", "TRUE", "True", "yes", "YES":
		return true
	}
	return false
}

// parseNack reads either of the two NACK attribute conventions:
//
//   - Spec v0.13: <nack mtid id code description/>  (lowercase keys
//     id / code / description)
//   - Wire-actual (verified 2026-04-26 against a live Cerebrum):
//     <NACK MTID ERROR ERROR_CODE/>  (UPPERCASE keys, ERROR carries
//     the symbolic name, ERROR_CODE carries the numeric id, no
//     description)
//
// The Element AST is already case-folded so e.Attr lookups are
// case-insensitive on the key.
func parseNack(e *Element) *NackError {
	// Numeric ID — try `id` first (spec), then `error_code` (wire-actual).
	idStr := e.Attr("id")
	if idStr == "" {
		idStr = e.Attr("error_code")
	}
	// Symbolic code — try `code` first (spec), then `error` (wire-actual).
	codeStr := e.Attr("code")
	if codeStr == "" {
		codeStr = e.Attr("error")
	}
	desc := e.Attr("description")
	if desc == "" {
		desc = e.Text
	}

	ne := &NackError{
		MTID:    e.Attr("mtid"),
		Code:    codeStr,
		Message: desc,
		ID:      -1,
	}
	if n, err := strconv.Atoi(idStr); err == nil {
		ne.ID = NackCode(n)
	} else if c, ok := NackCodeFromString(codeStr); ok {
		ne.ID = c
	}
	return ne
}

func parseRoutingChange(e *Element) *RoutingChange {
	return &RoutingChange{
		Type:       e.Attr("type"),
		DeviceName: e.Attr("device_name"),
		DeviceType: DeviceType(e.Attr("device_type")),
		SrceID:     e.Attr("srce_id"),
		SrceName:   e.Attr("srce_name"),
		DestID:     e.Attr("dest_id"),
		DestName:   e.Attr("dest_name"),
		LevelID:    e.Attr("level_id"),
		LevelName:  e.Attr("level_name"),
	}
}

func parseCategoryChange(e *Element) *CategoryChange {
	c := &CategoryChange{
		Type:     e.Attr("type"),
		Category: e.Attr("category"),
	}
	// TYPE=CATEGORY_LIST: server emits one inner <CATEGORY LIST="A,B,C"/>
	// with a comma-separated list (live wire 2026-04-27).
	if cat := e.Child("category"); cat != nil {
		if list := cat.Attr("list"); list != "" {
			c.Categories = splitCSV(list)
		}
	}
	// TYPE=CATEGORY_DETAILS: <details label="..." available="0|1"
	// description="..."/>. Server typo `descsription` accepted here as
	// a fallback (live capture 2026-04-27).
	if det := e.Child("details"); det != nil {
		c.Details = &CategoryDetailsInfo{
			Label:       det.Attr("label"),
			Available:   parseBoolAttr(det.Attr("available")),
			Description: firstNonEmpty(det.Attr("description"), det.Attr("descsription")),
		}
	}
	// <items> carries positional <ITEM_N TYPE="..." VALUE="..."/>
	// children. We collect non-BLANK items and preserve the index so
	// callers can rebuild the panel-grid layout. Live capture
	// 2026-04-27.
	if items := e.Child("items"); items != nil && c.Details != nil {
		for _, child := range items.Children {
			n, ok := strings.CutPrefix(child.Name, "item_")
			if !ok {
				continue
			}
			idx, err := strconv.Atoi(n)
			if err != nil {
				continue
			}
			t := child.Attr("type")
			if t == "BLANK" {
				continue
			}
			c.Details.Items = append(c.Details.Items, CategoryItem{
				Index: idx,
				Type:  t,
				Value: child.Attr("value"),
			})
		}
	}
	return c
}

func parseSalvoChange(e *Element) *SalvoChange {
	s := &SalvoChange{
		Type:     e.Attr("type"),
		Group:    e.Attr("group"),
		Instance: e.Attr("instance"),
	}
	// TYPE=GROUP_LIST: server emits one inner <GROUPS LIST="..."/>
	// (live wire 2026-04-27).
	if grp := e.Child("groups"); grp != nil {
		if list := grp.Attr("list"); list != "" {
			s.Groups = splitCSV(list)
		}
	}
	// TYPE=INSTANCE_LIST: <instances list="..."/> (live wire 2026-04-27).
	if ins := e.Child("instances"); ins != nil {
		if list := ins.Attr("list"); list != "" {
			s.Instances = splitCSV(list)
		}
	}
	// TYPE=INSTANCE_DETAILS: <details available="0|1"/>
	// (live wire 2026-04-27 — server returns empty details when the
	// instance does not exist).
	if det := e.Child("details"); det != nil && e.Attr("instance") != "" {
		s.InstanceDetails = &SalvoInstanceDetails{
			Available: parseBoolAttr(det.Attr("available")),
		}
	}
	return s
}

func parseDeviceChange(e *Element) *DeviceChange {
	d := &DeviceChange{
		Type:       e.Attr("type"),
		IPAddress:  firstNonEmpty(e.Attr("ip_address"), e.Attr("ip")),
		DeviceType: DeviceType(e.Attr("device_type")),
		DeviceName: e.Attr("device_name"),
		SubDevice:  e.Attr("sub_device"),
		Object:     e.Attr("object"),
	}
	// TYPE=LIST: server emits one <DEVICE IP="..."> per entry, with
	// one or more inner <INSTANCE DEVICE_TYPE="..."/> children — one
	// per class the device belongs to (Device / Router / SNMP per
	// spec §3.1). The outer attribute is "IP" (live) or the spec's
	// "IP_ADDRESS"; we accept both.
	for _, child := range e.ChildrenNamed("device") {
		entry := DeviceEntry{
			IPAddress:  firstNonEmpty(child.Attr("ip_address"), child.Attr("ip")),
			DeviceType: DeviceType(child.Attr("device_type")),
			DeviceName: child.Attr("device_name"),
		}
		// Walk every <INSTANCE> child — multiple classes per device
		// surface as multiple instances (verified against reference
		// driver Device.ParseInstances).
		for _, inst := range child.ChildrenNamed("instance") {
			if t := inst.Attr("device_type"); t != "" {
				entry.DeviceTypes = append(entry.DeviceTypes, DeviceType(t))
			}
		}
		// Backwards-compat: DeviceType is the first instance class.
		if entry.DeviceType == "" && len(entry.DeviceTypes) > 0 {
			entry.DeviceType = entry.DeviceTypes[0]
		}
		d.Devices = append(d.Devices, entry)
	}
	// TYPE=DETAILS: nested vendor metadata (verified 2026-04-27).
	if det := e.Child("details"); det != nil {
		d.Details = &DeviceDetails{
			IP1:        det.Attr("ip1"),
			IP2:        det.Attr("ip2"),
			Name:       det.Attr("name"),
			VendorType: det.Attr("type"),
		}
	}
	if svc := e.Child("service"); svc != nil {
		d.Service = &DeviceService{
			IP1: svc.Attr("ip1"),
			IP2: svc.Attr("ip2"),
		}
	}
	if conn := e.Child("connection"); conn != nil {
		d.Connection = &DeviceConnection{
			PrimaryState:   conn.Attr("primary_state"),
			SecondaryState: conn.Attr("secondary_state"),
		}
	}
	// TYPE=VALUE: <object_value available="0|1" object="..."/>
	// (live wire 2026-04-27).
	if ov := e.Child("object_value"); ov != nil {
		d.ObjectValue = &DeviceObjectValue{
			Available: parseBoolAttr(ov.Attr("available")),
			Object:    ov.Attr("object"),
		}
	}
	if subs := e.Child("sub_devices"); subs != nil {
		for _, child := range subs.ChildrenNamed("device") {
			entry := DeviceEntry{
				IPAddress:  firstNonEmpty(child.Attr("ip_address"), child.Attr("ip")),
				DeviceType: DeviceType(child.Attr("device_type")),
				DeviceName: child.Attr("device_name"),
			}
			for _, inst := range child.ChildrenNamed("instance") {
				if t := inst.Attr("device_type"); t != "" {
					entry.DeviceTypes = append(entry.DeviceTypes, DeviceType(t))
				}
			}
			if entry.DeviceType == "" && len(entry.DeviceTypes) > 0 {
				entry.DeviceType = entry.DeviceTypes[0]
			}
			d.SubDevices = append(d.SubDevices, entry)
		}
	}
	return d
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}

// parseBoolAttr maps Cerebrum's "0"/"1" string convention to Go bool.
// Anything else is treated as false.
func parseBoolAttr(s string) bool {
	return s == "1" || strings.EqualFold(s, "true")
}

func splitCSV(s string) []string {
	if s == "" {
		return nil
	}
	out := make([]string, 0, 8)
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == ',' {
			out = append(out, s[start:i])
			start = i + 1
		}
	}
	out = append(out, s[start:])
	return out
}

func parseDatastoreChange(e *Element) *DatastoreChange {
	return &DatastoreChange{
		Name: e.Attr("name"),
		Type: e.Attr("type"),
	}
}
