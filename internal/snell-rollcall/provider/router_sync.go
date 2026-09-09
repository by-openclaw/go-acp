package rollcall

import (
	"context"
	"fmt"

	"dhs/internal/snell-rollcall/codec"
	"dhs/internal/snell-rollcall/codec/dtp"
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
	case prt.router != nil && v.Command == uint32(router.CmdAssocMakeRoute):
		p.routeByAssociation(ctx, s, prt, v)
	case prt.router != nil && v.Command == uint32(router.CmdFireSalvo):
		p.fireSalvo(ctx, s, prt, v)
	case prt.router != nil && isGroupSelect(v.Command):
		p.showWhatAGroupSelects(ctx, s, prt, v)
	case prt.id.TypeID == codec.TypeIDTielines:
		p.tielineAction(ctx, s, prt, v)
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

	if isRoute {
		// A level names a source by its number alone: everything on this node
		// is one matrix and one level, so the pin's other two fields are the
		// level's own and a route made here is always a local one.
		// A level routes within itself, so whatever cable fed this
		// destination is no longer wanted.
		if r := p.model.routerModel(); r != nil {
			r.releaseTielines(lv, dest)
		}
		d.routed = router.SourcePin{
			Matrix: lv.matrixNumber,
			Level:  lv.levelNumber,
			Source: uint16(v.Val),
		}
		p.publishRoutedSource(ctx, s, p.model.tablePort(), base+router.OffDestRoutedSrc, d.routed)
		return
	}

	d.protect = router.ProtectState{Protected: v.Val == router.ProtectOn}
	p.publishRouterValue(ctx, s, p.model.tablePort(),
		base+router.OffDestProtect, int32(router.PackProtectState(d.protect)))
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
		pin, ok := decodeRoutedSource(v)
		if !ok {
			return
		}
		d.routed = pin
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

// routerModel returns the router this device serves, if it serves one.
func (m *model) routerModel() *routerModel {
	if prt := m.tablePort(); prt != nil {
		return prt.router
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

// routeByAssociation applies a route made across levels at once.
//
// The request names a source association, a destination association and which
// of the source's levels to carry across; a camera taken to a monitor without
// its audio is one request with two bits set rather than two requests. What is
// stored on the command afterwards is the result, which is what a client reads
// back.
func (p *Provider) routeByAssociation(ctx context.Context, s *session.Session, prt *port, v codec.Value) {
	req, err := router.DecodeMakeRoute(v.Data)
	result := router.RouteBadParameters
	var moved []routedChange
	if err == nil {
		result, moved = prt.router.makeRoute(req)
	}

	// Encoding one number as parameters cannot fail, and a result that could
	// not be stored would leave the client reading the previous one as though
	// it were this one.
	body, _ := router.AppendRouteResult(nil, result)
	prt.seed(uint32(router.CmdAssocMakeRoute), codec.Value{
		Command: uint32(router.CmdAssocMakeRoute), Mode: codec.ModeData, Data: body,
	})
	// Every crosspoint the route moved is published on both views, because a
	// route made this way is the same fact as a route made one destination at
	// a time and a panel watching either has to see it. Only what moved is
	// published: a plant of a thousand destinations has no business sending a
	// thousand messages because three of them changed.
	for _, c := range moved {
		p.republishCrosspoint(ctx, s, c.level, c.dest)
	}
}

// republishCrosspoint tells both views what a destination now carries.
func (p *Provider) republishCrosspoint(ctx context.Context, s *session.Session, lv *routerLevel, dest int) {
	d := &lv.dests[dest-1]

	p.publishRouterValue(ctx, s, p.model.levelPort(lv), router.LvlRoute(dest), int32(d.routed.Source))

	if base, ok := lv.dstTable.Command(uint32(dest)); ok {
		p.publishRoutedSource(ctx, s, p.model.tablePort(),
			base+router.OffDestRoutedSrc, d.routed)
	}
}

// fireSalvo makes every route in a salvo and answers with how many it made.
//
// There is no result code, only the count. The specification says "number of
// routes made or 0 on error" and does not distinguish an empty salvo from one
// that does not exist, so neither does this.
func (p *Provider) fireSalvo(ctx context.Context, s *session.Session, prt *port, v codec.Value) {
	salvo, ok := salvoAsked(prt, v)
	var made uint32
	var moved []routedChange
	if ok {
		made, moved = prt.router.fireSalvo(salvo)
	}

	body, _ := router.SalvoFired{Salvo: salvo, Routes: made}.AppendTo(nil)
	prt.seed(uint32(router.CmdFireSalvo), codec.Value{
		Command: uint32(router.CmdFireSalvo), Mode: codec.ModeData, Data: body,
	})

	// And in words, because the routes a salvo makes are on other nodes and an
	// operator looking at this page has no other way to tell a salvo that
	// fired from one that was refused.
	p.publishText(ctx, s, prt, cmdXYLastSalvo, salvoOutcome(prt.router, salvo, made))

	for _, c := range moved {
		p.republishCrosspoint(ctx, s, c.level, c.dest)
	}
}

// publishRoutedSource stores what is routed to a destination and tells its
// watchers.
//
// A routed source is carried as Data Transfer Params rather than as a number,
// which is what the specification says and what a client reads: it has room
// for the result of the last set beside the pin, and a bare number has not.
// Publishing it as a number made every read report a destination with nothing
// routed to it, whatever had been routed.
func (p *Provider) publishRoutedSource(ctx context.Context, s *session.Session,
	prt *port, cmd router.Command, pin router.SourcePin) {

	if prt == nil {
		return
	}
	v := routedSourceValue(cmd, pin)
	prt.seed(uint32(cmd), v)
	p.publishExcept(ctx, s, prt.number, v)
}

// routedSourceValue renders a routed source as the parameters a client reads.
func routedSourceValue(cmd router.Command, pin router.SourcePin) codec.Value {
	// One number as parameters: the encode has no way to fail.
	body, _ := dtp.Append(nil, dtp.Params{dtp.Uint(router.PackSourcePin(pin))}, false)
	return codec.Value{Command: uint32(cmd), Mode: codec.ModeData, Data: body}
}

// routedValue seeds a routed source into a value map being built.
func routedValue(out map[uint32]codec.Value, cmd router.Command, pin router.SourcePin) {
	out[uint32(cmd)] = routedSourceValue(cmd, pin)
}

// decodeRoutedSource reads the pin out of a write to a destination.
//
// A client sends the pin in a uint array, with a protect id after it when the
// route is a temporary override. A bare uint is accepted too: it is what a
// reply carries, and a client that echoes one back is asking for the same
// thing in a form that cannot be misread.
func decodeRoutedSource(v codec.Value) (router.SourcePin, bool) {
	items, err := dtp.Decode(v.Data)
	if err != nil || len(items) == 0 {
		return router.SourcePin{}, false
	}
	switch {
	case items[0].Type == dtp.TypeUintArray && len(items[0].Uints) > 0:
		return router.UnpackSourcePin(items[0].Uints[0]), true
	case items[0].Type == dtp.TypeUint:
		return router.UnpackSourcePin(items[0].Uint), true
	}
	return router.SourcePin{}, false
}

// routedSourceReply builds what a crosspoint write is answered with.
//
// Specification: the reply carries the pin routed before the change, and a
// second parameter holding the result — "It is always returned when setting a
// new crosspoint." A client that reads the reply as the new state is reading
// the old one, which is why the new one arrives separately as a push.
func routedSourceReply(prt *port, command uint32, before codec.Value) (codec.Value, bool) {
	if prt.router == nil {
		return codec.Value{}, false
	}
	_, _, field, ok := prt.router.destinationFor(router.Command(command))
	if !ok || field != router.OffDestRoutedSrc {
		return codec.Value{}, false
	}

	// A destination nothing had been routed to answers with a zeroed pin,
	// which is what "nothing was there" looks like on this interface.
	pin, _ := decodeRoutedSource(before)

	// Two numbers as parameters: the encode has no way to fail.
	body, _ := dtp.Append(nil, dtp.Params{
		dtp.Uint(router.PackSourcePin(pin)),
		dtp.Uint(uint32(router.RouteOK)),
	}, false)
	return codec.Value{Command: command, Mode: codec.ModeData, Data: body}, true
}

// salvoAsked reads which salvo a client asked for, in either form it may ask.
//
// The specification carries the request as parameters, and a client that reads
// the tables sends those, naming the salvo. A panel cannot: its Fire button
// sends one, the way the vendor's own Take button does, and which salvo it
// means is whichever the list beside it has selected.
func salvoAsked(prt *port, v codec.Value) (uint32, bool) {
	if len(v.Data) > 0 {
		req, err := router.DecodeFireSalvo(v.Data)
		if err != nil {
			return 0, false
		}
		return req.Salvo, true
	}
	if sel, ok := prt.value(cmdXYSalvoSelect); ok && sel.Val > 0 {
		return uint32(sel.Val), true
	}
	if v.Val > 0 {
		return uint32(v.Val), true
	}
	return 0, false
}

// isGroupSelect reports whether a command chooses a group of a category.
func isGroupSelect(cmd uint32) bool {
	return cmd >= cmdXYGroupSelect && cmd < cmdXYGroupSelect+maxCategoryGroups
}

// showWhatAGroupSelects answers the only question a category can answer here.
//
// A group is a search string and the character index to look for it at; it
// owns no set and nothing is tagged with it. A panel's grid has no category
// key to run the match with, so the match is run here and the result written
// where the page can show it.
func (p *Provider) showWhatAGroupSelects(ctx context.Context, s *session.Session,
	prt *port, v codec.Value) {

	i := int(v.Command - cmdXYGroupSelect)
	if i < 0 || i >= len(prt.router.categories) {
		return
	}
	c := &prt.router.categories[i]
	p.publishText(ctx, s, prt, uint32(cmdXYGroupMatch+i), c.selects(prt.router, int(v.Val)))
}

// salvoOutcome says what firing a salvo did, for an operator rather than a
// client.
func salvoOutcome(r *routerModel, salvo, made uint32) string {
	name := "unknown"
	if s, ok := r.salvoAt(salvo); ok {
		name = s.name
	}
	if made == 0 {
		return fmt.Sprintf("%d %s: no routes made", salvo, name)
	}
	return fmt.Sprintf("%d %s: %d route(s) made", salvo, name, made)
}

// publishText stores a string and tells the node's watchers.
func (p *Provider) publishText(ctx context.Context, s *session.Session,
	prt *port, cmd uint32, text string) {

	v := codec.Value{Command: cmd, Mode: codec.ModeString, Text: text}
	prt.seed(cmd, v)
	p.publishExcept(ctx, s, prt.number, v)
}

// tielineAction answers the node the cables are managed from.
//
// Choosing a cable says what is holding it; clearing one puts it back. Neither
// routes anything: a tieline is taken by the controller when a route needs it
// and given back when the destination it feeds is fed by something nearer, and
// a client that could route one directly could strand a signal on a cable
// nobody is watching.
func (p *Provider) tielineAction(ctx context.Context, s *session.Session, prt *port, v codec.Value) {
	r := prt.router

	switch v.Command {
	case cmdTLSelect:
		p.publishText(ctx, s, prt, cmdTLUsedBy, r.tielineUsedBy(uint32(v.Val)))

	case cmdTLClear:
		sel, _ := prt.value(cmdTLSelect)
		p.publishText(ctx, s, prt, cmdTLStatus, r.clearTieline(uint32(sel.Val)))
		p.publishText(ctx, s, prt, cmdTLUsedBy, r.tielineUsedBy(uint32(sel.Val)))

	case cmdTLMakeRoute:
		p.publishText(ctx, s, prt, cmdTLStatus,
			"routes are made on a level or through the tables; a cable is taken for them")
	}
}
