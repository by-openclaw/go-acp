package rollcall

import (
	"fmt"

	"dhs/internal/export/canonical"
	"dhs/internal/snell-rollcall/codec"
	"dhs/internal/snell-rollcall/codec/router"
)

// A router serves no menu.
//
// Measured on the vendor Centra: a menu walk of its panel node finds two or
// three lines and nothing else, because routing lives in the command space
// rather than in menus. So a router node answers reads and writes of commands
// from 100 upwards, and a client discovers everything from the tables those
// commands publish.
//
// The layout is the one codec/router encodes, which was read from the Full
// Control Command Set document and then confirmed against the Centra offset by
// offset. This side of the wire has to agree with it exactly: our own consumer
// is the first thing that will read it, and a vendor panel the second.

// routerModel is what a router node serves.
type routerModel struct {
	name     string
	matrices []routerMatrix

	// base and step are what command 102 to 104 publish. Every table below
	// carries its own pair, because a client walks them the same way.
	table router.Table
}

type routerMatrix struct {
	name   string
	levels []routerLevel
	table  router.Table
}

type routerLevel struct {
	name    string
	sources []string
	dests   []routerDest

	srcTable router.Table
	dstTable router.Table
}

// routerDest is one destination: what it is called and what is routed to it.
type routerDest struct {
	name    string
	routed  router.SourcePin
	protect router.ProtectState
}

// buildRouter lays a canonical matrix out in the command space.
//
// The allocation is sequential rather than by a fixed scheme, so no table can
// overlap another whatever the sizes are: a client is told every base and step
// it needs, and nothing in the protocol requires them to be round numbers.
func buildRouter(name string, matrices []*canonical.Matrix) *routerModel {
	r := &routerModel{name: name}

	// Everything below the root, laid out from the first free command after
	// the root block.
	next := router.CmdGetAHPNode + 1

	alloc := func(count, size uint32) router.Table {
		t := router.Table{Base: next, Step: size, Count: count}
		next += router.Command(count * size)
		return t
	}

	r.table = alloc(uint32(len(matrices)), router.MatrixTableSize)

	for _, m := range matrices {
		rm := routerMatrix{name: m.Common().Identifier}

		// One level per matrix for now, named after it. A canonical matrix
		// carries its levels as label sets, and mapping those onto RollCall
		// levels is its own piece of work: what is here is the shape a client
		// walks, not a claim about multi-level naming.
		lv := routerLevel{
			name:  fmt.Sprintf("Level %d", len(rm.levels)+1),
			dests: make([]routerDest, m.TargetCount),
		}
		for i := range int(m.SourceCount) {
			lv.sources = append(lv.sources, fmt.Sprintf("SRC %d", i+1))
		}
		for i := range lv.dests {
			lv.dests[i].name = fmt.Sprintf("DST %d", i+1)
		}
		rm.levels = append(rm.levels, lv)
		r.matrices = append(r.matrices, rm)
	}

	// Levels, then the sources and destinations under each, so a matrix's
	// whole subtree is contiguous and a reader can see the shape in a dump.
	for i := range r.matrices {
		m := &r.matrices[i]
		m.table = alloc(uint32(len(m.levels)), router.LevelTableSize)
		for j := range m.levels {
			l := &m.levels[j]
			l.srcTable = alloc(uint32(len(l.sources)), router.SrcTableSize)
			l.dstTable = alloc(uint32(len(l.dests)), router.DstTableSize)
		}
	}
	return r
}

// values renders the whole command space.
//
// A router's tables are small enough to hold: an emulated plant is tens of
// sources, not the sixty-five thousand a real one may carry. When that changes
// this becomes a lookup rather than a map, and the shape of what it answers
// does not.
func (r *routerModel) values() map[uint32]codec.Value {
	out := make(map[uint32]codec.Value)

	num := func(c router.Command, v int32) {
		out[uint32(c)] = codec.Value{Command: uint32(c), Mode: codec.ModeValue, Val: v}
	}
	str := func(c router.Command, v string) {
		out[uint32(c)] = codec.Value{Command: uint32(c), Mode: codec.ModeString, Text: v}
	}

	num(router.CmdInterfaceVersion, router.VersionRouteErrors)
	str(router.CmdRouterName, r.name)
	num(router.CmdNumMatrices, int32(r.table.Count))
	num(router.CmdMatrixBase, int32(r.table.Base))
	num(router.CmdMatrixStep, int32(r.table.Step))
	// Every command in the root block answers, including the ones whose count
	// is zero. A client walks the block to find out what a router has, and a
	// refusal there reads as "this is not a routing interface" rather than
	// "there are none of those": our own consumer gives up on the whole node
	// when command 106 will not answer.
	num(router.CmdNumCategories, 0)
	num(router.CmdCategoryBase, 0)
	num(router.CmdCategoryStep, 0)
	num(router.CmdAssocMakeRoute, 0)
	num(router.CmdNumTrackTemplates, 0)
	num(router.CmdGetTrackTemplate, 0)
	num(router.CmdNumAudioGroups, 0)
	num(router.CmdGetAudioGroup, 0)
	num(router.CmdNumSalvos, 0)
	str(router.CmdSalvoNames8File, "")
	str(router.CmdSalvoNames32File, "")
	num(router.CmdFireSalvo, 0)
	num(router.CmdNumDevices, 0)
	str(router.CmdDeviceNamesFile, "")
	num(router.CmdGetAHPNode, 0)

	// What the node says about itself while a client is reading the tables.
	// The vendor's own template for this node type binds its only control to
	// this command, so a node that will not answer it draws nothing at all.
	str(99, "Ready")

	for i := range r.matrices {
		m := &r.matrices[i]
		base, _ := r.table.Command(uint32(i) + 1)

		str(base+router.OffMatrixName, m.name)
		num(base+router.OffNumLevels, int32(m.table.Count))
		num(base+router.OffLevelBase, int32(m.table.Base))
		num(base+router.OffLevelStep, int32(m.table.Step))
		num(base+router.OffNumSrcAssocs, 0)
		num(base+router.OffSrcAssocBase, 0)
		num(base+router.OffSrcAssocStep, 0)
		num(base+router.OffNumDstAssocs, 0)
		num(base+router.OffDstAssocBase, 0)
		num(base+router.OffDstAssocStep, 0)
		str(base+router.OffAssocNames8File, "")
		str(base+router.OffAssocNames32File, "")
		str(base+router.OffAssocNamesAltFile, "")
		num(base+router.OffControllerNumber, 1)
		str(base+router.OffAssocMappingsFile, "")

		for j := range m.levels {
			l := &m.levels[j]
			lb, _ := m.table.Command(uint32(j) + 1)

			str(lb+router.OffLevelName, l.name)
			num(lb+router.OffLevelType, 0)
			num(lb+router.OffNumSrcs, int32(l.srcTable.Count))
			num(lb+router.OffSrcBase, int32(l.srcTable.Base))
			num(lb+router.OffSrcStep, int32(l.srcTable.Step))
			num(lb+router.OffNumDsts, int32(l.dstTable.Count))
			num(lb+router.OffDstBase, int32(l.dstTable.Base))
			num(lb+router.OffDstStep, int32(l.dstTable.Step))
			str(lb+router.OffSrcDstNames8File, "")
			str(lb+router.OffSrcDstNames32File, "")
			str(lb+router.OffSrcDstNamesAltFile, "")
			str(lb+router.OffSrcDstMCDataFile, "")

			for k, name := range l.sources {
				sb, _ := l.srcTable.Command(uint32(k) + 1)
				str(sb+router.OffSrcName8, name)
				str(sb+router.OffSrcName32, name)
				str(sb+router.OffSrcAltName, name)
			}
			for k := range l.dests {
				d := &l.dests[k]
				db, _ := l.dstTable.Command(uint32(k) + 1)
				str(db+router.OffDestName8, d.name)
				str(db+router.OffDestName32, d.name)
				num(db+router.OffDestRoutedSrc, int32(d.routed.Pack()))
				str(db+router.OffDestAltName, d.name)
				num(db+router.OffDestProtect, int32(d.protect.Pack()))
				num(db+router.OffDestMCSrcs, 0)
				num(db+router.OffDestMCProtects, 0)
				num(db+router.OffDestMCProtect, 0)
			}
		}
	}
	return out
}
