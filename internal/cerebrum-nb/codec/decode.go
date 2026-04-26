package codec

import (
	"fmt"
	"strconv"
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

func parseNack(e *Element) *NackError {
	idStr := e.Attr("id")
	codeStr := e.Attr("code")
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
	return &CategoryChange{
		Type:     e.Attr("type"),
		Category: e.Attr("category"),
	}
}

func parseSalvoChange(e *Element) *SalvoChange {
	return &SalvoChange{
		Type:     e.Attr("type"),
		Group:    e.Attr("group"),
		Instance: e.Attr("instance"),
	}
}

func parseDeviceChange(e *Element) *DeviceChange {
	return &DeviceChange{
		Type:       e.Attr("type"),
		IPAddress:  e.Attr("ip_address"),
		DeviceType: DeviceType(e.Attr("device_type")),
		DeviceName: e.Attr("device_name"),
		SubDevice:  e.Attr("sub_device"),
		Object:     e.Attr("object"),
	}
}

func parseDatastoreChange(e *Element) *DatastoreChange {
	return &DatastoreChange{
		Name: e.Attr("name"),
		Type: e.Attr("type"),
	}
}
