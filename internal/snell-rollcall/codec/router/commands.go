// Package router implements the address arithmetic of the RollCall Full
// Control Command Set: the flat 32-bit command space a router controller
// exposes, and the file layouts the names in it are fetched from.
//
// The command set exists only on the 32-bit generation. A matrix of any useful
// size needs more command numbers than the 16-bit field can hold, which is what
// the 2014 long-string extension was written to provide.
//
// Nothing here does I/O. Given the handful of base and step values a controller
// publishes, these functions say which command number names a given matrix,
// level, source or destination; the session layer fetches them, and the
// consumer fetches the names files over the file service.
//
// # Confidence
//
// The root table is pinned by the specification, which states outright that
// CMD_INTERFACE_VERSION is command 100 and the rest follow in order. The
// per-matrix, per-level and per-destination tables are documented as "defined
// in a similar fashion" with their commands listed in order but no numbers
// given, so their offsets are read from that ordering. That reading has not yet
// been checked against a router controller: the audit oracle answered NACK to
// command 100, so it exposes no routing interface at all. Treat the sub-table
// offsets as unvalidated until a real controller confirms them.
//
// This package is stdlib-only and imports nothing from dhs (ADR-0006).
package router

// Command is a 32-bit RollCall command number.
type Command uint32

// The routing interface root. These are the only fixed command numbers in the
// set; everything else is found by arithmetic from values read here
// ("Full Control Command Set" §Routing Interface).
const (
	CmdInterfaceVersion  Command = 100
	CmdRouterName        Command = 101
	CmdNumMatrices       Command = 102
	CmdMatrixBase        Command = 103
	CmdMatrixStep        Command = 104
	CmdNumCategories     Command = 105
	CmdCategoryBase      Command = 106
	CmdCategoryStep      Command = 107
	CmdAssocMakeRoute    Command = 108
	CmdNumTrackTemplates Command = 109
	CmdGetTrackTemplate  Command = 110
	CmdNumAudioGroups    Command = 111
	CmdGetAudioGroup     Command = 112
	CmdNumSalvos         Command = 113
	CmdSalvoNames8File   Command = 114
	CmdSalvoNames32File  Command = 115
	CmdFireSalvo         Command = 116
	CmdNumDevices        Command = 117
	CmdDeviceNamesFile   Command = 118
	CmdGetAHPNode        Command = 119
	cmdRootLast                  = CmdGetAHPNode
)

// IsRoutingInterface reports whether a command belongs to the fixed root table.
func IsRoutingInterface(c Command) bool {
	return c >= CmdInterfaceVersion && c <= cmdRootLast
}

// InterfaceVersion values, which are the revision numbers of the specification
// itself. A client reads the version first and uses it to decide which
// commands exist: commands are only ever added, never removed or repurposed,
// so anything introduced after the advertised version is absent.
const (
	// VersionMakeRoute added CMD_ASSOC_MAKE_ROUTE and Data Transfer Params.
	VersionMakeRoute = 2
	// VersionInterfaceVersion added the version command itself.
	VersionInterfaceVersion = 3
	// VersionProtectID16 widened the protect id to 16 bits and moved the
	// master bit to 24.
	VersionProtectID16 = 4
	// VersionSalvos added salvo names and firing.
	VersionSalvos = 8
	// VersionDeviceNames added CMD_DEVICE_NAMES_FILENAME.
	VersionDeviceNames = 9
	// VersionAHPNode added CMD_GET_AHP_NODE.
	VersionAHPNode = 11
	// VersionRouteErrors is the current revision, which settled the route
	// result codes.
	VersionRouteErrors = 12
)

// Offsets within a matrix's table, from MatrixBase.
//
// Read from the order the specification lists them in; see the package note on
// confidence.
const (
	OffMatrixName        = 0
	OffNumLevels         = 1
	OffLevelBase         = 2
	OffLevelStep         = 3
	OffNumSrcAssocs      = 4
	OffSrcAssocBase      = 5
	OffSrcAssocStep      = 6
	OffNumDstAssocs      = 7
	OffDstAssocBase      = 8
	OffDstAssocStep      = 9
	OffAssocNames8File   = 10
	OffAssocNames32File  = 11
	OffAssocNamesAltFile = 12
	OffControllerNumber  = 13
	OffAssocMappingsFile = 14
	MatrixTableSize      = 15
)

// Offsets within a level's table, from LevelBase.
const (
	OffLevelName          = 0
	OffLevelType          = 1
	OffNumSrcs            = 2
	OffSrcBase            = 3
	OffSrcStep            = 4
	OffNumDsts            = 5
	OffDstBase            = 6
	OffDstStep            = 7
	OffSrcDstNames8File   = 8
	OffSrcDstNames32File  = 9
	OffSrcDstNamesAltFile = 10
	OffSrcDstMCDataFile   = 11
	LevelTableSize        = 12
)

// Offsets within a source's table, from SrcBase.
const (
	OffSrcName8   = 0
	OffSrcName32  = 1
	OffSrcAltName = 2
	SrcTableSize  = 3
)

// Offsets within a destination's table, from DstBase.
const (
	OffDestName8      = 0
	OffDestName32     = 1
	OffDestAltName    = 2
	OffDestRoutedSrc  = 3
	OffDestProtect    = 4
	OffDestMCSrcs     = 5
	OffDestMCProtects = 6
	OffDestMCProtect  = 7
	DstTableSize      = 8
)

// Table holds the base and step a controller published for one kind of entity.
// Every level of the command space has this shape, which is why one type
// serves all of them.
type Table struct {
	Base  Command
	Step  uint32
	Count uint32
}

// Command returns the base command for the n-th entity, counting from 1 as the
// specification does, and reports whether n is within Count.
//
//	MatrixBase(m) = CMD_MATRIX_BASE + (m-1) * CMD_MATRIX_STEP
//
// The same form gives levels within a matrix and sources or destinations
// within a level.
func (t Table) Command(n uint32) (Command, bool) {
	if n < 1 || n > t.Count {
		return 0, false
	}
	return t.Base + Command((n-1)*t.Step), true
}

// Field returns the command at an offset inside the n-th entity's own table,
// such as the name of matrix 3 or the routed source of destination 40.
//
// It reports false when n is out of range, and also when the offset falls
// outside the step the controller published: a controller that allocates a
// step smaller than the table it documents has entities overlapping in the
// command space, and reading past the step would read the next entity's
// fields rather than this one's.
func (t Table) Field(n uint32, offset uint32) (Command, bool) {
	base, ok := t.Command(n)
	if !ok {
		return 0, false
	}
	if t.Step != 0 && offset >= t.Step {
		return 0, false
	}
	return base + Command(offset), true
}

// Contains reports whether c falls inside the span this table occupies.
func (t Table) Contains(c Command) bool {
	if t.Count == 0 || t.Step == 0 {
		return false
	}
	end := t.Base + Command(t.Count*t.Step)
	return c >= t.Base && c < end
}

// Index returns which entity a command belongs to, counting from 1, and the
// offset within that entity's table.
func (t Table) Index(c Command) (n uint32, offset uint32, ok bool) {
	if !t.Contains(c) {
		return 0, 0, false
	}
	d := uint32(c - t.Base)
	return d/t.Step + 1, d % t.Step, true
}
