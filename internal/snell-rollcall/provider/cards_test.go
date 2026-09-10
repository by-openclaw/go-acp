package rollcall

import (
	"net"
	"testing"

	"dhs/internal/export/canonical"
	"dhs/internal/plugin"
	"dhs/internal/snell-rollcall/codec"
)

// A real frame does not number its cards one after another, and it does not
// call every one of them by its own name. The IQ frame at 10.6.255.113 answers
// its Nodal cards on 01, 03, 05, 07 and 09 as type 562 v5.0 cs5, and its AES
// cards on 0B, 0C and 0D as type 389 v8.5 cs17. These tests place testTree's
// two cards the same way, on 3 and 11.

func placedProvider(t *testing.T) *Provider {
	t.Helper()
	p := New(plugin.Deps{}, testTree())
	if err := p.SetCards([]uint8{3, 11}, []string{"IQDBE00@5.0.cs5", "IQMUX42@8.5.cs17"}); err != nil {
		t.Fatalf("SetCards: %v", err)
	}
	return p
}

func TestCardsAnswerOnThePortsTheManifestNames(t *testing.T) {
	p := placedProvider(t)

	got := p.model.portNumbers()
	if len(got) != 2 || got[0] != 3 || got[1] != 11 {
		t.Fatalf("cards are on ports %v, want [3 11]", got)
	}
	// A client that asks port 1 is asking about a slot the manifest left
	// empty, and it is told so rather than shown a card that is not there.
	if _, ok := p.identityOf(1); ok {
		t.Error("port 1 answers with a card, though the manifest put none there")
	}
}

func TestACardAnswersWithTheIdentityItsDMWasFiledUnder(t *testing.T) {
	p := placedProvider(t)

	for _, c := range []struct {
		port   uint8
		typeID uint16
		v      codec.Version
		name   string
	}{
		{3, 562, codec.Version{Major: 5, Minor: 0, Alpha: ' ', CmdSet: 5}, "IQDBE00"},
		{11, 389, codec.Version{Major: 8, Minor: 5, Alpha: ' ', CmdSet: 17}, "IQMUX42"},
	} {
		id, ok := p.identityOf(c.port)
		if !ok {
			t.Fatalf("port %d answers nothing", c.port)
		}
		if id.TypeID != c.typeID || id.Version != c.v || id.Name != c.name {
			t.Errorf("port %d answers %d %v %q, want %d %v %q",
				c.port, id.TypeID, id.Version, id.Name, c.typeID, c.v, c.name)
		}
	}
}

func TestAPlacedCardKeepsItsMenuAndGetsItsOwnTemplate(t *testing.T) {
	p := placedProvider(t)

	// Moving a card changes where it answers, not what it is made of.
	want := len(buildModel(testTree(), gatewayName).port(1).lines)
	if got := len(p.model.port(3).lines); got != want {
		t.Errorf("the card on port 3 has %d menu lines, want the %d it was built with", got, want)
	}

	// A Control Panel reads the template before it draws a card, and the
	// template is keyed by the card's type and command set — so it is built
	// again for where each card now is and what it now says it is.
	if p.templates[3] == nil || p.templates[11] == nil {
		t.Error("a placed card has no template")
	}
	if _, ok := p.templates[1]; ok {
		t.Error("a template is still served for the port the card left")
	}
}

func TestSetCardsRefusesToServeSomethingElse(t *testing.T) {
	keys := []string{"IQDBE00@5.0.cs5", "IQDBE00@5.0.cs5"}
	for _, c := range []struct {
		name  string
		ports []uint8
		keys  []string
	}{
		{"a port for each card, but a key short", []uint8{1, 2}, keys[:1]},
		{"fewer cards placed than the tree holds", []uint8{1}, keys[:1]},
		{"a card on the gateway's port", []uint8{0, 2}, keys},
		{"a card where clients are given ports", []uint8{1, firstClientPort}, keys},
		{"two cards on one port", []uint8{5, 5}, keys},
		{"a key it cannot read back", []uint8{1, 2}, []string{"IQDBE00@5.0.cs5", "not a key"}},
	} {
		p := New(plugin.Deps{}, testTree())
		if err := p.SetCards(c.ports, c.keys); err == nil {
			t.Errorf("%s: accepted", c.name)
		}
	}
}

func TestCardsArePlacedBeforeTheFrameIsServed(t *testing.T) {
	// A client that has walked the frame has already been told where every
	// card is. Moving them underneath it would be lying to it.
	p := New(plugin.Deps{}, testTree())
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer func() { _ = ln.Close() }()

	p.mu.Lock()
	p.listener = ln
	p.mu.Unlock()

	if err := p.SetCards([]uint8{1, 2}, []string{"IQDBE00@5.0.cs5", "IQDBE00@5.0.cs5"}); err == nil {
		t.Error("cards were moved under a frame that was already being served")
	}
}

func TestAFrameWithNoCardsHasNothingToPlace(t *testing.T) {
	// A matrix becomes router nodes rather than a card, so a tree holding only
	// one has no card for a manifest to place.
	onlyRouter := &canonical.Export{Root: &canonical.Node{Header: canonical.Header{
		Identifier: "frame",
		Children:   []canonical.Element{&canonical.Matrix{}},
	}}}
	p := &Provider{tree: onlyRouter}
	if err := p.SetCards(nil, nil); err != nil {
		t.Errorf("placing no cards in a frame of none: %v", err)
	}
	if n := cardCount(nil); n != 0 {
		t.Errorf("no tree holds %d cards", n)
	}
	if n := cardCount(&canonical.Export{}); n != 0 {
		t.Errorf("a tree with no root holds %d cards", n)
	}
}
