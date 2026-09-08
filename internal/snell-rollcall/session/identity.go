package session

import (
	"context"
	"time"

	"dhs/internal/snell-rollcall/codec"
)

// Identity is who we say we are on a link.
//
// A provider announces it so that clients and RollMap can find us; a consumer
// carries it in every Call so the peer knows who connected. The type id is
// what a vendor tool uses to pick an icon and a manual, so reporting one the
// database knows makes us legible to tools we did not write.
type Identity struct {
	Info codec.DeviceInfo

	// Interval overrides the announcement period. Zero uses the default.
	Interval time.Duration
}

// Announcer sends Iam on a schedule.
//
// Announcements are never answered: Iam is one of the two message types that
// may be addressed to the broadcast address, and a peer that replied to one
// would be talking to everybody. So this does not go through the active-message
// queue, and it is the only sender in the package that does not.
//
// The period is spread by unit address. A frame announcing sixteen modules at
// the same instant produces a burst the specification's 12.5-to-17.5-second
// window exists to avoid, so the vendor adds twenty milliseconds per unit and
// so do we.
type Announcer struct {
	link *Link
	id   Identity
	sent uint64
}

// NewAnnouncer starts announcing until ctx ends or the link closes.
func NewAnnouncer(ctx context.Context, l *Link, id Identity) *Announcer {
	a := &Announcer{link: l, id: id}

	l.wg.Add(1)
	go func() {
		defer l.wg.Done()
		a.loop(ctx)
	}()
	return a
}

// Interval returns the announcement period this announcer uses.
func (a *Announcer) Interval() time.Duration {
	if a.id.Interval > 0 {
		return a.id.Interval
	}
	return DefaultIamInterval + time.Duration(a.id.Info.Address.Unit)*IamSpreadPerUnit
}

// Sent reports how many announcements have gone out.
func (a *Announcer) Sent() uint64 { return a.sent }

func (a *Announcer) loop(ctx context.Context) {
	// Once on joining, before the first interval elapses. A unit is not in
	// anybody's network map until it has announced, and a client that
	// connects, reads one value and leaves takes a couple of seconds — far
	// less than the twelve and a half the schedule starts with. Waiting for
	// the first tick would mean short-lived clients were never seen at all,
	// which is exactly the case an operator needs to see before pulling
	// everyone off for a firmware upgrade.
	_ = a.Announce()

	t := a.link.clk.NewTicker(a.Interval())
	defer t.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-a.link.done:
			return
		case <-t.C():
			// A failed announcement is not worth ending anything over: the
			// next one is a few seconds away, and if the link is really gone
			// the read loop will say so.
			_ = a.Announce()
		}
	}
}

// Announce sends one Iam immediately.
func (a *Announcer) Announce() error {
	payload, err := a.id.Info.AppendTo(nil)
	if err != nil {
		return err
	}
	a.sent++
	return a.link.send(codec.Frame{
		Dst:     codec.Broadcast(),
		Src:     addrWithIndex(a.id.Info.Address, codec.IndexUnknown),
		Type:    codec.MsgIam,
		Payload: payload,
	})
}

// ClientIdentity builds the DeviceInfo a consumer presents.
//
// It reports the vendor's own routing-client type id, so tools that read the
// id show us as something they recognise rather than as an unknown number. The
// services are what we are prepared to receive, not what we provide.
func ClientIdentity(name string, services codec.Service) codec.DeviceInfo {
	return codec.DeviceInfo{
		ProtocolVersion: codec.ProtocolVersion,
		Address:         codec.Address{Index: codec.IndexUnknown},
		ID: codec.ID{
			Services: services,
			TypeID:   codec.TypeIDRoutingIPShareClient,
			Version:  codec.Version{Major: 1, Minor: 0, Alpha: ' ', CmdSet: 1},
			Name:     codec.TruncateFixed(name, codec.MaxTextSize),
		},
		Status: codec.UnitStatus{Status: codec.StatusPresent | codec.StatusOnline},
	}
}
