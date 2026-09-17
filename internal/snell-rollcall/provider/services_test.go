package rollcall

import (
	"context"
	"testing"
	"time"

	"dhs/internal/snell-rollcall/codec"
	"dhs/internal/snell-rollcall/codec/router"
	"dhs/internal/snell-rollcall/session"
)

// What a session is told, and what it is not.
//
// A client negotiates services and is answered within them. Sending it
// anything else gives it a message it has no way to place: measured against a
// vendor Control Panel, which answered a value pushed at its map session with
// INVSESS every time.

func TestAValueGoesOnlyToASessionThatAskedForValues(t *testing.T) {
	s := newServed(t, testTree())

	control := s.open(firstCardPort, codec.SvcMenus|codec.SvcControl|codec.SvcLongStr)
	mapOnly := s.open(0, codec.SvcMap)

	// Both enable the back channel. Enabling is not a claim to be told
	// everything: it says where to send what this session's services cover.
	for _, sess := range []*session.Session{control, mapOnly} {
		if _, err := sess.Do(context.Background(), codec.MsgBkChnReady,
			[]byte{codec.BackChannelFutureOnly}); err != nil {
			t.Fatalf("back channel: %v", err)
		}
	}

	// The map session asked for the map, so no value is going to it.
	if got := s.p.slotSubscribers(0, codec.SvcControl); len(got) != 0 {
		t.Errorf("%d session(s) without the control service were listed for a value push", len(got))
	}
	// It is still a subscriber for what it did ask for.
	if len(s.p.slotSubscribers(0, 0)) == 0 {
		t.Error("enabling the back channel registered nothing at all")
	}
	// And the control session is told about its own port.
	if len(s.p.slotSubscribers(firstCardPort, codec.SvcControl)) != 1 {
		t.Error("the control session was not listed for values on its own node")
	}
}

func TestOnlyTheGatewayHasPorts(t *testing.T) {
	// A card is a leaf. Answering its port enquiry with the frame tells a
	// client that every card contains every card, which is what a vendor
	// Control Panel walked into when it asked our matrix what was inside it.
	//
	// A list is a transfer, so what says how many there are is the header.
	s := newServed(t, routerTree(4, 4))

	count := func(sess *session.Session) uint16 {
		t.Helper()
		reply, err := sess.Do(context.Background(), codec.MsgGetDevList, []byte{1, 0})
		if err != nil {
			t.Fatalf("port list: %v", err)
		}
		hdr, err := codec.DecodeBlockHeader(reply.Payload)
		if err != nil {
			t.Fatalf("block header: %v", err)
		}
		return hdr.Count
	}

	gw := s.open(0, codec.SvcPorts)
	if n := count(gw); n == 0 {
		t.Error("the gateway listed nothing below it")
	}

	// Every node that is not the gateway reports nothing below it, which is
	// the honest answer and the specification's.
	for _, port := range []uint8{firstCardPort, firstCardPort + 1, firstCardPort + 2} {
		sess := s.open(port, codec.SvcMenus|codec.SvcControl|codec.SvcLongStr)
		if n := count(sess); n != 0 {
			t.Errorf("node at port %d listed %d device(s) as being inside it", port, n)
		}
	}
}

func TestALineBeyondTheMenu(t *testing.T) {
	s := newServed(t, routerTree(4, 4))
	prt := s.p.model.port(firstCardPort + 1)

	if _, ok := prt.lineAt(uint32(prt.menuLen(true)), true); ok {
		t.Error("a line past the end of the menu was answered")
	}
}

func TestTheOlderGenerationSeesTheSameMenuLineForLine(t *testing.T) {
	// lineAt and menuLen answer per line what menu answers in bulk, and the
	// two must not disagree: one serves a walk and the other a transfer, and a
	// client that used both would see two different devices.
	s := newServed(t, routerTree(4, 4))
	prt := s.p.model.port(firstCardPort + 1)

	for _, longStrings := range []bool{true, false} {
		bulk := prt.menu(longStrings)
		if got := prt.menuLen(longStrings); got != len(bulk) {
			t.Fatalf("longStrings=%v: menuLen says %d, the menu has %d",
				longStrings, got, len(bulk))
		}
		for i := range bulk {
			one, ok := prt.lineAt(uint32(i), longStrings)
			if !ok {
				t.Fatalf("longStrings=%v: line %d is missing", longStrings, i)
			}
			if one != bulk[i] {
				t.Errorf("longStrings=%v: line %d differs between the two paths",
					longStrings, i)
			}
		}
	}
}

func TestACommandTooBigForTheOlderGenerationIsWithheldNotTruncated(t *testing.T) {
	// A level's direct routing lives at 10000 and up, which fits, but its
	// protects at 20000 do too; what does not fit is a plant large enough to
	// push a command past sixteen bits. A truncated number addresses a
	// different command, so the line is served inert instead.
	s := newServed(t, routerTree(4, 4))
	prt := s.p.model.port(firstCardPort + 1)
	prt.mu.Lock()
	prt.lines = append(prt.lines, line{
		Index: uint32(len(prt.lines)), Style: codec.StyleNumber,
		Command: uint32(router.LvlProtect(50000)), Text: "far",
	})
	prt.mu.Unlock()

	l, ok := prt.lineAt(uint32(prt.menuLen(false)-1), false)
	if !ok {
		t.Fatal("the line vanished from the older generation")
	}
	if l.Command != 0 || !l.Style.Disabled() {
		t.Errorf("a command that does not fit was served as %d, style %v",
			l.Command, l.Style)
	}
}

func TestAMenuTooLongForTheOlderGeneration(t *testing.T) {
	// A 16-bit client asks for a line by a sixteen-bit index, so a menu longer
	// than that has an end it cannot address. The menu it is shown stops
	// there rather than continuing with lines it could never fetch.
	s := newServed(t, routerTree(4, 4))
	prt := s.p.model.port(firstCardPort + 1)

	prt.mu.Lock()
	n := len(prt.lines)
	prt.lines = append(prt.lines, line{Index: 0x10000, Style: codec.StyleDisplay, Text: "far"})
	prt.mu.Unlock()

	if got := prt.menuLen(false); got != n {
		t.Errorf("the older generation sees %d lines, want the %d it can address", got, n)
	}
	// Asked for by its position, which the older generation can express, the
	// line is still withheld: what it cannot express is the index the line
	// carries, and that is what a later request would have to name.
	if _, ok := prt.lineAt(uint32(n), false); ok {
		t.Error("a line beyond the sixteen-bit index was served to a 16-bit client")
	}
	// The newer generation still sees it, because it can ask for it.
	if _, ok := prt.lineAt(uint32(n), true); !ok {
		t.Error("the line vanished from the generation that can address it")
	}
}

func TestFlushingToASessionWithNoValuesToSend(t *testing.T) {
	// The flush is a burst of values, so a session that did not ask for values
	// has nothing coming. Sending it the burst anyway is the same fault as
	// pushing one change at it, multiplied by the size of the menu.
	s := newServed(t, testTree())
	mapOnly := s.open(0, codec.SvcMap)

	if _, err := mapOnly.Do(context.Background(), codec.MsgBkChnReady,
		[]byte{codec.BackChannelFutureOnly}); err != nil {
		t.Fatalf("back channel: %v", err)
	}

	subs := s.p.slotSubscribers(0, 0)
	if len(subs) != 1 {
		t.Fatalf("%d subscribers, want the one that enabled the back channel", len(subs))
	}

	s.p.wg.Add(1)
	s.p.flush(subs[0])

	select {
	case msg := <-subs[0].queue:
		t.Errorf("a session that asked for the map was sent %v", msg)
	default:
	}
}

func TestEndingASessionIsAcknowledged(t *testing.T) {
	// Specification 9.4: the valid replies to SP_TERM are SP_ACK, "session
	// terminated", and SP_INVSESS — "An SP_ACK command is expected from the
	// receiver."
	//
	// This answered nothing, and a vendor Control Panel waited three seconds
	// for the acknowledgement before giving up, on every node an operator
	// closed. It is the whole of the delay closing a card.
	s := newServed(t, routerTree(4, 4))
	sess := s.open(firstCardPort, codec.SvcMenus|codec.SvcControl|codec.SvcLongStr)

	payload, err := codec.TermSess{Code: codec.TermUser}.AppendTo(nil)
	if err != nil {
		t.Fatalf("term payload: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	reply, err := sess.Do(ctx, codec.MsgTerm, payload)
	if err != nil {
		t.Fatalf("a session ending was not answered: %v", err)
	}
	if reply.Type != codec.MsgAck {
		t.Errorf("a session ending was answered with %s, want ACK", reply.Type)
	}

	// And the session really is gone: the acknowledgement is not a promise to
	// keep it, and a server that kept its half is how a unit runs out.
	if s.p.sessionState(sess) != nil {
		if got := len(s.p.slotSubscribers(firstCardPort, 0)); got != 0 {
			t.Errorf("%d subscriber(s) survived the session that owned them", got)
		}
	}
}
