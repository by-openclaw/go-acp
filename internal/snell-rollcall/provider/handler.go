package rollcall

import (
	"fmt"
	"sort"

	"dhs/internal/snell-rollcall/codec"
	"dhs/internal/snell-rollcall/session"
)

// This is the answering side. Every method here runs on a link's read loop, so
// none of them blocks: a slow answer stalls every session on that link,
// including the keepalives a client uses to decide whether we are alive.

// maxSessions is how many a link may hold before the provider says it is busy.
//
// A real unit has few, and running out is what stops it answering anybody, so
// refusing early with Busy is kinder than accepting until something breaks:
// Busy is worth retrying and a broken link is not.
const maxSessions = 32

// Call decides whether to accept a session.
func (p *Provider) Call(l *session.Link, req codec.Frame, conn codec.Connect) error {
	st := p.linkState(l)
	if st == nil {
		return session.RefuseNack("link is closing")
	}

	// Services are all-or-nothing: a client that asks for something we do not
	// serve is refused outright rather than granted a subset, because a subset
	// would leave it believing it had something it does not.
	// Against what this node offers, not what the frame does: an old card and
	// a new one in the same rack answer differently, and a session belongs to
	// the node it was opened on.
	if missing := conn.Services &^ p.servedAt(req.Dst.Port); missing != 0 {
		p.fire(EventUnservedService, fmt.Sprintf(
			"%s asked for %s, which this device does not serve", req.Src, missing))
		return session.RefuseNack("service not available: " + missing.String())
	}

	if !conn.UserLevel.Valid() {
		p.fire(EventInvalidUserLevel, fmt.Sprintf(
			"%s asked for user level %d, which is not one of the four", req.Src, conn.UserLevel))
		return session.RefuseNack("invalid user level")
	}

	st.mu.Lock()
	full := len(st.sessions) >= maxSessions
	st.mu.Unlock()
	if full {
		return session.RefuseBusy("no sessions left")
	}

	s, err := session.Accept(l, req, conn)
	if err != nil {
		return err
	}

	st.mu.Lock()
	st.sessions[s.LocalIndex()] = s
	st.mu.Unlock()

	p.log.Debug("rollcall: session opened",
		"session", s.Describe(),
		"port", fmt.Sprintf("%02X", req.Dst.Port),
		"generation", generationName(conn.Services))
	return nil
}

// checkGeneration notices a request that does not belong to its session.
//
// A session negotiates one generation and keeps it. The two use different
// message numbers for the same job, so a 32-bit message on a 16-bit session is
// a client asking for a reply in a shape it did not negotiate.
//
// It is absorbed rather than refused: the message is well formed, answering it
// costs nothing, and refusing would break a client that is otherwise working.
// What it must not do is pass silently, which is what it did before — leaving
// no trace in a log an operator could read.
func (p *Provider) checkGeneration(s *session.Session, req codec.Frame) {
	want := codec.Gen16
	if s.Uses32Bit() {
		want = codec.Gen32
	}
	got := req.Type.Generation()
	if got == codec.GenAny || got == want {
		return
	}
	p.fire(EventMixedGeneration, fmt.Sprintf(
		"%s arrived on a %s session (%s)", req.Type, want, s.Describe()))
	p.log.Debug("rollcall: message of the other generation",
		"type", req.Type.String(), "session", want.String())
}

// served is every service this provider can supply.
func (p *Provider) served() codec.Service {
	return p.advertise(codec.SvcMenus | codec.SvcControl | codec.SvcDisplay |
		codec.SvcFile | codec.SvcMap | codec.SvcPorts | codec.SvcLongStr)
}

// servedAt is what one node offers. The gateway serves the frame's services;
// a card serves its own.
func (p *Provider) servedAt(port uint8) codec.Service {
	if port != 0 {
		if prt := p.model.port(port); prt != nil {
			return p.advertiseAt(port, prt.id.Services)
		}
	}
	return p.served()
}

func generationName(s codec.Service) string {
	if s.LongStrings() {
		return "32-bit"
	}
	return "16-bit"
}

// Request answers a message on an established session.
func (p *Provider) Request(s *session.Session, req codec.Frame) {
	if req.Type == codec.MsgTerm {
		// The client is finished, and it is waiting to be told we heard.
		//
		// Specification 9.4: the valid replies to SP_TERM are SP_ACK, "session
		// terminated", and SP_INVSESS, "session number invalid" — "An SP_ACK
		// command is expected from the receiver."
		//
		// This answered nothing, on the belief recorded here that a client
		// sending Term "has stopped listening". It has not. Measured against a
		// vendor Control Panel: it sends Term when a node is closed and waits
		// three seconds for the acknowledgement before giving up, every time,
		// on every node — which is the whole of the delay an operator sees
		// closing a card.
		//
		// The acknowledgement goes first and the teardown second, because a
		// session that has been torn down has nothing left to answer on.
		if err := s.Answer(codec.MsgAck, nil); err != nil {
			p.log.Debug("rollcall: could not acknowledge a session ending",
				"session", s.LocalIndex(), "err", err)
		}
		p.endSession(s)
		return
	}

	if err := p.answer(s, req); err != nil {
		if rerr := s.Refuse(err); rerr != nil {
			p.log.Debug("rollcall: could not refuse a request",
				"type", req.Type.String(), "err", rerr)
		}
	}
}

// answer handles one request, returning an error to refuse it.
func (p *Provider) answer(s *session.Session, req codec.Frame) error {
	slot := req.Dst.Port
	prt := p.model.port(slot)

	p.checkGeneration(s, req)

	switch req.Type {
	case codec.MsgKeepAlive:
		return s.Answer(codec.MsgAck, nil)

	case codec.MsgGetID:
		id, ok := p.identityOf(slot)
		if !ok {
			return session.RefuseNack("no card in that slot")
		}
		// The name was cut to the field when the model was built, so the
		// identity always encodes.
		payload, _ := id.AppendTo(nil)
		return s.Answer(codec.MsgRetID, payload)

	case codec.MsgGetStat:
		return s.Answer(codec.MsgRetStat, p.statusOf(slot).AppendTo(nil))

	case codec.MsgGetDevInfo:
		return s.Answer(codec.MsgRetDevInfo, p.deviceInfoFor(slot))

	case codec.MsgBkChnReady:
		return p.backChannel(s, req)

	case codec.MsgRepFChg, codec.MsgStopRepFChg:
		// Reporting is per session rather than per command here: a provider
		// that tracked each command separately would have to walk the whole
		// menu on every change, and every client we have seen asks for all of
		// them anyway.
		return s.Answer(codec.MsgAck, nil)

	case codec.MsgGetDispData:
		return p.displayData(s, prt, req)

	case codec.MsgGetNextPkt:
		// Not a menu message: it continues whichever transfer is open, which
		// may equally be a device list or a directory. What it may fetch was
		// decided when that transfer was opened.
		return p.nextItem(s, req)

	case codec.MsgGetFunc, codec.MsgGetMenuCount, codec.MsgGetMenuItem:
		return p.menuRequest(s, prt, req)

	case codec.MsgGetFStat, codec.MsgGetValue:
		if slot == 0 {
			// The gateway's page is what the connector is doing now, not what
			// it was doing when the tree was loaded.
			p.refreshGateway()
		}
		return p.readValue(s, prt, req)

	case codec.MsgSetParam, codec.MsgSetValue:
		return p.writeValue(s, prt, req)

	case codec.MsgGetDevList, codec.MsgGetLocDevMap:
		return p.deviceList(s, req)

	case codec.MsgFileOpen, codec.MsgFileRead, codec.MsgFileClose, codec.MsgFileDir:
		return p.fileRequest(s, req)

	default:
		// A message we do not implement is answered with InvCmd, which says
		// "I do not know this" rather than "I will not do it" (spec 9.15).
		return session.RefuseInvalidCommand()
	}
}

// Unsolicited handles a message that belongs to no session.
//
// The important one is the first GetDevInfo of a connection, which is how a
// client learns both addresses: its own, which we assign, and ours, which it
// cannot know.
func (p *Provider) Unsolicited(l *session.Link, req codec.Frame) {
	switch req.Type {
	case codec.MsgGetDevInfo:
		p.handshake(l, req)

	case codec.MsgKeepAlive:
		p.answerUnsolicited(l, req, codec.MsgAck, nil)

	case codec.MsgGetStat:
		p.answerUnsolicited(l, req, codec.MsgRetStat, p.statusOf(req.Dst.Port).AppendTo(nil))

	case codec.MsgGetID:
		// A unit says what it is without being asked to open a session first.
		// Status was already answered this way and identity is the same kind
		// of question, so refusing it was an asymmetry with nothing behind it.
		//
		// Measured: the vendor Control Panel asks a card for its identity
		// outside any session the moment it is selected in the tree. We
		// answered InvCmd, and the panel reported "Cannot retrieve the unit
		// information" for every card in the frame.
		id, ok := p.identityOf(req.Dst.Port)
		if !ok {
			p.answerUnsolicited(l, req, codec.MsgNack, nil)
			return
		}
		payload, _ := id.AppendTo(nil)
		p.answerUnsolicited(l, req, codec.MsgRetID, payload)

	case codec.MsgIam, codec.MsgTime:
		// A peer announcing itself. Nothing to answer: Iam is one of the two
		// types that may be broadcast, and a reply would go to everybody.

	case codec.MsgTerm:
		// A client ending a session we have already forgotten.

	default:
		p.answerUnsolicited(l, req, codec.MsgInvCmd, nil)
	}
}

// handshake answers the first message of a connection.
//
// The reply's destination is the address we are assigning the client. A client
// on TCP does not know its own and takes it from here, and a gateway that
// leaves it zeroed is one the vendor's tools will not talk to.
func (p *Provider) handshake(l *session.Link, req codec.Frame) {
	st := p.linkState(l)
	if st == nil {
		return
	}

	st.mu.Lock()
	assigned := st.assigned
	st.mu.Unlock()

	// Where the answer goes depends on whether the asker knows who it is.
	//
	// A client that has no address yet says so by sending from the unknown
	// index, and the reply's destination is how a gateway hands it one - that
	// is how our own consumer learns it is 0000-01-E0. A client that supplies
	// a real index is not asking for an address; it is numbering a
	// transaction, and the answer belongs at the address it asked from.
	//
	// Measured against the vendor Control Panel, which asks from
	// 0000-00-00:007D and ignores anything not addressed back to it: the
	// Centra echoes that address, we invented one, and the panel sat through
	// its five second timeout and dropped the connection without ever asking
	// us a second question.
	dst := req.Src
	if req.Src.Index == codec.IndexUnknown {
		dst = codec.Address{Unit: p.unit, Port: assigned, Index: codec.IndexUnknown}
	}

	err := l.SendFrame(codec.Frame{
		Dst:     dst,
		Src:     codec.Address{Unit: p.unit, Port: req.Dst.Port, Index: codec.IndexUnknown},
		Type:    codec.MsgRetDevInfo,
		Payload: p.deviceInfoFor(req.Dst.Port),
	})
	if err != nil {
		p.log.Debug("rollcall: handshake reply failed", "err", err)
	}
}

// answerUnsolicited replies to a message outside any session.
func (p *Provider) answerUnsolicited(l *session.Link, req codec.Frame, typ codec.PacketType, payload []byte) {
	err := l.SendFrame(codec.Frame{
		Dst:     req.Src,
		Src:     req.Dst,
		Type:    typ,
		Payload: payload,
	})
	if err != nil {
		p.log.Debug("rollcall: could not answer", "type", req.Type.String(), "err", err)
	}
}

// identityOf is what a slot says it is: the gateway on port zero, whichever
// card is fitted on any other, and nothing at all on an empty one.
func (p *Provider) identityOf(slot uint8) (codec.ID, bool) {
	if prt := p.model.port(slot); prt != nil {
		id := prt.id
		id.Services = p.advertiseAt(slot, id.Services)
		return id, true
	}
	// No special case for the gateway: it is a port like any other now, and
	// its identity is the frame's because that is what it was built with.
	return codec.ID{}, false
}

// statusOf says whether a slot holds anything.
//
// Port zero is the gateway, which is present whenever anything is answering at
// all; every other port is a card slot, and an empty one says so rather than
// refusing, because "nothing is fitted" is an answer.
func (p *Provider) statusOf(slot uint8) codec.UnitStatus {
	if slot == 0 || p.model.port(slot) != nil {
		return codec.UnitStatus{Status: codec.StatusPresent | codec.StatusOnline}
	}
	return codec.UnitStatus{}
}

// deviceInfoFor renders what a slot says about itself. Port zero is the
// gateway; every other port is whichever card is in it.
func (p *Provider) deviceInfoFor(slot uint8) []byte {
	id, ok := p.identityOf(slot)
	if !ok {
		// An empty slot still answers, with the frame's identity and a status
		// that says nothing is fitted. A client walking a frame needs an answer
		// per slot, not a gap it cannot tell from a lost message.
		id = p.model.frame
	}

	info := codec.DeviceInfo{
		ProtocolVersion: codec.ProtocolVersion,
		Address:         codec.Address{Unit: p.unit, Port: slot, Index: codec.IndexUnknown},
		ID:              id,
		Status:          p.statusOf(slot),
	}
	// The identity is built from a name truncated to the field when the model
	// was built, so this cannot fail.
	payload, _ := info.AppendTo(nil)
	return payload
}

// deviceList answers the map and port enumerations, which is how a client
// discovers what is in the frame.
func (p *Provider) deviceList(s *session.Session, req codec.Frame) error {
	// A port list is scoped to the node it was asked of.
	//
	// Only the gateway has ports. A card is a leaf, and answering a card's
	// port enquiry with the whole frame tells a client that every card
	// contains every card — which is what we did, and what a vendor Control
	// Panel walked into when it asked our matrix node what was inside it.
	//
	// The empty list is the honest answer and the specification's: a unit with
	// nothing below it reports nothing below it.
	if req.Dst.Port != 0 {
		return p.beginTransfer(s, req.Type, codec.MsgRetDevInfo, nil)
	}

	// The gateway first, then its cards.
	//
	// Measured: both devices we can read put themselves at the head of their
	// own port list — the IQ frame reports 0000-0C-00 before its cards, the
	// Centra 0000-08-00 before its units. A provider that left itself out was
	// the only thing on the network doing so, and a client enumerating it by
	// port list found cards and no controller.
	ports := append([]uint8{0}, p.model.portNumbers()...)
	sort.Slice(ports, func(i, j int) bool { return ports[i] < ports[j] })

	items := make([][]byte, 0, len(ports))
	for _, n := range ports {
		items = append(items, p.deviceInfoFor(n))
	}

	// Map and port list now carry the same thing, and both are answered.
	//
	// A vendor Control Panel builds its tree from the map alone: measured, it
	// reads the map, and a map holding one device ends its interest — no port
	// list, ever. Our consumer walks the port list. Serving the same content
	// on both means a client of either habit works, and the gateway is at the
	// head of it either way.
	return p.beginTransfer(s, req.Type, codec.MsgRetDevInfo, items)
}

// linkState returns the per-connection bookkeeping.
func (p *Provider) linkState(l *session.Link) *linkState {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.links[l]
}

// endSession forgets a session the client ended, with everything it held: the
// transfer it had open, the files it did not close, and its subscription.
func (p *Provider) endSession(s *session.Session) {
	if st := p.sessionState(s); st != nil {
		st.forget(s.LocalIndex())
	}
	s.Terminate(nil)
}

// backChannel turns pushes on or off for a session.
func (p *Provider) backChannel(s *session.Session, req codec.Frame) error {
	if len(req.Payload) != 1 {
		return session.RefuseNack("back channel state is one byte")
	}

	st := p.sessionState(s)
	if st == nil {
		return session.RefuseNack("unknown session")
	}

	var sub *subscriber
	switch req.Payload[0] {
	case codec.BackChannelDisable:
		st.setSubscribed(s, false)
	case codec.BackChannelEnable, codec.BackChannelFutureOnly:
		sub = p.subscribe(st, s)
	default:
		return session.RefuseNack("unknown back channel state")
	}

	if err := s.Answer(codec.MsgAck, nil); err != nil {
		return err
	}

	// Enabling flushes everything that has changed, which is what the
	// specification asks for and what makes a client's first screen correct
	// without it having to read every value. Asking for future changes only
	// is the other state, and it skips exactly this.
	if req.Payload[0] == codec.BackChannelEnable {
		p.wg.Add(1)
		go p.flush(sub)
	}
	return nil
}

// displayData answers a request for one of a unit's status lines.
func (p *Provider) displayData(s *session.Session, prt *port, req codec.Frame) error {
	if prt == nil {
		return session.RefuseNack("no card in that slot")
	}
	if len(req.Payload) < 2 {
		return session.RefuseNack("display line is two bytes")
	}

	n := int16(uint16(req.Payload[0])<<8 | uint16(req.Payload[1]))
	text, ok := prt.displayLine(n)
	if !ok {
		return session.RefuseNack("no such display line")
	}

	// Cut to the field on the way in, so the encode cannot refuse it.
	payload, _ := codec.Disp{Line: n, Text: codec.TruncateFixed(text, codec.MaxTextSize)}.AppendTo(nil)
	return s.Answer(codec.MsgDispData, payload)
}
