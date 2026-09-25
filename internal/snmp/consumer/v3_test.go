package consumer

import (
	"context"
	"errors"
	"strings"
	"testing"

	"dhs/internal/clock"
	"dhs/internal/consumer/compliance"
	"dhs/internal/plugin"
	"dhs/internal/snmp/codec"
	"dhs/internal/snmp/provider"
	"dhs/internal/snmp/usm"
)

// v3 polling, manager side, against our own agent — which is the only
// peer that can be asked to reboot, forget a user or answer with a
// Report on demand. The IRDs in the lab prove the wire; this proves the
// state machine.

// v3AgentUnder starts an agent that speaks v3 as the given users and
// returns its address.
func v3AgentUnder(t *testing.T, boots int32, users ...usm.User) (string, *usm.Engine) {
	t.Helper()
	id, err := usm.NewEngineID(usm.Enterprise, "agent-under-test")
	if err != nil {
		t.Fatal(err)
	}
	engine, err := usm.NewEngine(id, boots, clock.System())
	if err != nil {
		t.Fatal(err)
	}
	for _, u := range users {
		if err := engine.AddUser(u); err != nil {
			t.Fatal(err)
		}
	}
	addr := agentUnderWithEngine(t, provider.Communities{Read: "public", Write: "private"}, engine)
	return addr, engine
}

func v3Session(t *testing.T, addr string, cred *V3, prof compliance.Recorder) *Session {
	t.Helper()
	s, err := Dial(context.Background(), Options{
		Addr: addr, Version: codec.Version3, V3: cred, Compliance: prof,
	}, plugin.Deps{Logger: quiet()})
	if err != nil {
		t.Fatalf("Dial v3: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func TestV3PollsAtEverySecurityLevel(t *testing.T) {
	// The three levels are three different messages on the wire — a
	// plain scoped PDU, a signed one, and a signed encrypted one — so
	// each is exercised end to end rather than assumed from the last.
	users := []usm.User{
		{Name: "plain"},
		{Name: "signed", Auth: usm.HMACSHA256, AuthPass: "authpass-authpass"},
		{Name: "sealed", Auth: usm.HMACSHA256, AuthPass: "authpass-authpass",
			Priv: usm.AES128CFB, PrivPass: "privpass-privpass"},
	}
	addr, _ := v3AgentUnder(t, 1, users...)

	for _, u := range users {
		t.Run(u.SecurityLevel(), func(t *testing.T) {
			s := v3Session(t, addr, &V3{
				User: u.Name, Auth: u.Auth, AuthPass: u.AuthPass,
				Priv: u.Priv, PrivPass: u.PrivPass,
			}, nil)
			binds, err := s.Get(context.Background(), oid("1.3.6.1.2.1.1.1.0"))
			if err != nil {
				t.Fatalf("v3 GET: %v", err)
			}
			if len(binds) != 1 || string(binds[0].Value.Bytes) != "dhs SNMP agent under test" {
				t.Errorf("sysDescr = %+v", binds)
			}
			// The verbs above GET have to work too: they all go
			// through the same sealed round trip.
			next, err := s.GetNext(context.Background(), oid("1.3.6.1.2.1.1.1.0"))
			if err != nil || len(next) == 0 {
				t.Errorf("v3 GETNEXT: %v %+v", err, next)
			}
			var walked int
			if err := s.Walk(context.Background(), oid("1.3.6.1.2.1.1"),
				func(codec.VarBind) error { walked++; return nil }); err != nil {
				t.Errorf("v3 walk: %v", err)
			}
			if walked < 6 {
				t.Errorf("v3 walked %d objects of the system group", walked)
			}
		})
	}
}

func TestV3DiscoveryLearnsTheAgentsEngine(t *testing.T) {
	// A manager is authoritative for nothing: everything it seals is
	// keyed on the agent's engine ID and stamped with the agent's
	// clock. If discovery did not happen, nothing else could.
	addr, agent := v3AgentUnder(t, 7, usm.User{Name: "plain"})
	s := v3Session(t, addr, &V3{User: "plain"}, nil)

	s.mu.Lock()
	engine := s.engine
	s.mu.Unlock()
	if engine == nil {
		t.Fatal("the session discovered no engine")
	}
	if got, want := string(engine.ID()), string(agent.ID()); got != want {
		t.Errorf("engine id = %x, want the agent's %x", got, want)
	}
	if got := engine.Boots(); got != 7 {
		t.Errorf("boots = %d, want the agent's 7", got)
	}
}

func TestV3SaysWhichCredentialTheAgentRefused(t *testing.T) {
	// The whole point of a Report: a wrong password and a dead device
	// are the same silence otherwise, and an operator needs to know
	// which one they have.
	addr, _ := v3AgentUnder(t, 1, usm.User{
		Name: "signed", Auth: usm.HMACSHA256, AuthPass: "authpass-authpass"})

	cases := []struct {
		name string
		cred *V3
		want string
	}{
		{"no such user", &V3{User: "nobody"}, "no such user"},
		{"wrong password", &V3{User: "signed", Auth: usm.HMACSHA256,
			AuthPass: "wrongpass-wrongpass"}, "password is wrong"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			s, err := Dial(context.Background(), Options{
				Addr: addr, Version: codec.Version3, V3: c.cred, Retries: 0,
			}, plugin.Deps{Logger: quiet()})
			if err != nil {
				// Refused at dial is just as good an answer, as long
				// as it names the reason.
				if !strings.Contains(err.Error(), c.want) {
					t.Fatalf("dial error = %v, want it to name %q", err, c.want)
				}
				return
			}
			defer func() { _ = s.Close() }()
			_, err = s.Get(context.Background(), oid("1.3.6.1.2.1.1.1.0"))
			if err == nil {
				t.Fatal("a credential the agent refuses must not read")
			}
			if !errors.Is(err, ErrReport) {
				t.Fatalf("err = %v, want a Report", err)
			}
			if !strings.Contains(err.Error(), c.want) {
				t.Errorf("err = %v, want it to name %q", err, c.want)
			}
		})
	}
}

func TestV3ReportErrorCarriesTheCounter(t *testing.T) {
	// Every counter RFC 3414 §5 defines gets an operator-facing line,
	// and the two a rediscovery can clear are marked so.
	cases := []struct {
		counter codec.OID
		retry   bool
		says    string
	}{
		{usmStatsNotInTimeWindows, true, "clock"},
		{usmStatsUnknownEngineIDs, true, "engine ID"},
		{usmStatsUnknownUserNames, false, "no such user"},
		{usmStatsWrongDigests, false, "authentication password"},
		{usmStatsDecryptionErrors, false, "privacy password"},
		{usmStatsUnsupportedSecLvl, false, "security level"},
	}
	for _, c := range cases {
		err := reportError(&codec.PDU{Type: codec.PDUTypeReport,
			VarBinds: []codec.VarBind{{Name: c.counter, Value: codec.Counter32(1)}}})
		if !errors.Is(err, ErrReport) {
			t.Errorf("%s: not a Report", c.counter)
		}
		if !strings.Contains(err.Error(), c.says) {
			t.Errorf("%s: %v does not say %q", c.counter, err, c.says)
		}
		if got := recoverable(err); got != c.retry {
			t.Errorf("%s: recoverable = %v, want %v", c.counter, got, c.retry)
		}
	}

	// A counter nothing here knows about is still a refusal, named by
	// its OID rather than guessed at.
	unknown := reportError(&codec.PDU{Type: codec.PDUTypeReport,
		VarBinds: []codec.VarBind{{Name: oid("1.3.6.1.6.3.15.1.1.9.0")}}})
	if !errors.Is(unknown, ErrReport) || !strings.Contains(unknown.Error(), "1.3.6.1.6.3.15.1.1.9.0") {
		t.Errorf("unknown counter = %v", unknown)
	}
	// And a Report with no varbinds at all is still a Report.
	empty := reportError(&codec.PDU{Type: codec.PDUTypeReport})
	if !errors.Is(empty, ErrReport) || recoverable(empty) {
		t.Errorf("empty report = %v", empty)
	}
	// Anything that is not a Report is not recoverable by rediscovery.
	if recoverable(ErrTimeout) {
		t.Error("a timeout is not a Report")
	}
}

func TestV3ResyncsWhenTheAgentsClockMovedOn(t *testing.T) {
	// An agent that reboots gets a new boot count, and every message
	// keyed on the old one is outside its time window. The agent says
	// so with a Report; a manager that ignored it would report a live
	// agent as down for as long as it stayed up.
	user := usm.User{Name: "signed", Auth: usm.HMACSHA256, AuthPass: "authpass-authpass"}
	addr, _ := v3AgentUnder(t, 1, user)
	prof := &compliance.Profile{}
	s := v3Session(t, addr, &V3{User: user.Name, Auth: user.Auth, AuthPass: user.AuthPass}, prof)

	// Forge exactly the state a reboot produces: the session believes
	// an engine that the agent no longer agrees with.
	stale, err := usm.NewRemoteEngine([]byte("not-this-agent"), 1, 0, clock.System())
	if err != nil {
		t.Fatal(err)
	}
	if err := stale.AddUser(user); err != nil {
		t.Fatal(err)
	}
	s.mu.Lock()
	s.engine = stale
	s.mu.Unlock()

	if _, err := s.Get(context.Background(), oid("1.3.6.1.2.1.1.1.0")); err != nil {
		t.Fatalf("the session must recover by rediscovering: %v", err)
	}
	if got := prof.Snapshot()[EngineResynced]; got == 0 {
		t.Error("a resync must be counted, not silent")
	}
	// And the session is usable afterwards, not just for that one read.
	if _, err := s.Get(context.Background(), oid("1.3.6.1.2.1.1.5.0")); err != nil {
		t.Errorf("after a resync the session must keep working: %v", err)
	}
}

func TestV3SessionWithoutAnEngineRefusesToSealOrOpen(t *testing.T) {
	// The internal contract, asserted rather than assumed: nothing
	// reaches the wire before discovery has run.
	s := &Session{opts: Options{Version: codec.Version3, V3: &V3{User: "plain"}},
		logger: quiet(), clk: clock.System(), prof: noRecorder{}}
	if _, err := s.sealV3(&codec.PDU{Type: codec.PDUTypeGet}); err == nil {
		t.Error("sealing without a discovered engine must fail")
	}
	if _, err := s.openV3([]byte{0x30, 0x00}); err == nil {
		t.Error("opening without a discovered engine must fail")
	}
}

func TestV3DiscoveryRefusesAnAgentThatNamesNoEngine(t *testing.T) {
	// An agent that answers discovery without an engine ID has given a
	// manager nothing to key on. Better to say so than to seal against
	// an empty identity and time out.
	addr := agentUnder(t, provider.Communities{Read: "public"}) // v1/v2c only: no engine
	_, err := Dial(context.Background(), Options{
		Addr: addr, Version: codec.Version3, V3: &V3{User: "plain"}, Retries: 0,
	}, plugin.Deps{Logger: quiet()})
	if err == nil {
		t.Fatal("an agent that does not speak v3 must not produce a session")
	}
	if !strings.Contains(err.Error(), "discovery") {
		t.Errorf("err = %v, want it to name discovery", err)
	}
}
