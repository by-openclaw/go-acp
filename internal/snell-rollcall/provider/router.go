package rollcall

import (
	"dhs/internal/export/canonical"
	"dhs/internal/snell-rollcall/codec"
	"dhs/internal/snell-rollcall/codec/dtp"
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

	// categories are how a panel narrows the plant down. They are derived
	// from the names in it rather than configured: see router_category.go.
	categories []routerCategory
	catTable   router.Table

	// salvos are sets of routes made together. What is in them is this
	// provider's own choice: a canonical tree has no field for a salvo.
	salvos []routerSalvo

	// base and step are what command 102 to 104 publish. Every table below
	// carries its own pair, because a client walks them the same way.
	table router.Table
}

type routerMatrix struct {
	name   string
	levels []routerLevel
	table  router.Table

	// srcAssocs and dstAssocs are the logical entities of this matrix: one
	// name gathering the same thing across every level, which is what a panel
	// takes when it takes a camera rather than four crosspoints.
	srcAssocs    []routerAssoc
	dstAssocs    []routerAssoc
	srcAssocTbl  router.Table
	dstAssocTbl  router.Table
	assocMembers int
}

type routerLevel struct {
	name    string
	sources []string
	dests   []routerDest

	// filter says whether the plant offers categories to narrow itself down
	// with. A panel draws the filter only when told to, and telling it so with
	// nothing behind it gives an operator a control that finds nothing.
	filter bool

	// matrixNumber and levelNumber are where this level sits in the plant,
	// counting from one. A crosspoint made on this node is a local route, and
	// a local route is one whose source names this same matrix and level; the
	// two numbers are what let it say so.
	matrixNumber uint8
	levelNumber  uint8

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

	// Whether the tree named anything, which is what decides if the plant can
	// be organised into categories at all.
	named := false

	alloc := func(count, size uint32) router.Table {
		t := router.Table{Base: next, Step: size, Count: count}
		next += router.Command(count * size)
		return t
	}

	r.table = alloc(uint32(len(matrices)), router.MatrixTableSize)

	for _, m := range matrices {
		rm := routerMatrix{name: m.Common().Identifier}

		// One RollCall level per label set the tree carries.
		//
		// A canonical matrix keys its labels by the level they belong to —
		// "Video", "Audio 1", "Audio 2" — which is the same thing this
		// protocol calls a level: a plane of crosspoints with its own
		// numbering. A tree that names one set has one level; a tree that
		// names none has one level called Level 1, because a matrix with no
		// levels has nowhere to put a crosspoint.
		for _, name := range levelNames(m) {
			lv := routerLevel{
				name:         name,
				dests:        make([]routerDest, m.TargetCount),
				matrixNumber: uint8(len(r.matrices) + 1),
				levelNumber:  uint8(len(rm.levels) + 1),
			}
			srcNames, srcNamed := levelLabels(m.SourceLabels, name, int(m.SourceCount), "SRC")
			dstNames, dstNamed := levelLabels(m.TargetLabels, name, int(m.TargetCount), "DST")
			lv.sources = srcNames
			for i := range lv.dests {
				lv.dests[i].name = dstNames[i]
			}
			named = named || srcNamed || dstNamed
			rm.levels = append(rm.levels, lv)
		}

		// Associations: one logical thing per entity, gathering the same
		// number on every level. A plant that numbers its levels alike is the
		// ordinary case, and it is the only one a tree of label sets can
		// describe — a tree that pairs video 4 with audio 7 is carrying a
		// mapping this format has no field for.
		rm.srcAssocs = buildAssocs(rm.levels, true)
		rm.dstAssocs = buildAssocs(rm.levels, false)

		r.matrices = append(r.matrices, rm)
	}

	r.salvos = buildSalvos(r.matrices)

	// Categories, then their groups. They come before the matrices because a
	// client reads the root block first and the categories are named in it.
	if named && len(r.matrices) > 0 && len(r.matrices[0].levels) > 0 {
		lv := &r.matrices[0].levels[0]
		r.categories = buildCategories(lv.sources, lv.dests)
	}
	r.catTable = alloc(uint32(len(r.categories)), router.CategoryTableSize)

	// Every level is told whether there is anything to filter by, because the
	// filter is drawn from the level's own template and the categories are
	// published on the node that serves the tables.
	for i := range r.matrices {
		for j := range r.matrices[i].levels {
			r.matrices[i].levels[j].filter = len(r.categories) > 0
		}
	}
	for i := range r.categories {
		r.categories[i].table = alloc(uint32(len(r.categories[i].groups)), router.GroupTableSize)
	}

	// Levels, then the sources and destinations under each, so a matrix's
	// whole subtree is contiguous and a reader can see the shape in a dump.
	for i := range r.matrices {
		m := &r.matrices[i]
		m.table = alloc(uint32(len(m.levels)), router.LevelTableSize)

		// An association's table is as long as the matrix has levels: three
		// names and then one member per level.
		m.assocMembers = len(m.levels)
		size := router.AssocTableSize(m.assocMembers)
		m.srcAssocTbl = alloc(uint32(len(m.srcAssocs)), size)
		m.dstAssocTbl = alloc(uint32(len(m.dstAssocs)), size)

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
	num(router.CmdNumCategories, int32(r.catTable.Count))
	num(router.CmdCategoryBase, int32(r.catTable.Base))
	num(router.CmdCategoryStep, int32(r.catTable.Step))
	// Routing by association is a data command in both directions: the request
	// carries the two associations and the levels to carry across, the reply
	// the result. Its resting value is the result of the last route made, so a
	// client that reads it before making one is told the last thing that
	// happened rather than refused.
	makeRouteResult, _ := router.AppendRouteResult(nil, router.RouteOK)
	out[uint32(router.CmdAssocMakeRoute)] = codec.Value{
		Command: uint32(router.CmdAssocMakeRoute), Mode: codec.ModeData, Data: makeRouteResult,
	}
	num(router.CmdNumTrackTemplates, 0)
	num(router.CmdGetTrackTemplate, 0)
	num(router.CmdNumAudioGroups, 0)
	num(router.CmdGetAudioGroup, 0)
	num(router.CmdNumSalvos, int32(len(r.salvos)))
	file := func(c router.Command, name string, crc uint32) {
		// A filename and its checksum, as Data Transfer Params. Encoding a
		// string and a number cannot fail.
		b, _ := dtp.Append(nil, dtp.Params{dtp.String(name), dtp.Uint(crc)}, false)
		out[uint32(c)] = codec.Value{Command: uint32(c), Mode: codec.ModeData, Data: b}
	}
	file(router.CmdSalvoNames8File, salvoNames8File, r.salvoNamesCRC(router.NameWidth8))
	file(router.CmdSalvoNames32File, salvoNames32File, r.salvoNamesCRC(router.NameWidth32))

	// Firing is a data command in both directions: the request names a salvo,
	// the reply says how many routes it made. Its resting value is the last
	// firing, so a client reading it before firing anything is told what
	// happened last rather than refused.
	fired, _ := router.SalvoFired{}.AppendTo(nil)
	out[uint32(router.CmdFireSalvo)] = codec.Value{
		Command: uint32(router.CmdFireSalvo), Mode: codec.ModeData, Data: fired,
	}
	num(router.CmdNumDevices, 0)
	str(router.CmdDeviceNamesFile, "")
	num(router.CmdGetAHPNode, 0)

	// What the node says about itself while a client is reading the tables.
	// The vendor's own template for this node type binds its only control to
	// this command, so a node that will not answer it draws nothing at all.
	str(cmdXYStatus, "Active. Routing tables published on this node.")

	// Categories and the groups under them, which is how a panel narrows a
	// plant down to what an operator is looking for.
	anyNum := func(c router.Command, v any) { num(c, v.(int32)) }
	anyStr := func(c router.Command, v any) { str(c, v.(string)) }
	for i := range r.categories {
		base, ok := r.catTable.Command(uint32(i) + 1)
		if !ok {
			continue
		}
		r.categories[i].values(base, anyNum, anyStr)
	}

	for i := range r.matrices {
		m := &r.matrices[i]
		base, _ := r.table.Command(uint32(i) + 1)

		str(base+router.OffMatrixName, m.name)
		num(base+router.OffNumLevels, int32(m.table.Count))
		num(base+router.OffLevelBase, int32(m.table.Base))
		num(base+router.OffLevelStep, int32(m.table.Step))
		num(base+router.OffNumSrcAssocs, int32(m.srcAssocTbl.Count))
		num(base+router.OffSrcAssocBase, int32(m.srcAssocTbl.Base))
		num(base+router.OffSrcAssocStep, int32(m.srcAssocTbl.Step))
		num(base+router.OffNumDstAssocs, int32(m.dstAssocTbl.Count))
		num(base+router.OffDstAssocBase, int32(m.dstAssocTbl.Base))
		num(base+router.OffDstAssocStep, int32(m.dstAssocTbl.Step))
		str(base+router.OffAssocNames8File, "")
		str(base+router.OffAssocNames32File, "")
		str(base+router.OffAssocNamesAltFile, "")
		num(base+router.OffControllerNumber, 1)
		str(base+router.OffAssocMappingsFile, "")

		// The associations themselves: a name at each width and then the
		// entity this association holds on every level.
		for k := range m.srcAssocs {
			ab, ok := m.srcAssocTbl.Command(uint32(k) + 1)
			if !ok {
				continue
			}
			m.srcAssocs[k].values(ab, num, str)
		}
		for k := range m.dstAssocs {
			ab, ok := m.dstAssocTbl.Command(uint32(k) + 1)
			if !ok {
				continue
			}
			m.dstAssocs[k].values(ab, num, str)
		}

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
				routedValue(out, db+router.OffDestRoutedSrc, d.routed)
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
