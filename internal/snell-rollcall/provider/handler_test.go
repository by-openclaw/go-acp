package rollcall

import (
	"context"
	"errors"
	"testing"
	"time"

	"dhs/internal/clock"
	"dhs/internal/snell-rollcall/codec"
	"dhs/internal/snell-rollcall/session"
)

func TestCallGrantsTheServicesItServes(t *testing.T) {
	s := newServed(t, testTree())

	sess := s.open(0, codec.SvcMenus|codec.SvcControl|codec.SvcLongStr)
	if !sess.Uses32Bit() {
		t.Error("a session that asked for long strings should have them")
	}
	if sess.RemoteIndex() == codec.IndexUnknown {
		t.Error("the acknowledgement should have carried the server's index")
	}
}

func TestCallRefusesAServiceItDoesNotServe(t *testing.T) {
	s := newServed(t, testTree())

	// Log is not among the services this provider supplies, and services are
	// all-or-nothing: the call is refused rather than partly granted.
	_, err := s.tryOpen(0, codec.SvcMenus|codec.SvcLogging, codec.LevelSupervisor)
	if err == nil {
		t.Fatal("a call for an unserved service should be refused")
	}
	if !hasEvent(s.p, EventUnservedService) {
		t.Error("the refusal should have been recorded as a compliance event")
	}
}

func TestCallRefusesAnInvalidUserLevel(t *testing.T) {
	s := newServed(t, testTree())

	// Our own client refuses this before it reaches the wire, so the frame is
	// built by hand: a server cannot assume every client checks.
	payload, err := codec.Connect{
		Services:  codec.SvcMenus,
		UserLevel: codec.UserLevel(9),
		Caller:    callerID(),
	}.AppendTo(nil)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}

	if got := s.raw(codec.MsgCall, payload); got.Type != codec.MsgNack {
		t.Errorf("answered %s, want Nack", got.Type)
	}
	if !hasEvent(s.p, EventInvalidUserLevel) {
		t.Error("the level should have been recorded as a compliance event")
	}
}

func TestCallWithAMalformedPayloadIsRefused(t *testing.T) {
	s := newServed(t, testTree())

	if got := s.raw(codec.MsgCall, []byte{0x01}); got.Type != codec.MsgNack {
		t.Errorf("answered %s, want Nack", got.Type)
	}
}

func TestCallSaysBusyWhenTheSessionsAreGone(t *testing.T) {
	s := newServed(t, testTree())

	for i := 0; i < maxSessions; i++ {
		s.open(0, codec.SvcMenus)
	}

	_, err := s.tryOpen(0, codec.SvcMenus, codec.LevelSupervisor)
	if err == nil {
		t.Fatal("a call past the limit should be refused")
	}
	// Busy is not the same refusal as Nack: it says the call is worth retrying.
	var perr *session.ProtocolError
	if !errors.As(err, &perr) || perr.Type != codec.MsgBusy {
		t.Errorf("refused with %v, want Busy", err)
	}
}

func TestCallOnAClosingLinkIsRefused(t *testing.T) {
	s := newServed(t, testTree())

	// The link is gone from the provider's map, which is what a connection
	// being torn down looks like from inside the handler.
	s.p.mu.Lock()
	s.p.links = map[*session.Link]*linkState{}
	s.p.mu.Unlock()

	if _, err := s.tryOpen(0, codec.SvcMenus, codec.LevelSupervisor); err == nil {
		t.Fatal("a call on a link that is closing should be refused")
	}
}

func TestKeepaliveIsAnsweredOnASession(t *testing.T) {
	s := newServed(t, testTree())
	sess := s.open(0, codec.SvcMenus)

	if got := do(t, sess, codec.MsgKeepAlive, nil); got.Type != codec.MsgAck {
		t.Errorf("keepalive answered with %s, want Ack", got.Type)
	}
}

func TestKeepaliveIsAnsweredOutsideASession(t *testing.T) {
	s := newServed(t, testTree())

	if got := s.raw(codec.MsgKeepAlive, nil); got.Type != codec.MsgAck {
		t.Errorf("answered with %s, want Ack", got.Type)
	}
}

func TestUnitStatusReportsWhatIsInTheSlot(t *testing.T) {
	s := newServed(t, testTree())

	sess := s.open(1, codec.SvcMenus)
	st, err := codec.DecodeUnitStatus(do(t, sess, codec.MsgGetStat, nil).Payload)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if st.Status&codec.StatusPresent == 0 {
		t.Errorf("slot 1 reported %v, want present", st.Status)
	}

	empty := s.open(0x40, codec.SvcMenus)
	st, err = codec.DecodeUnitStatus(do(t, empty, codec.MsgGetStat, nil).Payload)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if st.Status != 0 {
		t.Errorf("an empty slot reported %v, want nothing", st.Status)
	}
}

func TestIdentityComesFromTheSlot(t *testing.T) {
	s := newServed(t, testTree())

	sess := s.open(2, codec.SvcMenus)
	id, err := codec.DecodeID(do(t, sess, codec.MsgGetID, nil).Payload)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if id.Name != "card2" {
		t.Errorf("slot 1 identified as %q, want card2", id.Name)
	}

	empty := s.open(0x40, codec.SvcMenus)
	if got := refused(t, empty, codec.MsgGetID, nil); got.Type != codec.MsgNack {
		t.Errorf("an empty slot answered %s, want Nack", got.Type)
	}
}

func TestDeviceInfoOnASessionNamesTheSlot(t *testing.T) {
	s := newServed(t, testTree())
	sess := s.open(2, codec.SvcMenus)

	info, err := codec.DecodeDeviceInfo(do(t, sess, codec.MsgGetDevInfo, nil).Payload)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if info.Address.Port != 2 {
		t.Errorf("device info for port %d, want 2", info.Address.Port)
	}
	if info.ID.Name != "card2" {
		t.Errorf("name = %q, want card2", info.ID.Name)
	}
}

func TestDeviceInfoForAnEmptySlotStillAnswers(t *testing.T) {
	s := newServed(t, testTree())
	sess := s.open(0x40, codec.SvcMenus)

	// A client walking a frame needs an answer per slot. An empty one says so
	// in its status rather than by refusing, which is indistinguishable from a
	// lost message.
	info, err := codec.DecodeDeviceInfo(do(t, sess, codec.MsgGetDevInfo, nil).Payload)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if info.Status.Status&codec.StatusPresent != 0 {
		t.Errorf("an empty slot reported %v, want nothing fitted", info.Status.Status)
	}
	if info.Address.Port != 0x40 {
		t.Errorf("device info for port %02X, want 40", info.Address.Port)
	}
}

func TestDeviceListEnumeratesTheFrame(t *testing.T) {
	s := newServed(t, testTree())
	sess := s.open(0, codec.SvcMenus|codec.SvcMap)

	var names []string
	err := session.Walk(context.Background(), sess, codec.MsgGetDevList, nil,
		func(_ int, f codec.Frame) error {
			info, err := codec.DecodeDeviceInfo(f.Payload)
			if err != nil {
				return err
			}
			names = append(names, info.ID.Name)
			return nil
		})
	if err != nil {
		t.Fatalf("walk: %v", err)
	}
	if len(names) != 2 || names[0] != "card1" || names[1] != "card2" {
		t.Errorf("device list = %v, want [card1 card2]", names)
	}
}

// The map is what a client can reach through us: the gateway and its cards.
//
// It used to name the gateway alone, on the reading that a client walks the
// map for units and then asks the unit it found for a port list. Our consumer
// does exactly that, so this agreed with itself for a long time. The vendor
// Control Panel does not: measured from its own traffic, it reads the map, and
// a map holding one device ends its interest — no port list, ever, just
// keepalives against an empty tree.
func TestTheDeviceMapNamesTheGatewayAndItsCards(t *testing.T) {
	s := newServed(t, testTree())
	sess := s.open(0, codec.SvcMenus|codec.SvcMap)

	var names []string
	err := session.Walk(context.Background(), sess, codec.MsgGetLocDevMap, nil,
		func(_ int, f codec.Frame) error {
			info, err := codec.DecodeDeviceInfo(f.Payload)
			if err != nil {
				return err
			}
			names = append(names, info.ID.Name)
			return nil
		})
	if err != nil {
		t.Fatalf("walk: %v", err)
	}

	if len(names) != 3 {
		t.Fatalf("map = %v, want the gateway and both cards", names)
	}
	if names[0] != "dhs rollcall" {
		t.Errorf("map[0] = %q, want the gateway first", names[0])
	}
	if names[1] != "card1" || names[2] != "card2" {
		t.Errorf("map = %v, want the cards after it", names)
	}
}

func TestAnUnimplementedMessageIsInvalidCommand(t *testing.T) {
	s := newServed(t, testTree())
	sess := s.open(0, codec.SvcMenus)

	// Not "I will not", but "I do not know this" — the two are different
	// answers and a client treats them differently.
	if got := refused(t, sess, codec.MsgGetTime, nil); got.Type != codec.MsgInvCmd {
		t.Errorf("answered %s, want InvCmd", got.Type)
	}
}

func TestAnUnknownUnsolicitedMessageIsInvalidCommand(t *testing.T) {
	s := newServed(t, testTree())

	if got := s.raw(codec.MsgGetTime, nil); got.Type != codec.MsgInvCmd {
		t.Errorf("answered %s, want InvCmd", got.Type)
	}
}

func TestUnitStatusIsAnsweredOutsideASession(t *testing.T) {
	s := newServed(t, testTree())

	if got := s.raw(codec.MsgGetStat, nil); got.Type != codec.MsgRetStat {
		t.Errorf("answered %s, want RetStat", got.Type)
	}
}

func TestAnnouncementsAreNotAnswered(t *testing.T) {
	s := newServed(t, testTree())

	// Iam and Time may be broadcast, so answering one would send a reply to
	// everybody; a Term for a session we have already forgotten is nothing
	// either. None of them may produce a frame.
	//
	// Proving a negative without waiting: the keepalive after them is answered,
	// and the link's read loop is sequential, so if its acknowledgement is the
	// next frame to arrive then nothing was sent for the three before it.
	for _, typ := range []codec.PacketType{codec.MsgIam, codec.MsgTime, codec.MsgTerm} {
		s.send(typ, nil)
	}

	if got := s.raw(codec.MsgKeepAlive, nil); got.Type != codec.MsgAck {
		t.Errorf("an announcement was answered with %s", got.Type)
	}
}

func TestTermEndsTheSessionOnBothSides(t *testing.T) {
	s := newServed(t, testTree())
	sess := s.open(0, codec.SvcMenus)
	index := sess.RemoteIndex()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := sess.Term(ctx, codec.TermUser, "done"); err != nil {
		t.Fatalf("term: %v", err)
	}

	// A keepalive after it is the barrier: the read loop is sequential, so an
	// answer to this one proves the Term before it has been handled.
	s.raw(codec.MsgKeepAlive, nil)

	// The server drops its half without answering: a client that has said
	// goodbye is not listening, and a server that keeps its half is how a unit
	// runs out of sessions.
	st := s.p.linkState(providerLink(t, s.p))
	st.mu.Lock()
	_, held := st.sessions[index]
	st.mu.Unlock()

	if held {
		t.Error("the server kept a session the client ended")
	}
}

func TestDisplayLinesAreServedAndPushed(t *testing.T) {
	s := newServed(t, testTree())

	if err := s.p.SetDisplay(context.Background(), 1, 0, "OK"); err != nil {
		t.Fatalf("SetDisplay: %v", err)
	}

	sess := s.open(1, codec.SvcMenus|codec.SvcDisplay)
	reply := do(t, sess, codec.MsgGetDispData, []byte{0, 0})
	d, err := codec.DecodeDisp(reply.Payload)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if d.Text != "OK" {
		t.Errorf("display line 0 = %q, want OK", d.Text)
	}

	// A line nobody has set has no text: an empty string and a line that does
	// not exist are different answers.
	if got := refused(t, sess, codec.MsgGetDispData, []byte{0, 3}); got.Type != codec.MsgNack {
		t.Errorf("an unset line answered %s, want Nack", got.Type)
	}
}

func TestDisplayRefusals(t *testing.T) {
	s := newServed(t, testTree())
	sess := s.open(1, codec.SvcDisplay|codec.SvcMenus)

	if got := refused(t, sess, codec.MsgGetDispData, []byte{0}); got.Type != codec.MsgNack {
		t.Errorf("a one-byte line number answered %s, want Nack", got.Type)
	}

	empty := s.open(0x40, codec.SvcDisplay|codec.SvcMenus)
	if got := refused(t, empty, codec.MsgGetDispData, []byte{0, 0}); got.Type != codec.MsgNack {
		t.Errorf("an empty slot answered %s, want Nack", got.Type)
	}
}

func TestSetDisplayRefusesAnEmptySlot(t *testing.T) {
	p := New(testDeps(clock.NewFake(time.Time{})), testTree())
	if err := p.SetDisplay(context.Background(), 0x40, 0, "x"); err == nil {
		t.Fatal("setting a display line on a slot with no card should fail")
	}
}

func TestGenerationName(t *testing.T) {
	if got := generationName(codec.SvcLongStr); got != "32-bit" {
		t.Errorf("long strings named %q", got)
	}
	if got := generationName(codec.SvcMenus); got != "16-bit" {
		t.Errorf("without long strings named %q", got)
	}
}

// send writes a frame that belongs to no session, the way a client does before
// it has one.
func (s *served) send(typ codec.PacketType, payload []byte) {
	s.t.Helper()

	err := s.cl.SendFrame(codec.Frame{
		Dst:     addrOf(s.cl.RemoteAddress(), codec.IndexUnknown),
		Src:     addrOf(s.cl.LocalAddress(), codec.IndexUnknown),
		Type:    typ,
		Payload: payload,
	})
	if err != nil {
		s.t.Fatalf("send %s: %v", typ, err)
	}
}

// rawTo sends a frame outside any session, addressed at one port, which is how
// a panel asks a card about itself before opening anything.
func (s *served) rawTo(port uint8, typ codec.PacketType, payload []byte) codec.Frame {
	s.t.Helper()

	dst := addrOf(s.cl.RemoteAddress(), codec.IndexUnknown)
	dst.Port = port
	err := s.cl.SendFrame(codec.Frame{
		Dst:     dst,
		Src:     addrOf(s.cl.LocalAddress(), codec.IndexUnknown),
		Type:    typ,
		Payload: payload,
	})
	if err != nil {
		s.t.Fatalf("send %s to port %02X: %v", typ, port, err)
	}

	deadline := time.After(5 * time.Second)
	for {
		select {
		case f := <-s.cl.Unsolicited():
			if f.Type == codec.MsgIam {
				continue
			}
			return f
		case <-deadline:
			s.t.Fatalf("%s to port %02X went unanswered", typ, port)
			return codec.Frame{}
		}
	}
}

// raw sends a frame outside any session and returns what came back.
func (s *served) raw(typ codec.PacketType, payload []byte) codec.Frame {
	s.t.Helper()

	s.send(typ, payload)

	// An announcement is not an answer. Iam is broadcast to everybody and
	// arrives on the same channel as a reply, so a client that took the first
	// frame it saw would answer its own request with somebody's presence.
	deadline := time.After(5 * time.Second)
	for {
		select {
		case f := <-s.cl.Unsolicited():
			if f.Type == codec.MsgIam {
				continue
			}
			return f
		case <-deadline:
			s.t.Fatalf("%s went unanswered", typ)
			return codec.Frame{}
		}
	}
}

// hasEvent reports whether a compliance event of that kind was recorded.
func hasEvent(p *Provider, name string) bool {
	for _, e := range p.ComplianceEvents() {
		if e.Name == name {
			return true
		}
	}
	return false
}

// providerLink returns the one link a served provider holds.
func providerLink(t *testing.T, p *Provider) *session.Link {
	t.Helper()

	p.mu.RLock()
	defer p.mu.RUnlock()
	for l := range p.links {
		return l
	}
	t.Fatal("the provider has no link")
	return nil
}

// addrOf stamps an index onto an address, which a raw frame needs.
func addrOf(a codec.Address, index int16) codec.Address {
	a.Index = index
	return a
}

// A unit says what it is without being asked to open a session first. The
// vendor Control Panel asks a card for its identity outside any session the
// moment it is selected in the tree; answering InvCmd made it report "Cannot
// retrieve the unit information" for every card in the frame.
func TestIdentityIsAnsweredOutsideASession(t *testing.T) {
	s := newServed(t, testTree())

	got := s.rawTo(1, codec.MsgGetID, nil)
	if got.Type != codec.MsgRetID {
		t.Fatalf("a card answered %s, want RetID", got.Type)
	}
	id, err := codec.DecodeID(got.Payload)
	if err != nil {
		t.Fatalf("DecodeID: %v", err)
	}
	if id.Name != "card1" {
		t.Errorf("identity = %q, want card1", id.Name)
	}
}

func TestIdentityOfAnEmptySlotOutsideASession(t *testing.T) {
	// Nothing fitted is a refusal, not "I do not know this message".
	s := newServed(t, testTree())

	if got := s.rawTo(200, codec.MsgGetID, nil); got.Type != codec.MsgNack {
		t.Errorf("an empty slot answered %s, want Nack", got.Type)
	}
}
