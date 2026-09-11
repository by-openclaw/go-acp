package consumer

// The listener is tested against OUR trap sender, at all three versions.
// The normalisation is the thing worth pinning: a handler is written
// once, and a v1 trap and a v3 notification carrying the same event have
// to reach it looking the same.

import (
	"context"
	"errors"
	"net"
	"strings"
	"testing"
	"time"

	"dhs/internal/clock"
	"dhs/internal/consumer/compliance"
	"dhs/internal/plugin"
	"dhs/internal/snmp/codec"
	"dhs/internal/snmp/provider"
	"dhs/internal/snmp/usm"
)

// listenOn starts a listener on an ephemeral port and returns it with a
// channel of what it receives.
func listenOn(t *testing.T, opts ListenerOptions) (*Listener, string, <-chan Trap) {
	t.Helper()
	l := NewListener(opts, plugin.Deps{Logger: quiet()})
	got := make(chan Trap, 16)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- l.Listen(ctx, func(tr Trap) { got <- tr }) }()

	t.Cleanup(func() {
		cancel()
		select {
		case err := <-done:
			if err != nil {
				t.Errorf("Listen returned %v", err)
			}
		case <-time.After(5 * time.Second):
			t.Error("the listener did not stop")
		}
	})

	deadline := time.Now().Add(5 * time.Second)
	for l.Addr() == nil {
		if time.Now().After(deadline) {
			t.Fatal("the listener never bound")
		}
		time.Sleep(time.Millisecond)
	}
	return l, l.Addr().String(), got
}

func waitTrap(t *testing.T, ch <-chan Trap) Trap {
	t.Helper()
	select {
	case tr := <-ch:
		return tr
	case <-time.After(5 * time.Second):
		t.Fatal("no notification arrived")
		return Trap{}
	}
}

// alarm is the event: enterprise-specific trap 1 on the Snell tree,
// which is the shape a frame actually emits.
func alarm() provider.Notification {
	return provider.Notification{
		Enterprise: codec.MustParseOID("1.3.6.1.4.1.7995.1.3"),
		AgentAddr:  net.IPv4(10, 6, 255, 113),
		Generic:    codec.EnterpriseSpecific,
		Specific:   1,
		Uptime:     360000,
		VarBinds: []codec.VarBind{
			{Name: codec.MustParseOID("1.3.6.1.4.1.7995.2.1.1"), Value: codec.Int(1)},
		},
	}
}

func trapEngine(t *testing.T, u usm.User) *usm.Engine {
	t.Helper()
	id, err := usm.NewEngineID(usm.Enterprise, "dhs-sender")
	if err != nil {
		t.Fatal(err)
	}
	e, err := usm.NewEngine(id, 1, clock.NewFake(time.Unix(1_700_000_000, 0)))
	if err != nil {
		t.Fatal(err)
	}
	if err := e.AddUser(u); err != nil {
		t.Fatal(err)
	}
	return e
}

// One event, three versions, one shape at the handler. A v1 trap carries
// its identity in the PDU and a v2c one in a varbind; the handler must
// not have to know which arrived.
func TestOneEventLooksTheSameWhicheverVersionCarriedIt(t *testing.T) {
	user := usm.User{Name: "operator", Auth: usm.HMACSHA256, AuthPass: "maplesyrup",
		Priv: usm.AES128CFB, PrivPass: "maplesyrup"}
	engine := trapEngine(t, user)

	_, addr, got := listenOn(t, ListenerOptions{
		Addr: "127.0.0.1:0", Communities: []string{"public"}, Engine: engine,
	})

	for _, d := range []provider.TrapDestination{
		{Addr: addr, Version: codec.Version1, Community: "public"},
		{Addr: addr, Version: codec.Version2c, Community: "public"},
		{Addr: addr, Version: codec.Version3, User: "operator"},
	} {
		t.Run(d.Version.String(), func(t *testing.T) {
			s := provider.NewTrapSenderV3([]provider.TrapDestination{d}, engine,
				plugin.Deps{Logger: quiet()})
			t.Cleanup(func() { _ = s.Close() })
			if err := s.Send(context.Background(), alarm()); err != nil {
				t.Fatalf("Send: %v", err)
			}

			tr := waitTrap(t, got)
			if tr.Version != d.Version {
				t.Errorf("arrived as %s", tr.Version)
			}
			// The same identity, however it travelled.
			if tr.TrapOID.String() != "1.3.6.1.4.1.7995.1.3.0.1" {
				t.Errorf("TrapOID = %s", tr.TrapOID)
			}
			if tr.Generic != codec.EnterpriseSpecific || tr.Specific != 1 {
				t.Errorf("trap = %s/%d", tr.Generic, tr.Specific)
			}
			if tr.Enterprise.String() != "1.3.6.1.4.1.7995.1.3" {
				t.Errorf("enterprise = %s", tr.Enterprise)
			}
			if tr.Uptime != 360000 {
				t.Errorf("uptime = %d", tr.Uptime)
			}
			// The sender's own binding, WITHOUT the two mandatory ones a
			// v2c notification opens with.
			if len(tr.VarBinds) != 1 {
				t.Fatalf("%d bindings, want the sender's one: %v", len(tr.VarBinds), tr.VarBinds)
			}
			if tr.VarBinds[0].Value.Int != 1 {
				t.Errorf("binding = %s", tr.VarBinds[0].Value)
			}
			if tr.From == nil {
				t.Error("the sender's address must be recorded")
			}

			switch d.Version {
			case codec.Version3:
				if tr.User != "operator" || tr.SecurityLevel != "authPriv" {
					t.Errorf("v3 identity = %q/%q", tr.User, tr.SecurityLevel)
				}
				if tr.Community != "" {
					t.Errorf("v3 has no community, got %q", tr.Community)
				}
			default:
				if tr.Community != "public" {
					t.Errorf("community = %q", tr.Community)
				}
			}
		})
	}
}

// A generic trap keeps its RFC 3418 identity in both directions: a v1
// coldStart and a v2c one are the same event.
func TestAGenericTrapKeepsItsWellKnownIdentity(t *testing.T) {
	_, addr, got := listenOn(t, ListenerOptions{Addr: "127.0.0.1:0"})

	for _, v := range []codec.Version{codec.Version1, codec.Version2c} {
		s := provider.NewTrapSender([]provider.TrapDestination{
			{Addr: addr, Version: v, Community: "public"}}, plugin.Deps{Logger: quiet()})
		n := alarm()
		n.Generic, n.Specific = codec.ColdStart, 0
		if err := s.Send(context.Background(), n); err != nil {
			t.Fatal(err)
		}
		_ = s.Close()

		tr := waitTrap(t, got)
		if tr.TrapOID.String() != "1.3.6.1.6.3.1.1.5.1" {
			t.Errorf("%s: TrapOID = %s, want the coldStart identity", v, tr.TrapOID)
		}
		if tr.Generic != codec.ColdStart {
			t.Errorf("%s: generic = %s", v, tr.Generic)
		}
	}
}

// A v1 trap carries the sender's own idea of its address, which a v2c
// one has no field for at all.
func TestOnlyAV1TrapCarriesAnAgentAddress(t *testing.T) {
	_, addr, got := listenOn(t, ListenerOptions{Addr: "127.0.0.1:0"})
	s := provider.NewTrapSender([]provider.TrapDestination{
		{Addr: addr, Version: codec.Version1, Community: "public"},
		{Addr: addr, Version: codec.Version2c, Community: "public"},
	}, plugin.Deps{Logger: quiet()})
	t.Cleanup(func() { _ = s.Close() })

	if err := s.Send(context.Background(), alarm()); err != nil {
		t.Fatal(err)
	}

	var v1, v2 Trap
	for i := 0; i < 2; i++ {
		tr := waitTrap(t, got)
		if tr.Version == codec.Version1 {
			v1 = tr
		} else {
			v2 = tr
		}
	}
	if !v1.AgentAddr.Equal(net.IPv4(10, 6, 255, 113)) {
		t.Errorf("v1 agent-addr = %v", v1.AgentAddr)
	}
	if v2.AgentAddr != nil {
		t.Errorf("v2c has no agent-address field, got %v", v2.AgentAddr)
	}
}

// A community the listener was not told to accept is refused and
// counted. An empty accept-list admits any, which is a diagnostic
// listener's job — so it is the caller's decision, not a default.
func TestCommunityFiltering(t *testing.T) {
	prof := &compliance.Profile{}
	_, addr, got := listenOn(t, ListenerOptions{
		Addr: "127.0.0.1:0", Communities: []string{"secret"}, Compliance: prof,
	})

	s := provider.NewTrapSender([]provider.TrapDestination{
		{Addr: addr, Version: codec.Version2c, Community: "public"}},
		plugin.Deps{Logger: quiet()})
	t.Cleanup(func() { _ = s.Close() })
	if err := s.Send(context.Background(), alarm()); err != nil {
		t.Fatal(err)
	}

	select {
	case tr := <-got:
		t.Fatalf("an unaccepted community was delivered: %+v", tr)
	case <-time.After(200 * time.Millisecond):
	}
	if prof.Snapshot()[TrapUnauthenticated] == 0 {
		t.Error("a refused community must be counted")
	}
}

// A v3 notification the listener cannot authenticate is dropped: with no
// engine there is no way to know who sent it, and delivering it would be
// delivering an unauthenticated alarm as an authenticated one.
func TestAV3NotificationWithNoEngine(t *testing.T) {
	prof := &compliance.Profile{}
	_, addr, got := listenOn(t, ListenerOptions{Addr: "127.0.0.1:0", Compliance: prof})

	engine := trapEngine(t, usm.User{Name: "operator", Auth: usm.HMACSHA256, AuthPass: "p"})
	s := provider.NewTrapSenderV3([]provider.TrapDestination{
		{Addr: addr, Version: codec.Version3, User: "operator"}}, engine,
		plugin.Deps{Logger: quiet()})
	t.Cleanup(func() { _ = s.Close() })
	if err := s.Send(context.Background(), alarm()); err != nil {
		t.Fatal(err)
	}

	select {
	case tr := <-got:
		t.Fatalf("a notification nobody could authenticate was delivered: %+v", tr)
	case <-time.After(200 * time.Millisecond):
	}
	if prof.Snapshot()[TrapUnauthenticated] == 0 {
		t.Error("it must be counted")
	}
}

// A v3 notification from a user this engine does not know is refused the
// same way — and a listener that accepted it would be accepting an alarm
// from anybody who can reach the port.
func TestAV3NotificationFromAnUnknownUser(t *testing.T) {
	prof := &compliance.Profile{}
	receiver := trapEngine(t, usm.User{Name: "somebody-else",
		Auth: usm.HMACSHA256, AuthPass: "p"})
	_, addr, got := listenOn(t, ListenerOptions{
		Addr: "127.0.0.1:0", Engine: receiver, Compliance: prof,
	})

	sender := trapEngine(t, usm.User{Name: "operator", Auth: usm.HMACSHA256, AuthPass: "p"})
	s := provider.NewTrapSenderV3([]provider.TrapDestination{
		{Addr: addr, Version: codec.Version3, User: "operator"}}, sender,
		plugin.Deps{Logger: quiet()})
	t.Cleanup(func() { _ = s.Close() })
	if err := s.Send(context.Background(), alarm()); err != nil {
		t.Fatal(err)
	}

	select {
	case tr := <-got:
		t.Fatalf("an unknown user was delivered: %+v", tr)
	case <-time.After(200 * time.Millisecond):
	}
	if prof.Snapshot()[TrapUnauthenticated] == 0 {
		t.Error("it must be counted")
	}
}

// sendRaw puts arbitrary bytes on the listener's port.
func sendRaw(t *testing.T, addr string, raw []byte) {
	t.Helper()
	c, err := net.Dial("udp4", addr)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = c.Close() }()
	if _, err := c.Write(raw); err != nil {
		t.Fatal(err)
	}
}

// Everything the listener drops is counted, so "no alarms" can be told
// apart from "alarms we could not read".
func TestWhatTheListenerDropsIsCounted(t *testing.T) {
	prof := &compliance.Profile{}
	_, addr, got := listenOn(t, ListenerOptions{Addr: "127.0.0.1:0", Compliance: prof})

	// Not SNMP at all.
	sendRaw(t, addr, []byte{0xFF, 0xFF, 0xFF})

	// A Response arriving on the trap port: somebody else's traffic.
	resp, err := codec.Encode(codec.Message{Version: codec.Version2c, Community: "public",
		PDU: &codec.PDU{Type: codec.PDUTypeResponse, RequestID: 1}})
	if err != nil {
		t.Fatal(err)
	}
	sendRaw(t, addr, resp)

	// A v2c notification missing the two mandatory bindings. RFC 3416
	// §4.2.6 says a conforming receiver drops it; this one keeps it,
	// because a dropped alarm is worse than a malformed one — and counts
	// the deviation so the device can be fixed.
	bad, err := codec.Encode(codec.Message{Version: codec.Version2c, Community: "public",
		PDU: &codec.PDU{Type: codec.PDUTypeTrapV2, RequestID: 1,
			VarBinds: []codec.VarBind{
				{Name: codec.MustParseOID("1.3.6.1.4.1.7995.1"), Value: codec.Int(1)}}}})
	if err != nil {
		t.Fatal(err)
	}
	sendRaw(t, addr, bad)

	tr := waitTrap(t, got)
	if len(tr.VarBinds) != 1 {
		t.Errorf("a malformed notification must still be delivered: %+v", tr)
	}

	snap := prof.Snapshot()
	for _, k := range []string{TrapUndecodable, TrapUnexpectedPDU, TrapMissingBindings} {
		if snap[k] == 0 {
			t.Errorf("%s was not counted: %v", k, snap)
		}
	}
}

// A notification whose identity does not fit the RFC 3584 mapping still
// arrives; the v1 view of it is simply empty, which is honest.
func TestAnIdentityThatIsNotTheStandardMapping(t *testing.T) {
	_, addr, got := listenOn(t, ListenerOptions{Addr: "127.0.0.1:0"})

	raw, err := codec.Encode(codec.Message{Version: codec.Version2c, Community: "public",
		PDU: &codec.PDU{Type: codec.PDUTypeTrapV2, RequestID: 1,
			VarBinds: []codec.VarBind{
				{Name: sysUpTimeInstance, Value: codec.TimeTicks(1)},
				// No zero arc, so nothing to strip.
				{Name: snmpTrapOIDInstance,
					Value: codec.ObjectID(codec.MustParseOID("1.3.6.1.4.1.9999"))},
			}}})
	if err != nil {
		t.Fatal(err)
	}
	sendRaw(t, addr, raw)

	tr := waitTrap(t, got)
	if tr.TrapOID.String() != "1.3.6.1.4.1.9999" {
		t.Errorf("TrapOID = %s", tr.TrapOID)
	}
	if tr.Generic != codec.EnterpriseSpecific {
		t.Errorf("generic = %s", tr.Generic)
	}
	if len(tr.Enterprise) != 0 {
		t.Errorf("enterprise = %s, want nothing derivable", tr.Enterprise)
	}
}

// The line an operator greps.
func TestTrapRendering(t *testing.T) {
	tr := Trap{
		From:    &net.UDPAddr{IP: net.IPv4(10, 6, 255, 113), Port: 40000},
		Version: codec.Version1, Community: "public",
		TrapOID: codec.MustParseOID("1.3.6.1.4.1.7995.1.3.0.1"),
		Uptime:  360000,
	}
	got := tr.String()
	for _, want := range []string{"10.6.255.113", "v1", "public", "1.3.6.1.4.1.7995.1.3.0.1", "1h0m0s"} {
		if !strings.Contains(got, want) {
			t.Errorf("%q is missing %q", got, want)
		}
	}

	v3 := Trap{Version: codec.Version3, User: "operator", SecurityLevel: "authPriv"}
	if got := v3.String(); !strings.Contains(got, "operator/authPriv") || !strings.Contains(got, "?") {
		t.Errorf("= %q", got)
	}
}

// A bind that cannot happen is reported with the address in it.
func TestABindThatCannotHappen(t *testing.T) {
	l := NewListener(ListenerOptions{Addr: "256.256.256.256:162"}, plugin.Deps{Logger: quiet()})
	err := l.Listen(context.Background(), func(Trap) {})
	if err == nil || !strings.Contains(err.Error(), "256.256.256.256:162") {
		t.Fatalf("= %v, want the address named", err)
	}
}

// Close is idempotent and safe on a listener that never bound.
func TestListenerCloseIsIdempotent(t *testing.T) {
	// An ephemeral port on loopback: the default is 162, which needs
	// privilege on Linux, and this test is not about the port.
	l := NewListener(ListenerOptions{Addr: "127.0.0.1:0"}, plugin.Deps{Logger: quiet()})
	if l.Addr() != nil {
		t.Error("a listener that never bound is bound to nothing")
	}
	if err := l.Close(); err != nil {
		t.Errorf("Close before Listen: %v", err)
	}
	if err := l.Close(); err != nil {
		t.Errorf("second Close: %v", err)
	}
	// And a Listen after Close does not bind.
	if err := l.Listen(context.Background(), func(Trap) {}); !errors.Is(err, net.ErrClosed) {
		t.Errorf("= %v, want net.ErrClosed", err)
	}
}

// A Close that arrives while the bind is in flight is honoured, and the
// socket it raced is not left open.
func TestACloseThatRacesTheBind(t *testing.T) {
	l := NewListener(ListenerOptions{Addr: "127.0.0.1:0"}, plugin.Deps{Logger: quiet()})

	inBind := make(chan struct{})
	release := make(chan struct{})
	real := l.listen
	l.listen = func(ctx context.Context, network, addr string) (net.PacketConn, error) {
		close(inBind)
		<-release
		return real(ctx, network, addr)
	}

	done := make(chan error, 1)
	go func() { done <- l.Listen(context.Background(), func(Trap) {}) }()

	<-inBind
	if err := l.Close(); err != nil {
		t.Fatal(err)
	}
	close(release)

	if err := <-done; !errors.Is(err, net.ErrClosed) {
		t.Errorf("= %v, want net.ErrClosed", err)
	}
}
