package router

// The command set a router level serves.
//
// This is not the Full Control command set in commands.go. That one is served
// by a single controller node and describes a whole plant through tables of
// matrices, levels, sources and destinations; this one is served by each level
// on a node of its own, typed ID_ROUTER_LEVEL, and describes only that level.
// The two overlap in the numbers they use and agree on nothing: 100 is the
// interface version to a controller and the selected destination to a level.
// Which one a number means is decided by the type of the node it was asked of.
//
// Every number and range below was read off a vendor Centra, whose Matrix 1 at
// 0000-11-00 carries Level 1 and Level 2 as its own ports at 0000-11-01 and
// 0000-11-02. The menu those levels publish is the whole interface, and a
// vendor Control Panel drives an XY screen from nothing else.
const (
	// LvlDestSelect is which destination the panel is working on, counting
	// from one. The menu offers it as a list of buttons that all carry this
	// command, each with its own number in the minimum-range field.
	LvlDestSelect Command = 100

	// LvlSrcNameIndex and LvlDstNameIndex choose which name LvlSrcName and
	// LvlDstName read and write. Renaming is a two-step on this interface:
	// point the index at an entry, then write the string.
	LvlSrcNameIndex Command = 101
	LvlDstNameIndex Command = 102

	// LvlSrcSelect is the source the panel has picked but not yet taken.
	LvlSrcSelect Command = 110

	LvlSrcName Command = 111
	LvlDstName Command = 112

	// LvlDestProtect is the protect state of the selected destination. It is a
	// checkbox with the range 1..2 rather than 0..1, which is how this
	// protocol spells a two-state control.
	LvlDestProtect Command = 113

	// LvlTakeMode says whether a source selection routes at once or waits.
	// Zero takes immediately; one waits for LvlTake.
	LvlTakeMode Command = 120
	LvlTake     Command = 121
	LvlCancel   Command = 122

	LvlSourceCount Command = 130
	LvlDestCount   Command = 131
)

// Take modes, as the two buttons in the menu carry them.
const (
	TakeImmediate = 0
	TakeOnButton  = 1
)

// Protect states. A checkbox on this interface counts from one.
const (
	ProtectOff = 1
	ProtectOn  = 2
)

// Direct routing and direct protect: one command per destination rather than a
// selection followed by an action.
//
// This is what a panel subscribes to for tally. Reading LvlRoute(d) gives the
// source currently on destination d and writing it routes; the level pushes an
// unsolicited value on every change, so a panel that has subscribed to the
// block sees the whole plant move without polling.
const (
	LvlRouteBase   Command = 10000
	LvlProtectBase Command = 20000

	// LvlRefSourceBase carries what each source is, as against what it is
	// called: a name is what an operator types and a reference is where the
	// signal comes from. A panel draws it beside the name when its Show Source
	// Reference option is on, and it is wired by the CMDReferenceSourceBase
	// key of the panel's own section in the template.
	LvlRefSourceBase Command = 30000
)

// LvlRoute is the command carrying what is routed to a destination.
func LvlRoute(dest int) Command { return LvlRouteBase + Command(dest) }

// LvlProtect is the command carrying a destination's protect state.
func LvlProtect(dest int) Command { return LvlProtectBase + Command(dest) }

// LvlRefSource is the command carrying a source's reference.
func LvlRefSource(source int) Command { return LvlRefSourceBase + Command(source) }

// IsLevelRefSource reports whether c is a source reference, and for which
// source.
func IsLevelRefSource(c Command) (source int, ok bool) {
	if c > LvlRefSourceBase && c < LvlRefSourceBase+maxLevelDests {
		return int(c - LvlRefSourceBase), true
	}
	return 0, false
}

// Monitor outputs. Four of them, five readouts each, laid out as one base per
// readout and the monitor number added to it.
const (
	LvlMonKind    Command = 400 // "Source" or "Dest"
	LvlMonIndex   Command = 410
	LvlMonName    Command = 420
	LvlMonSrcAddr Command = 430
	LvlMonDstAddr Command = 440

	// LvlMonitors is how many a level publishes. It is fixed: the vendor's own
	// template draws exactly four and names them Monitor 1 to Monitor 4.
	LvlMonitors = 4
)

// LvlMonitor is one readout of one monitor output.
func LvlMonitor(base Command, monitor int) Command { return base + Command(monitor) }

// IsLevelRoute reports whether c is a direct-routing command, and for which
// destination.
//
// The block is open-ended upwards: a level with 1450 destinations uses 10001
// to 11450, which runs into no other block, and the protect block starts far
// enough above it that no plant we have to serve could reach it.
func IsLevelRoute(c Command) (dest int, ok bool) {
	if c > LvlRouteBase && c < LvlProtectBase {
		return int(c - LvlRouteBase), true
	}
	return 0, false
}

// IsLevelProtect reports whether c is a direct-protect command, and for which
// destination.
func IsLevelProtect(c Command) (dest int, ok bool) {
	if c > LvlProtectBase && c < LvlProtectBase+maxLevelDests {
		return int(c - LvlProtectBase), true
	}
	return 0, false
}

// maxLevelDests bounds the two direct blocks so neither can be mistaken for
// the other. It is not a limit on a plant: it is the distance between the
// blocks, and a destination past it would be addressed by a command in the
// next block whatever we decided here.
const maxLevelDests = LvlProtectBase - LvlRouteBase
