package rollcall

import (
	"context"
	"testing"
	"time"

	"dhs/internal/snell-rollcall/codec"
)

// A RollCall client is a node on the network like any other, and spec 9.31
// requires every unit to say so. It is what makes a connected client visible:
// the vendor's Control Panel appears in a frame's port list as "142:
// ControlPanel ... RC32 Control Panel", and that list is how an operator sees
// who is attached before disconnecting everyone for a firmware upgrade.

func waitForAnnounce(t *testing.T, h *harness) {
	t.Helper()
	select {
	case <-h.device.iamSeen:
	case <-time.After(2 * time.Second):
		t.Fatal("the client never announced itself")
	}
}

func TestConnectingAnnouncesUs(t *testing.T) {
	h := newHarness(t, nil)
	waitForAnnounce(t, h)

	got := h.device.announced()
	if len(got) == 0 {
		t.Fatal("nothing was announced")
	}
	first := got[0]

	// The address is the one the gateway assigned during the handshake. An
	// announcement from the empty address a client holds beforehand would name
	// nothing.
	if first.Address == (codec.Address{}) {
		t.Error("announced from no address at all")
	}
	if first.Address != h.plugin.link.sess.LocalAddress() {
		t.Errorf("announced as %s, but we are %s",
			first.Address, h.plugin.link.sess.LocalAddress())
	}
	if first.ID.Name != defaultClientName {
		t.Errorf("announced as %q, want %q", first.ID.Name, defaultClientName)
	}
	// A vendor tool reads the type id to pick an icon and a manual, so we
	// report the vendor's own routing-client id rather than an unknown number.
	if first.ID.TypeID != codec.TypeIDRoutingIPShareClient {
		t.Errorf("type id = %d", first.ID.TypeID)
	}
	if !first.Status.Status.Has(codec.StatusPresent) {
		t.Error("a client that is here should say it is present")
	}
}

func TestTheNameIsWhatAnOperatorReads(t *testing.T) {
	// Two dhs instances that both call themselves dhs do not answer the
	// question the connected-client list exists to answer. The name travels in
	// the handshake and in the announcement, and both come from one place, so
	// the peer's session list and its network map agree.
	p := New(testDeps())

	if got := p.identity().ID.Name; got != defaultClientName {
		t.Errorf("unnamed client calls itself %q", got)
	}

	p.SetName("dhs probe 2")
	if got := p.identity().ID.Name; got != "dhs probe 2" {
		t.Errorf("identity name = %q", got)
	}

	addr := codec.Address{Unit: 0x08, Port: 0xE0, Index: codec.IndexUnknown}
	ann := p.announceIdentity(addr)
	if ann.ID.Name != "dhs probe 2" {
		t.Errorf("announced name = %q", ann.ID.Name)
	}
	if ann.Address != addr {
		t.Errorf("announced address = %s, want the assigned one", ann.Address)
	}
}

func TestAnAnnouncementIsNeverAnswered(t *testing.T) {
	// Iam is broadcast, and a peer that replied to one would be talking to
	// everybody. A device that answers nothing must not break the client.
	h := newHarness(t, nil)
	waitForAnnounce(t, h)

	if _, err := h.plugin.GetDeviceInfo(context.Background()); err != nil {
		t.Errorf("the link should be usable after announcing: %v", err)
	}
}
