package router

import "fmt"

// SourcePin names a source anywhere in a router: which matrix, which level
// within it, and which source on that level.
//
// It is packed into one 32-bit value because it is what a destination's routed
// source reports and what a crosspoint set carries, and both travel as a single
// Data Transfer Params integer:
//
//	bits 31..24  matrix number
//	bits 23..16  level number
//	bits 15..0   source number
//
// Setting a source whose matrix and level match the destination's makes a local
// route. If either differs, the controller looks for tielines to complete it,
// which is why the packing carries the matrix at all.
type SourcePin struct {
	Matrix uint8
	Level  uint8
	Source uint16
}

// Limits of a packed source pin.
const (
	MaxPinMatrix = 0xFF
	MaxPinLevel  = 0xFF
	MaxPinSource = 0xFFFF
)

// PackSourcePin packs a pin into its wire form.
func PackSourcePin(p SourcePin) uint32 {
	return uint32(p.Matrix)<<24 | uint32(p.Level)<<16 | uint32(p.Source)
}

// UnpackSourcePin reads a packed pin.
func UnpackSourcePin(v uint32) SourcePin {
	return SourcePin{
		Matrix: uint8(v >> 24),
		Level:  uint8(v >> 16),
		Source: uint16(v),
	}
}

// Pack returns the wire form of p.
func (p SourcePin) Pack() uint32 { return PackSourcePin(p) }

// IsUnrouted reports whether the pin names no source. Source numbers are
// one-based throughout the command set, so zero means nothing is routed.
func (p SourcePin) IsUnrouted() bool { return p.Source == 0 }

// SameLevel reports whether two pins sit on the same matrix and level, which is
// what decides whether a route is local or needs a tieline.
func (p SourcePin) SameLevel(q SourcePin) bool {
	return p.Matrix == q.Matrix && p.Level == q.Level
}

func (p SourcePin) String() string {
	if p.IsUnrouted() {
		return "unrouted"
	}
	return fmt.Sprintf("m%d/l%d/s%d", p.Matrix, p.Level, p.Source)
}

// ProtectState is the protect word of a destination.
//
// On a write:
//
//	bit 0       1 to protect, 0 to release
//	bits 8..23  the protecting device's id, non-zero
//	bit 24      1 if the panel is a master
//
// On a read the same layout comes back with the master bit always clear, and
// the string half of the command carries the name of the device holding the
// protect. An id of zero on a read means the destination is not protected.
//
// A normal panel may only release a protect it set, matching on the id. A
// master panel may release any.
type ProtectState struct {
	Protected bool
	DeviceID  uint16
	Master    bool
}

// Protect word bit positions.
const (
	protectBit     = 0
	protectIDShift = 8
	protectIDMask  = 0xFFFF
	masterBit      = 24

	// MaxProtectID is the largest configured panel id. The field is 16 bits
	// wide, but the specification says a panel id is configured in the range
	// 1..1023, so a larger value means the word was built wrongly.
	MaxProtectID = 1023
)

// PackProtectState returns the numeric half of a protect command.
func PackProtectState(p ProtectState) uint32 {
	var v uint32
	if p.Protected {
		v |= 1 << protectBit
	}
	v |= uint32(p.DeviceID) << protectIDShift
	if p.Master {
		v |= 1 << masterBit
	}
	return v
}

// UnpackProtectState reads the numeric half of a protect command.
func UnpackProtectState(v uint32) ProtectState {
	return ProtectState{
		Protected: v&(1<<protectBit) != 0,
		DeviceID:  uint16((v >> protectIDShift) & protectIDMask),
		Master:    v&(1<<masterBit) != 0,
	}
}

// Pack returns the wire form of p.
func (p ProtectState) Pack() uint32 { return PackProtectState(p) }

// CanRelease reports whether a panel holding this state may release the
// protect held in other. A master releases anything; anyone else must match the
// id that set it.
func (p ProtectState) CanRelease(other ProtectState) bool {
	if !other.Protected {
		return true
	}
	return p.Master || p.DeviceID == other.DeviceID
}

func (p ProtectState) String() string {
	if !p.Protected {
		return "unprotected"
	}
	s := fmt.Sprintf("protected by %d", p.DeviceID)
	if p.Master {
		s += " (master)"
	}
	return s
}

// RouteResult is the outcome of a crosspoint set, reported as the second
// parameter of a routed-source read and always present on a set
// ("Full Control Command Set" §5.2.1, revision 12).
type RouteResult uint32

const (
	RouteOK            RouteResult = 0
	RouteIdle          RouteResult = 1 // the controller is idle
	RouteNotInstalled  RouteResult = 2
	RouteInhibited     RouteResult = 3
	RouteProtected     RouteResult = 4
	RouteInUse         RouteResult = 5 // RS422 level routing
	RouteNoTieline     RouteResult = 6
	RouteConfiguration RouteResult = 7
	RouteBadParameters RouteResult = 8
	RouteNotConnected  RouteResult = 9
)

// OK reports whether the route was made.
func (r RouteResult) OK() bool { return r == RouteOK }

// Retryable reports whether the same request might succeed later without any
// change to the request itself. A busy controller and a lost connection are
// worth retrying; a protected destination or a bad parameter never is.
func (r RouteResult) Retryable() bool {
	return r == RouteIdle || r == RouteNotConnected
}

func (r RouteResult) String() string {
	switch r {
	case RouteOK:
		return "ok"
	case RouteIdle:
		return "controller idle"
	case RouteNotInstalled:
		return "source or destination not installed"
	case RouteInhibited:
		return "route inhibited"
	case RouteProtected:
		return "destination protected"
	case RouteInUse:
		return "source or destination in use"
	case RouteNoTieline:
		return "no tieline available"
	case RouteConfiguration:
		return "configuration error"
	case RouteBadParameters:
		return "invalid parameters"
	case RouteNotConnected:
		return "not connected"
	default:
		return fmt.Sprintf("result(%d)", uint32(r))
	}
}
