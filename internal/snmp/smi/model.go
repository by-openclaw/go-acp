// Package smi reads SNMP MIB source — the Structure of Management
// Information, SMIv1 (RFC 1155/1212/1215) and SMIv2 (RFC 2578/2579/2580) —
// and resolves it into a flat, OID-ordered set of named objects.
//
// It is a compiler front end, not a validator: it accepts the subset of
// ASN.1 that MIBs actually use, recovers from what it does not recognise,
// and reports what it could not resolve as findings rather than failing
// the whole set. Vendor MIBs are not careful — the sets in this repo carry
// duplicated enumeration values, modules shipped three times in three
// revisions, and an SMIv1/SMIv2 mix inside one product — and a compiler
// that stopped at the first oddity would compile nothing.
//
// It runs OFFLINE. tools/mibc drives it over the MIB roots and writes a
// committed table that internal/snmp/mib embeds; the shipped binary never
// parses MIB source. See internal/snmp/CLAUDE.md, "MIB parsing happens
// OFFLINE, never in the binary".
//
// Stdlib only, like every codec here (ADR-0006).
package smi

import "fmt"

// Finding is something the compiler noticed and worked around. It is not
// an error: the set still compiles, and the finding says what was
// dropped, overridden or left unresolved.
type Finding struct {
	File string
	Line int
	Kind string
	Msg  string
}

func (f Finding) String() string {
	if f.Line > 0 {
		return fmt.Sprintf("%s:%d: %s: %s", f.File, f.Line, f.Kind, f.Msg)
	}
	return fmt.Sprintf("%s: %s: %s", f.File, f.Kind, f.Msg)
}

// NodeKind is which macro defined a node. It matters for output only: an
// OBJECT-TYPE has a syntax and an access, and everything else is a name
// on a branch of the tree.
type NodeKind int

const (
	KindObjectIdentifier NodeKind = iota // name OBJECT IDENTIFIER ::= { ... }
	KindObjectType                       // OBJECT-TYPE
	KindObjectIdentity                   // OBJECT-IDENTITY
	KindModuleIdentity                   // MODULE-IDENTITY
	KindNotification                     // NOTIFICATION-TYPE
	KindTrap                             // TRAP-TYPE (SMIv1)
	KindGroup                            // OBJECT-GROUP, NOTIFICATION-GROUP
	KindCompliance                       // MODULE-COMPLIANCE
	KindCapabilities                     // AGENT-CAPABILITIES
)

var kindNames = [...]string{
	"object-identifier", "object-type", "object-identity", "module-identity",
	"notification", "trap", "group", "compliance", "capabilities",
}

func (k NodeKind) String() string {
	if k >= 0 && int(k) < len(kindNames) {
		return kindNames[k]
	}
	return fmt.Sprintf("kind(%d)", int(k))
}

// OIDComponent is one element of an OID value: `parent`, `5`, or
// `iso(1)`.
type OIDComponent struct {
	Name   string
	Num    uint32
	HasNum bool
}

// Enum is one named number of an INTEGER or BITS enumeration.
type Enum struct {
	Name  string
	Value int64
}

// Syntax is a SYNTAX clause as written: a base type, and the enumeration
// or constraint that refines it.
type Syntax struct {
	// Base is the type named: a primitive ("INTEGER", "OCTET STRING"),
	// an application type ("Counter32"), or a textual convention
	// ("DisplayString", "RollCallString").
	Base string
	// Of is the entry type of a SEQUENCE OF.
	Of string
	// Enums are the named numbers, if any.
	Enums []Enum
	// Constraint is a size or range refinement, kept as written.
	Constraint string
}

// Type is a type assignment: `Name ::= TEXTUAL-CONVENTION ...` or
// `Name ::= <syntax>`.
type Type struct {
	Name        string
	Syntax      *Syntax
	IsTC        bool
	DisplayHint string
}

// Node is one OID-bearing definition.
type Node struct {
	Name   string
	Kind   NodeKind
	Value  []OIDComponent // the ::= { ... } value
	Line   int
	Syntax *Syntax
	Access string
	Status string
	Units  string
	Index  []string
	// Augments names the entry this one extends.
	Augments string
	// Enterprise and TrapNumber are a TRAP-TYPE's; its OID is derived
	// from them (RFC 3584 §3.1) rather than written.
	Enterprise string
	TrapNumber uint32
}

// Module is one parsed MIB module.
type Module struct {
	Name string
	File string
	// Imports maps each imported symbol to the module it comes from.
	Imports map[string]string
	// LastUpdated is the MODULE-IDENTITY's LAST-UPDATED, as written.
	// Empty for SMIv1 modules, which have none.
	LastUpdated string
	Nodes       []*Node
	Types       map[string]*Type

	byName map[string]*Node
}

func newModule(name, file string) *Module {
	return &Module{
		Name: name, File: file,
		Imports: map[string]string{},
		Types:   map[string]*Type{},
		byName:  map[string]*Node{},
	}
}

// addNode records a definition and reports whether it was new.
//
// A second definition of one name in one module is invalid ASN.1, and in
// practice a copy-paste slip: the Snell registry defines moduleIQDMX30 at
// both 531 and 532, where 532 is IQDMX31. Keeping the second would label
// arc 532 with the wrong product, so the first is kept and the caller
// reports the rest.
func (m *Module) addNode(n *Node) bool {
	if _, dup := m.byName[n.Name]; dup {
		return false
	}
	m.byName[n.Name] = n
	m.Nodes = append(m.Nodes, n)
	return true
}
