package provider

import (
	"sync"
	"time"

	"dhs/internal/clock"
	"dhs/internal/snmp/codec"
	"dhs/internal/snmp/mib"
)

// SystemInfo is what a manager learns about this device from the RFC
// 1213 system group — the first thing anything reads, and how an NMS
// decides what the device IS.
type SystemInfo struct {
	// Descr is sysDescr.0: a human-readable description. Convention is
	// product, version and platform on one line.
	Descr string
	// ObjectID is sysObjectID.0, the vendor's authoritative identity for
	// this product. An NMS classifies on it, so a device that answers
	// the wrong one is a device that is inventoried as something else.
	ObjectID codec.OID
	// Contact, Name and Location are the three an operator sets. They
	// are read-WRITE per RFC 1213, and this agent honours that.
	Contact  string
	Name     string
	Location string
	// Services is the RFC 1213 layer bitmask. Zero means the default
	// for a protocol gateway.
	Services int64
}

// SystemGroup builds the RFC 1213 §3.7 system group as served objects.
//
// Every object in it is mandatory. A manager discovers a device by
// reading this group, and one that omits sysObjectID.0 is a device an
// NMS cannot classify however much else it serves — which is why this is
// a constructor rather than something each caller assembles.
//
// sysUpTime.0 is TimeTicks since the agent started, in CENTISECONDS.
// Hundredths, not seconds: an agent reporting seconds looks a hundred
// times younger than it is, and every trap it sends is timestamped
// wrong, which is what a receiver correlates on.
func SystemGroup(info SystemInfo, clk clock.Clock) []Object {
	if clk == nil {
		clk = clock.System()
	}
	started := clk.Now()

	// The three writable strings live behind one lock: a SET arrives on
	// the read loop's goroutine and a GET may be answered from another.
	var mu sync.RWMutex
	contact, name, location := info.Contact, info.Name, info.Location

	str := func(get func() string, set func(string)) (func() codec.Value, func(codec.Value) error) {
		return func() codec.Value {
				mu.RLock()
				defer mu.RUnlock()
				return codec.String(get())
			}, func(v codec.Value) error {
				mu.Lock()
				defer mu.Unlock()
				set(string(v.Bytes))
				return nil
			}
	}

	contactGet, contactSet := str(func() string { return contact }, func(s string) { contact = s })
	nameGet, nameSet := str(func() string { return name }, func(s string) { name = s })
	locGet, locSet := str(func() string { return location }, func(s string) { location = s })

	services := info.Services
	if services == 0 {
		services = mib.SysServicesApplication
	}
	objectID := info.ObjectID
	if len(objectID) == 0 {
		objectID = mib.DHS
	}

	return []Object{
		Scalar(mib.SysDescr, codec.String(info.Descr)),
		Scalar(mib.SysObjectID, codec.ObjectID(objectID)),
		Live(mib.SysUpTime, codec.TypeTimeTicks, func() codec.Value {
			return codec.TimeTicks(uint32(clk.Now().Sub(started) / (10 * time.Millisecond)))
		}),
		Writable(mib.SysContact, codec.TypeOctetString, contactGet, contactSet),
		Writable(mib.SysName, codec.TypeOctetString, nameGet, nameSet),
		Writable(mib.SysLocation, codec.TypeOctetString, locGet, locSet),
		Scalar(mib.SysServices, codec.Int(services)),
	}
}

// Uptime reports sysUpTime for a notification's timestamp, reading it
// from the agent's own tree.
//
// A trap carries sysUpTime.0 as its first binding, and it must be the
// SAME clock the agent answers polls from: a receiver correlating a trap
// against a poll it took a second earlier compares the two, and two
// clocks that disagree make an event look like it happened before the
// state it reports.
func Uptime(m *MIB) uint32 {
	obj, ok := m.Get(mib.SysUpTime)
	if !ok {
		return 0
	}
	return uint32(obj.Get().Uint)
}
