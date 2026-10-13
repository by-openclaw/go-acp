package consumer

import (
	"net"
	"sort"
	"strconv"
	"strings"
)

// Meta is what the specification says about the values of a
// property: its range, its default, its unit, its named values.
type Meta struct {
	Min, Max, Default, Unit, Enum string
}

// MetaOf holds the ranges printed in the 9.0.1 text, by block and
// member (§8.10.4.6 for the streams, §8.10.4.34 for the client cards,
// §6.5 for the volumes). A member without an entry has none printed.
var MetaOf = map[string]Meta{
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

// MetaFor finds the entry of a member of a block.
func MetaFor(block, field string) Meta {
	switch block {
	case "PortAes67Input", "PortAes67Output":
		block = "Stream"
	case "Media_1", "Media_2":
		block = "Media"
	}
	return MetaOf[block+"."+field]
}

// export and import follow the contract of the acp connectors: one
// snapshot file, one row per value, `export --format --out`, `import
// --file [--path] [--dry-run]`. The CSV carries the same header as
// theirs (internal/export/csv.go); the columns this protocol has nothing
// for stay empty.
var CSVHeader = []string{
	"ip", "protocol", "slot", "oid", "path",
	"id", "label", "kind", "access",
	"value", "value_name",
	"unit", "min", "max", "step", "default",
	"enum_items",
	"max_len",
	"alarm_priority", "alarm_tag", "alarm_on", "alarm_off",
	"slot_status",
}

// Enums are the numbers RRCS uses, with their word (§8.10.4.6
// Protocol, §8.10.4.34 PtpMode and RegistrationMode).
var Enums = map[string]map[int]string{
	"Protocol":         {2: "Manual", 3: "RTSP", 5: "NMOS"},
	"PtpMode":          {0: "Multicast", 1: "Hybrid"},
	"RegistrationMode": {0: "Automatic", 1: "Peer2Peer", 2: "Manual"},
}

// Writable are the properties import sends, by block. Everything
// else is exported for reading and skipped as read_only on import. The
// list is what the codeowner asked to provision; it grows as edits are
// verified against a real RRCS.
var Writable = map[string]map[string]bool{
	// The top level of a portex: PortType creates a port that does not
	// exist (ChangeType create, proven 2026-10-14 on -7.33).
	"": {"PortType": true},
	"PortAes67Input": {"Protocol": true, "Multicast": true, "MulticastPort": true, "Multicast2": true, "MulticastPort2": true,
		"SourceIp": true, "SourceIp2": true, "RTSPUri": true, "RTSPUri2": true,
		"Channels": true, "BitDepth": true, "PacketTime": true, "PayloadType": true,
		"Mode": true, "Selection": true},
	"PortAes67Output": {"Protocol": true, "Multicast": true, "MulticastPort": true, "Multicast2": true, "MulticastPort2": true,
		"Channels": true, "BitDepth": true, "PacketTime": true, "PayloadType": true,
		"Mode": true, "Selection": true},
	"Ptp":     {"PTP": true, "PtpPriority": true, "PtpPriority2": true, "PtpMode": true, "PtpAnnounceInterval": true},
	"Nmos":    {"Enable": true, "RegistrationMode": true, "RegistrationIp": true, "RegistrationPort": true},
	"Media_1": {"IpAddress": true, "DefaultGateway": true},
	"Media_2": {"IpAddress": true, "DefaultGateway": true},
}

// Row is one value of the system.
type Row struct {
	IP        string `json:"ip"`
	Protocol  string `json:"protocol"`
	Path      string `json:"path"`
	ID        string `json:"id,omitempty"`
	Label     string `json:"label"`
	Kind      string `json:"kind"`
	Access    string `json:"access"`
	Value     string `json:"value"`
	ValueName string `json:"value_name,omitempty"`
	Unit      string `json:"unit,omitempty"`
	Min       string `json:"min,omitempty"`
	Max       string `json:"max,omitempty"`
	Default   string `json:"default,omitempty"`
	EnumItems string `json:"enum_items,omitempty"`
	MaxLen    string `json:"max_len,omitempty"`
}

func (r Row) Record() []string {
	rec := make([]string, len(CSVHeader))
	for i, col := range CSVHeader {
		switch col {
		case "ip":
			rec[i] = r.IP
		case "protocol":
			rec[i] = r.Protocol
		case "path":
			rec[i] = r.Path
		case "id", "oid":
			rec[i] = r.ID
		case "unit":
			rec[i] = r.Unit
		case "min":
			rec[i] = r.Min
		case "max":
			rec[i] = r.Max
		case "default":
			rec[i] = r.Default
		case "label":
			rec[i] = r.Label
		case "kind":
			rec[i] = r.Kind
		case "access":
			rec[i] = r.Access
		case "value":
			rec[i] = r.Value
		case "value_name":
			rec[i] = r.ValueName
		case "enum_items":
			rec[i] = r.EnumItems
		case "max_len":
			rec[i] = r.MaxLen
		}
	}
	return rec
}

// Scalar prints a value RRCS gave, and names its kind.
func Scalar(field string, v any) (value, kind, name, items string, ok bool) {
	switch t := v.(type) {
	case string:
		return t, "string", "", "", true
	case bool:
		return strconv.FormatBool(t), "bool", "", "", true
	case float64:
		n := int(t)
		if words, isEnum := Enums[field]; isEnum {
			keys := make([]int, 0, len(words))
			for k := range words {
				keys = append(keys, k)
			}
			sort.Ints(keys)
			parts := make([]string, 0, len(keys))
			for _, k := range keys {
				parts = append(parts, strconv.Itoa(k)+"="+words[k])
			}
			return strconv.Itoa(n), "enum", words[n], strings.Join(parts, "|"), true
		}
		return strconv.Itoa(n), "int", "", "", true
	}
	return "", "", "", "", false
}

// RowsOf flattens one object: its own values, and those of the
// blocks one level down.
//
// object is the object type of the specification the properties belong
// to (portex, client-card, panel-key, or the type of a command): it is
// what finds the range, the default and the named values of each one.
func RowsOf(target, object, path string, id int, props map[string]any) []Row {
	var rows []Row
	add := func(block, field string, v any) {
		value, kind, name, items, ok := Scalar(field, v)
		if !ok {
			return
		}
		prop, access := field, "R--"
		if block != "" {
			prop = block + "." + field
			if Writable[block][field] {
				access = "RW-"
			}
		}
		// What the specification prints for this property, then the few
		// values kept by hand (units, and the ranges of the blocks the
		// printed tables do not give cleanly).
		spec := SpecFor(object, block, field)
		meta := MetaFor(block, field)
		if meta.Min == "" && meta.Max == "" {
			meta.Min, meta.Max = spec.Min, spec.Max
		}
		if meta.Default == "" {
			meta.Default = spec.Default
		}
		if meta.Enum == "" {
			meta.Enum = spec.Enum
		}
		if meta.Enum != "" && items == "" {
			items = meta.Enum
			if kind == "int" {
				// A complete list of named values makes an enum. Where
				// the specification prints only some of them — the ends
				// of a scale, or a list of allowed numbers — the value
				// stays a number, the printed points are kept as hints,
				// and they bound the range when none is given.
				lo, hi, named, count := 0, 0, true, 0
				for _, item := range strings.Split(items, "|") {
					n, label, ok := strings.Cut(item, "=")
					key, err := strconv.Atoi(n)
					if err != nil {
						named = false
						continue
					}
					if !ok {
						named = false
					}
					if count == 0 || key < lo {
						lo = key
					}
					if count == 0 || key > hi {
						hi = key
					}
					count++
					if ok && n == value {
						name = label
					}
				}
				switch {
				case named && count > 0 && hi-lo+1 == count && meta.Min == "" && meta.Max == "":
					kind = "enum"
				case count > 0 && named:
					if meta.Min == "" || meta.Max == "" {
						meta.Min, meta.Max = strconv.Itoa(lo), strconv.Itoa(hi)
					} else {
						if n, err := strconv.Atoi(meta.Min); err == nil && lo < n {
							meta.Min = strconv.Itoa(lo)
						}
						if n, err := strconv.Atoi(meta.Max); err == nil && hi > n {
							meta.Max = strconv.Itoa(hi)
						}
					}
				}
			}
		}
		rows = append(rows, Row{IP: target, Protocol: Name, Path: path + "." + prop, ID: strconv.Itoa(id),
			Label: field, Kind: kind, Access: access, Value: value, ValueName: name,
			Unit: meta.Unit, Min: meta.Min, Max: meta.Max, Default: meta.Default, EnumItems: items, MaxLen: spec.MaxLen})
	}
	names := make([]string, 0, len(props))
	for k := range props {
		names = append(names, k)
	}
	sort.Strings(names)
	for _, k := range names {
		if block := jMap(props[k]); block != nil {
			fields := make([]string, 0, len(block))
			for f := range block {
				fields = append(fields, f)
			}
			sort.Strings(fields)
			for _, f := range fields {
				add(k, f, block[f])
			}
			continue
		}
		add("", k, props[k])
	}
	return rows
}

// Rows flattens a model: the nodes, the client cards, the ports with
// their keys, then the objects that belong to no card — conferences,
// groups, IFBs, logic sources, users, audio patches, logic destinations.
// The crosspoints are not rows; they are the matrix.
func Rows(m *Model) []Row {
	target := hostOnly(m.Target)
	rows := nodeRows(target, m)
	for _, c := range m.Cards {
		rows = append(rows, RowsOf(target, "client-card", c.Path, c.ObjectID, c.Raw)...)
	}
	for _, p := range m.Ports {
		rows = append(rows, RowsOf(target, "portex", p.Path, p.ObjectID, p.Raw)...)
		// Each key position of a panel is an object of its own, and so
		// is the command on it: both go out under the key, with their
		// own object ID.
		assigned := map[string]bool{}
		for _, k := range p.Keys {
			assigned[k.Path] = true
		}
		for _, kc := range p.KeyConfigs {
			rows = append(rows, RowsOf(target, "panel-key", kc.Path, kc.ObjectID, kc.Props)...)
		}
		for _, k := range p.Keys {
			cmd := map[string]any{}
			for name, v := range k.Raw {
				cmd[name] = v
			}
			if k.Target != "" {
				cmd["Target"] = k.Target
			}
			if k.TargetName != "" {
				cmd["TargetName"] = k.TargetName
			}
			rows = append(rows, RowsOf(target, k.CommandType, k.Path+".cmd", k.ObjectID, cmd)...)
		}
	}
	return append(rows, objectRows(target, m)...)
}

// nodeRows flattens the nodes.
func nodeRows(target string, m *Model) []Row {
	var rows []Row
	for _, n := range m.Nodes {
		rows = append(rows, RowsOf(target, "node", n.Path, n.ObjectID, n.Raw)...)
	}
	return rows
}

// objectRows flattens what belongs to no card: conferences, groups,
// IFBs, logic sources, users, audio patches, logic destinations.
func objectRows(target string, m *Model) []Row {
	var rows []Row
	for _, kind := range []string{"conference", "group", "ifb", "logic", "user", "patch", "logicdest"} {
		for _, o := range m.Objects[kind] {
			rows = append(rows, RowsOf(target, kind, o.Path, o.ObjectID, o.Raw)...)
			// The members, one row each: the path of the member port and
			// its talk/listen flags, which is what a CRUD of the object
			// changes.
			for i, mem := range o.Members {
				v := mem.Port
				if mem.Flags != "" {
					v += " " + mem.Flags
				}
				rows = append(rows, Row{IP: target, Protocol: Name,
					Path: o.Path + ".member." + strconv.Itoa(i+1), ID: strconv.Itoa(o.ObjectID),
					Label: mem.Label, Kind: "string", Access: "R--", Value: v})
			}
		}
	}
	return rows
}

// hostOnly strips an optional :port from an address.
func hostOnly(addr string) string {
	if h, _, err := net.SplitHostPort(addr); err == nil {
		return h
	}
	return addr
}
