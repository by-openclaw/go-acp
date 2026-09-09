package rollcall

import (
	"fmt"

	"dhs/internal/snell-rollcall/codec"
	"dhs/internal/snell-rollcall/codec/dtp"
	"dhs/internal/snell-rollcall/codec/router"
)

// A fake router controller, laid out the way the vendor Centra lays itself
// out: a root table at command 100, matrices at 121 with a step of 15, and
// everything below found by the arithmetic the controller publishes rather than
// by numbers written here. Building it that way is the point — a reader that
// gets an offset wrong fails against this for the same reason it would fail
// against the real thing.
//
// Its two deliberate behaviours are the ones a client gets wrong: the reply to
// a route set carries the value from before the set, and the new value arrives
// afterwards as a back-channel push.

// fakeRouter is a routing interface served on one port.
type fakeRouter struct {
	// salvoRoutes is how many routes this controller's salvos report making.
	salvoRoutes uint32

	// catBase and grpBase are where the category and group tables were laid
	// out, so a test can take one of their commands away.
	catBase uint32
	grpBase uint32

	port uint8

	version  uint32
	name     string
	matrices []fakeMatrix

	salvos  uint32
	devices uint32

	// values is the command space: every command the interface answers.
	values map[uint32]codec.Value

	// routed maps a routed-source command to what is on it.
	routed map[uint32]router.SourcePin

	// tieline routes a source pin somewhere else, so the tally reports a
	// different pin from the one that was written — which is what a real
	// controller does when a route crosses a tieline.
	tieline map[uint32]uint32

	// refuse is a command that answers NACK, for the paths that give up.
	refuse map[uint32]bool

	// badReply makes a set answer with a value whose data is not Data Transfer
	// Params, so it decodes as a value and holds no crosspoint.
	badReply map[uint32]bool

	// garble makes a command answer with a payload that is not the structure
	// it claims to be, which is what a unit with a firmware fault looks like.
	garble map[uint32]bool

	// results are the route results a destination's next set answers with,
	// for the refusals a controller expresses in the result code rather than
	// by refusing the message.
	results map[uint32]router.RouteResult

	// files are the bulk files this controller publishes, by the name it
	// publishes them under. The checksums it advertises are computed from
	// these, so a client that verifies one is verifying the real thing.
	files map[string][]byte
}

type fakeMatrix struct {
	name   string
	levels []fakeLevel

	srcAssocs uint32
	dstAssocs uint32
}

type fakeLevel struct {
	name string
	srcs uint32
	dsts uint32
}

// newFakeRouter builds a controller with the given shape and lays out its
// command space.
func newFakeRouter(port uint8, matrices []fakeMatrix) *fakeRouter {
	r := &fakeRouter{
		port:        port,
		version:     router.VersionRouteErrors,
		name:        "Fake Router",
		matrices:    matrices,
		salvos:      4,
		salvoRoutes: 12,
		devices:     8,
		values:      make(map[uint32]codec.Value),
		routed:      make(map[uint32]router.SourcePin),
		tieline:     make(map[uint32]uint32),
		refuse:      make(map[uint32]bool),
		garble:      make(map[uint32]bool),
		files:       make(map[string][]byte),
	}
	r.layout()
	return r
}

// layout fills the command space, following the same arithmetic a client uses
// to read it back.
func (r *fakeRouter) layout() {
	const (
		matrixBase = 121
		matrixStep = router.MatrixTableSize
	)

	r.num(uint32(router.CmdInterfaceVersion), int32(r.version))
	r.str(uint32(router.CmdRouterName), r.name)
	r.num(uint32(router.CmdNumMatrices), int32(len(r.matrices)))
	r.num(uint32(router.CmdMatrixBase), matrixBase)
	r.num(uint32(router.CmdMatrixStep), matrixStep)

	// One category with two groups, which is how a panel narrows a plant down:
	// a group matches a name rather than owning a set, carrying the string to
	// look for and the character index to look for it at.
	categoryBase := uint32(matrixBase + len(r.matrices)*matrixStep)
	groupBase := categoryBase + router.CategoryTableSize
	r.catBase, r.grpBase = categoryBase, groupBase
	r.num(uint32(router.CmdNumCategories), 1)
	r.num(uint32(router.CmdCategoryBase), int32(categoryBase))
	r.num(uint32(router.CmdCategoryStep), router.CategoryTableSize)

	r.str(categoryBase+router.OffCategoryName, "Type")
	r.num(categoryBase+router.OffCategoryExclusive, 1)
	r.num(categoryBase+router.OffCategorySortIndex, 1)
	r.num(categoryBase+router.OffNumGroups, 2)
	r.num(categoryBase+router.OffGroupBase, int32(groupBase))
	r.num(categoryBase+router.OffGroupStep, router.GroupTableSize)

	for i, g := range []struct {
		name, search string
		start        int32
	}{{"Cameras", "CAM", 0}, {"Monitors", "MON", 0}} {
		base := groupBase + uint32(i)*router.GroupTableSize
		r.str(base+router.OffGroupName, g.name)
		r.str(base+router.OffGroupSearchString, g.search)
		r.num(base+router.OffGroupSearchStart, g.start)
	}

	r.num(uint32(router.CmdNumSalvos), int32(r.salvos))
	// Salvo names are a list rather than a pair of lists: the collated format
	// carries sources then destinations, and a salvo is neither, so they go in
	// the source half with nothing after it.
	salvoNames := router.NamesFile{Srcs: make([]string, r.salvos)}
	for i := range salvoNames.Srcs {
		salvoNames.Srcs[i] = fmt.Sprintf("Salvo %d", i+1)
	}
	r.names(uint32(router.CmdSalvoNames8File), `RC_Files\SalvoNames_8.dat`,
		router.NameWidth8, salvoNames)
	r.names(uint32(router.CmdSalvoNames32File), `RC_Files\SalvoNames_32.dat`,
		router.NameWidth32, salvoNames)
	// A controller answers a fire with the salvo and how many routes it made,
	// which is the whole of the result: there is no separate code.
	fired, _ := router.SalvoFired{}.AppendTo(nil)
	r.data(uint32(router.CmdFireSalvo), fired)
	r.num(uint32(router.CmdNumDevices), int32(r.devices))
	r.file(uint32(router.CmdDeviceNamesFile), `RC_Files\DeviceNames.dat`, 0x3333)

	// Levels and entities are laid out after the matrix tables, each block
	// following the last, exactly as a controller allocates them.
	next := categoryBase + 32

	for i := range r.matrices {
		m := &r.matrices[i]
		n := uint32(i + 1)
		base := uint32(matrixBase) + (n-1)*matrixStep

		levelBase := next
		next += uint32(len(m.levels)) * router.LevelTableSize

		r.str(base+router.OffMatrixName, m.name)
		r.num(base+router.OffNumLevels, int32(len(m.levels)))
		r.num(base+router.OffLevelBase, int32(levelBase))
		r.num(base+router.OffLevelStep, router.LevelTableSize)
		r.num(base+router.OffNumSrcAssocs, int32(m.srcAssocs))
		r.num(base+router.OffSrcAssocBase, int32(next))
		r.num(base+router.OffSrcAssocStep, 5)
		next += m.srcAssocs * 5
		r.num(base+router.OffNumDstAssocs, int32(m.dstAssocs))
		r.num(base+router.OffDstAssocBase, int32(next))
		r.num(base+router.OffDstAssocStep, 5)
		next += m.dstAssocs * 5

		r.names(base+router.OffAssocNames8File,
			fmt.Sprintf(`RC_Files\AssocNames_%d_8.dat`, n), router.NameWidth8,
			namesOf("M%dSA%d", "M%dDA%d", n, m.srcAssocs, m.dstAssocs))
		r.names(base+router.OffAssocNames32File,
			fmt.Sprintf(`RC_Files\AssocNames_%d_32.dat`, n), router.NameWidth32,
			namesOf("matrix%d source assoc %d", "matrix%d dest assoc %d", n, m.srcAssocs, m.dstAssocs))
		r.names(base+router.OffAssocNamesAltFile,
			fmt.Sprintf(`RC_Files\AssocNames_%d_alt.dat`, n), router.NameWidth32,
			namesOf("alt-sa%d-%d", "alt-da%d-%d", n, m.srcAssocs, m.dstAssocs))
		r.num(base+router.OffControllerNumber, 1)
		r.mappings(base+router.OffAssocMappingsFile,
			fmt.Sprintf(`RC_Files\AssocMap_%d.dat`, n),
			len(m.levels), m.srcAssocs, m.dstAssocs)

		for j := range m.levels {
			lv := &m.levels[j]
			v := uint32(j + 1)
			lbase := levelBase + (v-1)*router.LevelTableSize

			srcBase := next
			next += lv.srcs * router.SrcTableSize
			dstBase := next
			next += lv.dsts * router.DstTableSize

			r.str(lbase+router.OffLevelName, lv.name)
			r.num(lbase+router.OffLevelType, 0)
			r.num(lbase+router.OffNumSrcs, int32(lv.srcs))
			r.num(lbase+router.OffSrcBase, int32(srcBase))
			r.num(lbase+router.OffSrcStep, router.SrcTableSize)
			r.num(lbase+router.OffNumDsts, int32(lv.dsts))
			r.num(lbase+router.OffDstBase, int32(dstBase))
			r.num(lbase+router.OffDstStep, router.DstTableSize)
			// The names in the file are the same ones the per-entity
			// commands below return, so a client that falls back to those
			// gets the same answer more slowly.
			r.names(lbase+router.OffSrcDstNames8File,
				fmt.Sprintf(`RC_Files\SrcDstNames_%d_%d_8.dat`, n, v), router.NameWidth8,
				levelNames("M%dL%dS%d", "M%dL%dD%d", n, v, lv.srcs, lv.dsts))
			r.names(lbase+router.OffSrcDstNames32File,
				fmt.Sprintf(`RC_Files\SrcDstNames_%d_%d_32.dat`, n, v), router.NameWidth32,
				levelNames("matrix%d level%d source%d", "matrix%d level%d dest%d",
					n, v, lv.srcs, lv.dsts))
			r.names(lbase+router.OffSrcDstNamesAltFile,
				fmt.Sprintf(`RC_Files\SrcDstNames_%d_%d_alt.dat`, n, v), router.NameWidth32,
				levelNames("alt-s%d-%d-%d", "alt-d%d-%d-%d", n, v, lv.srcs, lv.dsts))
			r.file(lbase+router.OffSrcDstMCDataFile,
				fmt.Sprintf(`RC_Files\SrcDstMCData_%d_%d.dat`, n, v), 0x8000+n*16+v)

			for s := uint32(1); s <= lv.srcs; s++ {
				sb := srcBase + (s-1)*router.SrcTableSize
				r.str(sb+router.OffSrcName8, fmt.Sprintf("M%dL%dS%d", n, v, s))
				r.str(sb+router.OffSrcName32, fmt.Sprintf("matrix%d level%d source%d", n, v, s))
				r.str(sb+router.OffSrcAltName, fmt.Sprintf("alt-s%d", s))
			}
			for d := uint32(1); d <= lv.dsts; d++ {
				db := dstBase + (d-1)*router.DstTableSize
				r.str(db+router.OffDestName8, fmt.Sprintf("M%dL%dD%d", n, v, d))
				r.str(db+router.OffDestName32, fmt.Sprintf("matrix%d level%d dest%d", n, v, d))
				r.str(db+router.OffDestAltName, fmt.Sprintf("alt-d%d", d))

				// Every destination starts with source one of its own level
				// routed to it, which is what a controller comes up with.
				pin := router.SourcePin{Matrix: uint8(n), Level: uint8(v), Source: 1}
				r.routed[db+router.OffDestRoutedSrc] = pin
				r.data(db+router.OffDestRoutedSrc, mustDTP(dtp.Params{dtp.Uint(pin.Pack())}))

				r.values[db+router.OffDestProtect] = codec.Value{
					Command: db + router.OffDestProtect,
					Mode:    codec.ModeValue | codec.ModeString,
				}
				r.data(db+router.OffDestMCSrcs, nil)
				r.data(db+router.OffDestMCProtects, nil)
				r.data(db+router.OffDestMCProtect, nil)
			}
		}
	}
}

func (r *fakeRouter) num(cmd uint32, v int32) {
	r.values[cmd] = codec.Value{Command: cmd, Mode: codec.ModeValue, Val: v}
}

func (r *fakeRouter) str(cmd uint32, s string) {
	r.values[cmd] = codec.Value{Command: cmd, Mode: codec.ModeString, Text: s}
}

func (r *fakeRouter) data(cmd uint32, b []byte) {
	r.values[cmd] = codec.Value{Command: cmd, Mode: codec.ModeData, Data: b}
}

// file publishes a filename and its checksum as Data Transfer Params, which is
// how a controller names every bulk file.
func (r *fakeRouter) file(cmd uint32, name string, crc uint32) {
	r.data(cmd, mustDTP(dtp.Params{dtp.String(name), dtp.Uint(crc)}))
}

// names publishes a names file: the bytes go in the file service, and the
// checksum the controller advertises is computed from them.
func (r *fakeRouter) names(cmd uint32, path string, width int, nf router.NamesFile) {
	nf.Width = width
	body, err := nf.AppendTo(nil)
	if err != nil {
		panic(err)
	}
	r.files[path] = body
	r.file(cmd, path, nf.CRC())
}

// mappings publishes an association mappings file, where association n reaches
// entity n on every level.
func (r *fakeRouter) mappings(cmd uint32, path string, levels int, srcs, dsts uint32) {
	mf := router.MappingsFile{Levels: levels}
	for i := uint32(1); i <= srcs; i++ {
		row := make([]uint32, levels)
		for j := range row {
			row[j] = i
		}
		mf.Srcs = append(mf.Srcs, row)
	}
	for i := uint32(1); i <= dsts; i++ {
		row := make([]uint32, levels)
		for j := range row {
			row[j] = i
		}
		mf.Dsts = append(mf.Dsts, row)
	}

	r.files[path] = mf.AppendTo(nil)
	r.file(cmd, path, mf.CRC())
}

// levelNames builds the source and destination names of one level.
func levelNames(srcFmt, dstFmt string, matrix, level, srcs, dsts uint32) router.NamesFile {
	var nf router.NamesFile
	for i := uint32(1); i <= srcs; i++ {
		nf.Srcs = append(nf.Srcs, fmt.Sprintf(srcFmt, matrix, level, i))
	}
	for i := uint32(1); i <= dsts; i++ {
		nf.Dsts = append(nf.Dsts, fmt.Sprintf(dstFmt, matrix, level, i))
	}
	return nf
}

// namesOf builds a matrix's association names.
func namesOf(srcFmt, dstFmt string, matrix, srcs, dsts uint32) router.NamesFile {
	var nf router.NamesFile
	for i := uint32(1); i <= srcs; i++ {
		nf.Srcs = append(nf.Srcs, fmt.Sprintf(srcFmt, matrix, i))
	}
	for i := uint32(1); i <= dsts; i++ {
		nf.Dsts = append(nf.Dsts, fmt.Sprintf(dstFmt, matrix, i))
	}
	return nf
}

func mustDTP(p dtp.Params) []byte {
	b, err := dtp.Encode(p, false)
	if err != nil {
		panic(err)
	}
	return b
}

// garbled reports whether a command answers with a malformed payload.
func (r *fakeRouter) garbled(cmd uint32) bool { return r.garble[cmd] }

// get answers a read, reporting whether the command belongs to this router.
func (r *fakeRouter) get(cmd uint32) (codec.Value, bool, bool) {
	if r.refuse[cmd] {
		return codec.Value{}, true, false
	}
	v, ok := r.values[cmd]
	return v, ok, ok
}

// set applies a write and returns what to answer with, plus what to push.
//
// The answer carries the value from *before* the change, with the result code
// appended; the new value goes out afterwards on the back channel. That is what
// the vendor controller does, and a client that trusts the reply shows a stale
// crosspoint because of it.
func (r *fakeRouter) set(v codec.Value) (reply codec.Value, push *codec.Value, mine bool) {
	if r.refuse[v.Command] {
		return codec.Value{}, nil, false
	}
	if _, isRoute := r.routed[v.Command]; isRoute {
		before := r.routed[v.Command]

		items, err := dtp.Decode(v.Data)
		if err != nil || len(items) == 0 {
			return codec.Value{}, nil, true
		}
		var pin uint32
		switch items[0].Type {
		case dtp.TypeUintArray:
			if len(items[0].Uints) == 0 {
				return codec.Value{}, nil, true
			}
			pin = items[0].Uints[0]
		case dtp.TypeUint:
			pin = items[0].Uint
		}
		if to, ok := r.tieline[pin]; ok {
			// The route completed through a tieline, so the tally reports the
			// far end rather than what was asked for.
			pin = to
		}

		r.routed[v.Command] = router.UnpackSourcePin(pin)
		r.data(v.Command, mustDTP(dtp.Params{dtp.Uint(pin)}))

		result := router.RouteOK
		if r.results != nil {
			if got, ok := r.results[v.Command]; ok {
				result = got
				// A route the controller would not make leaves the crosspoint
				// where it was.
				r.routed[v.Command] = before
				r.data(v.Command, mustDTP(dtp.Params{dtp.Uint(before.Pack())}))
			}
		}

		reply = codec.Value{
			Command: v.Command,
			Mode:    codec.ModeData,
			Data:    mustDTP(dtp.Params{dtp.Uint(before.Pack()), dtp.Uint(uint32(result))}),
		}
		if r.badReply[v.Command] {
			reply.Data = []byte{0xFF, 0xFF}
		}
		pushed := r.values[v.Command]
		return reply, &pushed, true
	}

	// Firing a salvo is a request, not a setting: the reply says which salvo
	// and how many routes it made, rather than echoing what was asked.
	if v.Command == uint32(router.CmdFireSalvo) {
		req, err := router.DecodeFireSalvo(v.Data)
		if err != nil {
			return codec.Value{}, nil, true
		}
		body, _ := router.SalvoFired{Salvo: req.Salvo, Routes: r.salvoRoutes}.AppendTo(nil)
		if r.badReply[v.Command] {
			body = []byte{0xFF, 0xFF}
		}
		stored := codec.Value{Command: v.Command, Mode: codec.ModeData, Data: body}
		r.values[v.Command] = stored
		return stored, nil, true
	}

	if _, ok := r.values[v.Command]; !ok {
		return codec.Value{}, nil, false
	}
	stored := codec.Value{Command: v.Command, Mode: v.Mode, Val: v.Val, Text: v.Text, Data: v.Data}
	r.values[v.Command] = stored
	return stored, nil, true
}
