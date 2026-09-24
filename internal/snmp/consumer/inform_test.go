package consumer

import (
	"context"
	"errors"
	"net"
	"testing"
	"time"

	"dhs/internal/clock"
	"dhs/internal/consumer/compliance"
	"dhs/internal/plugin"
	"dhs/internal/snmp/codec"
	"dhs/internal/snmp/provider"
	"dhs/internal/snmp/usm"
)

// The inform loop closed: our own sender to our own receiver. The two
// halves are written independently — the sender retries until it is
// answered, the receiver answers before it hands the notification on —
// so the only thing that proves they agree is running them against
// each other.

func informNotification() provider.Notification {
	return provider.Notification{
		Enterprise: codec.MustParseOID("1.3.6.1.4.1.54981.1.1"),
		Generic:    codec.EnterpriseSpecific,
		Specific:   1,
		Uptime:     4242,
		VarBinds: []codec.VarBind{
			{Name: codec.MustParseOID("1.3.6.1.2.1.1.5.0"), Value: codec.String("the-sender")},
		},
	}
}

func TestAnInformIsReceivedAndAcknowledged(t *testing.T) {
	prof := &compliance.Profile{}
	_, addr, got := listenOn(t, ListenerOptions{
		Addr: "127.0.0.1:0", Communities: []string{"public"}, Compliance: prof,
	})

	s := provider.NewTrapSender([]provider.TrapDestination{
		{Addr: addr, Version: codec.Version2c, Community: "public"},
	}, plugin.Deps{Logger: quiet(), Clock: clock.System()})
	defer func() { _ = s.Close() }()

	results := s.SendInform(context.Background(), informNotification())
	if results[0].Err != nil {
		t.Fatalf("the listener must acknowledge: %v", results[0].Err)
	}
	if results[0].Attempts != 1 {
		t.Errorf("attempts = %d — the first inform was not answered", results[0].Attempts)
	}

	tr := waitTrap(t, got)
	if !tr.Inform {
		t.Error("the handler must be told this was an inform, not a trap")
	}
	// Everything a trap carries, an inform carries: the two mandatory
	// bindings are lifted out the same way and the sender's own
	// varbinds are left alone.
	if tr.Uptime != 4242 {
		t.Errorf("uptime = %d", tr.Uptime)
	}
	if len(tr.VarBinds) != 1 || string(tr.VarBinds[0].Value.Bytes) != "the-sender" {
		t.Errorf("varbinds = %+v", tr.VarBinds)
	}
	if n := prof.Snapshot()[InformUnacknowledged]; n != 0 {
		t.Errorf("%d acknowledgement(s) failed to send", n)
	}
}

func TestAV3InformIsAcknowledgedAsTheUserItArrivedAs(t *testing.T) {
	// A sender that authenticated the notification will not accept an
	// unauthenticated acknowledgement of it, so the answer is sealed as
	// the same user and scoped to the sender's engine — which is the
	// authoritative one for a notification.
	user := usm.User{Name: "operator", Auth: usm.HMACSHA256, AuthPass: "authpass-authpass",
		Priv: usm.AES128CFB, PrivPass: "privpass-privpass"}
	id, err := usm.NewEngineID(usm.Enterprise, "inform-sender")
	if err != nil {
		t.Fatal(err)
	}
	engine, err := usm.NewEngine(id, 1, clock.System())
	if err != nil {
		t.Fatal(err)
	}
	if err := engine.AddUser(user); err != nil {
		t.Fatal(err)
	}
	// One engine, both ends: the receiver holds the sender's engine
	// because that is what a configured trap receiver does — a v3
	// notification is keyed on the SENDER's engine ID.
	_, addr, got := listenOn(t, ListenerOptions{Addr: "127.0.0.1:0", Engine: engine})

	s := provider.NewTrapSenderV3([]provider.TrapDestination{
		{Addr: addr, Version: codec.Version3, User: user.Name},
	}, engine, plugin.Deps{Logger: quiet(), Clock: clock.System()})
	defer func() { _ = s.Close() }()

	results := s.SendInform(context.Background(), informNotification())
	if results[0].Err != nil {
		t.Fatalf("a v3 inform must be acknowledged: %v", results[0].Err)
	}
	tr := waitTrap(t, got)
	if !tr.Inform || tr.User != user.Name || tr.SecurityLevel != "authPriv" {
		t.Errorf("trap = inform:%v user:%q level:%q", tr.Inform, tr.User, tr.SecurityLevel)
	}
}

func TestAV3InformCannotBeAcknowledgedWithoutAnEngine(t *testing.T) {
	// Without an engine the receiver cannot even OPEN a v3
	// notification, so it never reaches the acknowledgement — which is
	// the honest outcome: answering in the clear would be refused by
	// the sender anyway.
	prof := &compliance.Profile{}
	_, addr, _ := listenOn(t, ListenerOptions{Addr: "127.0.0.1:0", Compliance: prof})

	user := usm.User{Name: "operator", Auth: usm.HMACSHA256, AuthPass: "authpass-authpass"}
	id, err := usm.NewEngineID(usm.Enterprise, "lonely-sender")
	if err != nil {
		t.Fatal(err)
	}
	engine, err := usm.NewEngine(id, 1, clock.System())
	if err != nil {
		t.Fatal(err)
	}
	if err := engine.AddUser(user); err != nil {
		t.Fatal(err)
	}
	s := provider.NewTrapSenderV3([]provider.TrapDestination{
		{Addr: addr, Version: codec.Version3, User: user.Name},
	}, engine, plugin.Deps{Logger: quiet(), Clock: clock.System()})
	defer func() { _ = s.Close() }()

	results := s.SendInform(context.Background(), informNotification())
	if results[0].Err == nil {
		t.Error("an inform nobody could open must not be reported as delivered")
	}
	if n := prof.Snapshot()[TrapUnauthenticated]; n == 0 {
		t.Error("the refusal must be counted")
	}
}

func TestTheAcknowledgementIsSentBeforeTheHandlerRuns(t *testing.T) {
	// A handler that blocks — writing to a log, judging an alarm — must
	// not turn into a sender retrying the same notification. So the
	// answer goes out first.
	blocked := make(chan struct{})
	release := make(chan struct{})
	l := NewListener(ListenerOptions{Addr: "127.0.0.1:0", Communities: []string{"public"}},
		plugin.Deps{Logger: quiet()})
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- l.Listen(ctx, func(Trap) {
			close(blocked)
			<-release
		})
	}()
	t.Cleanup(func() {
		close(release)
		cancel()
		<-done
	})
	deadline := time.Now().Add(5 * time.Second)
	for l.Addr() == nil {
		if time.Now().After(deadline) {
			t.Fatal("the listener never bound")
		}
		time.Sleep(time.Millisecond)
	}

	s := provider.NewTrapSender([]provider.TrapDestination{
		{Addr: l.Addr().String(), Version: codec.Version2c, Community: "public"},
	}, plugin.Deps{Logger: quiet(), Clock: clock.System()})
	defer func() { _ = s.Close() }()

	results := s.SendInform(context.Background(), informNotification())
	if results[0].Err != nil {
		t.Fatalf("the acknowledgement must not wait for the handler: %v", results[0].Err)
	}
	select {
	case <-blocked:
	case <-time.After(5 * time.Second):
		t.Error("the handler never ran")
	}
}

// The acknowledgement paths that only fail on a bad day: a socket that
// will not take the answer, a v3 inform this receiver cannot seal for.
// Each is counted rather than dropped, because the symptom otherwise is
// a sender retrying an alarm nobody can explain.

type refusingConn struct {
	net.PacketConn
}

func (refusingConn) WriteTo([]byte, net.Addr) (int, error) {
	return 0, errors.New("network is down")
}

func TestAnAcknowledgementThatCannotBeSentIsCounted(t *testing.T) {
	prof := &compliance.Profile{}
	l := NewListener(ListenerOptions{Compliance: prof}, plugin.Deps{Logger: quiet()})
	to := &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 1}

	msg := codec.Message{Version: codec.Version2c, Community: "public",
		PDU: &codec.PDU{Type: codec.PDUTypeInform, RequestID: 1}}
	l.acknowledge(refusingConn{}, to, msg, "")
	if n := prof.Snapshot()[InformUnacknowledged]; n != 1 {
		t.Errorf("a failed send counted %d times, want 1", n)
	}

	// A message with no PDU is not an inform and owes nobody anything.
	l.acknowledge(refusingConn{}, to, codec.Message{Version: codec.Version2c}, "")
	if n := prof.Snapshot()[InformUnacknowledged]; n != 1 {
		t.Errorf("a PDU-less message must not be answered (count %d)", n)
	}
}

func TestAV3AcknowledgementNeedsAnEngineAndAUser(t *testing.T) {
	prof := &compliance.Profile{}
	to := &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 1}
	v3 := codec.Message{Version: codec.Version3,
		V3:  &codec.V3{ID: 1, ContextEngineID: []byte("sender")},
		PDU: &codec.PDU{Type: codec.PDUTypeInform, RequestID: 1}}

	// No engine at all.
	l := NewListener(ListenerOptions{Compliance: prof}, plugin.Deps{Logger: quiet()})
	l.acknowledge(refusingConn{}, to, v3, "operator")
	if n := prof.Snapshot()[InformV3Unsealed]; n != 1 {
		t.Errorf("no engine counted %d, want 1", n)
	}

	// An engine that does not know the user: sealing fails, and that
	// is a failure to acknowledge rather than a silent drop.
	id, err := usm.NewEngineID(usm.Enterprise, "receiver")
	if err != nil {
		t.Fatal(err)
	}
	engine, err := usm.NewEngine(id, 1, clock.System())
	if err != nil {
		t.Fatal(err)
	}
	prof2 := &compliance.Profile{}
	l2 := NewListener(ListenerOptions{Engine: engine, Compliance: prof2},
		plugin.Deps{Logger: quiet()})
	l2.acknowledge(refusingConn{}, to, v3, "nobody")
	if n := prof2.Snapshot()[InformUnacknowledged]; n != 1 {
		t.Errorf("an unsealable answer counted %d, want 1", n)
	}
}
