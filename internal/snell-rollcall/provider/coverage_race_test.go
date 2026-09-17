package rollcall

import (
	"bytes"
	"strings"
	"testing"

	"dhs/internal/snell-rollcall/codec"
)

// These two branches were covered only by chance before: one by a test that
// closed the client and hoped the acknowledgement write lost the race, one not
// at all. Both flipped between runs under -race on Linux and dropped the
// provider below its coverage floor. Here they are hit deterministically.

// TestACallWhoseAckWriteFailsIsNotAccepted drives the error return from
// session.Accept: registering the session succeeds, but writing the
// acknowledgement — which is part of accepting — fails, so the call is not
// accepted. The failing write is forced rather than raced.
func TestACallWhoseAckWriteFailsIsNotAccepted(t *testing.T) {
	s, blocked := newServedFaulty(t, testTree())
	link := providerLink(t, s.p)

	blocked.failing.Store(true) // every write from the server now fails

	err := s.p.Call(link, codec.Frame{
		Dst: addrOf(s.cl.RemoteAddress(), codec.IndexUnknown),
		Src: addrOf(s.cl.LocalAddress(), 7),
	}, codec.Connect{Services: codec.SvcMenus, UserLevel: codec.LevelSupervisor})

	if err == nil {
		t.Error("a call whose acknowledgement cannot be written is not accepted")
	}
}

// TestTielineTemplateClampsALongCableList hits the height clamp in the tieline
// page. The list grows with the number of cables and is capped so a plant with
// many tielines does not draw a listbox taller than the page; the committed
// router fixtures have two cables and never reach it.
func TestTielineTemplateClampsALongCableList(t *testing.T) {
	var buf bytes.Buffer
	prt := &port{
		id:     codec.ID{TypeID: codec.TypeIDTielines, Version: codec.Version{CmdSet: 1}},
		router: &routerModel{tielines: make([]routerTieline, 25)},
	}

	writeTielinePage(&buf, prt)

	// 25 cables would be 25*14+10 = 360; clamped to 300, the page is 300+120.
	if !strings.Contains(buf.String(), "Size=0,0,420,420") {
		t.Errorf("the cable list was not clamped to the page height:\n%s", buf.String())
	}
}
