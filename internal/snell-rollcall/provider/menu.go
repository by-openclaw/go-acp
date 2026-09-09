package rollcall

import (
	"dhs/internal/snell-rollcall/codec"
	"dhs/internal/snell-rollcall/session"
)

// A menu is served two ways, and the difference is not cosmetic.
//
// The original form is a stateful transfer: a request opens it, the server
// remembers which menu it opened, and each fetch names an offset from that
// point. The 2014 form is stateless: a count, then each item by absolute
// index. A provider has to hold the first one's state per session, which is
// what transfers below is for.

// transfer is a multi-packet exchange a session has open.
//
// It is held per session on the link that session belongs to, not in a package
// variable: two providers in one process, or two links on one provider, would
// otherwise overwrite each other's transfers whenever their session indices
// happened to collide.
type transfer struct {
	items [][]byte
	typ   codec.PacketType
}

// beginTransfer answers a request with a block header and remembers the items
// so the fetches that follow can be served.
func (p *Provider) beginTransfer(s *session.Session, req codec.PacketType,
	itemType codec.PacketType, items [][]byte) error {

	if st := p.sessionState(s); st != nil {
		st.setTransfer(s.LocalIndex(), &transfer{items: items, typ: itemType})
	}

	hdr := codec.BlockHeader{
		PktType: req,
		Count:   uint16(len(items)),
		MaxSize: codec.MaxPayload,
	}
	return s.Answer(codec.MsgBlockHeader, hdr.AppendTo(nil))
}

// nextItem serves one item of an open transfer.
func (p *Provider) nextItem(s *session.Session, req codec.Frame) error {
	next, err := codec.DecodeGetNext(req.Payload)
	if err != nil {
		return session.RefuseNack("malformed fetch")
	}

	st := p.sessionState(s)
	if st == nil {
		return session.RefuseNack("unknown session")
	}
	t := st.transfer(s.LocalIndex())
	if t == nil {
		return session.RefuseNack("no transfer is open")
	}
	if int(next.Index) >= len(t.items) {
		return session.RefuseNack("past the end of the transfer")
	}
	return s.Answer(t.typ, t.items[next.Index])
}

// menuRequest serves both generations' menu messages.
func (p *Provider) menuRequest(s *session.Session, prt *port, req codec.Frame) error {
	if prt == nil {
		return session.RefuseNack("no card in that slot")
	}
	if !s.Services().Has(codec.SvcMenus) {
		return session.RefuseNack("this session did not ask for the menu service")
	}

	switch req.Type {
	case codec.MsgGetFunc:
		return p.menuBlock(s, prt)
	case codec.MsgGetMenuCount:
		return p.menuCount(s, prt, req)
	default:
		return p.menuItem(s, prt, req)
	}
}

// menuBlock opens a 16-bit menu transfer.
func (p *Provider) menuBlock(s *session.Session, prt *port) error {
	lines := prt.menu(false)

	items := make([][]byte, 0, len(lines))
	for _, l := range lines {
		// Neither conversion can fail here: menu(false) has already replaced
		// every number that does not fit the older generation and stopped at
		// the last index it can address, and both projections truncate labels
		// rather than refusing them.
		fn, _ := l.menuItem().ToFunc()
		payload, _ := fn.AppendTo(nil)
		items = append(items, payload)
	}
	return p.beginTransfer(s, codec.MsgGetFunc, codec.MsgRetFunc, items)
}

// menuCount answers how many lines hang below a base index.
//
// The base is echoed rather than implied, so a client with several requests
// outstanding can match the answer to the question without sequence state.
func (p *Provider) menuCount(s *session.Session, prt *port, req codec.Frame) error {
	r, err := codec.DecodeMenuReq(req.Payload)
	if err != nil {
		return session.RefuseNack("malformed menu request")
	}

	n := prt.menuLen(true)
	count := uint32(0)
	if int(r.MenuIndex) < n {
		count = uint32(n) - r.MenuIndex
	}

	size := codec.MenuSize{MenuIndex: r.MenuIndex, MenuCount: count}
	return s.Answer(codec.MsgRetMenuCount, size.AppendTo(nil))
}

// menuItem answers one line by absolute index.
func (p *Provider) menuItem(s *session.Session, prt *port, req codec.Frame) error {
	r, err := codec.DecodeMenuReq(req.Payload)
	if err != nil {
		return session.RefuseNack("malformed menu request")
	}

	// One line, not the whole menu: a walk asks for every line in turn, and
	// copying the menu to answer for one of them is what made a walk of a
	// large level cost gigabytes.
	l, ok := prt.lineAt(r.MenuIndex, true)
	if !ok {
		return session.RefuseNack("past the end of the menu")
	}

	// The label was cut to the long-string ceiling when the model was built,
	// so the only encode this could refuse cannot arise.
	payload, _ := l.menuItem().AppendTo(nil)
	return s.Answer(codec.MsgRetMenuItem, payload)
}
