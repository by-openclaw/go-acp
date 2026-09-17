// Package provider is the SNMP agent: the half of the connector that IS
// a device, answering somebody else's manager.
//
// It is split the way the protocol is. [MIB] is the tree of objects this
// agent serves, ordered the way GETNEXT walks. [Agent] turns one request
// message into one response message and touches no socket, so every rule
// in RFC 1157 §4 and RFC 3416 §4 is exercised without a port. [Server]
// binds the UDP socket and hands datagrams to the Agent.
//
// The peers this is built against are in docs/testbed.md: Cerebrum's
// manager and trap receiver poll and listen, and the Tandberg IRDs are
// the vendor oracle on the consumer side.
package provider

import (
	"fmt"
	"sort"
	"sync"

	"dhs/internal/snmp/codec"
	"dhs/internal/snmp/mibgen"
)

// Access is what a manager may do with an object.
//
// It is the SMI's MAX-ACCESS narrowed to what an agent has to enforce: an
// object is either not visible, readable, or writable. read-create and
// accessible-for-notify are row-creation and trap-only refinements that
// only mean something once this agent serves a conceptual table, and
// inventing them now would be three values nothing consults.
type Access int

const (
	// NotAccessible names a structural node — a table or a row — that a
	// walk passes over. It has no value.
	NotAccessible Access = iota
	// ReadOnly answers GET and GETNEXT and refuses SET.
	ReadOnly
	// ReadWrite answers everything.
	ReadWrite
)

func (a Access) String() string {
	switch a {
	case NotAccessible:
		return "not-accessible"
	case ReadOnly:
		return "read-only"
	case ReadWrite:
		return "read-write"
	default:
		return fmt.Sprintf("access(%d)", int(a))
	}
}

// Object is one INSTANCE in the served tree — an OID that names a value,
// not a MIB definition. sysDescr.0 is an Object; sysDescr is not.
//
// The distinction matters because it is the one an agent gets wrong:
// answering a GET of sysDescr (no instance sub-identifier) with the
// value of sysDescr.0 is exactly the bug RFC 3416's noSuchInstance
// exists to report, and a manager that gets a value there caches an OID
// that will never appear in a walk.
type Object struct {
	// OID is the full instance name, index included.
	OID codec.OID
	// Type is the syntax Get promises to return, and the syntax a SET
	// must carry. Checked on registration and on every write.
	Type codec.ValueType
	// Access says what a manager may do here.
	Access Access

	// Get reads the current value. It is a function rather than a stored
	// value so an agent answers with what is true NOW — a counter read
	// at poll time, not at registration time. Called on the read path,
	// so it must not block on anything slower than the value it reports.
	Get func() codec.Value
	// Set applies a write. Required when Access is ReadWrite, ignored
	// otherwise. An error here becomes the response's error-status.
	Set func(codec.Value) error

	// Name and Description are the object's MIB definition: what
	// `dhs producer snmp mib` writes for it. The agent answers without
	// them, but an object served under our own enterprise (mib.DHS) needs
	// both, or the MIB that documents it cannot be generated.
	Name        string
	Description string
}

// MIB is the ordered set of objects an agent serves.
//
// A sorted slice, not a map or a trie: GETNEXT's answer is "the first
// object after this one in lexicographic OID order", which IS the sort
// order, so a walk is a binary search and a step. A map would need the
// whole key set sorted per request, and a trie would buy nothing at the
// sizes an agent serves — the Tandberg product branch, the largest tree
// in the testbed, is 842 objects.
type MIB struct {
	mu      sync.RWMutex
	objects []Object
}

// NewMIB returns an empty tree.
func NewMIB() *MIB { return &MIB{} }

// Register adds objects, keeping the tree sorted.
//
// It refuses rather than repairs: an object with no OID, no getter, or a
// writable object with no setter is a wiring mistake in our own code,
// and an agent that started anyway would answer a manager with a nil
// dereference or a silent read-only object.
func (m *MIB) Register(objs ...Object) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	for _, o := range objs {
		switch {
		case len(o.OID) == 0:
			return fmt.Errorf("snmp: object with no OID")
		case o.Access != NotAccessible && o.Get == nil:
			return fmt.Errorf("snmp: %s is %s but has no Get", o.OID, o.Access)
		case o.Access == ReadWrite && o.Set == nil:
			return fmt.Errorf("snmp: %s is read-write but has no Set", o.OID)
		}
		if i := m.indexOf(o.OID); i >= 0 {
			return fmt.Errorf("snmp: %s is already registered", o.OID)
		}
		m.objects = append(m.objects, o)
	}
	sort.Slice(m.objects, func(i, j int) bool {
		return m.objects[i].OID.Compare(m.objects[j].OID) < 0
	})
	return nil
}

// MustRegister is Register for a tree this repo builds itself. It panics,
// which is what a wiring mistake in our own source deserves.
func (m *MIB) MustRegister(objs ...Object) {
	if err := m.Register(objs...); err != nil {
		panic(err)
	}
}

// Len reports how many objects are served, which is what an operator
// wants in the line the agent logs at startup.
func (m *MIB) Len() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.objects)
}

// Get returns the object with exactly this OID.
func (m *MIB) Get(oid codec.OID) (Object, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	i := m.indexOf(oid)
	if i < 0 {
		return Object{}, false
	}
	return m.objects[i], true
}

// Next returns the first ACCESSIBLE object after oid, which is what
// GETNEXT answers with. A not-accessible node is stepped over rather
// than returned: a walk that stopped on a table's structural OID would
// hand the manager a name with no value and then be asked for the next
// one from there anyway.
//
// The bool is false at the end of the tree — endOfMibView.
func (m *MIB) Next(oid codec.OID) (Object, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	// The first index whose OID is strictly greater than oid.
	i := sort.Search(len(m.objects), func(i int) bool {
		return m.objects[i].OID.Compare(oid) > 0
	})
	for ; i < len(m.objects); i++ {
		if m.objects[i].Access != NotAccessible {
			return m.objects[i], true
		}
	}
	return Object{}, false
}

// First returns the first accessible object in the tree, which is where
// a walk of the whole agent starts.
func (m *MIB) First() (Object, bool) { return m.Next(nil) }

// Definitions returns the MIB definitions of every accessible object
// served under root, which is what a generated module is written from.
//
// It refuses rather than guesses: an object with no Name cannot be
// defined, and one whose OID does not end in .0 is a table cell, which the
// generator does not write yet. Either would produce a module that did not
// describe what the agent serves, and a manager would believe the module.
func (m *MIB) Definitions(root codec.OID) ([]mibgen.Object, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	var out []mibgen.Object
	for _, o := range m.objects {
		if !o.OID.HasPrefix(root) || o.Access == NotAccessible {
			continue
		}
		n := len(o.OID)
		switch {
		case o.Name == "":
			return nil, fmt.Errorf("snmp: %s is served under %s with no Name, so no MIB can define it", o.OID, root)
		case o.OID[n-1] != 0:
			return nil, fmt.Errorf("snmp: %s (%s) is not a scalar instance ending in .0; tables are not generated yet", o.Name, o.OID)
		}
		access := mibgen.ReadOnly
		if o.Access == ReadWrite {
			access = mibgen.ReadWrite
		}
		out = append(out, mibgen.Object{
			Name: o.Name, OID: append(codec.OID(nil), o.OID[:n-1]...),
			Syntax: o.Type, Access: access, Description: o.Description,
		})
	}
	return out, nil
}

// indexOf finds an exact OID. Caller holds the lock.
func (m *MIB) indexOf(oid codec.OID) int {
	i := sort.Search(len(m.objects), func(i int) bool {
		return m.objects[i].OID.Compare(oid) >= 0
	})
	if i < len(m.objects) && m.objects[i].OID.Compare(oid) == 0 {
		return i
	}
	return -1
}

// Scalar builds a read-only object whose value is fixed for the life of
// the agent — a product name, an object identifier, a serial number.
func Scalar(oid codec.OID, v codec.Value) Object {
	return Object{
		OID:    oid,
		Type:   v.Type,
		Access: ReadOnly,
		Get:    func() codec.Value { return v },
	}
}

// Live builds a read-only object whose value is read at poll time. This
// is the shape almost every real object has: a counter, a temperature, a
// lock state.
func Live(oid codec.OID, typ codec.ValueType, get func() codec.Value) Object {
	return Object{OID: oid, Type: typ, Access: ReadOnly, Get: get}
}

// Writable builds a read-write object. Set is given the value AFTER the
// agent has checked its syntax against typ, so an implementation never
// has to re-check the type it declared.
func Writable(oid codec.OID, typ codec.ValueType,
	get func() codec.Value, set func(codec.Value) error) Object {
	return Object{OID: oid, Type: typ, Access: ReadWrite, Get: get, Set: set}
}
