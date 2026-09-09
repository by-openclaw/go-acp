package rollcall

import (
	"context"

	"dhs/internal/snell-rollcall/codec"
	"dhs/internal/snell-rollcall/codec/router"
	"dhs/internal/snell-rollcall/session"
)

// One crosspoint, two places it is published.
//
// A level publishes its whole routing state as one command per destination —
// 10000+N for the routed source, 20000+N for the protect — and a panel drives
// and watches those. The node that serves the Full Control tables publishes
// the same state again, as a field inside each destination's table entry, and
// that is what a client reading a plant structurally sees.
//
// They are two views of one fact, so a write to either has to move both. Left
// alone they drift the moment anybody routes anything: the panel shows the
// route it just made and our own tally verb, reading the tables, still reports
// what was there before. Nothing on the wire would say which was lying.
//
// The state lives in the routerModel. The value maps on the two ports are
// projections of it, and this file is what keeps them projections.

// syncRouterWrite mirrors a write between the two views of a router's state.
//
// It runs after the write has been stored, so a refusal has already happened
// and what arrives here is a value the node accepted.
func (p *Provider) syncRouterWrite(ctx context.Context, s *session.Session, prt *port, v codec.Value) {
	switch {
	case prt.level != nil:
		p.levelWriteToTables(ctx, s, prt, v)
	case prt.router != nil:
		p.tableWriteToLevel(ctx, s, prt, v)
	}
}

// levelWriteToTables carries a panel's crosspoint or protect into the tables.
func (p *Provider) levelWriteToTables(ctx context.Context, s *session.Session, prt *port, v codec.Value) {
	lv := prt.level
	cmd := router.Command(v.Command)

	dest, isRoute := router.IsLevelRoute(cmd)
	if !isRoute {
		var isProtect bool
		dest, isProtect = router.IsLevelProtect(cmd)
		if !isProtect {
			return
		}
	}
	if dest < 1 || dest > len(lv.dests) {
		return
	}
	d := &lv.dests[dest-1]

	base, ok := lv.dstTable.Command(uint32(dest))
	if !ok {
		return
	}

	var field router.Command
	var packed uint32
	if isRoute {
		// A level names a source by its number alone: everything on this node
		// is one matrix and one level, so the pin's other two fields are the
		// level's own and a route made here is always a local one.
		d.routed = router.SourcePin{
			Matrix: lv.matrixNumber,
			Level:  lv.levelNumber,
			Source: uint16(v.Val),
		}
		field, packed = router.OffDestRoutedSrc, router.PackSourcePin(d.routed)
	} else {
		d.protect = router.ProtectState{Protected: v.Val == router.ProtectOn}
		field, packed = router.OffDestProtect, router.PackProtectState(d.protect)
	}

	p.publishRouterValue(ctx, s, p.model.tablePort(), base+field, int32(packed))
}

// tableWriteToLevel carries a structural write into the level a panel watches.
func (p *Provider) tableWriteToLevel(ctx context.Context, s *session.Session, prt *port, v codec.Value) {
	lv, dest, field, ok := prt.router.destinationFor(router.Command(v.Command))
	if !ok {
		return
	}
	d := &lv.dests[dest-1]

	var cmd router.Command
	var val int32
	switch field {
	case router.OffDestRoutedSrc:
		d.routed = router.UnpackSourcePin(uint32(v.Val))
		cmd, val = router.LvlRoute(dest), int32(d.routed.Source)
	case router.OffDestProtect:
		d.protect = router.UnpackProtectState(uint32(v.Val))
		cmd = router.LvlProtect(dest)
		val = router.ProtectOff
		if d.protect.Protected {
			val = router.ProtectOn
		}
	default:
		return
	}

	p.publishRouterValue(ctx, s, p.model.levelPort(lv), cmd, val)
}

// publishRouterValue stores a value on another node and tells its watchers.
//
// The write that caused this was answered on its own node; this is the same
// fact appearing on the other one, so every session watching that node hears
// about it. The session that made the original write is excluded because it
// has already been answered, and telling it twice about one change is how a
// panel ends up fighting its own echo.
func (p *Provider) publishRouterValue(ctx context.Context, s *session.Session,
	prt *port, cmd router.Command, val int32) {

	if prt == nil {
		return
	}
	v := codec.Value{Command: uint32(cmd), Mode: codec.ModeValue, Val: val}
	prt.seed(uint32(cmd), v)
	p.publishExcept(ctx, s, prt.number, v)
}

// destinationFor resolves a table command to the destination it belongs to.
//
// It walks the tables rather than doing arithmetic on the command, because the
// bases and steps are allocated at build time and nothing in the protocol says
// they are round numbers: a client is told each one and so is this.
func (r *routerModel) destinationFor(cmd router.Command) (*routerLevel, int, router.Command, bool) {
	for i := range r.matrices {
		m := &r.matrices[i]
		for j := range m.levels {
			lv := &m.levels[j]
			for d := 1; d <= len(lv.dests); d++ {
				base, ok := lv.dstTable.Command(uint32(d))
				if !ok {
					continue
				}
				if off := cmd - base; off < router.Command(lv.dstTable.Step) {
					return lv, d, off, true
				}
			}
		}
	}
	return nil, 0, 0, false
}

// tablePort returns the node serving the Full Control tables, if one is served.
func (m *model) tablePort() *port {
	m.mu.RLock()
	defer m.mu.RUnlock()
	for _, prt := range m.ports {
		if prt.router != nil {
			return prt
		}
	}
	return nil
}

// levelPort returns the node serving one level, if one is served.
func (m *model) levelPort(lv *routerLevel) *port {
	m.mu.RLock()
	defer m.mu.RUnlock()
	for _, prt := range m.ports {
		if prt.level == lv {
			return prt
		}
	}
	return nil
}
