package consumer

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"dhs/internal/rrcs/codec"
)

// ChangeLine is one value an event carries, the way watch prints it:
// the object ID where the thing has one, the path, the member, the value.
type ChangeLine struct {
	Time    string `json:"ts"`
	Event   string `json:"event"`
	OID     int    `json:"oid,omitempty"`
	Path    string `json:"path"`
	Label   string `json:"label"`
	Value   string `json:"value"`
	Unit    string `json:"unit,omitempty"`
	Min     string `json:"min,omitempty"`
	Max     string `json:"max,omitempty"`
	Default string `json:"default,omitempty"`
	Enum    string `json:"enum_items,omitempty"`
	Name    string `json:"name,omitempty"` // what the path names, for a reader
	Detail  string `json:"detail,omitempty"`
	Raw     any    `json:"raw,omitempty"`
}

// WatchCSVHeader is the header of watch --output csv: the columns of
// export between the time and event in front and a description behind.
func WatchCSVHeader() []string {
	h := append([]string{"ts", "event"}, CSVHeader...)
	return append(h, "description")
}

// kind names the type of the value the way export does.
func (l ChangeLine) kindEnum() (kind, enum string) {
	switch {
	case l.Value == "on" || l.Value == "off":
		return "enum", "off|on"
	case l.Value == "true" || l.Value == "false":
		return "bool", ""
	case l.Unit == "dB" || l.Min != "" || l.Max != "":
		if strings.Contains(l.Min+l.Max+l.Value, ".") {
			return "float", l.Enum
		}
		return "int", l.Enum
	case l.Enum != "":
		return "enum", l.Enum
	}
	if _, err := strconv.Atoi(l.Value); err == nil {
		return "int", ""
	}
	return "string", ""
}

// record is the line of watch --output csv.
func (l ChangeLine) Record(target string) []string {
	kind, enum := l.kindEnum()
	row := Row{IP: target, Protocol: Name, Path: l.Path + "." + l.Label, Label: l.Label, Kind: kind, Access: "r",
		Value: l.Value, Unit: l.Unit, Min: l.Min, Max: l.Max, Default: l.Default, EnumItems: enum}
	if l.OID != 0 {
		row.ID = strconv.Itoa(l.OID)
	}
	desc := l.Name
	if l.Detail != "" {
		desc = strings.TrimSpace(desc + "  " + l.Detail)
	}
	rec := append([]string{l.Time, l.Event}, row.Record()...)
	return append(rec, desc)
}

// text is the line of the terminal.
func (l ChangeLine) Text() string {
	var b strings.Builder
	b.WriteString(l.Time)
	b.WriteString("  ")
	if l.OID != 0 {
		fmt.Fprintf(&b, "oid=%d ", l.OID)
	}
	fmt.Fprintf(&b, "%s %s = %s", l.Path, l.Label, l.Value)
	if l.Unit != "" {
		b.WriteString(" " + l.Unit)
	}
	if l.Min != "" || l.Max != "" {
		fmt.Fprintf(&b, " [%s..%s]", l.Min, l.Max)
	}
	if l.Default != "" {
		b.WriteString(" default " + l.Default)
	}
	if l.Name != "" {
		b.WriteString("  (" + l.Name + ")")
	}
	if l.Detail != "" {
		b.WriteString("  " + l.Detail)
	}
	return b.String()
}

func ParamInt(p []codec.Value, i int) int {
	if i < len(p) && p[i].Kind == codec.KindInt {
		return int(p[i].Int)
	}
	return 0
}

func onOff(v codec.Value) string {
	if v.Kind == codec.KindBool && v.Bool {
		return "on"
	}
	return "off"
}

func MemberInt(v codec.Value, name string) int {
	n, _ := FieldInt(v, name)
	return int(n)
}

// VolumeText reads a crosspoint volume (§9.4): -1 unavailable, 0 mute,
// 1..255 = (x - 230) / 2 dB.
func VolumeText(x int) (value, unit string) {
	switch {
	case x < 0:
		return "unavailable", ""
	case x == 0:
		return "mute", ""
	}
	db, _ := codec.VolumeDB(x)
	return strconv.FormatFloat(db, 'f', 1, 64), "dB"
}

// Decode turns a notification into the values it carries. An event
// this function does not know comes back as one line holding its
// parameters as received, so nothing RRCS sends is dropped.
func Decode(m *Model, e Event) []ChangeLine {
	p := e.Params
	if e.TransKey != "" {
		p = p[1:]
	}
	stamp := e.Time.UTC().Format("15:04:05.000")
	line := func(path, label, value string) ChangeLine {
		return ChangeLine{Time: stamp, Event: e.Method, Path: path, Label: label, Value: value}
	}
	netNode := func() string { return fmt.Sprintf("net.%d.node.%d", ParamInt(p, 0), ParamInt(p, 1)) }

	switch e.Method {
	case "PortActive", "PortInactive": // §9.7: Net, Node, Port
		path, oid, label := m.PortRef(ParamInt(p, 0), ParamInt(p, 1), ParamInt(p, 2), false)
		l := line(path, "Online", strconv.FormatBool(e.Method == "PortActive"))
		l.OID, l.Name = oid, label
		return []ChangeLine{l}

	case "CrosspointChange": // §9.6: count, struct of XP#n arrays
		if len(p) < 2 || p[1].Kind != codec.KindStruct {
			break
		}
		var out []ChangeLine
		for _, xp := range p[1].Members {
			it := xp.Value.Items
			if xp.Value.Kind != codec.KindArray || len(it) < 7 {
				continue
			}
			src, _, srcLabel := m.PortRef(ParamInt(it, 0), ParamInt(it, 1), ParamInt(it, 2), true)
			dst, oid, dstLabel := m.PortRef(ParamInt(it, 3), ParamInt(it, 4), ParamInt(it, 5), false)
			l := line("xp."+src+">"+dst, "State", onOff(it[6]))
			l.OID, l.Name = oid, srcLabel+" > "+dstLabel
			out = append(out, l)
		}
		if len(out) > 0 {
			return out
		}

	case "XpVolumeChange": // §9.4: array of {Source, Destination, SingleVolume | ConferenceVolume}
		if len(p) < 1 || p[0].Kind != codec.KindArray {
			break
		}
		var out []ChangeLine
		for _, ch := range p[0].Items {
			s, _ := ch.Field("Source")
			d, _ := ch.Field("Destination")
			src, _, srcLabel := m.PortRef(MemberInt(s, "Net"), MemberInt(s, "Node"), MemberInt(s, "Port"), true)
			dst, oid, dstLabel := m.PortRef(MemberInt(d, "Net"), MemberInt(d, "Node"), MemberInt(d, "Port"), false)
			for _, member := range []string{"SingleVolume", "ConferenceVolume"} {
				v, ok := ch.Field(member)
				if !ok {
					continue
				}
				x := int(v.Int)
				if v.Kind == codec.KindString {
					x, _ = strconv.Atoi(v.Str)
				}
				value, unit := VolumeText(x)
				meta := MetaOf["Xp.Volume"]
				l := line("xp."+src+">"+dst, member, value)
				l.OID, l.Name, l.Unit, l.Min, l.Max = oid, srcLabel+" > "+dstLabel, unit, meta.Min, meta.Max
				l.Enum = "mute|unavailable"
				out = append(out, l)
			}
		}
		if len(out) > 0 {
			return out
		}

	case "LogicSourceChange": // §9.3: Object ID, state
		id := ParamInt(p, 0)
		l := line("logic."+strconv.Itoa(id), "State", "off")
		if len(p) > 1 {
			l.Value = onOff(p[1])
		}
		l.OID = id
		if m != nil {
			for _, o := range m.Objects["logic"] {
				if o.ObjectID == id {
					l.Name = o.LongName
				}
			}
		}
		return []ChangeLine{l}

	case "GpInputChange", "GpOutputChange": // §9.2: Net, Node, Port, Slot, GPIO no., state
		kind := map[string]string{"GpInputChange": "gpi", "GpOutputChange": "gpo"}[e.Method]
		path, oid, label := m.PortRef(ParamInt(p, 0), ParamInt(p, 1), ParamInt(p, 2), false)
		l := line(fmt.Sprintf("%s.%s.%d", path, kind, ParamInt(p, 4)), "State", "off")
		if len(p) > 5 {
			l.Value = onOff(p[5])
		}
		l.OID, l.Name, l.Detail = oid, label, "slot "+strconv.Itoa(ParamInt(p, 3))
		return []ChangeLine{l}

	case "SendString", "SendStringOff": // §9.1: String
		text := ""
		if len(p) > 0 {
			text = p[0].Str
		}
		l := line("gateway", "SendString", text)
		if e.Method == "SendStringOff" {
			l.Detail = "off"
		}
		return []ChangeLine{l}

	case "SicFailed": // §9.7: {Bay, Description, Net, Node, Path, Severity, Status, Type}
		if len(p) < 1 || p[0].Kind != codec.KindStruct {
			break
		}
		d := p[0]
		l := line(fmt.Sprintf("net.%d.node.%d.card.%d", MemberInt(d, "Net"), MemberInt(d, "Node"), MemberInt(d, "Bay")),
			"SicFailed", strconv.FormatBool(FieldBool(d, "Status")))
		desc, _ := d.Field("Description")
		l.Detail = fmt.Sprintf("severity %d %s", MemberInt(d, "Severity"), desc.Str)
		return []ChangeLine{l}

	case "ConfigurationChange": // §9.5
		return []ChangeLine{line("gateway", "Configuration", "changed")}

	case "ConnectArtistRestored", "ConnectArtistFailure", "GatewayShutdown": // §9.7: GatewayState
		state := ""
		if len(p) > 0 {
			state = p[0].Str
		}
		l := line("gateway", "ArtistConnection", map[string]string{
			"ConnectArtistRestored": "connected", "ConnectArtistFailure": "failed", "GatewayShutdown": "shutdown"}[e.Method])
		l.Detail = "gateway state " + state
		return []ChangeLine{l}

	case "UpstreamFailed", "UpstreamFailedCleared", "DownstreamFailed", "DownstreamFailedCleared",
		"NodeControllerFailed", "NodeControllerReboot": // §9.7: Net, Node
		label := strings.TrimSuffix(e.Method, "Cleared")
		return []ChangeLine{line(netNode(), label, strconv.FormatBool(!strings.HasSuffix(e.Method, "Cleared")))}

	case "ClientFailed", "ClientFailedCleared": // §9.7: Net, Node, Slot-of-client-card
		l := line(fmt.Sprintf("%s.card.%d", netNode(), ParamInt(p, 2)), "ClientFailed", strconv.FormatBool(e.Method == "ClientFailed"))
		return []ChangeLine{l}

	case MethodPanelSpyKeyEvent: // §9.9.4: KeyEventData
		if len(p) < 1 || p[0].Kind != codec.KindStruct {
			break
		}
		d := p[0]
		path, oid, label := m.SpyRef(MemberInt(d, "Node"), MemberInt(d, "Port"))
		action := map[int]string{0: "pressed", 1: "released", 2: "double-click", 3: "long-press"}[MemberInt(d, "KeyAction")]
		if action == "" {
			action = "action " + strconv.Itoa(MemberInt(d, "KeyAction"))
		}
		l := line(fmt.Sprintf("%s.keyevent.%d.%d", path, MemberInt(d, "SubPanel"), MemberInt(d, "Key")), "KeyAction", action)
		l.OID, l.Name = oid, label
		l.Detail = fmt.Sprintf("key type %d latched %v", MemberInt(d, "KeyType"), FieldBool(d, "IsKeyLatched"))
		// What the first page holds on that key, for a reader. The event
		// does not say which page was shown.
		if m != nil {
			if port := m.Find(MemberInt(d, "Node"), MemberInt(d, "Port"), false); port != nil {
				for _, k := range port.Keys {
					if k.Position == "key" && k.Expansion == MemberInt(d, "SubPanel") && k.Key == MemberInt(d, "Key") && k.Page == 1 {
						l.Detail += "; page 1: " + k.CommandType + " " + Target(k)
					}
				}
			}
		}
		return []ChangeLine{l}

	case MethodPanelSpyFuncKeyEvent, MethodPanelSpyRotateEvent, MethodPanelSpyNumKeyEvent: // §9.9.3, §9.9.5, §9.9.6
		if len(p) < 1 || p[0].Kind != codec.KindStruct {
			break
		}
		d := p[0]
		path, oid, label := m.PortRef(1, MemberInt(d, "Node"), MemberInt(d, "Port"), false)
		var out []ChangeLine
		for _, member := range d.Members {
			if member.Name == "Node" || member.Name == "Port" || member.Value.Kind == codec.KindStruct {
				continue
			}
			l := line(path+"."+strings.ToLower(strings.TrimSuffix(strings.TrimPrefix(e.Method, "PanelSpy"), "Event")), member.Name, Compact(member.Value))
			l.OID, l.Name = oid, label
			out = append(out, l)
		}
		if len(out) > 0 {
			return out
		}

	case MethodPanelSpyStateChange, MethodPanelSpyStateChanged: // §9.9.2: array of panel states
		if len(p) < 1 || p[0].Kind != codec.KindArray {
			break
		}
		states := map[int]string{0: "unregistered", 1: "busy", 2: "active", 3: "error"}
		var out []ChangeLine
		for _, panel := range p[0].Items {
			path, oid, label := m.SpyRef(MemberInt(panel, "Node"), MemberInt(panel, "Port"))
			for _, kind := range []string{"Key", "FuncKey", "NumKey", "Rotate"} {
				st, ok := panel.Field(kind)
				if !ok {
					continue
				}
				l := line(path+".spy", kind, states[MemberInt(st, "State")])
				l.OID, l.Name = oid, label
				if desc, ok := st.Field("ErrorDescription"); ok {
					l.Detail = desc.Str
				}
				out = append(out, l)
			}
		}
		if len(out) > 0 {
			return out
		}

	case MethodGetAlive:
		return []ChangeLine{line("gateway", "Alive", "ping")}
	}

	// Unknown, or not in the shape the specification prints.
	l := line("event", e.Method, Compact(codec.Array(p...)))
	l.Raw = JSON(codec.Array(p...))
	return []ChangeLine{l}
}

// ListenerWait is how long our listener lets a connection from RRCS
// stay silent before its first request. RRCS opens one connection per
// registration and keeps the one of RegisterForEventsEx idle until a level
// changes; with 10 seconds we closed it each time, and RRCS logged
// "Connection to server lost: Error code 10057" (its log of 2026-10-07,
// nine times). The registration survived, but an error line on a
// production system is ours to avoid.
const ListenerWait = 12 * time.Hour

// Compact is the one-line rendering of a value.
func Compact(v codec.Value) string {
	b, err := json.Marshal(JSON(v))
	if err != nil {
		return err.Error()
	}
	return string(b)
}

// Target names what a key calls, for a reader.
func Target(k *Key) string {
	if k.Target == "" {
		return ""
	}
	if k.TargetName == "" {
		return k.Target
	}
	return k.Target + " (" + k.TargetName + ")"
}
