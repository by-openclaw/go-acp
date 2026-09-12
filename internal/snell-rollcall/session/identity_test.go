package session

import (
	"context"
	"strings"
	"testing"
	"time"

	"dhs/internal/snell-rollcall/codec"
)

// TestAnnouncer_IsNeverAnswered pins what makes announcements different from
// everything else in this package. Iam is one of only two types that may be
// addressed to the broadcast address, and a peer that replied to one would be
// answering everybody, so it does not go through the active-message queue.
func TestAnnouncer_IsNeverAnswered(t *testing.T) {
	h := newHarness(t, Config{})

	id := Identity{
		Info:     ClientIdentity("dhs", codec.SvcMenus|codec.SvcControl),
		Interval: 12500 * time.Millisecond,
	}
	id.Info.Address = codec.Address{Unit: 0x41, Port: 0x00, Index: codec.IndexUnknown}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	a := NewAnnouncer(ctx, h.link, id)

	h.fire(t, 1, 12500*time.Millisecond)

	f := h.peer.recvType(codec.MsgIam)
	if !f.Dst.IsBroadcast() {
		t.Errorf("announced to %s, want the broadcast address", f.Dst)
	}
	if f.Dst.Index != codec.IndexUnknown {
		t.Errorf("index = %d, want %d", f.Dst.Index, codec.IndexUnknown)
	}
	if !codec.MsgIam.Broadcastable() {
		t.Error("IAM must be one of the types allowed on the broadcast address")
	}

	info, err := codec.DecodeDeviceInfo(f.Payload)
	if err != nil {
		t.Fatalf("DecodeDeviceInfo: %v", err)
	}
	if info.ID.Name != "dhs" {
		t.Errorf("name = %q, want dhs", info.ID.Name)
	}

	// The next announcement goes out without anything having answered the
	// first, which is the whole point.
	h.fire(t, 1, 12500*time.Millisecond)
	h.peer.recvType(codec.MsgIam)

	if a.Sent() < 2 {
		t.Errorf("Sent = %d, want at least 2", a.Sent())
	}
}

// TestAnnouncer_SpreadsByUnit covers the per-unit offset. A frame announcing
// sixteen modules at the same instant produces exactly the burst the
// specification's 12.5-to-17.5-second window exists to avoid.
func TestAnnouncer_SpreadsByUnit(t *testing.T) {
	h := newHarness(t, Config{})

	var last time.Duration
	for _, unit := range []uint8{0x10, 0x11, 0x20, 0x41} {
		id := Identity{Info: ClientIdentity("x", codec.SvcMenus)}
		id.Info.Address = codec.Address{Unit: unit, Index: codec.IndexUnknown}

		ctx, cancel := context.WithCancel(context.Background())
		a := NewAnnouncer(ctx, h.link, id)
		got := a.Interval()
		cancel()

		want := DefaultIamInterval + time.Duration(unit)*IamSpreadPerUnit
		if got != want {
			t.Errorf("unit %02X interval = %s, want %s", unit, got, want)
		}
		if got <= last {
			t.Errorf("unit %02X did not spread past the previous unit", unit)
		}
		last = got

		// Every spread interval must stay inside the window the
		// specification allows.
		if got < 12500*time.Millisecond || got > 17500*time.Millisecond {
			t.Errorf("unit %02X interval %s falls outside the 12.5-17.5s window", unit, got)
		}
	}
}

func TestAnnouncer_ExplicitInterval(t *testing.T) {
	h := newHarness(t, Config{})

	id := Identity{Info: ClientIdentity("x", codec.SvcMenus), Interval: 5 * time.Second}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if got := NewAnnouncer(ctx, h.link, id).Interval(); got != 5*time.Second {
		t.Errorf("Interval = %s, want the explicit 5s", got)
	}
}

// TestAnnouncer_SurvivesAFailedSend covers a transient write failure. The next
// announcement is seconds away, and if the link is really gone the read loop
// reports it, so one lost announcement must not end anything.
func TestAnnouncer_SurvivesAFailedSend(t *testing.T) {
	h := newHarness(t, Config{})

	// A name too long for the field cannot be encoded, so the announcement
	// fails every time.
	id := Identity{Info: ClientIdentity("x", codec.SvcMenus), Interval: time.Second}
	id.Info.ID.Name = strings.Repeat("n", codec.MaxTextSize)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	a := NewAnnouncer(ctx, h.link, id)

	if err := a.Announce(); err == nil {
		t.Fatal("an unencodable identity should fail to announce")
	}

	h.fire(t, 1, time.Second)
	select {
	case <-h.link.Done():
		t.Fatal("a failed announcement must not close the link")
	case <-time.After(100 * time.Millisecond):
	}
}

func TestAnnouncer_StopsWithTheContext(t *testing.T) {
	h := newHarness(t, Config{})

	ctx, cancel := context.WithCancel(context.Background())
	NewAnnouncer(ctx, h.link, Identity{Info: ClientIdentity("x", codec.SvcMenus)})
	h.armed(t, 1)

	cancel()

	deadline := time.Now().Add(2 * time.Second)
	for h.clk.Waiters() != 0 {
		if time.Now().After(deadline) {
			t.Fatalf("%d timers still armed after cancellation", h.clk.Waiters())
		}
		time.Sleep(time.Millisecond)
	}
}

func TestAnnouncer_StopsWithTheLink(t *testing.T) {
	h := newHarness(t, Config{})

	NewAnnouncer(context.Background(), h.link, Identity{Info: ClientIdentity("x", codec.SvcMenus)})
	h.armed(t, 1)

	_ = h.link.Close()

	deadline := time.Now().Add(2 * time.Second)
	for h.clk.Waiters() != 0 {
		if time.Now().After(deadline) {
			t.Fatalf("%d timers still armed after the link closed", h.clk.Waiters())
		}
		time.Sleep(time.Millisecond)
	}
}

// TestClientIdentity covers what we present to a peer. The type id is the one
// the vendor database assigns to a routing client, so tools that read the id
// show us as something they recognise rather than as an unknown number.
func TestClientIdentity(t *testing.T) {
	id := ClientIdentity("dhs consumer", codec.SvcMenus|codec.SvcControl|codec.SvcLongStr)

	if id.ID.TypeID != codec.TypeIDRoutingIPShareClient {
		t.Errorf("type id = %d, want the routing client %d",
			id.ID.TypeID, codec.TypeIDRoutingIPShareClient)
	}
	if _, ok := codec.LookupUnitType(id.ID.TypeID); !ok {
		t.Error("the type id we present must exist in the vendor database")
	}
	if id.ProtocolVersion != codec.ProtocolVersion {
		t.Errorf("protocol version = %d", id.ProtocolVersion)
	}
	if id.Address.Index != codec.IndexUnknown {
		t.Errorf("index = %d, want %d before any session", id.Address.Index, codec.IndexUnknown)
	}
	if !id.Status.Status.Has(codec.StatusPresent) {
		t.Errorf("status = %s, want it to say we are present", id.Status.Status)
	}

	// Whatever we are called, the identity must encode: a client that cannot
	// connect because its own name is long is a worse outcome than one that
	// appears under a shortened name.
	long := ClientIdentity(strings.Repeat("n", 100), codec.SvcMenus)
	if _, err := long.AppendTo(nil); err != nil {
		t.Errorf("a long name must be truncated rather than refused: %v", err)
	}
	if len(long.ID.Name) != codec.MaxTextSize-1 {
		t.Errorf("name is %d bytes, want %d", len(long.ID.Name), codec.MaxTextSize-1)
	}
}
