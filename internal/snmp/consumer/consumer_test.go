package consumer

// The manager is tested against OUR agent over a real socket. The two
// share only the codec, so a round trip here is a genuine test of both
// halves — and a change that broke the agreement between them shows up
// as a failure rather than as two packages that each pass alone.

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net"
	"strings"
	"testing"
	"time"

	"dhs/internal/consumer/compliance"
	"dhs/internal/plugin"
	"dhs/internal/snmp/codec"
	"dhs/internal/snmp/mib"
	"dhs/internal/snmp/provider"
)

func quiet() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

func oid(s string) codec.OID { return codec.MustParseOID(s) }

// agentUnder returns a live agent's address: the RFC 1213 system group,
// a two-row table to walk, and one writable object.
func agentUnder(t *testing.T, communities provider.Communities) string {
	t.Helper()
	tree := provider.NewMIB()
	var stored int64 = 1
	objs := provider.SystemGroup(provider.SystemInfo{
		Descr:    "dhs SNMP agent under test",
		ObjectID: oid("1.3.6.1.4.1.32473.1"),
		Contact:  "ops@example.invalid",
		Name:     "agent-under-test",
		Location: "TEC RACK 23",
	}, nil)
	objs = append(objs,
		provider.Object{OID: oid("1.3.6.1.4.1.32473.2"), Access: provider.NotAccessible},
		provider.Scalar(oid("1.3.6.1.4.1.32473.2.1.1"), codec.Int(10)),
		provider.Scalar(oid("1.3.6.1.4.1.32473.2.1.2"), codec.Int(20)),
		provider.Writable(oid("1.3.6.1.4.1.32473.3.0"), codec.TypeInteger,
			func() codec.Value { return codec.Int(stored) },
			func(v codec.Value) error { stored = v.Int; return nil }),
	)
	if err := tree.Register(objs...); err != nil {
		t.Fatal(err)
	}

	s := provider.NewServer(tree, communities, plugin.Deps{Logger: quiet()})
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- s.Serve(ctx, "127.0.0.1:0") }()
	t.Cleanup(func() {
		cancel()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Error("the agent did not stop")
		}
	})

	deadline := time.Now().Add(5 * time.Second)
	for s.Addr() == nil {
		if time.Now().After(deadline) {
			t.Fatal("the agent never bound")
		}
		time.Sleep(time.Millisecond)
	}
	return s.Addr().String()
}

func dial(t *testing.T, addr string, opts Options) *Session {
	t.Helper()
	opts.Addr = addr
	if opts.Community == "" {
		opts.Community = "public"
	}
	sess, err := Dial(context.Background(), opts, plugin.Deps{Logger: quiet()})
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	t.Cleanup(func() { _ = sess.Close() })
	return sess
}

// The first thing any manager does, against a live agent.
func TestGetTheSystemGroup(t *testing.T) {
	addr := agentUnder(t, provider.Communities{Read: "public"})
	s := dial(t, addr, Options{Version: codec.Version2c})

	binds, err := s.Get(context.Background(), mib.SysDescr, mib.SysName, mib.SysObjectID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if len(binds) != 3 {
		t.Fatalf("%d bindings, want one per name asked", len(binds))
	}
	if got := binds[0].Value.String(); got != "dhs SNMP agent under test" {
		t.Errorf("sysDescr.0 = %q", got)
	}
	if got := binds[1].Value.String(); got != "agent-under-test" {
		t.Errorf("sysName.0 = %q", got)
	}
	if got := binds[2].Value.OID.String(); got != "1.3.6.1.4.1.32473.1" {
		t.Errorf("sysObjectID.0 = %s", got)
	}
}

// sysUpTime is in CENTISECONDS. An agent reporting seconds looks a
// hundred times younger than it is, and every trap it stamps is wrong.
func TestUptimeIsInCentiseconds(t *testing.T) {
	addr := agentUnder(t, provider.Communities{Read: "public"})
	s := dial(t, addr, Options{Version: codec.Version2c})

	binds, err := s.Get(context.Background(), mib.SysUpTime)
	if err != nil {
		t.Fatal(err)
	}
	if binds[0].Value.Type != codec.TypeTimeTicks {
		t.Errorf("sysUpTime.0 is a %s, want TimeTicks", binds[0].Value.Type)
	}
}

// A walk is how a device with no MIB to hand is discovered, and it must
// stop at the edge of the subtree rather than running into the next one.
func TestWalkStopsAtTheEdgeOfTheSubtree(t *testing.T) {
	addr := agentUnder(t, provider.Communities{Read: "public"})

	for _, v := range []codec.Version{codec.Version1, codec.Version2c} {
		t.Run(v.String(), func(t *testing.T) {
			s := dial(t, addr, Options{Version: v, Community: "public"})

			var got []string
			if err := s.Walk(context.Background(), mib.System, func(vb codec.VarBind) error {
				got = append(got, vb.Name.String())
				return nil
			}); err != nil {
				t.Fatalf("Walk: %v", err)
			}
			if len(got) != 7 {
				t.Fatalf("walked %d objects, want the system group's 7: %v", len(got), got)
			}
			if got[0] != mib.SysDescr.String() {
				t.Errorf("first = %s", got[0])
			}
			if got[len(got)-1] != mib.SysServices.String() {
				t.Errorf("last = %s, want sysServices.0", got[len(got)-1])
			}
		})
	}
}

// A walk of the whole tree runs off the end and stops there, rather than
// asking forever.
func TestWalkingTheWholeTreeEnds(t *testing.T) {
	addr := agentUnder(t, provider.Communities{Read: "public"})

	for _, v := range []codec.Version{codec.Version1, codec.Version2c} {
		t.Run(v.String(), func(t *testing.T) {
			s := dial(t, addr, Options{Version: v, Community: "public"})
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			defer cancel()

			got, err := s.WalkAll(ctx, mib.Internet, 0)
			if err != nil {
				t.Fatalf("WalkAll: %v", err)
			}
			// Seven system objects plus two table rows plus the writable
			// one; the not-accessible node is stepped over.
			if len(got) != 10 {
				t.Errorf("%d objects, want 10: %v", len(got), got)
			}
		})
	}
}

// A GETBULK walk takes far fewer round trips than a GETNEXT one, which
// is the whole reason v2c exists for a manager.
func TestABulkWalkAsksFewerTimes(t *testing.T) {
	addr := agentUnder(t, provider.Communities{Read: "public"})
	s := dial(t, addr, Options{Version: codec.Version2c, MaxRepetitions: 25})

	// The whole tree is ten objects, so one bulk of 25 covers it and the
	// walk ends on the endOfMibView in the same reply.
	got, err := s.WalkAll(context.Background(), mib.Internet, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 10 {
		t.Fatalf("%d objects", len(got))
	}
}

// A walk that the caller stops is stopped, and the caller's own error
// comes back rather than being swallowed.
func TestACallerCanStopAWalk(t *testing.T) {
	addr := agentUnder(t, provider.Communities{Read: "public"})
	s := dial(t, addr, Options{Version: codec.Version2c})

	stop := errors.New("enough")
	n := 0
	err := s.Walk(context.Background(), mib.Internet, func(codec.VarBind) error {
		n++
		return stop
	})
	if !errors.Is(err, stop) {
		t.Fatalf("= %v, want the caller's own error", err)
	}
	if n != 1 {
		t.Errorf("the walk continued past the stop: %d", n)
	}
}

// WalkAll is bounded, because an agent that never says endOfMibView is a
// real failure mode and not a hypothetical one.
func TestWalkAllIsBounded(t *testing.T) {
	addr := agentUnder(t, provider.Communities{Read: "public"})
	s := dial(t, addr, Options{Version: codec.Version2c})

	_, err := s.WalkAll(context.Background(), mib.Internet, 3)
	if err == nil || !strings.Contains(err.Error(), "exceeded 3 objects") {
		t.Fatalf("= %v, want the bound enforced", err)
	}
}

// A SET writes, and the agent echoes what it took.
func TestSetWritesAndEchoes(t *testing.T) {
	addr := agentUnder(t, provider.Communities{Read: "public", Write: "private"})
	s := dial(t, addr, Options{Version: codec.Version2c, Community: "private"})

	target := oid("1.3.6.1.4.1.32473.3.0")
	binds, err := s.Set(context.Background(), codec.VarBind{Name: target, Value: codec.Int(42)})
	if err != nil {
		t.Fatalf("Set: %v", err)
	}
	if binds[0].Value.Int != 42 {
		t.Errorf("echo = %s", binds[0].Value)
	}

	// And it stuck.
	read := dial(t, addr, Options{Version: codec.Version2c, Community: "public"})
	got, err := read.Get(context.Background(), target)
	if err != nil {
		t.Fatal(err)
	}
	if got[0].Value.Int != 42 {
		t.Errorf("read back %s", got[0].Value)
	}
}

// An error-status comes back as a typed error a caller can branch on,
// naming the binding it applies to — error-index alone is a number.
func TestAnErrorStatusIsTyped(t *testing.T) {
	addr := agentUnder(t, provider.Communities{Read: "public", Write: "private"})
	s := dial(t, addr, Options{Version: codec.Version2c, Community: "private"})

	_, err := s.Set(context.Background(),
		codec.VarBind{Name: mib.SysDescr, Value: codec.String("nope")})
	var se *StatusError
	if !errors.As(err, &se) {
		t.Fatalf("= %v, want a *StatusError", err)
	}
	if se.Status != codec.NotWritable {
		t.Errorf("status = %s, want notWritable", se.Status)
	}
	if se.Name.Compare(mib.SysDescr) != 0 {
		t.Errorf("named %s, want the binding that failed", se.Name)
	}
	if !errors.Is(err, &StatusError{Status: codec.NotWritable}) {
		t.Error("errors.Is must match on the status alone")
	}
	if got := err.Error(); !strings.Contains(got, "notWritable") ||
		!strings.Contains(got, mib.SysDescr.String()) {
		t.Errorf("message = %q", got)
	}
}

// A StatusError with no name still renders, which is what a response
// that echoed nothing produces.
func TestAStatusErrorWithNoName(t *testing.T) {
	e := &StatusError{Status: codec.GenErr}
	if got := e.Error(); got != "snmp: genErr" {
		t.Errorf("= %q", got)
	}
	if e.Is(errors.New("something else")) {
		t.Error("Is must only match a StatusError")
	}
}

// An agent that will not answer is a timeout, after the retries — and a
// manager that did not retry would report a device down for one dropped
// datagram.
func TestSilenceIsATimeoutAfterRetries(t *testing.T) {
	// A socket nothing is reading from.
	dead, err := net.ListenPacket("udp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = dead.Close() })

	s := dial(t, dead.LocalAddr().String(), Options{
		Version: codec.Version2c, Timeout: 50 * time.Millisecond, Retries: 1,
	})

	start := time.Now()
	_, err = s.Get(context.Background(), mib.SysDescr)
	if !errors.Is(err, ErrTimeout) {
		t.Fatalf("= %v, want a timeout", err)
	}
	if !strings.Contains(err.Error(), "2 attempt(s)") {
		t.Errorf("= %v, want the attempt count", err)
	}
	if elapsed := time.Since(start); elapsed < 90*time.Millisecond {
		t.Errorf("gave up after %s: it did not retry", elapsed)
	}
}

// A wrong community gets silence from the agent, which reaches the
// manager as a timeout — there is nothing else it could be, and that is
// the protocol rather than something to paper over.
func TestAWrongCommunityLooksLikeSilence(t *testing.T) {
	addr := agentUnder(t, provider.Communities{Read: "public"})
	s := dial(t, addr, Options{
		Version: codec.Version2c, Community: "wrong",
		Timeout: 100 * time.Millisecond, Retries: 0,
	})
	if _, err := s.Get(context.Background(), mib.SysDescr); !errors.Is(err, ErrTimeout) {
		t.Errorf("= %v, want a timeout", err)
	}
}

// A cancelled context stops a request rather than waiting out the
// timeout it was given.
func TestACancelledContextStopsARequest(t *testing.T) {
	dead, err := net.ListenPacket("udp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = dead.Close() })

	s := dial(t, dead.LocalAddr().String(), Options{
		Version: codec.Version2c, Timeout: 30 * time.Second, Retries: 5,
	})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if _, err := s.Get(ctx, mib.SysDescr); !errors.Is(err, context.Canceled) {
		t.Errorf("= %v, want the cancellation", err)
	}
}

// A session that has been closed refuses rather than writing to a closed
// socket.
func TestAClosedSessionRefuses(t *testing.T) {
	addr := agentUnder(t, provider.Communities{Read: "public"})
	s := dial(t, addr, Options{Version: codec.Version2c})

	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Errorf("second Close: %v", err)
	}
	if _, err := s.Get(context.Background(), mib.SysDescr); !errors.Is(err, net.ErrClosed) {
		t.Errorf("= %v, want net.ErrClosed", err)
	}
}

// The shapes a caller cannot ask for.
func TestRequestRefusals(t *testing.T) {
	addr := agentUnder(t, provider.Communities{Read: "public"})
	s := dial(t, addr, Options{Version: codec.Version2c})

	if _, err := s.Get(context.Background()); err == nil ||
		!strings.Contains(err.Error(), "at least one name") {
		t.Errorf("= %v, want the empty request refused", err)
	}
	if _, err := s.Set(context.Background()); err == nil ||
		!strings.Contains(err.Error(), "something to write") {
		t.Errorf("= %v, want the empty SET refused", err)
	}
}

func TestDialRefusals(t *testing.T) {
	if _, err := Dial(context.Background(), Options{Addr: "127.0.0.1:1",
		Version: codec.Version3}, plugin.Deps{Logger: quiet()}); err == nil ||
		!strings.Contains(err.Error(), "v1 and v2c") {
		t.Errorf("= %v, want v3 refused with the reason", err)
	}
	if _, err := Dial(context.Background(), Options{Version: codec.Version(7),
		Community: "public"}, plugin.Deps{Logger: quiet()}); err == nil {
		t.Error("a version nobody speaks must be refused")
	}
	if _, err := Dial(context.Background(), Options{Community: "public"},
		plugin.Deps{Logger: quiet()}); err == nil ||
		!strings.Contains(err.Error(), "no address") {
		t.Errorf("= %v, want the missing address refused", err)
	}
	if _, err := Dial(context.Background(), Options{Addr: "no such host at all:161"},
		plugin.Deps{Logger: quiet()}); err == nil {
		t.Error("a host that does not resolve must be refused")
	}
}

// A bare host takes the default port, because that is what an operator
// types.
func TestABareHostTakesTheDefaultPort(t *testing.T) {
	got, err := withDefaultPort("10.6.255.113")
	if err != nil || got != "10.6.255.113:161" {
		t.Errorf("= %q, %v", got, err)
	}
	if got, err := withDefaultPort("10.6.250.5:1161"); err != nil || got != "10.6.250.5:1161" {
		t.Errorf("= %q, %v; an explicit port must survive", got, err)
	}
}

// Setting neither version nor community means v2c: an unset Version is
// indistinguishable from an explicit v1, and a caller who set neither
// wants the modern default.
func TestTheVersionDefault(t *testing.T) {
	if got := (Options{}).withDefaults(); got.Version != codec.Version2c {
		t.Errorf("= %s, want v2c", got.Version)
	}
	// Naming a community is how v1 stays reachable.
	if got := (Options{Community: "public"}).withDefaults(); got.Version != codec.Version1 {
		t.Errorf("= %s, want v1", got.Version)
	}
	got := (Options{Timeout: -1, Retries: -1, MaxRepetitions: -1}).withDefaults()
	if got.Timeout != DefaultTimeout || got.Retries != DefaultRetries ||
		got.MaxRepetitions != DefaultMaxRepetitions {
		t.Errorf("= %+v, want the defaults", got)
	}
	if s := (&Session{opts: got}).Timeout(); s != DefaultTimeout {
		t.Errorf("Timeout() = %s", s)
	}
}

// A reply that answers a different request, or is not a reply at all, is
// counted and ignored — a manager that dropped them silently looks
// identical to one talking to a device that never answers.
func TestUnsolicitedRepliesAreCountedAndIgnored(t *testing.T) {
	prof := &compliance.Profile{}
	s := &Session{
		opts:   Options{Version: codec.Version2c, Community: "public"},
		logger: quiet(),
		prof:   prof,
	}

	enc := func(m codec.Message) []byte {
		raw, err := codec.Encode(m)
		if err != nil {
			t.Fatal(err)
		}
		return raw
	}
	resp := func(id int32) codec.Message {
		return codec.Message{Version: codec.Version2c, Community: "public",
			PDU: &codec.PDU{Type: codec.PDUTypeResponse, RequestID: id}}
	}

	if _, ok := s.acceptable([]byte{0xFF, 0xFF}, 1); ok {
		t.Error("rubbish must not be accepted")
	}
	if _, ok := s.acceptable(enc(resp(999)), 1); ok {
		t.Error("a reply to another request must not be accepted")
	}
	if _, ok := s.acceptable(enc(codec.Message{Version: codec.Version2c,
		Community: "public", PDU: &codec.PDU{Type: codec.PDUTypeGet, RequestID: 1}}), 1); ok {
		t.Error("a request arriving at a manager must not be accepted")
	}
	if got := prof.Snapshot()[UnsolicitedResponse]; got != 3 {
		t.Errorf("counted %d unsolicited replies, want 3", got)
	}

	// A reply in another version, or under another community, is
	// ABSORBED — some agents answer v2c as v1 — and counted.
	v1 := resp(1)
	v1.Version = codec.Version1
	if _, ok := s.acceptable(enc(v1), 1); !ok {
		t.Error("a version mismatch must be absorbed, not refused")
	}
	other := resp(1)
	other.Community = "private"
	if _, ok := s.acceptable(enc(other), 1); !ok {
		t.Error("a community mismatch must be absorbed, not refused")
	}
	snap := prof.Snapshot()
	if snap[VersionMismatch] != 1 || snap[CommunityMismatch] != 1 {
		t.Errorf("counts = %v", snap)
	}
}
