package main

import (
	"fmt"
	"strconv"
	"strings"

	"dhs/internal/rrcs/codec"
	rrcs "dhs/internal/rrcs/consumer"
)

// rrcsMeta is what the specification says about the values of a
// property: its range, its default, its unit, its named values.
type rrcsMeta struct {
	Min, Max, Default, Unit, Enum string
}

// rrcsMetaOf holds the ranges printed in the 9.0.1 text, by block and
// member (§8.10.4.6 for the streams, §8.10.4.34 for the client cards,
// §6.5 for the volumes). A member without an entry has none printed.
var rrcsMetaOf = map[string]rrcsMeta{
	"Ptp.PTP":                 {Min: "0", Max: "127"},
	"Ptp.PtpPriority":         {Min: "0", Max: "255"},
	"Ptp.PtpPriority2":        {Min: "0", Max: "255"},
	"Ptp.PtpMode":             {Enum: "0=Multicast|1=Hybrid"},
	"Ptp.PtpAnnounceInterval": {Min: "-3", Max: "4", Unit: "log2 s"},
	"Nmos.LocalPort":          {Min: "1024", Max: "65535"},
	"Nmos.RegistrationPort":   {Min: "1", Max: "65535"},
	"Nmos.RegistrationMode":   {Enum: "0=Automatic|1=Peer2Peer|2=Manual"},
	"Nmos.NetworkInterface":   {Enum: "0=Media_1|1=Media_2|2=Config"},
	"Media.TcpUdpPort":        {Min: "1024", Max: "65535"},
	"Media.ServiceCodePoint":  {Min: "0", Max: "63"},
	"Media.IGMPVersion":       {Min: "0", Max: "1"},
	"Media.NetworkSpeed":      {Enum: "0=Auto|1=FullDuplex 1G"},
	"Stream.Protocol":         {Enum: "2=Manual|3=RTSP|5=NMOS"},
	"Stream.BitDepth":         {Enum: "16=L16|24=L24"},
	"Stream.PacketTime":       {Enum: "125|250|333|1000|1333", Unit: "us"},
	"Stream.PayloadType":      {Min: "96", Max: "127"},
	"Stream.Channels":         {Min: "1", Max: "16"},
	"Stream.ReceiveBuffer":    {Default: "3", Unit: "packets"},
	"Stream.PlayMode":         {Enum: "0=Synton|1=Synchron"},
	"Xp.Volume":               {Min: "-114.5", Max: "12.5", Unit: "dB"},
	".InputGain":              {Min: "-36", Max: "36", Unit: "0.5 dB", Enum: "-128=mute"},
	".OutputGain":             {Min: "-36", Max: "36", Unit: "0.5 dB", Enum: "-128=mute"},
}

// rrcsMetaFor finds the entry of a member of a block.
func rrcsMetaFor(block, field string) rrcsMeta {
	switch block {
	case "PortAes67Input", "PortAes67Output":
		block = "Stream"
	case "Media_1", "Media_2":
		block = "Media"
	}
	return rrcsMetaOf[block+"."+field]
}

// rrcsChangeLine is one value an event carries, the way watch prints it:
// the object ID where the thing has one, the path, the member, the value.
type rrcsChangeLine struct {
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
	Name    string `json:"name,omitempty"` // what the path names, for a reader
	Detail  string `json:"detail,omitempty"`
	Raw     any    `json:"raw,omitempty"`
}

// text is the line of the terminal.
func (l rrcsChangeLine) text() string {
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

func rrcsParamInt(p []codec.Value, i int) int {
	if i < len(p) && p[i].Kind == codec.KindInt {
		return int(p[i].Int)
	}
	return 0
}

func rrcsOnOff(v codec.Value) string {
	if v.Kind == codec.KindBool && v.Bool {
		return "on"
	}
	return "off"
}

func rrcsMemberInt(v codec.Value, name string) int {
	n, _ := rrcsFieldInt(v, name)
	return int(n)
}

// portRef names a port for a line: path, object ID, label.
func (m *rrcsModel) portRef(net, node, port int, wantInput bool) (string, int, string) {
	if m != nil {
		if p := m.find(node, port, wantInput); p != nil {
			return p.Path, p.ObjectID, p.Label
		}
	}
	return fmt.Sprintf("net.%d.node.%d.port.%d", net, node, port), 0, ""
}

// spyRef names the panel of a panel spy message. A real RRCS 9.0 on an
// Artist-1024 gives the smart panels there with their port number less
// 1024 (port 1026 comes back as 2), and the beltpacks unchanged; a number
// that names no port is tried again with 1024 added.
func (m *rrcsModel) spyRef(node, port int) (string, int, string) {
	if m != nil && m.find(node, port, false) == nil && m.find(node, port+1024, false) != nil {
		port += 1024
	}
	return m.portRef(1, node, port, false)
}

// rrcsVolume reads a crosspoint volume (§9.4): -1 unavailable, 0 mute,
// 1..255 = (x - 230) / 2 dB.
func rrcsVolume(x int) (value, unit string) {
	switch {
	case x < 0:
		return "unavailable", ""
	case x == 0:
		return "mute", ""
	}
	db, _ := codec.VolumeDB(x)
	return strconv.FormatFloat(db, 'f', 1, 64), "dB"
}

// rrcsDecode turns a notification into the values it carries. An event
// this function does not know comes back as one line holding its
// parameters as received, so nothing RRCS sends is dropped.
func rrcsDecode(m *rrcsModel, e rrcs.Event) []rrcsChangeLine {
	p := e.Params
	if e.TransKey != "" {
		p = p[1:]
	}
	stamp := e.Time.UTC().Format("15:04:05.000")
	line := func(path, label, value string) rrcsChangeLine {
		return rrcsChangeLine{Time: stamp, Event: e.Method, Path: path, Label: label, Value: value}
	}
	netNode := func() string { return fmt.Sprintf("net.%d.node.%d", rrcsParamInt(p, 0), rrcsParamInt(p, 1)) }

	switch e.Method {
	case "PortActive", "PortInactive": // §9.7: Net, Node, Port
		path, oid, label := m.portRef(rrcsParamInt(p, 0), rrcsParamInt(p, 1), rrcsParamInt(p, 2), false)
		l := line(path, "Online", strconv.FormatBool(e.Method == "PortActive"))
		l.OID, l.Name = oid, label
		return []rrcsChangeLine{l}

	case "CrosspointChange": // §9.6: count, struct of XP#n arrays
		if len(p) < 2 || p[1].Kind != codec.KindStruct {
			break
		}
		var out []rrcsChangeLine
		for _, xp := range p[1].Members {
			it := xp.Value.Items
			if xp.Value.Kind != codec.KindArray || len(it) < 7 {
				continue
			}
			src, _, srcLabel := m.portRef(rrcsParamInt(it, 0), rrcsParamInt(it, 1), rrcsParamInt(it, 2), true)
			dst, oid, dstLabel := m.portRef(rrcsParamInt(it, 3), rrcsParamInt(it, 4), rrcsParamInt(it, 5), false)
			l := line("xp."+src+">"+dst, "State", rrcsOnOff(it[6]))
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
		var out []rrcsChangeLine
		for _, ch := range p[0].Items {
			s, _ := ch.Field("Source")
			d, _ := ch.Field("Destination")
			src, _, srcLabel := m.portRef(rrcsMemberInt(s, "Net"), rrcsMemberInt(s, "Node"), rrcsMemberInt(s, "Port"), true)
			dst, oid, dstLabel := m.portRef(rrcsMemberInt(d, "Net"), rrcsMemberInt(d, "Node"), rrcsMemberInt(d, "Port"), false)
			for _, member := range []string{"SingleVolume", "ConferenceVolume"} {
				v, ok := ch.Field(member)
				if !ok {
					continue
				}
				x := int(v.Int)
				if v.Kind == codec.KindString {
					x, _ = strconv.Atoi(v.Str)
				}
				value, unit := rrcsVolume(x)
				meta := rrcsMetaOf["Xp.Volume"]
				l := line("xp."+src+">"+dst, member, value)
				l.OID, l.Name, l.Unit, l.Min, l.Max = oid, srcLabel+" > "+dstLabel, unit, meta.Min, meta.Max
				out = append(out, l)
			}
		}
		if len(out) > 0 {
			return out
		}

	case "LogicSourceChange": // §9.3: Object ID, state
		id := rrcsParamInt(p, 0)
		l := line("logic."+strconv.Itoa(id), "State", "off")
		if len(p) > 1 {
			l.Value = rrcsOnOff(p[1])
		}
		l.OID = id
		if m != nil {
			for _, o := range m.Objects["logic"] {
				if o.ObjectID == id {
					l.Name = o.LongName
				}
			}
		}
		return []rrcsChangeLine{l}

	case "GpInputChange", "GpOutputChange": // §9.2: Net, Node, Port, Slot, GPIO no., state
		kind := map[string]string{"GpInputChange": "gpi", "GpOutputChange": "gpo"}[e.Method]
		path, oid, label := m.portRef(rrcsParamInt(p, 0), rrcsParamInt(p, 1), rrcsParamInt(p, 2), false)
		l := line(fmt.Sprintf("%s.%s.%d", path, kind, rrcsParamInt(p, 4)), "State", "off")
		if len(p) > 5 {
			l.Value = rrcsOnOff(p[5])
		}
		l.OID, l.Name, l.Detail = oid, label, "slot "+strconv.Itoa(rrcsParamInt(p, 3))
		return []rrcsChangeLine{l}

	case "SendString", "SendStringOff": // §9.1: String
		text := ""
		if len(p) > 0 {
			text = p[0].Str
		}
		l := line("gateway", "SendString", text)
		if e.Method == "SendStringOff" {
			l.Detail = "off"
		}
		return []rrcsChangeLine{l}

	case "SicFailed": // §9.7: {Bay, Description, Net, Node, Path, Severity, Status, Type}
		if len(p) < 1 || p[0].Kind != codec.KindStruct {
			break
		}
		d := p[0]
		l := line(fmt.Sprintf("net.%d.node.%d.card.%d", rrcsMemberInt(d, "Net"), rrcsMemberInt(d, "Node"), rrcsMemberInt(d, "Bay")),
			"SicFailed", strconv.FormatBool(rrcsFieldBool(d, "Status")))
		desc, _ := d.Field("Description")
		l.Detail = fmt.Sprintf("severity %d %s", rrcsMemberInt(d, "Severity"), desc.Str)
		return []rrcsChangeLine{l}

	case "ConfigurationChange": // §9.5
		return []rrcsChangeLine{line("gateway", "Configuration", "changed")}

	case "ConnectArtistRestored", "ConnectArtistFailure", "GatewayShutdown": // §9.7: GatewayState
		state := ""
		if len(p) > 0 {
			state = p[0].Str
		}
		l := line("gateway", "ArtistConnection", map[string]string{
			"ConnectArtistRestored": "connected", "ConnectArtistFailure": "failed", "GatewayShutdown": "shutdown"}[e.Method])
		l.Detail = "gateway state " + state
		return []rrcsChangeLine{l}

	case "UpstreamFailed", "UpstreamFailedCleared", "DownstreamFailed", "DownstreamFailedCleared",
		"NodeControllerFailed", "NodeControllerReboot": // §9.7: Net, Node
		label := strings.TrimSuffix(e.Method, "Cleared")
		return []rrcsChangeLine{line(netNode(), label, strconv.FormatBool(!strings.HasSuffix(e.Method, "Cleared")))}

	case "ClientFailed", "ClientFailedCleared": // §9.7: Net, Node, Slot-of-client-card
		l := line(fmt.Sprintf("%s.card.%d", netNode(), rrcsParamInt(p, 2)), "ClientFailed", strconv.FormatBool(e.Method == "ClientFailed"))
		return []rrcsChangeLine{l}

	case rrcs.MethodPanelSpyKeyEvent: // §9.9.4: KeyEventData
		if len(p) < 1 || p[0].Kind != codec.KindStruct {
			break
		}
		d := p[0]
		path, oid, label := m.spyRef(rrcsMemberInt(d, "Node"), rrcsMemberInt(d, "Port"))
		action := map[int]string{0: "pressed", 1: "released", 2: "double-click", 3: "long-press"}[rrcsMemberInt(d, "KeyAction")]
		if action == "" {
			action = "action " + strconv.Itoa(rrcsMemberInt(d, "KeyAction"))
		}
		l := line(fmt.Sprintf("%s.keyevent.%d.%d", path, rrcsMemberInt(d, "SubPanel"), rrcsMemberInt(d, "Key")), "KeyAction", action)
		l.OID, l.Name = oid, label
		l.Detail = fmt.Sprintf("key type %d latched %v", rrcsMemberInt(d, "KeyType"), rrcsFieldBool(d, "IsKeyLatched"))
		// What the first page holds on that key, for a reader. The event
		// does not say which page was shown.
		if m != nil {
			if port := m.find(rrcsMemberInt(d, "Node"), rrcsMemberInt(d, "Port"), false); port != nil {
				for _, k := range port.Keys {
					if k.Position == "key" && k.Expansion == rrcsMemberInt(d, "SubPanel") && k.Key == rrcsMemberInt(d, "Key") && k.Page == 1 {
						l.Detail += "; page 1: " + k.CommandType + " " + rrcsTarget(k)
					}
				}
			}
		}
		return []rrcsChangeLine{l}

	case rrcs.MethodPanelSpyFuncKeyEvent, rrcs.MethodPanelSpyRotateEvent, rrcs.MethodPanelSpyNumKeyEvent: // §9.9.3, §9.9.5, §9.9.6
		if len(p) < 1 || p[0].Kind != codec.KindStruct {
			break
		}
		d := p[0]
		path, oid, label := m.portRef(1, rrcsMemberInt(d, "Node"), rrcsMemberInt(d, "Port"), false)
		var out []rrcsChangeLine
		for _, member := range d.Members {
			if member.Name == "Node" || member.Name == "Port" || member.Value.Kind == codec.KindStruct {
				continue
			}
			l := line(path+"."+strings.ToLower(strings.TrimSuffix(strings.TrimPrefix(e.Method, "PanelSpy"), "Event")), member.Name, rrcsCompact(member.Value))
			l.OID, l.Name = oid, label
			out = append(out, l)
		}
		if len(out) > 0 {
			return out
		}

	case rrcs.MethodPanelSpyStateChange, rrcs.MethodPanelSpyStateChanged: // §9.9.2: array of panel states
		if len(p) < 1 || p[0].Kind != codec.KindArray {
			break
		}
		states := map[int]string{0: "unregistered", 1: "busy", 2: "active", 3: "error"}
		var out []rrcsChangeLine
		for _, panel := range p[0].Items {
			path, oid, label := m.spyRef(rrcsMemberInt(panel, "Node"), rrcsMemberInt(panel, "Port"))
			for _, kind := range []string{"Key", "FuncKey", "NumKey", "Rotate"} {
				st, ok := panel.Field(kind)
				if !ok {
					continue
				}
				l := line(path+".spy", kind, states[rrcsMemberInt(st, "State")])
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

	case rrcs.MethodGetAlive:
		return []rrcsChangeLine{line("gateway", "Alive", "ping")}
	}

	// Unknown, or not in the shape the specification prints.
	l := line("event", e.Method, rrcsCompact(codec.Array(p...)))
	l.Raw = rrcsJSON(codec.Array(p...))
	return []rrcsChangeLine{l}
}
