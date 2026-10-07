package main

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
)

// The model is the walk seen as a tree of addressable things. It is built
// from the snapshot document, so a live gateway and a snapshot file give
// the same tree.
//
// Paths:
//
//	net.N.node.N                      a node
//	net.N.node.N.card.B               a client card, by bay
//	net.N.node.N.port.P               a port with both directions
//	net.N.node.N.port.P.in  / .out    a port with one direction: an input
//	                                  and an output may share a number
//	<port>.key.E.G.K                  expansion panel · page · key
//	<port>.vfunc.TYPE.K               a virtual function
//	conference.ID  group.ID  ifb.ID  logic.ID

type rrcsNode struct {
	Path     string         `json:"path"`
	Net      int            `json:"net"`
	Node     int            `json:"node"`
	LongName string         `json:"long_name,omitempty"`
	Type     string         `json:"type,omitempty"`
	ObjectID int            `json:"object_id,omitempty"`
	Ports    int            `json:"ports"`
	Raw      map[string]any `json:"-"`
}

type rrcsCard struct {
	Path     string         `json:"path"`
	Node     int            `json:"node"`
	Bay      int            `json:"bay"`
	Type     string         `json:"type"`
	LongName string         `json:"long_name"`
	ObjectID int            `json:"object_id"`
	Raw      map[string]any `json:"-"`
}

type rrcsPort struct {
	Path     string         `json:"path"`
	Net      int            `json:"net"`
	Node     int            `json:"node"`
	Port     int            `json:"port"`
	Input    bool           `json:"input"`
	Output   bool           `json:"output"`
	Type     string         `json:"type"`
	Label    string         `json:"label"`
	LongName string         `json:"long_name"`
	ObjectID int            `json:"object_id"`
	KeyCount int            `json:"key_count"`
	Pages    int            `json:"page_count"`
	// Read per port by walk; empty when the snapshot has no port values
	// or the port was not online.
	Alias      string     `json:"alias,omitempty"`
	InputGain  string     `json:"input_gain_db,omitempty"`
	OutputGain string     `json:"output_gain_db,omitempty"`
	Keys       []*rrcsKey `json:"-"`
	// KeyConfigs is every key position of the panel, assigned or not,
	// with its own object ID and properties (GetAllKeyConfiguration).
	KeyConfigs []rrcsKeyConfig `json:"-"`
	Raw      map[string]any `json:"-"`
}

type rrcsKey struct {
	Path        string         `json:"path"`
	Port        string         `json:"port"`
	PortLabel   string         `json:"port_label"`
	Position    string         `json:"position"` // key | virtual-function
	Expansion   int            `json:"expansion"`
	Page        int            `json:"page"`
	Key         int            `json:"key"`
	CommandType string         `json:"command_type"`
	Target      string         `json:"target,omitempty"`
	TargetName  string         `json:"target_name,omitempty"`
	ObjectID    int            `json:"object_id"`
	Description string         `json:"description"`
	Raw         map[string]any `json:"-"`
}

// rrcsKeyConfig is one key position of a panel and how the key behaves.
type rrcsKeyConfig struct {
	Path     string
	ObjectID int
	Props    map[string]any
}

type rrcsMemberRef struct {
	Port  string `json:"port"`
	Label string `json:"label,omitempty"`
	Flags string `json:"flags,omitempty"`
}

// rrcsObject is a conference, a group, an IFB or a logic source.
type rrcsObject struct {
	Path     string          `json:"path"`
	Kind     string          `json:"kind"`
	ObjectID int             `json:"object_id"`
	Label    string          `json:"label,omitempty"`
	LongName string          `json:"long_name"`
	Members  []rrcsMemberRef `json:"members,omitempty"`
	State    string          `json:"state,omitempty"`
	Raw      map[string]any  `json:"-"`
}

// rrcsXp is one active crosspoint: a source heard at a destination.
type rrcsXp struct {
	Source           string `json:"source"`
	SourceLabel      string `json:"source_label,omitempty"`
	Destination      string `json:"destination"`
	DestinationLabel string `json:"destination_label,omitempty"`

	srcNode, dstNode int
}

type rrcsModel struct {
	Target  string
	Status  map[string]string
	Nodes   []*rrcsNode
	Cards   []*rrcsCard
	Ports   []*rrcsPort
	Keys    []*rrcsKey
	Objects map[string][]*rrcsObject // conference, group, ifb, logic
	Xps     []rrcsXp

	byAddr map[[2]int][]*rrcsPort
}

func jMap(v any) map[string]any {
	m, _ := v.(map[string]any)
	return m
}

func jList(v any) []any {
	a, _ := v.([]any)
	return a
}

func jInt(m map[string]any, key string) int {
	f, _ := m[key].(float64)
	return int(f)
}

func jStr(m map[string]any, key string) string {
	s, _ := m[key].(string)
	return s
}

func jBool(m map[string]any, key string) bool {
	b, _ := m[key].(bool)
	return b
}

// jWrapped returns the list of an answer of the "[list]" payload shape.
func jWrapped(payload any) []any {
	outer := jList(payload)
	if len(outer) == 1 {
		return jList(outer[0])
	}
	return nil
}

func rrcsPortPath(net, node, port int, in, out bool) string {
	p := fmt.Sprintf("net.%d.node.%d.port.%d", net, node, port)
	switch {
	case in && !out:
		return p + ".in"
	case out && !in:
		return p + ".out"
	}
	return p
}

// find returns the port at an address. Where an input and an output share
// the number, wantInput picks one.
func (m *rrcsModel) find(node, port int, wantInput bool) *rrcsPort {
	c := m.byAddr[[2]int{node, port}]
	if len(c) == 1 {
		return c[0]
	}
	for _, p := range c {
		if p.Input == wantInput && p.Output != wantInput {
			return p
		}
	}
	if len(c) > 0 {
		return c[0]
	}
	return nil
}

// ref names the port at an address, known or not.
func (m *rrcsModel) ref(net, node, port int, wantInput bool) (path, label string) {
	if p := m.find(node, port, wantInput); p != nil {
		return p.Path, p.Label
	}
	return rrcsPortPath(net, node, port, wantInput, !wantInput), ""
}

// rrcsBuildModel reads a snapshot that went through encoding/json.
func rrcsBuildModel(snap map[string]any) *rrcsModel {
	m := &rrcsModel{
		Target:  jStr(snap, "target"),
		Status:  map[string]string{},
		Objects: map[string][]*rrcsObject{},
		byAddr:  map[[2]int][]*rrcsPort{},
	}
	for _, c := range jList(snap["status"]) {
		call := jMap(c)
		switch p := call["payload"].(type) {
		case []any:
			if len(p) == 1 {
				m.Status[jStr(call, "method")] = fmt.Sprint(p[0])
			}
		case map[string]any:
			for k, v := range p {
				m.Status[k] = fmt.Sprint(v)
			}
		}
	}
	lists := map[string]any{}
	for _, c := range jList(snap["lists"]) {
		call := jMap(c)
		lists[jStr(call, "method")] = call["payload"]
	}

	net := 1
	for _, it := range jWrapped(lists["GetAllPorts"]) {
		raw := jMap(it)
		p := &rrcsPort{
			Net: jInt(raw, "Net"), Node: jInt(raw, "Node"), Port: jInt(raw, "Port"),
			Input: jBool(raw, "Input"), Output: jBool(raw, "Output"),
			Type: jStr(raw, "PortType"), Label: jStr(raw, "Label"), LongName: jStr(raw, "LongName"),
			ObjectID: jInt(raw, "ObjectID"), KeyCount: jInt(raw, "KeyCount"), Pages: jInt(raw, "PageCount"),
			Raw: raw,
		}
		p.Path = rrcsPortPath(p.Net, p.Node, p.Port, p.Input, p.Output)
		net = p.Net
		m.Ports = append(m.Ports, p)
		m.byAddr[[2]int{p.Node, p.Port}] = append(m.byAddr[[2]int{p.Node, p.Port}], p)
	}
	sort.SliceStable(m.Ports, func(i, j int) bool {
		a, b := m.Ports[i], m.Ports[j]
		if a.Node != b.Node {
			return a.Node < b.Node
		}
		if a.Port != b.Port {
			return a.Port < b.Port
		}
		return a.Input && !b.Input
	})

	// Nodes: those RRCS lists, and those the ports name. On an
	// Artist-1024 the ports carry node numbers the node list does not
	// hold (seen on 9.0: one node 60, ports on 61 to 66).
	nodes := map[int]*rrcsNode{}
	for _, it := range jWrapped(lists["GetAllNodes"]) {
		raw := jMap(it)
		n := jInt(raw, "NodeAddress")
		nodes[n] = &rrcsNode{Net: net, Node: n, LongName: jStr(raw, "LongName"),
			Type: jStr(raw, "NodeTypeString"), ObjectID: jInt(raw, "ObjectID"), Raw: raw}
	}
	for _, p := range m.Ports {
		if nodes[p.Node] == nil {
			nodes[p.Node] = &rrcsNode{Net: p.Net, Node: p.Node}
		}
		nodes[p.Node].Ports++
	}
	for _, n := range nodes {
		n.Path = fmt.Sprintf("net.%d.node.%d", n.Net, n.Node)
		m.Nodes = append(m.Nodes, n)
	}
	sort.Slice(m.Nodes, func(i, j int) bool { return m.Nodes[i].Node < m.Nodes[j].Node })

	for _, it := range jWrapped(lists["GetAllClientCards"]) {
		raw := jMap(it)
		c := &rrcsCard{Node: jInt(raw, "Node"), Bay: jInt(raw, "Bay"), Type: jStr(raw, "ClientCardTypeString"),
			LongName: jStr(raw, "LongName"), ObjectID: jInt(raw, "ObjectID"), Raw: raw}
		c.Path = fmt.Sprintf("net.%d.node.%d.card.%d", net, c.Node, c.Bay)
		m.Cards = append(m.Cards, c)
	}
	sort.Slice(m.Cards, func(i, j int) bool {
		if m.Cards[i].Node != m.Cards[j].Node {
			return m.Cards[i].Node < m.Cards[j].Node
		}
		return m.Cards[i].Bay < m.Cards[j].Bay
	})

	// §8.1 GetAllActiveXps: "XP#N": [source net, node, port, destination
	// net, node, port].
	for name, v := range jMap(lists["GetAllActiveXps"]) {
		row := jList(v)
		if !strings.HasPrefix(name, "XP#") || len(row) < 6 {
			continue
		}
		n := func(i int) int { f, _ := row[i].(float64); return int(f) }
		x := rrcsXp{srcNode: n(1), dstNode: n(4)}
		x.Source, x.SourceLabel = m.ref(n(0), n(1), n(2), true)
		x.Destination, x.DestinationLabel = m.ref(n(3), n(4), n(5), false)
		m.Xps = append(m.Xps, x)
	}
	sort.Slice(m.Xps, func(i, j int) bool {
		if m.Xps[i].Destination != m.Xps[j].Destination {
			return m.Xps[i].Destination < m.Xps[j].Destination
		}
		return m.Xps[i].Source < m.Xps[j].Source
	})

	m.buildObjects(lists, net)
	m.buildKeys(jList(snap["commands"]))
	m.buildPortValues(jList(snap["port_values"]))
	m.buildKeyConfigs(jList(snap["key_configs"]))
	// What walk listed by object type and the model has no table for:
	// users, audio patches, logic destinations.
	for typ, kind := range map[string]string{"user": "user", "audiopatch": "patch", "logic-destination": "logicdest"} {
		for _, it := range jList(jMap(snap["objects"])[typ]) {
			raw := jMap(it)
			props := jMap(raw["properties"])
			if props == nil {
				props = map[string]any{}
			}
			id := jInt(raw, "object_id")
			props["ObjectID"], props["LongName"] = float64(id), jStr(raw, "long_name")
			m.Objects[kind] = append(m.Objects[kind], &rrcsObject{Path: kind + "." + strconv.Itoa(id), Kind: kind,
				ObjectID: id, LongName: jStr(raw, "long_name"), Label: jStr(props, "Label"), Raw: props})
		}
		objs := m.Objects[kind]
		sort.Slice(objs, func(i, j int) bool { return objs[i].LongName < objs[j].LongName })
	}
	return m
}

func (m *rrcsModel) buildObjects(lists map[string]any, net int) {
	add := func(kind string, raw map[string]any) *rrcsObject {
		o := &rrcsObject{Kind: kind, ObjectID: jInt(raw, "ObjectID"), Label: jStr(raw, "Label"),
			LongName: jStr(raw, "LongName"), Raw: raw}
		o.Path = kind + "." + strconv.Itoa(o.ObjectID)
		m.Objects[kind] = append(m.Objects[kind], o)
		return o
	}
	for _, it := range jWrapped(lists["GetAllConferences"]) {
		raw := jMap(it)
		o := add("conference", raw)
		for _, mem := range jList(raw["MemberList"]) {
			mm := jMap(mem)
			var flags []string
			if jBool(mm, "Talk") {
				flags = append(flags, "talk")
			}
			if jBool(mm, "Listen") {
				flags = append(flags, "listen")
			}
			path, label := m.ref(net, jInt(mm, "Node"), jInt(mm, "Port"), false)
			o.Members = append(o.Members, rrcsMemberRef{Port: path, Label: label, Flags: strings.Join(flags, "+")})
		}
	}
	for _, it := range jWrapped(lists["GetAllGroups"]) {
		raw := jMap(it)
		o := add("group", raw)
		for _, mem := range jList(raw["MemberList"]) {
			mm := jMap(mem)
			path, label := m.ref(net, jInt(mm, "Node"), jInt(mm, "Port"), false)
			o.Members = append(o.Members, rrcsMemberRef{Port: path, Label: label})
		}
	}
	for _, it := range jWrapped(lists["GetAllIFBs"]) {
		raw := jMap(it)
		o := add("ifb", raw)
		for _, role := range []string{"Input", "Output", "MixMinus"} {
			a := jMap(raw[role])
			// Node 0, port 0 is how RRCS prints an unassigned leg.
			if a == nil || (jInt(a, "Node") == 0 && jInt(a, "Port") == 0) {
				continue
			}
			path, label := m.ref(jInt(a, "Net"), jInt(a, "Node"), jInt(a, "Port"), jBool(a, "IsInput"))
			o.Members = append(o.Members, rrcsMemberRef{Port: path, Label: label, Flags: strings.ToLower(role)})
		}
	}
	// §8.7: "LogicSource#N": [long name, 8 char. label, object ID, state].
	for name, v := range jMap(lists["GetAllLogicSources_v2"]) {
		row := jList(v)
		if !strings.HasPrefix(name, "LogicSource#") || len(row) < 4 {
			continue
		}
		id, _ := row[2].(float64)
		o := &rrcsObject{Kind: "logic", ObjectID: int(id), Path: "logic." + strconv.Itoa(int(id)),
			LongName: fmt.Sprint(row[0]), Label: fmt.Sprint(row[1]), State: "off",
			Raw: map[string]any{"LongName": row[0], "Label": row[1], "ObjectID": row[2], "State": row[3]}}
		if on, _ := row[3].(bool); on {
			o.State = "on"
		}
		m.Objects["logic"] = append(m.Objects["logic"], o)
	}
	for _, objs := range m.Objects {
		sort.Slice(objs, func(i, j int) bool {
			if objs[i].LongName != objs[j].LongName {
				return objs[i].LongName < objs[j].LongName
			}
			return objs[i].ObjectID < objs[j].ObjectID
		})
	}
}

func (m *rrcsModel) buildKeys(commands []any) {
	seen := map[string]bool{}
	for _, c := range commands {
		call := jMap(c)
		for _, e := range jList(jMap(call["payload"])["CommandLists"]) {
			entry := jMap(e)
			pos := jMap(entry["CommandPosition"])
			port := m.find(jInt(pos, "Node"), jInt(pos, "Port"), jBool(pos, "IsInput"))
			if port == nil {
				continue
			}
			base := rrcsKey{Port: port.Path, PortLabel: port.Label, Position: jStr(pos, "PositionType"),
				Expansion: jInt(pos, "ExpansionPanel"), Page: jInt(pos, "Page"), Key: jInt(pos, "KeyNumber")}
			if base.Position == "key" {
				base.Path = fmt.Sprintf("%s.key.%d.%d.%d", port.Path, base.Expansion, base.Page, base.Key)
			} else {
				base.Path = fmt.Sprintf("%s.vfunc.%s.%d", port.Path, jStr(pos, "VirtualFunctionType"), base.Key)
			}
			for i, cmdAny := range jList(entry["CommandList"]) {
				cmd := jMap(cmdAny)
				k := base
				if i > 0 {
					k.Path += ".cmd." + strconv.Itoa(i+1)
				}
				// The two directions of a port answer the same list.
				if seen[k.Path] {
					continue
				}
				seen[k.Path] = true
				k.CommandType, k.Description = jStr(cmd, "CommandType"), strings.TrimSpace(jStr(cmd, "Description"))
				k.ObjectID, k.Raw = jInt(cmd, "ObjectID"), cmd
				switch {
				case cmd["DestinationPortAddress"] != nil:
					a := jMap(cmd["DestinationPortAddress"])
					k.Target, k.TargetName = m.ref(jInt(a, "Net"), jInt(a, "Node"), jInt(a, "Port"), jBool(a, "IsInput"))
				case cmd["Group"] != nil:
					k.Target, k.TargetName = "group."+strconv.Itoa(jInt(cmd, "Group")), jStr(cmd, "GroupName")
				case cmd["Conference"] != nil:
					k.Target, k.TargetName = "conference."+strconv.Itoa(jInt(cmd, "Conference")), jStr(cmd, "ConferenceName")
				case cmd["TrunkingNetAddr"] != nil:
					k.Target = fmt.Sprintf("trunk.%d.%d", jInt(cmd, "TrunkingNetAddr"), jInt(cmd, "TrunkingPortAddr"))
				}
				key := k
				port.Keys = append(port.Keys, &key)
				m.Keys = append(m.Keys, &key)
			}
		}
	}
	less := func(a, b *rrcsKey) bool {
		if a.Port != b.Port {
			return a.Port < b.Port
		}
		if a.Position != b.Position {
			return a.Position < b.Position
		}
		if a.Expansion != b.Expansion {
			return a.Expansion < b.Expansion
		}
		if a.Page != b.Page {
			return a.Page < b.Page
		}
		return a.Key < b.Key
	}
	for _, p := range m.Ports {
		keys := p.Keys
		sort.SliceStable(keys, func(i, j int) bool { return less(keys[i], keys[j]) })
	}
	// Keys in the order of their ports.
	m.Keys = m.Keys[:0]
	for _, p := range m.Ports {
		m.Keys = append(m.Keys, p.Keys...)
	}
}

// lookup finds what a path names and returns its properties as RRCS gave
// them.
func (m *rrcsModel) lookup(path string) (map[string]any, bool) {
	for _, n := range m.Nodes {
		if n.Path == path {
			if n.Raw == nil {
				return map[string]any{"Node": n.Node, "Ports": n.Ports}, true
			}
			return n.Raw, true
		}
	}
	for _, c := range m.Cards {
		if c.Path == path {
			return c.Raw, true
		}
	}
	for _, p := range m.Ports {
		if p.Path == path {
			return p.Raw, true
		}
	}
	for _, k := range m.Keys {
		if k.Path == path {
			return k.Raw, true
		}
	}
	for _, objs := range m.Objects {
		for _, o := range objs {
			if o.Path == path {
				return o.Raw, true
			}
		}
	}
	return nil, false
}

// buildPortValues reads the per-port answers of walk into the ports. The
// values are also put into the properties of the port, so get, export
// and import see them under Alias, InputGain and OutputGain.
func (m *rrcsModel) buildPortValues(calls []any) {
	for _, c := range calls {
		call := jMap(c)
		args := jList(call["args"])
		payload := jList(call["payload"])
		if len(payload) == 0 {
			continue
		}
		num := func(i int) int {
			if i < len(args) {
				f, _ := args[i].(float64)
				return int(f)
			}
			return 0
		}
		method := jStr(call, "method")
		var port *rrcsPort
		switch method {
		case "GetPortLabel": // node, port, input
			in, _ := args[2].(bool)
			port = m.find(num(0), num(1), in)
		case "GetPortAlias": // net, node, port, input
			in, _ := args[3].(bool)
			port = m.find(num(1), num(2), in)
		case "GetInputGain":
			port = m.find(num(1), num(2), true)
		case "GetOutputGain":
			port = m.find(num(1), num(2), false)
		}
		if port == nil {
			continue
		}
		switch method {
		case "GetPortAlias":
			port.Alias, _ = payload[0].(string)
			port.Raw["Alias"] = port.Alias
		case "GetInputGain", "GetOutputGain":
			// §8.5: gain [dB] = Gain / 2.0, -128 = mute.
			raw, _ := payload[0].(float64)
			db := "mute"
			if int(raw) != -128 {
				db = strconv.FormatFloat(raw/2, 'f', 1, 64)
			}
			if method == "GetInputGain" {
				port.InputGain = db
				port.Raw["InputGain"] = raw
			} else {
				port.OutputGain = db
				port.Raw["OutputGain"] = raw
			}
		}
	}
}

// buildKeyConfigs reads GetAllKeyConfiguration of each panel: args are
// node, port, input, pool port; the answer holds KeyList, each entry a
// KeyPosition and its KeyProperties.
func (m *rrcsModel) buildKeyConfigs(calls []any) {
	for _, c := range calls {
		call := jMap(c)
		args := jList(call["args"])
		if len(args) < 3 {
			continue
		}
		node, _ := args[0].(float64)
		number, _ := args[1].(float64)
		in, _ := args[2].(bool)
		port := m.find(int(node), int(number), in)
		if port == nil {
			continue
		}
		for _, e := range jList(jMap(call["payload"])["KeyList"]) {
			entry := jMap(e)
			pos, props := jMap(entry["KeyPosition"]), jMap(entry["KeyProperties"])
			if pos == nil || props == nil {
				continue
			}
			path := fmt.Sprintf("%s.key.%d.%d.%d", port.Path, jInt(pos, "ExpansionPanel"), jInt(pos, "Page"), jInt(pos, "KeyNumber"))
			if typ := jStr(pos, "PositionType"); typ != "" && typ != "key" {
				path = fmt.Sprintf("%s.%s.%d", port.Path, typ, jInt(pos, "KeyNumber"))
			}
			port.KeyConfigs = append(port.KeyConfigs, rrcsKeyConfig{Path: path, ObjectID: jInt(props, "ObjectID"), Props: props})
		}
		keys := port.KeyConfigs
		sort.SliceStable(keys, func(i, j int) bool { return keys[i].Path < keys[j].Path })
	}
}
