package rollcall

import (
	"context"
	"fmt"

	"dhs/internal/snell-rollcall/codec"
	"dhs/internal/snell-rollcall/codec/dtp"
	"dhs/internal/snell-rollcall/codec/router"
)

// A router has no menu, and that is the whole difference.
//
// Every other RollCall device describes itself through the menu service: a
// client walks the lines and learns what commands exist. A router controller
// serves its routing, its names, its protects and its salvos as control
// variables instead, in a flat 32-bit command space whose numbers a client
// works out by arithmetic from a root table at command 100. Walking a router's
// menu finds three lines and nothing useful.
//
// So this file is discovery of a different kind: read the root table, follow
// the bases and steps it publishes down through matrices, levels, sources and
// destinations, and address a crosspoint by the command number that falls out.
// The arithmetic lives in codec/router, measured against a vendor controller;
// what lives here is the reading, the caching and the two behaviours a client
// gets wrong if nobody tells it:
//
//   - The reply to a route set carries the value from *before* the set. The
//     new one arrives on the back channel.
//   - The tally follows tielines. Setting source 7 can read back as matrix 2
//     source 1, because the pin reports the final upstream source rather than
//     what was asked for.

// RouterInterface is what a router node publishes about itself.
type RouterInterface struct {
	// Slot is the node this was read from, and Addr the address behind it.
	Slot int
	Addr codec.Address

	// Version is the interface revision. Commands are only ever added, so it
	// says which of them exist: anything introduced after this is absent.
	Version uint32

	// Name is what the router calls itself.
	Name string

	// Matrices are its matrices, in order.
	Matrices []RouterMatrix

	// Salvos is how many salvos it holds, and SalvoNames8 / SalvoNames32 the
	// files their names are in.
	Salvos      uint32
	SalvoNames8 RouterFile
	SalvoNames  RouterFile

	// Devices is how many external devices it knows, and DeviceNames the file
	// naming them.
	Devices     uint32
	DeviceNames RouterFile

	// Categories is the category table, which groups sources and destinations
	// for a panel's filter buttons.
	Categories router.Table
}

// RouterMatrix is one matrix of a router.
type RouterMatrix struct {
	Number uint32
	Name   string

	// Controller is the controller number this matrix lives on, which is what
	// tells two matrices of one router apart on a replicated system.
	Controller int32

	Levels []RouterLevel

	// SrcAssocs and DstAssocs are the association tables. An association
	// groups one entity per level so that routing by association routes every
	// level at once, which is how a panel with level buttons works.
	SrcAssocs router.Table
	DstAssocs router.Table

	// AssocNames8, AssocNames and AssocNamesAlt are the association name files
	// at their three widths; AssocMappings says which entity each association
	// reaches on each level.
	AssocNames8   RouterFile
	AssocNames    RouterFile
	AssocNamesAlt RouterFile
	AssocMappings RouterFile
}

// RouterLevel is one level of one matrix: a plane of crosspoints.
type RouterLevel struct {
	Number uint32
	Name   string

	// Kind is the level type the controller published. Zero is the ordinary
	// one; the rest are not documented anywhere we hold.
	Kind int32

	// Srcs and Dsts are the entity tables. A source's commands are its names;
	// a destination's are its names, its routed source and its protect.
	Srcs router.Table
	Dsts router.Table

	// Names8, Names and NamesAlt are the collated source-and-destination name
	// files at their three widths, and MCData the multi-channel configuration.
	Names8   RouterFile
	Names    RouterFile
	NamesAlt RouterFile
	MCData   RouterFile
}

// RouterFile is a file the controller publishes, named with a checksum.
//
// The checksum is what makes caching possible: it is computed from the names
// themselves rather than from the bytes, so a client that has the file already
// can tell it is still current without fetching it. Sixty-five thousand names
// per level is why that matters.
type RouterFile struct {
	Name string
	CRC  uint32
}

// Empty reports whether the controller named a file at all.
func (f RouterFile) Empty() bool { return f.Name == "" }

// RouterAt reads the routing interface a node publishes.
//
// A node that is not a router refuses command 100, which is how a client tells
// them apart: the controller answers, its matrices refuse, and refusing is not
// a fault.
func (p *Plugin) RouterAt(ctx context.Context, slot int) (*RouterInterface, error) {
	s, err := p.session(ctx, slot)
	if err != nil {
		return nil, err
	}
	if !s.Uses32Bit() {
		// The command set exists only on the 32-bit generation: a matrix of
		// any useful size needs more command numbers than the 16-bit field
		// holds, which is what the long-string extension was written for.
		return nil, fmt.Errorf(
			"rollcall: slot %d speaks the 16-bit generation, which has no routing interface", slot)
	}

	r := &RouterInterface{Slot: slot, Addr: s.Peer()}

	// The interface version is the whole of the test for "is this a router",
	// so it has to be a strict one. A card is not obliged to refuse command
	// 100: on the vendor Centra the input cards answer it with a string —
	// "05915", their own model number — because their menus happen to use
	// those command numbers for something else. A client that accepts any
	// answer builds a router model out of a card's menu.
	v, err := p.readValue(ctx, slot, uint32(router.CmdInterfaceVersion))
	if err != nil {
		return nil, fmt.Errorf("rollcall: slot %d has no routing interface: %w", slot, err)
	}
	if !v.Mode.Has(codec.ModeValue) || v.Val < 1 || v.Val > maxInterfaceVersion {
		return nil, fmt.Errorf(
			"rollcall: slot %d answered command %d with %s, which is not an interface version",
			slot, router.CmdInterfaceVersion, describeValue(v))
	}
	r.Version = uint32(v.Val)

	if r.Name, err = p.readString(ctx, slot, uint32(router.CmdRouterName)); err != nil {
		return nil, err
	}

	matrices, err := p.readTable(ctx, slot,
		uint32(router.CmdNumMatrices), uint32(router.CmdMatrixBase), uint32(router.CmdMatrixStep))
	if err != nil {
		return nil, err
	}
	if r.Categories, err = p.readTable(ctx, slot,
		uint32(router.CmdNumCategories), uint32(router.CmdCategoryBase),
		uint32(router.CmdCategoryStep)); err != nil {
		return nil, err
	}

	// Salvos and device names arrived at known interface versions. Reading a
	// command the controller does not have is a refusal rather than a fault,
	// but asking anyway would fire a compliance event for something the
	// version already told us, so the version decides.
	if r.Version >= router.VersionSalvos {
		if r.Salvos, err = p.readUint(ctx, slot, uint32(router.CmdNumSalvos)); err != nil {
			return nil, err
		}
		if r.SalvoNames8, err = p.readFile(ctx, slot, uint32(router.CmdSalvoNames8File)); err != nil {
			return nil, err
		}
		if r.SalvoNames, err = p.readFile(ctx, slot, uint32(router.CmdSalvoNames32File)); err != nil {
			return nil, err
		}
	}
	if r.Version >= router.VersionDeviceNames {
		if r.Devices, err = p.readUint(ctx, slot, uint32(router.CmdNumDevices)); err != nil {
			return nil, err
		}
		if r.DeviceNames, err = p.readFile(ctx, slot, uint32(router.CmdDeviceNamesFile)); err != nil {
			return nil, err
		}
	}

	for m := uint32(1); m <= matrices.Count; m++ {
		mx, err := p.readMatrix(ctx, slot, matrices, m)
		if err != nil {
			return nil, err
		}
		r.Matrices = append(r.Matrices, mx)
	}
	return r, nil
}

// maxInterfaceVersion bounds what can be believed as a version.
//
// Revisions are document revisions and there have been thirteen. A node
// answering with a large number is answering about something else, and
// following it would mean reading a command space that does not exist.
const maxInterfaceVersion = 1000

// describeValue renders what a node answered with, for the message that says
// why it was not taken for a router.
func describeValue(v codec.Value) string {
	switch {
	case v.Mode.Has(codec.ModeString):
		return fmt.Sprintf("the string %q", v.Text)
	case v.Mode.Has(codec.ModeData):
		return fmt.Sprintf("%d bytes of data", len(v.Data))
	case v.Mode.Has(codec.ModeValue):
		return fmt.Sprintf("the number %d", v.Val)
	default:
		return "nothing at all"
	}
}

// FindRouter looks for a routing interface on any node the device enumerated.
//
// Probing is safe because a node without one refuses command 100 rather than
// misbehaving, and it is necessary because nothing in a device list says which
// node is the panel driver: on the vendor Centra the matrices are their own
// nodes and neither of them serves the interface.
func (p *Plugin) FindRouter(ctx context.Context) (*RouterInterface, error) {
	t, err := p.nodes(ctx)
	if err != nil {
		return nil, err
	}

	// The table always holds at least the device itself, so there is always
	// something to probe and always a reason to report when none of them
	// answers.
	var last error
	for slot := range t.addrs {
		r, err := p.RouterAt(ctx, slot)
		if err == nil {
			p.log.Debug("rollcall: found a routing interface",
				"slot", slot, "addr", r.Addr.String(), "version", r.Version,
				"matrices", len(r.Matrices))
			return r, nil
		}
		last = err
	}
	return nil, fmt.Errorf("rollcall: none of the %d nodes serves a routing interface: %w",
		len(t.addrs), last)
}

// readMatrix reads one matrix's table and everything below it.
func (p *Plugin) readMatrix(ctx context.Context, slot int,
	matrices router.Table, m uint32) (RouterMatrix, error) {

	mx := RouterMatrix{Number: m}
	field := func(off uint32) (uint32, bool) {
		c, ok := matrices.Field(m, off)
		return uint32(c), ok
	}

	// The first field of a table is always addressable: m is inside the count
	// the controller published, and offset zero is inside any step.
	name, _ := field(router.OffMatrixName)

	var err error
	if mx.Name, err = p.readString(ctx, slot, name); err != nil {
		return mx, err
	}

	levels, err := p.readTableAt(ctx, slot, matrices, m,
		router.OffNumLevels, router.OffLevelBase, router.OffLevelStep)
	if err != nil {
		return mx, err
	}
	if mx.SrcAssocs, err = p.readTableAt(ctx, slot, matrices, m,
		router.OffNumSrcAssocs, router.OffSrcAssocBase, router.OffSrcAssocStep); err != nil {
		return mx, err
	}
	if mx.DstAssocs, err = p.readTableAt(ctx, slot, matrices, m,
		router.OffNumDstAssocs, router.OffDstAssocBase, router.OffDstAssocStep); err != nil {
		return mx, err
	}

	for _, f := range []struct {
		off  uint32
		into *RouterFile
	}{
		{router.OffAssocNames8File, &mx.AssocNames8},
		{router.OffAssocNames32File, &mx.AssocNames},
		{router.OffAssocNamesAltFile, &mx.AssocNamesAlt},
		{router.OffAssocMappingsFile, &mx.AssocMappings},
	} {
		cmd, _ := field(f.off)
		if *f.into, err = p.readFile(ctx, slot, cmd); err != nil {
			return mx, err
		}
	}

	if controller, ok := field(router.OffControllerNumber); ok {
		n, err := p.readInt(ctx, slot, controller)
		if err != nil {
			return mx, err
		}
		mx.Controller = n
	}

	for v := uint32(1); v <= levels.Count; v++ {
		level, err := p.readLevel(ctx, slot, levels, v)
		if err != nil {
			return mx, err
		}
		mx.Levels = append(mx.Levels, level)
	}
	return mx, nil
}

// readLevel reads one level's table.
func (p *Plugin) readLevel(ctx context.Context, slot int,
	levels router.Table, v uint32) (RouterLevel, error) {

	lv := RouterLevel{Number: v}

	// Offset zero again, so it is there.
	name, _ := levels.Field(v, router.OffLevelName)

	var err error
	if lv.Name, err = p.readString(ctx, slot, uint32(name)); err != nil {
		return lv, err
	}

	kind, _ := levels.Field(v, router.OffLevelType)
	if lv.Kind, err = p.readInt(ctx, slot, uint32(kind)); err != nil {
		return lv, err
	}

	if lv.Srcs, err = p.readTableAt(ctx, slot, levels, v,
		router.OffNumSrcs, router.OffSrcBase, router.OffSrcStep); err != nil {
		return lv, err
	}
	if lv.Dsts, err = p.readTableAt(ctx, slot, levels, v,
		router.OffNumDsts, router.OffDstBase, router.OffDstStep); err != nil {
		return lv, err
	}

	for _, f := range []struct {
		off  uint32
		into *RouterFile
	}{
		{router.OffSrcDstNames8File, &lv.Names8},
		{router.OffSrcDstNames32File, &lv.Names},
		{router.OffSrcDstNamesAltFile, &lv.NamesAlt},
		{router.OffSrcDstMCDataFile, &lv.MCData},
	} {
		cmd, _ := levels.Field(v, f.off)
		if *f.into, err = p.readFile(ctx, slot, uint32(cmd)); err != nil {
			return lv, err
		}
	}
	return lv, nil
}

// readTable reads a count, a base and a step, which is the shape every level
// of this command space has.
func (p *Plugin) readTable(ctx context.Context, slot int, count, base, step uint32) (router.Table, error) {
	n, err := p.readUint(ctx, slot, count)
	if err != nil {
		return router.Table{}, err
	}
	b, err := p.readUint(ctx, slot, base)
	if err != nil {
		return router.Table{}, err
	}
	s, err := p.readUint(ctx, slot, step)
	if err != nil {
		return router.Table{}, err
	}
	return router.Table{Base: router.Command(b), Step: s, Count: n}, nil
}

// readTableAt reads a table whose three commands sit at offsets inside another
// entity's table.
func (p *Plugin) readTableAt(ctx context.Context, slot int, outer router.Table,
	n uint32, countOff, baseOff, stepOff uint32) (router.Table, error) {

	count, ok := outer.Field(n, countOff)
	if !ok {
		return router.Table{}, fmt.Errorf(
			"rollcall: entity %d is outside the command space the router published", n)
	}
	base, _ := outer.Field(n, baseOff)
	step, _ := outer.Field(n, stepOff)
	return p.readTable(ctx, slot, uint32(count), uint32(base), uint32(step))
}

// readValue reads one command from a router node.
func (p *Plugin) readValue(ctx context.Context, slot int, command uint32) (codec.Value, error) {
	s, err := p.session(ctx, slot)
	if err != nil {
		return codec.Value{}, err
	}

	reply, err := s.Do(ctx, codec.MsgGetValue, codec.GetValue{Command: command}.AppendTo(nil))
	if err != nil {
		return codec.Value{}, fmt.Errorf("rollcall: command %d: %w", command, err)
	}
	v, err := codec.DecodeValue(reply.Payload)
	if err != nil {
		return codec.Value{}, fmt.Errorf("rollcall: command %d: %w", command, err)
	}
	return v, nil
}

func (p *Plugin) readUint(ctx context.Context, slot int, command uint32) (uint32, error) {
	n, err := p.readInt(ctx, slot, command)
	if err != nil {
		return 0, err
	}
	if n < 0 {
		// A count or a base cannot be negative. A controller that says so is
		// describing a command space that does not exist, and following it
		// would read whatever happens to live at the wrapped-around number.
		return 0, fmt.Errorf("rollcall: command %d returned %d, which cannot be a count or an address",
			command, n)
	}
	return uint32(n), nil
}

func (p *Plugin) readInt(ctx context.Context, slot int, command uint32) (int32, error) {
	v, err := p.readValue(ctx, slot, command)
	if err != nil {
		return 0, err
	}
	return v.Val, nil
}

func (p *Plugin) readString(ctx context.Context, slot int, command uint32) (string, error) {
	v, err := p.readValue(ctx, slot, command)
	if err != nil {
		return "", err
	}
	return v.Text, nil
}

// readFile reads a filename and its checksum, which the controller carries as
// Data Transfer Params rather than as a string.
//
// A command that names no file is not an error: a level with no alternate
// names simply has none, and the empty value says so.
func (p *Plugin) readFile(ctx context.Context, slot int, command uint32) (RouterFile, error) {
	v, err := p.readValue(ctx, slot, command)
	if err != nil {
		return RouterFile{}, err
	}
	if len(v.Data) == 0 {
		return RouterFile{}, nil
	}

	items, err := dtp.Decode(v.Data)
	if err != nil {
		return RouterFile{}, fmt.Errorf("rollcall: command %d: %w", command, err)
	}

	var f RouterFile
	for _, it := range items {
		switch it.Type {
		case dtp.TypeString:
			f.Name = it.Str
		case dtp.TypeUint:
			f.CRC = it.Uint
		}
	}
	return f, nil
}
