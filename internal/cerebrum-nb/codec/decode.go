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
	KindContinue         // <CONTINUE/> flow-control after BUSY (§1.4 p5)
	KindWildcardComplete // <WILDCARD_COMPLETE MTID/> end-of-snapshot (§1.6 p6)
	KindDeviceConfigResult // <DEVICE_CONFIGURATION …><RESULT VALUE=…/> (§4.5 p20-23)

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
	case KindContinue:
		return "continue"
	case KindWildcardComplete:
		return "wildcard_complete"
	case KindDeviceConfigResult:
		return "device_config_result"
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

	LoginReply   *LoginReply
	PollReply    *PollReply
	Nack         *NackError
	DeviceConfig *DeviceConfigResult // KindDeviceConfigResult (§4.5)

	Routing   *RoutingChange
	Category  *CategoryChange
	Salvo     *SalvoChange
	Device    *DeviceChange
	Datastore *DatastoreChange
}

// LoginReply is the body of <LOGIN_REPLY MTID="…" API_VER="…">. As of
// 0v16 (§2.1 p7) a successful login also carries a <DATASTORES> body
// listing the Data Stores assigned to the Cerebrum API, each with an
// AVAILABLE flag. Older servers omit it; Datastores is then nil.
type LoginReply struct {
	MTID       string
	APIVer     string
	Datastores []DatastoreEntry
}

// DatastoreEntry is one <DATASTORE NAME="…" AVAILABLE="0|1"/> row of a
// 0v16 LOGIN_REPLY <DATASTORES> body (§2.1 p7). Name is the relative
// path used later as the obtain key (§5.5.1). Available reports whether
// the store was read successfully.
type DatastoreEntry struct {
	Name      string
	Available bool
}

// DeviceConfigResult is the §4.5 reply body. The server echoes the
// request's <DEVICE_CONFIGURATION> envelope wrapping a single
// <RESULT VALUE="ACCEPTED|FAILED"/> (p20-23).
type DeviceConfigResult struct {
	Type       string // ADD / MODIFY / REMOVE (echoed)
	IPAddress  string
	DeviceType string
	Value      string // ACCEPTED / FAILED
	Accepted   bool   // Value == "ACCEPTED"
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
		f.LoginReply = parseLoginReply(root)
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
	case "continue":
		f.Kind = KindContinue
	case "wildcard_complete":
		f.Kind = KindWildcardComplete
	case "device_configuration":
		f.Kind = KindDeviceConfigResult
		f.DeviceConfig = parseDeviceConfigResult(root)
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

// parseLoginReply reads <LOGIN_REPLY MTID API_VER> plus the optional
// 0v16 <DATASTORES><DATASTORE NAME AVAILABLE/>…</DATASTORES> body
// (§2.1 p7). Pre-0v16 servers omit the body; Datastores is then nil.
func parseLoginReply(e *Element) *LoginReply {
	lr := &LoginReply{
		MTID:   e.Attr("mtid"),
		APIVer: e.Attr("api_ver"),
	}
	if stores := e.Child("datastores"); stores != nil {
		for _, ds := range stores.ChildrenNamed("datastore") {
			lr.Datastores = append(lr.Datastores, DatastoreEntry{
				Name:      ds.Attr("name"),
				Available: parseBoolAttr(ds.Attr("available")),
			})
		}
	}
	return lr
}

// parseDeviceConfigResult reads the §4.5 reply envelope
// <DEVICE_CONFIGURATION TYPE IP_ADDRESS DEVICE_TYPE><RESULT VALUE=…/>
// </DEVICE_CONFIGURATION> (p20-23). The <RESULT> child carries the
// ACCEPTED / FAILED verdict.
func parseDeviceConfigResult(e *Element) *DeviceConfigResult {
	r := &DeviceConfigResult{
		Type:       e.Attr("type"),
		IPAddress:  firstNonEmpty(e.Attr("ip_address"), e.Attr("ip")),
		DeviceType: e.Attr("device_type"),
	}
	if res := e.Child("result"); res != nil {
		r.Value = res.Attr("value")
		r.Accepted = strEqualFold(r.Value, "ACCEPTED")
	}
	return r
}

func parseRoutingChange(e *Element) *RoutingChange {
	r := &RoutingChange{
		Type:       e.Attr("type"),
		DeviceName: e.Attr("device_name"),
		DeviceType: DeviceType(e.Attr("device_type")),
		IPAddress:  firstNonEmpty(e.Attr("ip_address"), e.Attr("ip")),
		SrceID:     e.Attr("srce_id"),
		SrceName:   e.Attr("srce_name"),
		DestID:     e.Attr("dest_id"),
		DestName:   e.Attr("dest_name"),
		LevelID:    e.Attr("level_id"),
		LevelName:  e.Attr("level_name"),
		Mnemonic:   e.Attr("mnemonic"),
		AltMne:     e.Attr("alt_mne"),
	}
	// RX shape (Cerebrum wire 2026-04-30):
	//   <mne available="1" mnemonic="..."/>          → slot 0
	//   <alt_mne_N available="1" mnemonic="..."/>    → slot N
	//   <route source_level_id source_id available/> → ROUTE source
	for _, c := range e.Children {
		switch {
		case c.Name == "mne":
			// ORIGINAL_MNE is the device's un-overridden name
			// (§5.1.4-§5.1.6 pp25-26); captured even when AVAILABLE="0".
			if om := c.Attr("original_mne"); om != "" {
				r.OriginalMne = om
			}
			if c.Attr("available") == "0" {
				continue
			}
			name := c.Attr("mnemonic")
			if r.Mnemonics == nil {
				r.Mnemonics = map[int]string{}
			}
			r.Mnemonics[0] = name
			if r.Mnemonic == "" {
				r.Mnemonic = name
			}
		case strings.HasPrefix(c.Name, "alt_mne_"):
			if c.Attr("available") == "0" {
				continue
			}
			n, err := strconv.Atoi(strings.TrimPrefix(c.Name, "alt_mne_"))
			if err != nil {
				continue
			}
			if r.Mnemonics == nil {
				r.Mnemonics = map[int]string{}
			}
			r.Mnemonics[n] = c.Attr("mnemonic")
		case c.Name == "route":
			r.RouteSourceID = c.Attr("source_id")
			r.RouteSourceLevelID = c.Attr("source_level_id")
			r.RouteAvailable = c.Attr("available") == "1"
		case c.Name == "value" || c.Name == "lock":
			// §5.1.2 SRCE_LOCK uses <VALUE …/>; §5.1.3 DEST_LOCK uses
			// <LOCK …/> (p24). Same attribute set on both.
			r.Lock = &RoutingLock{
				Available:  parseBoolAttr(c.Attr("available")),
				LockState:  LockKind(c.Attr("lock_state")),
				LockedBy:   c.Attr("locked_by"),
				UnlockTime: c.Attr("unlock_time"),
				UnlockDate: c.Attr("unlock_date"),
			}
		case c.Name == "associations":
			// §5.1.5/§5.1.6 pp25-26: positional <ASSOCIATION_N …/>.
			for _, assoc := range c.Children {
				n, ok := strings.CutPrefix(assoc.Name, "association_")
				if !ok {
					continue
				}
				idx, err := strconv.Atoi(n)
				if err != nil {
					continue
				}
				r.Associations = append(r.Associations, RoutingAssociation{
					Index:        idx,
					SrceID:       assoc.Attr("srce_id"),
					DestID:       assoc.Attr("dest_id"),
					RMDeviceName: assoc.Attr("rm_device_name"),
					RMDeviceType: DeviceType(assoc.Attr("rm_device_type")),
					RMLevelID:    assoc.Attr("rm_level_id"),
					RMReversed:   assoc.Attr("rm_reversed"),
					RMTags:       assoc.Attr("rm_tags"),
					RMIPUID:      assoc.Attr("rm_ip_uid"),
				})
			}
		case c.Name == "rm_default_device":
			r.RMDefaultDevice = &RMDefaultDevice{
				DeviceName: c.Attr("device_name"),
				DeviceType: DeviceType(c.Attr("device_type")),
				LevelID:    c.Attr("level_id"),
			}
		case c.Name == "tags":
			// §5.1.7/§5.1.8 p27: <TAGS [AVAILABLE] LIST="Tag1|Tag2"/>.
			// LIST uses the PIPE delimiter (0.13 change history p33),
			// NOT the comma used by category/group LISTs.
			r.TagsAvailable = parseBoolAttr(c.Attr("available"))
			r.TagList = splitPipe(c.Attr("list"))
		}
	}
	return r
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
				Group: child.Attr("group"),
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
			Available:   parseBoolAttr(det.Attr("available")),
			Description: det.Attr("description"),
			Active:      parseBoolAttr(det.Attr("active")),
			Date:        det.Attr("date"),
			Time:        det.Attr("time"),
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
	// TYPE=VALUE: one or more <OBJECT_VALUE …/> children (§5.4.3 p29).
	// A scalar object yields one; a group / table / wildcard path yields
	// several. The full 0v16 descriptor is captured per entry.
	for _, ov := range e.ChildrenNamed("object_value") {
		d.ObjectValues = append(d.ObjectValues, DeviceObjectValue{
			Object:    ov.Attr("object"),
			Value:     ov.Attr("value"),
			Available: parseBoolAttr(ov.Attr("available")),
			DataType:  ov.Attr("data_type"),
			Readable:  parseBoolAttr(ov.Attr("readable")),
			Writable:  parseBoolAttr(ov.Attr("writable")),
			Units:     ov.Attr("units"),
			Label:     ov.Attr("label"),
			Default:   ov.Attr("default"),
			EnumList:  splitCSV(ov.Attr("enum_list")),
		})
	}
	if len(d.ObjectValues) > 0 {
		d.ObjectValue = &d.ObjectValues[0]
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
	return splitOn(s, ',')
}

// splitPipe splits a Routemaster TAGS LIST on the '|' delimiter
// (§4.2.1/§4.2.2 p15, §5.1.7/§5.1.8 p27, 0.13 change history p33). The
// pipe delimiter is specific to Tags; category/group LISTs use comma.
func splitPipe(s string) []string {
	return splitOn(s, '|')
}

func splitOn(s string, sep byte) []string {
	if s == "" {
		return nil
	}
	out := make([]string, 0, 8)
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == sep {
			out = append(out, s[start:i])
			start = i + 1
		}
	}
	out = append(out, s[start:])
	return out
}

// parseDatastoreChange decodes the three §5.5 RX shapes:
//
//   - Obtain reply (§5.5.1 p30): <DATASTORE_CHANGE TYPE="FILE"
//     AVAILABLE="1"><DATA>…</DATA></DATASTORE_CHANGE>.
//   - Bare terminator (§5.5.1 p30): <DATASTORE_CHANGE MTID="1"/> — no
//     TYPE and no children.
//   - Change event (§5.5.2 p31): <DATASTORE_CHANGE NAME="…" TYPE="…">
//     <ELEMENT ATTRIBUTE PATH VALUE AVAILABLE CHILD_COUNT/>
//     </DATASTORE_CHANGE>.
//
// Path accepts either PATH (the §5.5.1 example) or NAME (the §5.5.1
// attribute table / §5.5.2 event) — see the PATH/NAME ambiguity note on
// DatastoreChange.
func parseDatastoreChange(e *Element) *DatastoreChange {
	path := firstNonEmpty(e.Attr("path"), e.Attr("name"))
	d := &DatastoreChange{
		Path:      path,
		Name:      path, // deprecated alias, kept in sync for 0v13 callers
		MTID:      e.Attr("mtid"),
		Type:      e.Attr("type"),
		Available: parseBoolAttr(e.Attr("available")),
	}
	if data := e.Child("data"); data != nil {
		// Re-render the parsed body back to XML. Lossy on case
		// (children are case-folded), but preserves structure + values.
		var sb strings.Builder
		for _, c := range data.Children {
			writeElement(&sb, c)
		}
		d.Data = sb.String()
	}
	if el := e.Child("element"); el != nil {
		d.Element = &DatastoreElement{
			Attribute:  el.Attr("attribute"),
			Path:       el.Attr("path"),
			Value:      el.Attr("value"),
			Available:  parseBoolAttr(el.Attr("available")),
			ChildCount: el.Attr("child_count"),
		}
	}
	// Bare terminator: a TYPE-less, child-less element carrying only MTID.
	if d.Type == "" && len(e.Children) == 0 && d.MTID != "" {
		d.Terminator = true
	}
	return d
}
