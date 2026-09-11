// Package mibgen writes an SMIv2 MIB module (RFC 2578, 2579, 2580) from a
// description of what an agent serves.
//
// It is the inverse of internal/snmp/smi, and deliberately narrower: it
// writes the one shape this repo's own agent needs — identities, scalar
// objects, notifications, and the conformance statements RFC 2580 asks
// every module to carry — and refuses anything else rather than emitting a
// module a manager's compiler would reject. A MIB is published once and
// loaded into other people's NMSes; a mistake in one is not something a
// later release can quietly take back.
//
// Every value is written as { parent n }, one arc under a node the module
// itself defines (or under enterprises, for the root). That is the form
// every MIB compiler accepts without a warning, and requiring it means the
// description names each level of the tree instead of skipping one.
//
// The output depends only on the input: no clock is read, so regenerating
// an unchanged module produces the same bytes and a diff is a real change.
//
// Stdlib only, like every codec here (ADR-0006).
package mibgen

import (
	"fmt"
	"io"
	"sort"
	"strings"
	"time"

	"dhs/internal/snmp/codec"
)

// enterprises is 1.3.6.1.4.1, the one node a module here may hang off
// without defining it.
var enterprises = codec.OID{1, 3, 6, 1, 4, 1}

// Module is one MIB module.
type Module struct {
	// Name is the module name, DHS-MIB.
	Name string
	// Identity names the MODULE-IDENTITY, dhsMIB, and OID places it.
	Identity string
	OID      codec.OID
	// Organization, ContactInfo and Description are the MODULE-IDENTITY
	// clauses of the same names.
	Organization string
	ContactInfo  string
	Description  string
	// Revisions are newest first; the newest is also LAST-UPDATED, so the
	// two cannot disagree.
	Revisions []Revision

	Branches      []Branch
	Objects       []Object
	Notifications []Notification
}

// Revision is one REVISION clause.
type Revision struct {
	Date        time.Time
	Description string
}

// Branch is a node that names a place in the tree. With a Description it
// is an OBJECT-IDENTITY (a product, say, which a sysObjectID points at);
// without one it is a plain OBJECT IDENTIFIER.
type Branch struct {
	Name        string
	OID         codec.OID
	Description string
}

// Access is an object's MAX-ACCESS.
type Access int

const (
	ReadOnly Access = iota
	ReadWrite
)

func (a Access) String() string {
	if a == ReadWrite {
		return "read-write"
	}
	return "read-only"
}

// Object is a scalar OBJECT-TYPE. OID is the object, not its instance:
// sysDescr, not sysDescr.0.
type Object struct {
	Name        string
	OID         codec.OID
	Syntax      codec.ValueType
	Access      Access
	Units       string
	Description string
}

// Notification is a NOTIFICATION-TYPE. Objects names module objects it
// carries, in order.
type Notification struct {
	Name        string
	OID         codec.OID
	Objects     []string
	Description string
}

// Write renders m and writes it, or writes nothing and says why.
func Write(w io.Writer, m Module) error {
	text, err := Render(m)
	if err != nil {
		return err
	}
	_, err = io.WriteString(w, text)
	return err
}

// node is one assignment after the MODULE-IDENTITY.
type node struct {
	name string
	oid  codec.OID
	// macro writes everything between the name and the value.
	macro func(b *strings.Builder)
	// inline puts the value on the name's line, for OBJECT IDENTIFIER.
	inline bool
}

type gen struct {
	m       Module
	nodes   []node
	oids    map[string]string // dotted OID → name
	names   map[string]bool
	objects map[string]bool
	smi     map[string]bool // imported FROM SNMPv2-SMI
	conf    map[string]bool // imported FROM SNMPv2-CONF
}

// Render returns m as module source.
func Render(m Module) (string, error) {
	g := &gen{
		m:       m,
		oids:    map[string]string{},
		names:   map[string]bool{},
		objects: map[string]bool{},
		smi:     map[string]bool{"MODULE-IDENTITY": true},
		conf:    map[string]bool{},
	}
	if err := g.header(); err != nil {
		return "", err
	}
	if err := g.collect(); err != nil {
		return "", err
	}
	return g.render()
}

func (g *gen) header() error {
	m := g.m
	if !validName(m.Name, true) {
		return fmt.Errorf("mibgen: module name %q is not an SMI module name", m.Name)
	}
	for _, f := range []struct{ field, text string }{
		{"ORGANIZATION", m.Organization}, {"CONTACT-INFO", m.ContactInfo}, {"DESCRIPTION", m.Description},
	} {
		if err := checkText(m.Name+" "+f.field, f.text); err != nil {
			return err
		}
	}
	if len(m.Revisions) == 0 {
		return fmt.Errorf("mibgen: %s has no REVISION; LAST-UPDATED is taken from the newest", m.Name)
	}
	for i, r := range m.Revisions {
		if err := checkText(fmt.Sprintf("%s revision %d", m.Name, i), r.Description); err != nil {
			return err
		}
		if i > 0 && !m.Revisions[i-1].Date.After(r.Date) {
			return fmt.Errorf("mibgen: %s revisions must be newest first, each older than the one before", m.Name)
		}
	}
	return g.claim(m.Identity, m.OID)
}

// claim registers a name and an OID, refusing duplicates of either.
func (g *gen) claim(name string, oid codec.OID) error {
	if !validName(name, false) {
		return fmt.Errorf("mibgen: %q is not an SMI identifier (a lower-case letter, then letters, digits and single hyphens, at most 64)", name)
	}
	if g.names[name] {
		return fmt.Errorf("mibgen: %s is defined twice", name)
	}
	if len(oid) <= len(enterprises) || !oid.HasPrefix(enterprises) {
		return fmt.Errorf("mibgen: %s at %s is not under enterprises (%s)", name, oid, enterprises)
	}
	key := oid.String()
	if other, dup := g.oids[key]; dup {
		return fmt.Errorf("mibgen: %s and %s are both at %s", other, name, key)
	}
	g.names[name] = true
	g.oids[key] = name
	return nil
}

func (g *gen) add(n node) error {
	if err := g.claim(n.name, n.oid); err != nil {
		return err
	}
	g.nodes = append(g.nodes, n)
	return nil
}

func (g *gen) collect() error {
	m := g.m
	for _, br := range m.Branches {
		br := br
		n := node{name: br.Name, oid: br.OID, inline: true,
			macro: func(b *strings.Builder) { b.WriteString(" OBJECT IDENTIFIER") }}
		if br.Description != "" {
			if err := checkText(br.Name, br.Description); err != nil {
				return err
			}
			g.smi["OBJECT-IDENTITY"] = true
			n.inline = false
			n.macro = func(b *strings.Builder) {
				b.WriteString(" OBJECT-IDENTITY\n    STATUS      current\n")
				description(b, br.Description)
			}
		}
		if err := g.add(n); err != nil {
			return err
		}
	}

	for _, o := range m.Objects {
		o := o
		syntax, imp, ok := syntaxOf(o.Syntax)
		if !ok {
			return fmt.Errorf("mibgen: %s has syntax %s, which a new SMIv2 module cannot declare", o.Name, o.Syntax)
		}
		if err := checkText(o.Name, o.Description); err != nil {
			return err
		}
		if strings.Contains(o.Units, `"`) {
			return fmt.Errorf("mibgen: %s UNITS contains a double quote, which an SMI string cannot hold", o.Name)
		}
		if imp != "" {
			g.smi[imp] = true
		}
		g.smi["OBJECT-TYPE"] = true
		if err := g.add(node{name: o.Name, oid: o.OID, macro: func(b *strings.Builder) {
			fmt.Fprintf(b, " OBJECT-TYPE\n    SYNTAX      %s\n", syntax)
			if o.Units != "" {
				fmt.Fprintf(b, "    UNITS       %q\n", o.Units)
			}
			fmt.Fprintf(b, "    MAX-ACCESS  %s\n    STATUS      current\n", o.Access)
			description(b, o.Description)
		}}); err != nil {
			return err
		}
		g.objects[o.Name] = true
	}

	for _, nt := range m.Notifications {
		nt := nt
		if err := checkText(nt.Name, nt.Description); err != nil {
			return err
		}
		for _, obj := range nt.Objects {
			if !g.objects[obj] {
				return fmt.Errorf("mibgen: %s carries %s, which is not an object in %s", nt.Name, obj, m.Name)
			}
		}
		g.smi["NOTIFICATION-TYPE"] = true
		if err := g.add(node{name: nt.Name, oid: nt.OID, macro: func(b *strings.Builder) {
			b.WriteString(" NOTIFICATION-TYPE\n")
			if len(nt.Objects) > 0 {
				fmt.Fprintf(b, "    OBJECTS     { %s }\n", strings.Join(nt.Objects, ", "))
			}
			b.WriteString("    STATUS      current\n")
			description(b, nt.Description)
		}}); err != nil {
			return err
		}
	}
	return g.conformance()
}

// conformance adds the RFC 2580 statements: a group for the objects, one
// for the notifications, and a compliance that makes both mandatory. They
// sit at <module>.2 — groups at .2.1, compliances at .2.2 — which is the
// layout most IETF modules use, so an operator finds them where they look.
func (g *gen) conformance() error {
	m := g.m
	if len(m.Objects) == 0 && len(m.Notifications) == 0 {
		return nil
	}
	prefix := strings.TrimSuffix(m.Identity, "MIB")
	conf := m.OID.Append(2)
	groups, compliances := conf.Append(1), conf.Append(2)
	oidNode := func(name string, oid codec.OID) node {
		return node{name: name, oid: oid, inline: true,
			macro: func(b *strings.Builder) { b.WriteString(" OBJECT IDENTIFIER") }}
	}
	for _, n := range []node{
		oidNode(prefix+"Conformance", conf),
		oidNode(prefix+"Groups", groups),
		oidNode(prefix+"Compliances", compliances),
	} {
		if err := g.add(n); err != nil {
			return err
		}
	}

	var mandatory []string
	if len(m.Objects) > 0 {
		names := make([]string, len(m.Objects))
		for i, o := range m.Objects {
			names[i] = o.Name
		}
		name := prefix + "ObjectGroup"
		g.conf["OBJECT-GROUP"] = true
		if err := g.add(node{name: name, oid: groups.Append(1), macro: func(b *strings.Builder) {
			fmt.Fprintf(b, " OBJECT-GROUP\n    OBJECTS     { %s }\n    STATUS      current\n", strings.Join(names, ", "))
			description(b, "Every object "+m.Name+" defines.")
		}}); err != nil {
			return err
		}
		mandatory = append(mandatory, name)
	}
	if len(m.Notifications) > 0 {
		names := make([]string, len(m.Notifications))
		for i, n := range m.Notifications {
			names[i] = n.Name
		}
		name := prefix + "NotificationGroup"
		g.conf["NOTIFICATION-GROUP"] = true
		if err := g.add(node{name: name, oid: groups.Append(2), macro: func(b *strings.Builder) {
			fmt.Fprintf(b, " NOTIFICATION-GROUP\n    NOTIFICATIONS { %s }\n    STATUS      current\n", strings.Join(names, ", "))
			description(b, "Every notification "+m.Name+" defines.")
		}}); err != nil {
			return err
		}
		mandatory = append(mandatory, name)
	}
	g.conf["MODULE-COMPLIANCE"] = true
	return g.add(node{name: prefix + "Compliance", oid: compliances.Append(1), macro: func(b *strings.Builder) {
		b.WriteString(" MODULE-COMPLIANCE\n    STATUS      current\n")
		description(b, "An agent implementing "+m.Name+" implements all of it.")
		fmt.Fprintf(b, "    MODULE      -- this module\n        MANDATORY-GROUPS { %s }\n", strings.Join(mandatory, ", "))
	}})
}

// value writes oid as { parent n }. The parent must be a node of this
// module, or enterprises.
func (g *gen) value(name string, oid codec.OID) (string, error) {
	parent := oid[:len(oid)-1]
	arc := oid[len(oid)-1]
	if parent.Compare(enterprises) == 0 {
		g.smi["enterprises"] = true
		return fmt.Sprintf("{ enterprises %d }", arc), nil
	}
	p, ok := g.oids[parent.String()]
	if !ok {
		return "", fmt.Errorf("mibgen: %s is at %s, and nothing in %s is at %s to hang it from; add a Branch there",
			name, oid, g.m.Name, parent)
	}
	return fmt.Sprintf("{ %s %d }", p, arc), nil
}

func (g *gen) render() (string, error) {
	m := g.m
	sort.Slice(g.nodes, func(i, j int) bool { return g.nodes[i].oid.Compare(g.nodes[j].oid) < 0 })

	// Values first: they decide whether enterprises is imported.
	identityValue, err := g.value(m.Identity, m.OID)
	if err != nil {
		return "", err
	}
	values := make([]string, len(g.nodes))
	for i, n := range g.nodes {
		if values[i], err = g.value(n.name, n.oid); err != nil {
			return "", err
		}
	}

	var b strings.Builder
	fmt.Fprintf(&b, "%s DEFINITIONS ::= BEGIN\n\nIMPORTS\n", m.Name)
	fmt.Fprintf(&b, "    %s\n        FROM SNMPv2-SMI", strings.Join(ordered(g.smi), ", "))
	if len(g.conf) > 0 {
		fmt.Fprintf(&b, "\n    %s\n        FROM SNMPv2-CONF", strings.Join(ordered(g.conf), ", "))
	}
	b.WriteString(";\n\n")

	latest := date(m.Revisions[0].Date)
	fmt.Fprintf(&b, "%s MODULE-IDENTITY\n    LAST-UPDATED %q\n", m.Identity, latest)
	fmt.Fprintf(&b, "    ORGANIZATION\n        %s\n", quoted(m.Organization))
	fmt.Fprintf(&b, "    CONTACT-INFO\n        %s\n", quoted(m.ContactInfo))
	description(&b, m.Description)
	for _, r := range m.Revisions {
		fmt.Fprintf(&b, "    REVISION    %q\n", date(r.Date))
		description(&b, r.Description)
	}
	fmt.Fprintf(&b, "    ::= %s\n", identityValue)

	for i, n := range g.nodes {
		b.WriteString("\n")
		b.WriteString(n.name)
		n.macro(&b)
		if n.inline {
			fmt.Fprintf(&b, " ::= %s\n", values[i])
		} else {
			fmt.Fprintf(&b, "    ::= %s\n", values[i])
		}
	}
	b.WriteString("\nEND\n")
	return b.String(), nil
}

// ordered lists the imports in a fixed order: the macros, then the rest,
// each alphabetically — so the same module always imports the same way.
func ordered(set map[string]bool) []string {
	var macros, rest []string
	for k := range set {
		if strings.ToUpper(k) == k {
			macros = append(macros, k)
		} else {
			rest = append(rest, k)
		}
	}
	sort.Strings(macros)
	sort.Strings(rest)
	return append(macros, rest...)
}

func description(b *strings.Builder, text string) {
	fmt.Fprintf(b, "    DESCRIPTION\n        %s\n", quoted(text))
}

// quoted writes text as an SMI string, continuation lines indented to
// match. checkText has already refused a double quote, which SMIv2 gives
// no way to escape.
func quoted(text string) string {
	return `"` + strings.ReplaceAll(text, "\n", "\n         ") + `"`
}

func checkText(what, text string) error {
	if strings.TrimSpace(text) == "" {
		return fmt.Errorf("mibgen: %s has no text; SMIv2 requires it", what)
	}
	if strings.Contains(text, `"`) {
		return fmt.Errorf("mibgen: %s contains a double quote, which an SMI string cannot hold", what)
	}
	return nil
}

// date is RFC 2578's ExtUTCTime with a four-digit year, which §2 requires
// for any date from 2000 on.
func date(t time.Time) string {
	return t.UTC().Format("200601021504") + "Z"
}

// validName is RFC 2578 §3.1 and §3.3: a module name starts upper-case, an
// identifier lower-case; then letters, digits and hyphens, no two hyphens
// together and none at the end; at most 64 characters.
func validName(s string, module bool) bool {
	if s == "" || len(s) > 64 || strings.Contains(s, "--") || strings.HasSuffix(s, "-") {
		return false
	}
	first := s[0]
	if module && (first < 'A' || first > 'Z') || !module && (first < 'a' || first > 'z') {
		return false
	}
	for _, c := range s {
		if !identChar(c) {
			return false
		}
	}
	return true
}

// identChar is a character an SMI name may hold: an ASCII letter, a digit
// or a hyphen.
func identChar(c rune) bool {
	switch {
	case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9', c == '-':
		return true
	}
	return false
}

// syntaxOf maps a wire type to the SMIv2 syntax that declares it, and the
// SNMPv2-SMI name that syntax needs imported. Opaque is refused: RFC 2578
// §7.1.9 keeps it only for SMIv1 compatibility, and a new module that used
// it would be defining an object no manager can interpret.
func syntaxOf(t codec.ValueType) (syntax, imp string, ok bool) {
	switch t {
	case codec.TypeInteger:
		return "Integer32", "Integer32", true
	case codec.TypeOctetString:
		return "OCTET STRING", "", true
	case codec.TypeOID:
		return "OBJECT IDENTIFIER", "", true
	case codec.TypeIPAddress:
		return "IpAddress", "IpAddress", true
	case codec.TypeCounter32:
		return "Counter32", "Counter32", true
	case codec.TypeGauge32:
		return "Gauge32", "Gauge32", true
	case codec.TypeTimeTicks:
		return "TimeTicks", "TimeTicks", true
	case codec.TypeCounter64:
		return "Counter64", "Counter64", true
	}
	return "", "", false
}
