package consumer

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	"dhs/internal/export/canonical"
	"dhs/internal/manifest"
)

// The device models of a system (ADR-0022), in the canonical form: one
// file for the node type, one per client card type. Every type the
// system holds — a port type, a key, a command, a conference — is one
// entry of templates[]; the instances are small elements that refer to
// it. The specification gives the shape of every type in advance; one
// walk says which types this version has, which blocks each one
// carries, and what it exposes that the printed tables do not.

// Version keeps of what GetVersion answers the part that names the
// surface: "9.0.RR1-11.f33d55f" → "9.0.RR1-11". A last dotted part that
// reads as a build hash is dropped.
func Version(s string) string {
	s = strings.TrimSpace(s)
	i := strings.LastIndex(s, ".")
	if i < 0 {
		return s
	}
	tail := s[i+1:]
	if len(tail) >= 6 && strings.Trim(tail, "0123456789abcdef") == "" {
		return s[:i]
	}
	return s
}

// TypeName names a port type for a file: the name its PortExType has in
// the type list, else the PortType string in capitals with every other
// character as "_" — the plant holds types 33 and 48, which the list of
// RRCS 9.0 does not name.
func TypeName(exType int, typeString string, names map[int]string) string {
	if n, ok := names[exType]; ok && n != "" {
		return n
	}
	return slugType(typeString)
}

func slugType(s string) string {
	var b strings.Builder
	underscore := false
	for _, r := range strings.ToUpper(s) {
		switch {
		case r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
			b.WriteRune(r)
			underscore = false
		default:
			if b.Len() > 0 && !underscore {
				b.WriteByte('_')
			}
			underscore = true
		}
	}
	return strings.TrimRight(b.String(), "_")
}

// ExTypeNames reads the type list of a snapshot (GetPortExTypeList):
// the number of a port type and its name.
func ExTypeNames(snap map[string]any) map[int]string {
	names := map[int]string{}
	for _, c := range jList(snap["lists"]) {
		call := jMap(c)
		if jStr(call, "method") != "GetPortExTypeList" {
			continue
		}
		for _, it := range jWrapped(call["payload"]) {
			raw := jMap(it)
			if name := jStr(raw, "PortExTypeString"); name != "" {
				names[jInt(raw, "PortExType")] = name
			}
		}
	}
	return names
}

// TypeStat is what the walk said about one type, beside the template.
type TypeStat struct {
	Name     string         `json:"name"`
	Object   string         `json:"object"`
	ExType   int            `json:"ex_type"`
	Strings  []string       `json:"strings,omitempty"`
	Count    int            `json:"count"`
	Props    int            `json:"properties"`
	Observed []string       `json:"observed,omitempty"`
	Hidden   []string       `json:"not_exposed,omitempty"`
	Roles    map[string]int `json:"roles,omitempty"`
}

// DMFile is one device model: its identity and its canonical content.
type DMFile struct {
	Identity string
	Model    string
	SwRev    string
	Export   canonical.Export
	Types    []TypeStat
}

// DMSet is what one walk gives: the node's model and the card models.
type DMSet struct {
	Version string
	Node    *DMFile
	Cards   []*DMFile
	// Types lists every type met, in the order of the templates.
	Types []TypeStat
	// typeOfCard says which card model holds the template of a type.
	typeOfCard map[string]*DMFile
}

// BuildDMs makes the models of a system from its model and the version
// RRCS answered. exTypes names the port types (ExTypeNames).
func BuildDMs(m *Model, version string, exTypes map[int]string) (*DMSet, error) {
	if m == nil {
		return nil, fmt.Errorf("rrcs: no model")
	}
	version = Version(version)
	if version == "" {
		return nil, fmt.Errorf("rrcs: the version of the gateway is not known")
	}
	target := hostOnly(m.Target)
	set := &DMSet{Version: version, typeOfCard: map[string]*DMFile{}}

	// The node: its own properties as the root, the system's types as
	// templates.
	nodeType, nodeRaw := "node", map[string]any{}
	for _, n := range m.Nodes {
		if n.Type != "" {
			nodeType, nodeRaw = n.Type, n.Raw
			break
		}
	}
	nodeDM := &DMFile{Identity: nodeType + "@" + version, Model: nodeType, SwRev: version}
	root := newTNode(nodeType)
	for _, r := range RowsOf(target, "node", "node", 0, nodeRaw) {
		root.add(strings.TrimPrefix(r.Path, "node."), r, false)
	}
	var templates []*canonical.TemplateEntry
	var stats []TypeStat
	next := func(t *tnode, desc string) {
		n := len(templates) + 1
		oid := "9." + strconv.Itoa(n)
		el := t.build(n, t.ident, oid)
		var d *string
		if desc != "" {
			d = &desc
		}
		templates = append(templates, &canonical.TemplateEntry{Number: n, OID: oid, Identifier: t.ident, Description: d, Template: el})
	}
	for _, kind := range []string{"conference", "group", "ifb", "logic"} {
		t, st := objectTemplate(target, kind, m.Objects[kind])
		if st.Count > 0 || len(SpecFields(kind, "")) > 0 {
			next(t, "")
			stats = append(stats, st)
		}
	}
	keyT, keySt := keyTemplate(target, m)
	next(keyT, "a key of a panel; cmd is one of the command templates, by CommandType")
	stats = append(stats, keySt)
	for _, t := range commandTemplates(target, m) {
		next(t.node, t.desc)
		stats = append(stats, t.stat)
	}
	nodeDM.Export = canonical.Export{Root: root.build(1, nodeType, "1"), Templates: templates}
	nodeDM.Types = stats
	set.Node = nodeDM
	set.Types = append(set.Types, stats...)

	// The cards: one model per card type, its own properties as the
	// root, the port types of its nodes as templates.
	cardOfNode := m.CardOfNode()
	cards := map[string]*DMFile{}
	cardRoots := map[string]*tnode{}
	cardTemplates := map[string][]*canonical.TemplateEntry{}
	var cardOrder []string
	for _, c := range m.Cards {
		if cards[c.Type] == nil {
			cards[c.Type] = &DMFile{Identity: c.Type + "@" + version, Model: c.Type, SwRev: version}
			cardRoots[c.Type] = newTNode(c.Type)
			cardOrder = append(cardOrder, c.Type)
		}
		for _, r := range RowsOf(target, "client-card", "card", c.ObjectID, c.Raw) {
			cardRoots[c.Type].add(strings.TrimPrefix(r.Path, "card."), r, false)
		}
	}
	for _, name := range cardOrder {
		set.Cards = append(set.Cards, cards[name])
	}
	for _, t := range portTemplates(target, m, exTypes, cardOfNode) {
		owner := cards[t.card]
		if owner == nil && len(set.Cards) > 0 {
			owner = set.Cards[0]
		}
		if owner == nil {
			// A system without client cards: the port types sit with
			// the node.
			n := len(templates) + 1
			oid := "9." + strconv.Itoa(n)
			templates = append(templates, &canonical.TemplateEntry{Number: n, OID: oid, Identifier: t.node.ident, Template: t.node.build(n, t.node.ident, oid)})
			nodeDM.Export.Templates = templates
			nodeDM.Types = append(nodeDM.Types, t.stat)
			set.typeOfCard[t.node.ident] = nodeDM
		} else {
			list := cardTemplates[owner.Model]
			n := len(list) + 1
			oid := "9." + strconv.Itoa(n)
			list = append(list, &canonical.TemplateEntry{Number: n, OID: oid, Identifier: t.node.ident, Template: t.node.build(n, t.node.ident, oid)})
			cardTemplates[owner.Model] = list
			owner.Types = append(owner.Types, t.stat)
			set.typeOfCard[t.node.ident] = owner
		}
		set.Types = append(set.Types, t.stat)
	}
	for _, c := range set.Cards {
		c.Export = canonical.Export{Root: cardRoots[c.Model].build(1, c.Model, "1"), Templates: cardTemplates[c.Model]}
	}
	return set, nil
}

// ---- templates ----------------------------------------------------------

// tnode is a node of a template while it is built.
type tnode struct {
	ident    string
	desc     string
	params   map[string]Row
	observed map[string]bool
	kids     map[string]*tnode
}

func newTNode(ident string) *tnode {
	return &tnode{ident: ident, params: map[string]Row{}, observed: map[string]bool{}, kids: map[string]*tnode{}}
}

func (n *tnode) child(ident string) *tnode {
	k := n.kids[ident]
	if k == nil {
		k = newTNode(ident)
		n.kids[ident] = k
	}
	return k
}

// add places one row at its relative path; the first row of a property
// is kept, so the shape is that of the first instance met.
func (n *tnode) add(rel string, r Row, observed bool) {
	parts := strings.Split(rel, ".")
	node := n
	for _, p := range parts[:len(parts)-1] {
		node = node.child(p)
	}
	field := parts[len(parts)-1]
	if _, seen := node.params[field]; !seen {
		node.params[field] = r
		if observed {
			node.observed[field] = true
		}
	}
}

func (n *tnode) count() int {
	c := len(n.params)
	for _, k := range n.kids {
		c += k.count()
	}
	return c
}

// build turns the node into canonical elements: the parameters first,
// then the child nodes, both by name.
func (n *tnode) build(number int, path, oid string) *canonical.Node {
	node := &canonical.Node{Header: canonical.Header{Number: number, Identifier: n.ident, Path: path, OID: oid, IsOnline: true, Access: canonical.AccessRead}}
	if n.desc != "" {
		d := n.desc
		node.Description = &d
	}
	names := make([]string, 0, len(n.params))
	for f := range n.params {
		names = append(names, f)
	}
	sort.Strings(names)
	i := 0
	for _, f := range names {
		i++
		node.Children = append(node.Children, paramOf(n.params[f], f, n.observed[f], i, path+"."+f, oid+"."+strconv.Itoa(i)))
	}
	kids := make([]string, 0, len(n.kids))
	for k := range n.kids {
		kids = append(kids, k)
	}
	sort.Strings(kids)
	for _, k := range kids {
		i++
		node.Children = append(node.Children, n.kids[k].build(i, path+"."+k, oid+"."+strconv.Itoa(i)))
	}
	if node.Children == nil {
		node.Children = canonical.EmptyChildren()
	}
	return node
}

// paramOf is one property of a type: its kind, its bounds and its named
// values as the specification prints them; never a value of the plant.
func paramOf(r Row, field string, observed bool, number int, path, oid string) *canonical.Parameter {
	p := &canonical.Parameter{Header: canonical.Header{Number: number, Identifier: field, Path: path, OID: oid, IsOnline: true, Access: canonical.AccessRead}}
	if strings.Contains(r.Access, "W") {
		p.Access = canonical.AccessReadWrite
	}
	switch r.Kind {
	case "bool":
		p.Type = canonical.ParamBoolean
	case "int":
		p.Type = canonical.ParamInteger
	case "enum":
		p.Type = canonical.ParamEnum
		for _, item := range strings.Split(r.EnumItems, "|") {
			n, label, ok := strings.Cut(item, "=")
			v, err := strconv.ParseInt(n, 10, 64)
			if !ok || err != nil {
				continue
			}
			p.EnumMap = append(p.EnumMap, canonical.EnumEntry{Key: label, Value: v})
		}
	default:
		p.Type = canonical.ParamString
	}
	p.Minimum, p.Maximum, p.Default = numberOr(r.Min), numberOr(r.Max), numberOr(r.Default)
	if p.Type == canonical.ParamInteger && r.EnumItems != "" {
		// Named points of a scale, kept as the hint they are.
		e := r.EnumItems
		p.Enumeration = &e
	}
	if r.Unit != "" {
		u := r.Unit
		p.Unit = &u
	}
	var notes []string
	if r.MaxLen != "" {
		notes = append(notes, "max_len "+r.MaxLen)
	}
	if observed {
		notes = append(notes, "observed: not in the specification's tables")
	}
	if len(notes) > 0 {
		d := strings.Join(notes, "; ")
		p.Description = &d
	}
	p.Children = canonical.EmptyChildren()
	return p
}

// specKnows says whether the specification prints a property of a block
// of an object type; RRCS and the tables differ in case.
func specKnows(object, block, field string) bool {
	if block == "" && (addressFields[field] || readFields[field]) {
		return true
	}
	if block != "" && portAddressFields[field] {
		// A port address structure (§6.1) wherever it sits: the legs
		// of an IFB, the destination of a call, the ports of a clone.
		return true
	}
	objects := []string{object}
	if object == "portex" {
		// A port is read as a `port` (§8.3) and changed as a `portex`
		// (§8.10): the tables of both describe it.
		objects = append(objects, "port")
	}
	for _, o := range objects {
		for _, f := range SpecFields(o, block) {
			if strings.EqualFold(f, field) {
				return true
			}
		}
	}
	return false
}

// addressFields are what every answer carries beside the properties the
// tables print: the address and the identity of the object (§6: the
// port address structures, the object ID, the type, the key and page
// counts of a panel, the expansions plugged into it).
var addressFields = map[string]bool{
	"Net": true, "Node": true, "Port": true, "IsInput": true, "Input": true, "Output": true,
	"ObjectID": true, "Name": true, "PortType": true, "PortExType": true,
	"KeyCount": true, "PageCount": true, "ConnectedExpansions": true, "Owner": true,
}

// portAddressFields are the members of a port address structure (§6.1).
var portAddressFields = map[string]bool{"IsInput": true, "Net": true, "Node": true, "Port": true}

// readFields are what the command lists answer about a command beside
// its properties (§8.6): its type, its description, the name of what it
// targets, and the target as this connector names it.
var readFields = map[string]bool{
	"CommandType": true, "Description": true, "Target": true, "TargetName": true,
	"GroupName": true, "ConferenceName": true, "IfbName": true,
}

// hidden lists the properties of a block the specification prints and
// the walk did not show, for the blocks the walk showed. The top level
// of a portex is the union over every port type — a panel's fields on
// a 4-wire port are not missing — so it is left out there.
func hidden(object string, blocks map[string]map[string]bool) []string {
	var out []string
	for block, fields := range blocks {
		if block == "" && object == "portex" {
			continue
		}
		for _, f := range SpecFields(object, block) {
			known := false
			for g := range fields {
				if strings.EqualFold(f, g) {
					known = true
					break
				}
			}
			if !known {
				if block == "" {
					out = append(out, f)
				} else {
					out = append(out, block+"."+f)
				}
			}
		}
	}
	sort.Strings(out)
	return out
}

// splitRel splits a relative property path into its block and field.
func splitRel(rel string) (block, field string) {
	if i := strings.LastIndex(rel, "."); i >= 0 {
		return rel[:i], rel[i+1:]
	}
	return "", rel
}

// addInstance places the rows of one instance under a node and keeps
// the blocks and fields seen, for the comparison with the tables.
func addInstance(n *tnode, object string, rows []Row, prefix string, seen map[string]map[string]bool, observed *[]string, noted map[string]bool) {
	// An object the tables do not describe at all (a logic source is
	// listed, not printed) is observed as a whole: its properties are
	// not flagged one by one.
	tabled := len(SpecBlocks(object)) > 0
	for _, r := range rows {
		rel := strings.TrimPrefix(r.Path, prefix)
		block, field := splitRel(rel)
		if seen[block] == nil {
			seen[block] = map[string]bool{}
		}
		seen[block][field] = true
		known := !tabled || specKnows(object, block, field)
		n.add(rel, r, !known)
		if !known && !noted[rel] {
			noted[rel] = true
			*observed = append(*observed, rel)
		}
	}
}

// specRows synthesises the rows of a type from the tables alone, for a
// type the walk did not meet.
func specRows(target, object string) []Row {
	props := map[string]any{}
	for _, block := range SpecBlocks(object) {
		fields := map[string]any{}
		for _, f := range SpecFields(object, block) {
			var v any
			switch SpecFor(object, block, f).Type {
			case "bool":
				v = false
			case "int":
				v = float64(0)
			default:
				v = ""
			}
			if block == "" {
				props[f] = v
			} else {
				fields[f] = v
			}
		}
		if block != "" {
			props[block] = fields
		}
	}
	return RowsOf(target, object, object, 0, props)
}

// objectTemplate is the shape of a conference, a group, an IFB or a
// logic source: the properties met, and the member as a repeated child.
func objectTemplate(target, kind string, objs []*Object) (*tnode, TypeStat) {
	t := newTNode(kind)
	st := TypeStat{Name: kind, Object: kind}
	seen := map[string]map[string]bool{}
	noted := map[string]bool{}
	for _, o := range objs {
		st.Count++
		addInstance(t, kind, RowsOf(target, kind, kind, o.ObjectID, o.Raw), kind+".", seen, &st.Observed, noted)
	}
	if st.Count == 0 {
		for _, r := range specRows(target, kind) {
			t.add(strings.TrimPrefix(r.Path, kind+"."), r, false)
		}
	} else {
		st.Hidden = hidden(kind, seen)
	}
	if kind != "logic" {
		mem := t.child("member")
		mem.desc = "repeated: member.N = the path of a member port, with its talk/listen flags"
		mem.params["port"] = Row{Kind: "string", Access: "RW-"}
	}
	st.Props = t.count()
	return t, st
}

// keyTemplate is the shape of a key: its configuration (§8.8) and the
// command it carries, which is one of the command templates.
func keyTemplate(target string, m *Model) (*tnode, TypeStat) {
	t := newTNode("key")
	st := TypeStat{Name: "key", Object: "panel-key"}
	seen := map[string]map[string]bool{}
	noted := map[string]bool{}
	for _, p := range m.Ports {
		for _, kc := range p.KeyConfigs {
			st.Count++
			addInstance(t, "panel-key", RowsOf(target, "panel-key", "key", kc.ObjectID, kc.Props), "key.", seen, &st.Observed, noted)
		}
	}
	if st.Count == 0 {
		for _, r := range specRows(target, "panel-key") {
			t.add(strings.TrimPrefix(r.Path, "panel-key."), r, false)
		}
	} else {
		st.Hidden = hidden("panel-key", seen)
	}
	cmd := t.child("cmd")
	cmd.desc = "the command of the key: one of the command templates, named by CommandType"
	cmd.params["CommandType"] = Row{Kind: "string", Access: "RW-"}
	st.Props = t.count()
	return t, st
}

type namedTemplate struct {
	node *tnode
	desc string
	stat TypeStat
	card string
}

// commandTemplates is one shape per command type: those of the
// specification, confirmed by the keys met, plus any the walk showed
// that the tables do not print.
func commandTemplates(target string, m *Model) []namedTemplate {
	byType := map[string][]*Key{}
	for _, k := range m.Keys {
		if k.CommandType != "" {
			byType[k.CommandType] = append(byType[k.CommandType], k)
		}
	}
	types := CommandTypes()
	for ct := range byType {
		known := false
		for _, t := range types {
			if t == ct {
				known = true
				break
			}
		}
		if !known {
			types = append(types, ct)
		}
	}
	sort.Strings(types)
	var out []namedTemplate
	for _, ct := range types {
		t := newTNode(ct)
		st := TypeStat{Name: ct, Object: ct}
		seen := map[string]map[string]bool{}
		noted := map[string]bool{}
		for _, k := range byType[ct] {
			st.Count++
			props := map[string]any{}
			for name, v := range k.Raw {
				props[name] = v
			}
			if k.Target != "" {
				props["Target"] = k.Target
			}
			addInstance(t, ct, RowsOf(target, ct, ct, k.ObjectID, props), ct+".", seen, &st.Observed, noted)
		}
		if st.Count == 0 {
			for _, r := range specRows(target, ct) {
				t.add(strings.TrimPrefix(r.Path, ct+"."), r, false)
			}
		} else {
			st.Hidden = hidden(ct, seen)
		}
		st.Props = t.count()
		desc := ""
		if len(SpecFields(ct, "")) == 0 {
			desc = "observed: a command type the specification's tables do not print"
		}
		out = append(out, namedTemplate{node: t, desc: desc, stat: st})
	}
	return out
}

// portType groups the ports of one type while the templates are built.
type portType struct {
	name    string
	exType  int
	strings map[string]bool
	ports   map[[2]int]bool
	halves  map[string]*tnode
	seen    map[string]map[string]map[string]bool
	stat    TypeStat
	noted   map[string]bool
	card    string
	keys    bool
	vfunc   bool
}

// portTemplates is one shape per port type met: a half per direction for
// a split port, the blocks each type carries, keys and virtual functions
// as references to the key and command templates.
func portTemplates(target string, m *Model, exTypes map[int]string, cardOfNode map[int]*Card) []namedTemplate {
	types := map[string]*portType{}
	var order []string
	for _, p := range m.Ports {
		exType, name := -1, slugType(p.Type)
		if _, ok := p.Raw["PortExType"]; ok {
			exType = jInt(p.Raw, "PortExType")
			name = TypeName(exType, p.Type, exTypes)
		}
		t := types[name]
		if t == nil {
			t = &portType{name: name, exType: exType, strings: map[string]bool{}, ports: map[[2]int]bool{},
				halves: map[string]*tnode{}, seen: map[string]map[string]map[string]bool{}, noted: map[string]bool{},
				stat: TypeStat{Name: name, Object: "portex", ExType: exType, Roles: map[string]int{}}}
			types[name] = t
			order = append(order, name)
		}
		if c := cardOfNode[p.Node]; c != nil && t.card == "" {
			t.card = c.Type
		}
		t.strings[p.Type] = true
		t.ports[[2]int{p.Node, p.Port}] = true
		half := ""
		switch {
		case p.Input && !p.Output:
			half = "in"
		case p.Output && !p.Input:
			half = "out"
		}
		if t.halves[half] == nil {
			t.halves[half] = newTNode(half)
			t.seen[half] = map[string]map[string]bool{}
		}
		addInstance(t.halves[half], "portex", RowsOf(target, "portex", p.Path, p.ObjectID, p.Raw), p.Path+".", t.seen[half], &t.stat.Observed, t.noted)
		for _, k := range p.Keys {
			if k.Position == "key" {
				t.keys = true
			} else {
				t.vfunc = true
			}
		}
		if p.KeyCount > 0 {
			t.keys = true
		}
		for _, role := range Roles(p) {
			if kind, _ := role["kind"].(string); kind != "" {
				t.stat.Roles[kind]++
			}
		}
	}
	sort.Strings(order)
	var out []namedTemplate
	for _, name := range order {
		t := types[name]
		root := newTNode(name)
		for half, n := range t.halves {
			if half == "" {
				root.params, root.kids, root.observed = n.params, n.kids, n.observed
				continue
			}
			root.kids[half] = n
		}
		if t.keys {
			k := root.child("key")
			k.desc = "repeated: key.<expansion>.<page>.<number>, one per key of the panel — the key template"
		}
		if t.vfunc {
			v := root.child("vfunc")
			v.desc = "repeated: vfunc.<type>.<number> — a virtual function; cmd is one of the command templates"
		}
		for half, seen := range t.seen {
			for _, h := range hidden("portex", seen) {
				if half != "" {
					h = half + "." + h
				}
				t.stat.Hidden = append(t.stat.Hidden, h)
			}
		}
		sort.Strings(t.stat.Hidden)
		for s := range t.strings {
			t.stat.Strings = append(t.stat.Strings, s)
		}
		sort.Strings(t.stat.Strings)
		t.stat.Count = len(t.ports)
		t.stat.Props = root.count()
		out = append(out, namedTemplate{node: root, stat: t.stat, card: t.card})
	}
	return out
}

// ---- the inventory --------------------------------------------------------

// Roles says what a port does, per direction, from what its blocks
// hold: a stream whose Selection is above 1 is linked to a main port,
// one whose Channels are above 1 is a main port, a port with keys is a
// panel. The main port of a linked one is never reported by RRCS, so it
// is not guessed here.
func Roles(p *Port) map[string]map[string]any {
	roles := map[string]map[string]any{}
	stream := func(dir, block string) {
		b := jMap(p.Raw[block])
		if b == nil {
			return
		}
		switch {
		case jInt(b, "Selection") > 1:
			roles[dir] = map[string]any{"kind": "stream-linked", "channel": jInt(b, "Selection")}
		case jInt(b, "Channels") > 1:
			roles[dir] = map[string]any{"kind": "stream-block", "channels": jInt(b, "Channels")}
		default:
			roles[dir] = map[string]any{"kind": "stream"}
		}
	}
	stream("in", "PortAes67Input")
	stream("out", "PortAes67Output")
	if len(roles) > 0 {
		return roles
	}
	if p.KeyCount > 0 {
		return map[string]map[string]any{"": {"kind": "panel"}}
	}
	return map[string]map[string]any{"": {"kind": "port"}}
}

// Manifest is the inventory of a system: where each instance is and
// which template it is an instance of (ADR-0022, ADR-0028).
func Manifest(m *Model, host string, port int, set *DMSet) *manifest.Manifest {
	name := "rrcs"
	for _, n := range m.Nodes {
		if n.LongName != "" {
			name = n.LongName
			break
		}
	}
	mf := &manifest.Manifest{Device: manifest.Device{
		Name: name, Protocol: Name, IP: host,
		Endpoints: []manifest.Endpoint{{IP: host, Port: port, Transport: "tcp"}},
	}}
	cardOfNode := m.CardOfNode()
	nodeOfCard := map[*Card]int{}
	for node, c := range cardOfNode {
		nodeOfCard[c] = node
	}
	exTypes := map[int]string{}
	for _, st := range set.Types {
		if st.Object == "portex" && st.ExType >= 0 {
			exTypes[st.ExType] = st.Name
		}
	}
	// A port belongs to the frame of its card; without a card to tell,
	// to the first frame.
	frameOfPort := func(p *Port, first int) int {
		if c := cardOfNode[p.Node]; c != nil {
			return c.Node
		}
		return first
	}
	first := -1
	for _, n := range m.Nodes {
		if n.Type != "" {
			first = n.Node
			break
		}
	}
	for _, n := range m.Nodes {
		if n.Type == "" {
			continue
		}
		frame := manifest.Frame{Name: n.Path}
		frame.Slots = append(frame.Slots, manifest.Slot{
			Addr: map[string]any{"bay": 0, "node": n.Node, "oid": n.ObjectID, "type_name": n.Type},
			DM:   set.Node.Identity,
		})
		for _, c := range m.Cards {
			if c.Node != n.Node {
				continue
			}
			// node is the number the card's ports carry; card_node the
			// node the card is listed on.
			portsNode := c.Node
			if pn, ok := nodeOfCard[c]; ok {
				portsNode = pn
			}
			addr := map[string]any{"bay": c.Bay, "node": portsNode, "card_node": c.Node, "oid": c.ObjectID, "type_name": c.Type, "long_name": c.LongName}
			frame.Slots = append(frame.Slots, manifest.Slot{Addr: addr, DM: c.Type + "@" + set.Version})
		}
		// The ports, one entry per address: a split port's halves share it.
		type entry struct {
			addr map[string]any
			dm   string
		}
		entries := map[[2]int]*entry{}
		var keys [][2]int
		for _, p := range m.Ports {
			if frameOfPort(p, first) != n.Node {
				continue
			}
			k := [2]int{p.Node, p.Port}
			e := entries[k]
			if e == nil {
				exType, typeName := -1, slugType(p.Type)
				if _, ok := p.Raw["PortExType"]; ok {
					exType = jInt(p.Raw, "PortExType")
					typeName = TypeName(exType, p.Type, exTypes)
				}
				bay := 0
				if c := cardOfNode[p.Node]; c != nil {
					bay = c.Bay
				}
				addr := map[string]any{"bay": bay, "node": p.Node, "port": p.Port, "template": typeName}
				if exType >= 0 {
					addr["type"] = exType
				}
				dm := set.Node.Identity
				if owner := set.typeOfCard[typeName]; owner != nil {
					dm = owner.Identity
				}
				e = &entry{addr: addr, dm: dm}
				entries[k] = e
				keys = append(keys, k)
			}
			half := ""
			switch {
			case p.Input && !p.Output:
				half = "in"
			case p.Output && !p.Input:
				half = "out"
			}
			if half == "" {
				e.addr["oid"] = p.ObjectID
			} else {
				oids, _ := e.addr["oid"].(map[string]any)
				if oids == nil {
					oids = map[string]any{}
				}
				oids[half] = p.ObjectID
				e.addr["oid"] = oids
			}
			roles, _ := e.addr["role"].(map[string]any)
			if roles == nil {
				roles = map[string]any{}
			}
			for dir, role := range Roles(p) {
				if dir == "" {
					for k, v := range role {
						roles[k] = v
					}
				} else {
					roles[dir] = role
				}
			}
			e.addr["role"] = roles
			if p.KeyCount > 0 {
				e.addr["keys"] = p.KeyCount
				e.addr["pages"] = p.Pages
				if ex := jList(p.Raw["ConnectedExpansions"]); len(ex) > 0 {
					e.addr["expansions"] = len(ex)
				}
			}
		}
		for _, k := range keys {
			frame.Slots = append(frame.Slots, manifest.Slot{Addr: entries[k].addr, DM: entries[k].dm})
		}
		mf.Frames = append(mf.Frames, frame)
	}
	return mf
}

// ExtractLists are the requests extract is made of: the node, the
// cards, the ports with every property, the system objects, and the
// names of the port types. Eight requests; the keys and the commands of
// one port per type come on top.
var ExtractLists = []string{
	"GetAllNodes", "GetAllClientCards", "GetAllPorts",
	"GetAllConferences", "GetAllGroups", "GetAllIFBs", "GetAllLogicSources_v2",
	"GetPortExTypeList",
}
