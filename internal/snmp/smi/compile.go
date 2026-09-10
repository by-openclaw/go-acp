package smi

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
)

// Object is one resolved, named node of the tree.
type Object struct {
	OID    []uint32
	Name   string
	Module string
	Kind   NodeKind
	// Base is the primitive the value is carried as: INTEGER, OCTET
	// STRING, Counter32, ... Empty for anything that is not an
	// OBJECT-TYPE, and for tables and rows.
	Base string
	// TC is the textual convention or named type the object was declared
	// with, when it was not a primitive: DisplayString, RollCallString.
	TC     string
	Access string
	Enums  []Enum
	Index  []string
}

// Dotted renders the OID.
func (o Object) Dotted() string {
	parts := make([]string, len(o.OID))
	for i, a := range o.OID {
		parts[i] = strconv.FormatUint(uint64(a), 10)
	}
	return strings.Join(parts, ".")
}

// Compiled is a resolved set.
type Compiled struct {
	// Objects is every node that resolved, in OID order.
	Objects  []Object
	Findings []Finding
}

// roots are the three arcs ASN.1 itself names.
var roots = map[string]uint32{"ccitt": 0, "iso": 1, "joint-iso-ccitt": 2}

// application maps every SMI application type — and its SMIv1 spelling —
// to the name the runtime carries it as. They are recognised by name
// rather than resolved, because their SMI definitions are `[APPLICATION n]
// IMPLICIT INTEGER` and resolving that gives INTEGER, which is exactly the
// distinction the wire makes.
var application = map[string]string{
	"Integer32": "Integer32", "Unsigned32": "Gauge32",
	"Counter": "Counter32", "Counter32": "Counter32", "Counter64": "Counter64",
	"Gauge": "Gauge32", "Gauge32": "Gauge32",
	"TimeTicks": "TimeTicks", "Opaque": "Opaque",
	"IpAddress": "IpAddress", "NetworkAddress": "IpAddress",
}

var primitives = map[string]bool{
	"INTEGER": true, "OCTET STRING": true, "OBJECT IDENTIFIER": true,
	"BITS": true, "BIT STRING": true, "SEQUENCE": true, "SEQUENCE OF": true,
	"CHOICE": true, "NULL": true,
}

type compiler struct {
	mods     map[string]*Module
	oids     map[*Node][]uint32
	visiting map[*Node]bool
	findings []Finding
}

// Compile resolves every node of every module to a full OID and every
// OBJECT-TYPE's syntax to a primitive. Pass the output of Select: two
// modules with one name make every lookup into them ambiguous.
func Compile(mods []*Module) *Compiled {
	c := &compiler{
		mods:     map[string]*Module{},
		oids:     map[*Node][]uint32{},
		visiting: map[*Node]bool{},
	}
	for _, m := range mods {
		c.mods[m.Name] = m
	}

	var out []Object
	for _, m := range mods {
		for _, n := range m.Nodes {
			oid, ok := c.resolve(m, n)
			if !ok {
				continue
			}
			o := Object{OID: oid, Name: n.Name, Module: m.Name, Kind: n.Kind,
				Access: n.Access, Index: n.Index}
			if n.Kind == KindObjectType && n.Syntax != nil {
				o.Base, o.TC, o.Enums = c.syntax(m, n.Syntax, 0)
			}
			out = append(out, o)
		}
	}

	sort.SliceStable(out, func(i, j int) bool { return compareOID(out[i].OID, out[j].OID) < 0 })
	out = c.dedupeOIDs(out)
	return &Compiled{Objects: out, Findings: c.findings}
}

func (c *compiler) note(m *Module, n *Node, kind, format string, args ...any) {
	c.findings = append(c.findings, Finding{m.File, n.Line, kind, fmt.Sprintf(format, args...)})
}

// lookup finds a symbol as seen from module m: its own definition first,
// then the module it is imported from — following re-imports a few
// levels, because some vendor MIBs import a name from a module that only
// imported it itself.
func (c *compiler) lookup(m *Module, sym string) (*Module, *Node) {
	for depth := 0; m != nil && depth < 8; depth++ {
		if n, ok := m.byName[sym]; ok {
			return m, n
		}
		from, ok := m.Imports[sym]
		if !ok {
			return nil, nil
		}
		m = c.mods[from]
	}
	return nil, nil
}

func (c *compiler) resolve(m *Module, n *Node) ([]uint32, bool) {
	if oid, ok := c.oids[n]; ok {
		return oid, oid != nil
	}
	if c.visiting[n] {
		c.note(m, n, "unresolved", "%s is defined in terms of itself", n.Name)
		c.oids[n] = nil
		return nil, false
	}
	c.visiting[n] = true
	defer delete(c.visiting, n)

	var oid []uint32
	var ok bool
	if n.Kind == KindTrap {
		oid, ok = c.resolveTrap(m, n)
	} else {
		oid, ok = c.resolveValue(m, n)
	}
	if !ok {
		c.oids[n] = nil
		return nil, false
	}
	c.oids[n] = oid
	return oid, true
}

func (c *compiler) resolveValue(m *Module, n *Node) ([]uint32, bool) {
	if len(n.Value) == 0 {
		c.note(m, n, "unresolved", "%s has an empty OID value", n.Name)
		return nil, false
	}
	var oid []uint32
	first := n.Value[0]
	switch {
	case first.HasNum:
		oid = []uint32{first.Num}
	default:
		if arc, ok := roots[first.Name]; ok {
			oid = []uint32{arc}
			break
		}
		pm, parent := c.lookup(m, first.Name)
		if parent == nil {
			c.note(m, n, "unresolved", "%s hangs off %s, which is neither defined nor imported",
				n.Name, first.Name)
			return nil, false
		}
		base, ok := c.resolve(pm, parent)
		if !ok {
			c.note(m, n, "unresolved", "%s hangs off %s, which did not resolve", n.Name, first.Name)
			return nil, false
		}
		oid = append([]uint32(nil), base...)
	}
	for _, comp := range n.Value[1:] {
		if !comp.HasNum {
			c.note(m, n, "unresolved", "%s: arc %q has no number", n.Name, comp.Name)
			return nil, false
		}
		oid = append(oid, comp.Num)
	}
	return oid, true
}

// resolveTrap derives a TRAP-TYPE's OID the way RFC 3584 §3.1 maps a v1
// trap to a v2 notification: enterprise, then 0, then the trap number.
func (c *compiler) resolveTrap(m *Module, n *Node) ([]uint32, bool) {
	em, ent := c.lookup(m, n.Enterprise)
	if ent == nil {
		c.note(m, n, "unresolved", "trap %s names enterprise %q, which is neither defined nor imported",
			n.Name, n.Enterprise)
		return nil, false
	}
	base, ok := c.resolve(em, ent)
	if !ok {
		return nil, false
	}
	return append(append(append([]uint32(nil), base...), 0), n.TrapNumber), true
}

// syntax resolves a SYNTAX to the primitive it is carried as, following
// textual conventions and type assignments. The first named type met is
// kept as TC; enumerations are the object's own, or inherited from the
// type when it declares none.
func (c *compiler) syntax(m *Module, s *Syntax, depth int) (base, tc string, enums []Enum) {
	enums = s.Enums
	switch {
	case primitives[s.Base]:
		return s.Base, "", enums
	case application[s.Base] != "":
		return application[s.Base], "", enums
	}
	if depth > 16 {
		return "", s.Base, enums
	}
	tm, ty := c.lookupType(m, s.Base)
	if ty == nil || ty.Syntax == nil {
		// A type this set does not define. The name is still worth
		// keeping: it is usually a TC the reader recognises.
		return "", s.Base, enums
	}
	b, _, inherited := c.syntax(tm, ty.Syntax, depth+1)
	if len(enums) == 0 {
		enums = inherited
	}
	return b, s.Base, enums
}

func (c *compiler) lookupType(m *Module, name string) (*Module, *Type) {
	for depth := 0; m != nil && depth < 8; depth++ {
		if t, ok := m.Types[name]; ok {
			return m, t
		}
		from, ok := m.Imports[name]
		if !ok {
			return nil, nil
		}
		m = c.mods[from]
	}
	return nil, nil
}

// dedupeOIDs collapses the same name at one OID to one row, and keeps
// DIFFERENT names at one OID as alternatives.
//
// The same name from two modules is a product family sharing a branch —
// the TT1260 and RX1290 MIBs both define 1.3.6.1.4.1.1773.1.3.200 — and
// is collapsed silently. A different name is not a duplicate at all: the
// two receivers report the SAME sysObjectID and put different objects at
// the same OIDs (…200.2.6.4 is userAudio3 on the RX1290 and userLsdPid on
// the TT1260), so which name is right depends on the device being asked.
// Both rows are kept, the first by module name as the default, and the
// reader chooses per device.
func (c *compiler) dedupeOIDs(in []Object) []Object {
	out := make([]Object, 0, len(in))
	for i := 0; i < len(in); {
		j := i + 1
		for j < len(in) && compareOID(in[j].OID, in[i].OID) == 0 {
			j++
		}
		group := in[i:j]
		sort.SliceStable(group, func(a, b int) bool {
			if group[a].Module != group[b].Module {
				return group[a].Module < group[b].Module
			}
			return group[a].Name < group[b].Name
		})
		keep := group[0]
		out = append(out, keep)
		seen := map[string]bool{keep.Name: true}
		for _, o := range group[1:] {
			if seen[o.Name] {
				continue
			}
			seen[o.Name] = true
			c.findings = append(c.findings, Finding{o.Module, 0, "conflicting-name",
				fmt.Sprintf("%s is %s in %s and %s in %s; both kept, %s is the default",
					keep.Dotted(), keep.Name, keep.Module, o.Name, o.Module, keep.Name)})
			out = append(out, o)
		}
		i = j
	}
	return out
}

func compareOID(a, b []uint32) int {
	for i := 0; i < len(a) && i < len(b); i++ {
		switch {
		case a[i] < b[i]:
			return -1
		case a[i] > b[i]:
			return 1
		}
	}
	switch {
	case len(a) < len(b):
		return -1
	case len(a) > len(b):
		return 1
	}
	return 0
}
