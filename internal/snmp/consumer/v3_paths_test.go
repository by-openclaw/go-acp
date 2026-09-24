package consumer

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
	"dhs/internal/snmp/usm"
)

// The paths a well-behaved agent never takes: an agent that answers
// discovery in the wrong version, one that names no engine, a datagram
// that cannot be opened, a socket that dies mid-exchange. Each of them
// is a real field failure — a proxy in the middle, a half-configured
// agent, a firewall closing a port — and each has to produce a sentence
// an operator can act on rather than a timeout.

// rawAgent answers every datagram with what the script returns.
// Returning nil answers nothing, which is how a timeout is staged.
func rawAgent(t *testing.T, reply func(req []byte) []byte) string {
	t.Helper()
	pc, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = pc.Close() })
	go func() {
		buf := make([]byte, 64*1024)
		for {
			n, from, rerr := pc.ReadFrom(buf)
			if rerr != nil {
				return
			}
			if out := reply(append([]byte(nil), buf[:n]...)); out != nil {
				_, _ = pc.WriteTo(out, from)
			}
		}
	}()
	return pc.LocalAddr().String()
}

func v3Dial(t *testing.T, addr string, cred *V3) (*Session, error) {
	t.Helper()
	return Dial(context.Background(), Options{
		Addr: addr, Version: codec.Version3, V3: cred, Retries: 0,
		Timeout: 200 * time.Millisecond,
	}, plugin.Deps{Logger: quiet()})
}

func TestV3DiscoveryRefusalsAreNamed(t *testing.T) {
	cases := []struct {
		name  string
		reply func([]byte) []byte
		want  string
	}{
		{
			name: "answered in v2c",
			reply: func([]byte) []byte {
				raw, _ := codec.Encode(codec.Message{Version: codec.Version2c,
					Community: "public",
					PDU:       &codec.PDU{Type: codec.PDUTypeResponse, RequestID: 1}})
				return raw
			},
			want: "discovery answered in",
		},
		{
			name: "no engine id",
			reply: func([]byte) []byte {
				params, _ := codec.EncodeUSMParameters(codec.USMParameters{})
				raw, _ := codec.Encode(codec.Message{Version: codec.Version3,
					V3: &codec.V3{ID: 1, MaxSize: codec.DefaultMaxSize,
						SecurityModel: codec.SecurityModelUSM, SecurityParameters: params},
					PDU: &codec.PDU{Type: codec.PDUTypeReport, RequestID: 1}})
				return raw
			},
			want: "named no engine ID",
		},
		{
			name: "security parameters that are not USM's",
			reply: func([]byte) []byte {
				raw, _ := codec.Encode(codec.Message{Version: codec.Version3,
					V3: &codec.V3{ID: 1, MaxSize: codec.DefaultMaxSize,
						SecurityModel:      codec.SecurityModelUSM,
						SecurityParameters: []byte{0x04, 0x01, 0x00}},
					PDU: &codec.PDU{Type: codec.PDUTypeReport, RequestID: 1}})
				return raw
			},
			want: "discovery parameters",
		},
		{
			name:  "nothing at all",
			reply: func([]byte) []byte { return nil },
			want:  "discovery",
		},
		{
			name:  "not an SNMP message",
			reply: func([]byte) []byte { return []byte{0xFF, 0xFF, 0xFF} },
			want:  "discovery reply",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			addr := rawAgent(t, c.reply)
			_, err := v3Dial(t, addr, &V3{User: "plain"})
			if err == nil {
				t.Fatal("this agent cannot produce a usable session")
			}
			if !strings.Contains(err.Error(), c.want) {
				t.Errorf("err = %v, want it to name %q", err, c.want)
			}
		})
	}
}

func TestV3DiscoveryAbsorbsAResponseWhereAReportBelongs(t *testing.T) {
	// RFC 3414 §4 says Report. Agents exist that answer the probe with
	// a Response carrying the same engine identity — which is usable,
	// so it is used, and counted rather than refused.
	id, err := usm.NewEngineID(usm.Enterprise, "scripted")
	if err != nil {
		t.Fatal(err)
	}
	addr := rawAgent(t, func([]byte) []byte {
		params, _ := codec.EncodeUSMParameters(codec.USMParameters{
			AuthoritativeEngineID: id, AuthoritativeEngineBoots: 3, AuthoritativeEngineTime: 9,
		})
		raw, _ := codec.Encode(codec.Message{Version: codec.Version3,
			V3: &codec.V3{ID: 1, MaxSize: codec.DefaultMaxSize,
				SecurityModel: codec.SecurityModelUSM, SecurityParameters: params},
			PDU: &codec.PDU{Type: codec.PDUTypeResponse, RequestID: 1}})
		return raw
	})
	prof := &compliance.Profile{}
	s, err := Dial(context.Background(), Options{
		Addr: addr, Version: codec.Version3, V3: &V3{User: "plain"},
		Retries: 0, Timeout: 200 * time.Millisecond, Compliance: prof,
	}, plugin.Deps{Logger: quiet()})
	if err != nil {
		t.Fatalf("a usable discovery answer must be accepted: %v", err)
	}
	defer func() { _ = s.Close() }()
	if got := prof.Snapshot()[DiscoveryNotAReport]; got != 1 {
		t.Errorf("the deviation must be counted once, got %d", got)
	}
}

func TestV3NoticesTheEngineChangingUnderIt(t *testing.T) {
	// Two engines answering one address is a proxy, a failover pair, or
	// a device that was swapped. The session keeps working — it just
	// says so.
	first, err := usm.NewEngineID(usm.Enterprise, "first")
	if err != nil {
		t.Fatal(err)
	}
	second, err := usm.NewEngineID(usm.Enterprise, "second")
	if err != nil {
		t.Fatal(err)
	}
	var calls int
	addr := rawAgent(t, func([]byte) []byte {
		calls++
		id := first
		if calls > 1 {
			id = second
		}
		params, _ := codec.EncodeUSMParameters(codec.USMParameters{
			AuthoritativeEngineID: id, AuthoritativeEngineBoots: 1, AuthoritativeEngineTime: 1,
		})
		raw, _ := codec.Encode(codec.Message{Version: codec.Version3,
			V3: &codec.V3{ID: 1, MaxSize: codec.DefaultMaxSize,
				SecurityModel: codec.SecurityModelUSM, SecurityParameters: params},
			PDU: &codec.PDU{Type: codec.PDUTypeReport, RequestID: 1}})
		return raw
	})
	prof := &compliance.Profile{}
	s, err := Dial(context.Background(), Options{
		Addr: addr, Version: codec.Version3, V3: &V3{User: "plain"},
		Retries: 0, Timeout: 200 * time.Millisecond, Compliance: prof,
	}, plugin.Deps{Logger: quiet()})
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer func() { _ = s.Close() }()
	if err := s.discover(context.Background()); err != nil {
		t.Fatalf("rediscovery: %v", err)
	}
	if got := prof.Snapshot()[EngineChanged]; got != 1 {
		t.Errorf("an engine swap must be counted once, got %d", got)
	}
	if engineIDsEqual(first, second) || !engineIDsEqual(first, first) {
		t.Error("engineIDsEqual compares the bytes, not the lengths")
	}
	if engineIDsEqual(first, first[:len(first)-1]) {
		t.Error("a prefix is not the same engine")
	}
	// Same length, one byte apart — which is what two engines from one
	// vendor's ID block look like, and the case a length comparison
	// alone would call equal.
	nearly := append([]byte(nil), first...)
	nearly[len(nearly)-1] ^= 0x01
	if engineIDsEqual(first, nearly) {
		t.Error("two engine IDs differing in one byte are not the same engine")
	}
}

func TestV3ExchangeGivesUpOnADeadSocket(t *testing.T) {
	// A closed socket is not a timeout and must not be retried as one.
	addr := rawAgent(t, func([]byte) []byte { return nil })
	s := &Session{
		opts:   Options{Version: codec.Version3, V3: &V3{User: "plain"}, Timeout: time.Second},
		logger: quiet(), clk: clock.System(), prof: noRecorder{},
	}
	conn, err := net.Dial("udp", addr)
	if err != nil {
		t.Fatal(err)
	}
	_ = conn.Close()
	s.conn = conn
	if _, err := s.exchange(context.Background(), conn, []byte{0x30, 0x00}); err == nil {
		t.Error("a write to a closed socket must fail")
	}
	// A context already done is answered before anything is sent.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := s.exchange(ctx, conn, []byte{0x30, 0x00}); err == nil {
		t.Error("a cancelled context must stop the exchange")
	}
	// And discovery on a dead socket reports as discovery, not as a
	// mystery.
	if err := s.discover(ctx); err == nil {
		t.Error("discovery on a cancelled context must fail")
	}
}

func TestV3IgnoresDatagramsItCannotOpen(t *testing.T) {
	// Somebody else's traffic on our port, or a tampered reply: counted
	// and waited past, never treated as this request's answer.
	user := usm.User{Name: "signed", Auth: usm.HMACSHA256, AuthPass: "authpass-authpass"}
	addr, _ := v3AgentUnder(t, 1, user)
	prof := &compliance.Profile{}
	s := v3Session(t, addr, &V3{User: user.Name, Auth: user.Auth, AuthPass: user.AuthPass}, prof)

	if pdu, err := s.acceptable([]byte("not an snmp message at all"), 1); pdu != nil || err != nil {
		t.Errorf("unopenable bytes = %v, %v; want ignored", pdu, err)
	}
	// A message this session CAN open, but which answers another
	// request, is also ignored rather than returned.
	s.mu.Lock()
	engine := s.engine
	s.mu.Unlock()
	other, err := engine.Seal(codec.Message{Version: codec.Version3,
		V3:  &codec.V3{ID: 9, MaxSize: codec.DefaultMaxSize, ContextEngineID: engine.ID()},
		PDU: &codec.PDU{Type: codec.PDUTypeResponse, RequestID: 999}}, user.Name)
	if err != nil {
		t.Fatal(err)
	}
	if pdu, err := s.acceptable(other, 1); pdu != nil || err != nil {
		t.Errorf("a reply to another request = %v, %v; want ignored", pdu, err)
	}
	// And a PDU that is not a Response at all.
	notAReply, err := engine.Seal(codec.Message{Version: codec.Version3,
		V3:  &codec.V3{ID: 10, MaxSize: codec.DefaultMaxSize, ContextEngineID: engine.ID()},
		PDU: &codec.PDU{Type: codec.PDUTypeGet, RequestID: 1}}, user.Name)
	if err != nil {
		t.Fatal(err)
	}
	if pdu, err := s.acceptable(notAReply, 1); pdu != nil || err != nil {
		t.Errorf("a request arriving at a manager = %v, %v; want ignored", pdu, err)
	}
	if got := prof.Snapshot()[UnsolicitedResponse]; got != 3 {
		t.Errorf("counted %d unsolicited datagrams, want 3", got)
	}
}

func TestV3RoundTripStopsWhenRediscoveryItselfFails(t *testing.T) {
	// The agent says "re-discover", and then does not answer the
	// discovery. That is the agent going away mid-session, and the
	// error an operator sees must say discovery rather than repeating
	// the Report forever.
	var seen int
	addr := rawAgent(t, func(req []byte) []byte {
		seen++
		if seen == 1 {
			// Discovery at Dial: answer properly.
			id, _ := usm.NewEngineID(usm.Enterprise, "going-away")
			params, _ := codec.EncodeUSMParameters(codec.USMParameters{
				AuthoritativeEngineID: id, AuthoritativeEngineBoots: 1, AuthoritativeEngineTime: 1,
			})
			raw, _ := codec.Encode(codec.Message{Version: codec.Version3,
				V3: &codec.V3{ID: 1, MaxSize: codec.DefaultMaxSize,
					SecurityModel: codec.SecurityModelUSM, SecurityParameters: params},
				PDU: &codec.PDU{Type: codec.PDUTypeReport, RequestID: 1}})
			return raw
		}
		if seen == 2 {
			// The request: refuse it with a counter that asks for a
			// rediscovery.
			params, _ := codec.EncodeUSMParameters(codec.USMParameters{})
			raw, _ := codec.Encode(codec.Message{Version: codec.Version3,
				V3: &codec.V3{ID: 2, MaxSize: codec.DefaultMaxSize,
					SecurityModel: codec.SecurityModelUSM, SecurityParameters: params},
				PDU: &codec.PDU{Type: codec.PDUTypeReport, RequestID: 1,
					VarBinds: []codec.VarBind{{Name: usmStatsNotInTimeWindows}}}})
			return raw
		}
		return nil // and then silence
	})
	s, err := v3Dial(t, addr, &V3{User: "plain"})
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer func() { _ = s.Close() }()
	_, err = s.Get(context.Background(), oid("1.3.6.1.2.1.1.1.0"))
	if err == nil {
		t.Fatal("an agent that stopped answering must not look like a success")
	}
	if !strings.Contains(err.Error(), "discovery") {
		t.Errorf("err = %v, want it to name discovery", err)
	}
}

// brokenConn is a socket that fails where a real one does: a write that
// cannot leave, a read that fails for a reason that is not a timeout.
// Both are ordinary on a fabric — an interface going down, a port
// closing — and neither is a retry.
type brokenConn struct {
	net.Conn
	writeErr error
	readErr  error
}

func (c *brokenConn) SetDeadline(time.Time) error { return nil }
func (c *brokenConn) Write([]byte) (int, error) {
	if c.writeErr != nil {
		return 0, c.writeErr
	}
	return 0, nil
}
func (c *brokenConn) Read([]byte) (int, error) { return 0, c.readErr }
func (c *brokenConn) Close() error             { return nil }

func v3BareSession() *Session {
	return &Session{
		opts:   Options{Version: codec.Version3, V3: &V3{User: "plain"}, Timeout: time.Second},
		logger: quiet(), clk: clock.System(), prof: noRecorder{},
	}
}

func TestV3ExchangeReportsSocketFaultsAsThemselves(t *testing.T) {
	s := v3BareSession()
	if _, err := s.exchange(context.Background(),
		&brokenConn{writeErr: errors.New("network is down")}, []byte{0x30, 0x00}); err == nil ||
		!strings.Contains(err.Error(), "send") {
		t.Errorf("= %v, want the send named", err)
	}
	if _, err := s.exchange(context.Background(),
		&brokenConn{readErr: errors.New("connection refused")}, []byte{0x30, 0x00}); err == nil ||
		!strings.Contains(err.Error(), "receive") {
		t.Errorf("= %v, want the receive named", err)
	}
}

func TestV3ExchangeHonoursAContextDeadlineShorterThanTheTimeout(t *testing.T) {
	// The session's timeout is a ceiling, not a floor: a caller that
	// gave the whole operation 50ms does not get to wait a second here.
	addr := rawAgent(t, func([]byte) []byte { return nil })
	conn, err := net.Dial("udp", addr)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = conn.Close() }()
	s := v3BareSession()
	s.opts.Timeout = time.Hour

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	start := time.Now()
	if _, err := s.exchange(ctx, conn, []byte{0x30, 0x00}); err == nil {
		t.Fatal("an unanswered exchange must not succeed")
	}
	if waited := time.Since(start); waited > 5*time.Second {
		t.Errorf("waited %s — the context deadline was ignored", waited)
	}
}

func TestV3DiscoveryRefusesAnImpossibleEngine(t *testing.T) {
	// An agent that reports a negative engine time has said something
	// no clock can mean. Refused here, where it is one line, rather
	// than by every message sealed against it afterwards.
	id, err := usm.NewEngineID(usm.Enterprise, "negative")
	if err != nil {
		t.Fatal(err)
	}
	addr := rawAgent(t, func([]byte) []byte {
		params, _ := codec.EncodeUSMParameters(codec.USMParameters{
			AuthoritativeEngineID: id, AuthoritativeEngineBoots: 1, AuthoritativeEngineTime: -5,
		})
		raw, _ := codec.Encode(codec.Message{Version: codec.Version3,
			V3: &codec.V3{ID: 1, MaxSize: codec.DefaultMaxSize,
				SecurityModel: codec.SecurityModelUSM, SecurityParameters: params},
			PDU: &codec.PDU{Type: codec.PDUTypeReport, RequestID: 1}})
		return raw
	})
	_, err = v3Dial(t, addr, &V3{User: "plain"})
	if err == nil || !strings.Contains(err.Error(), "engine") {
		t.Errorf("= %v, want the engine refused", err)
	}
}

func TestV3DiscoveryRefusesAUserTheModelWontHave(t *testing.T) {
	// Dial validates the credential, so this is reachable only from a
	// resync — but a resync is exactly when nobody is watching, and
	// "no keys, no session" has to hold there too.
	id, err := usm.NewEngineID(usm.Enterprise, "fine")
	if err != nil {
		t.Fatal(err)
	}
	addr := rawAgent(t, func([]byte) []byte {
		params, _ := codec.EncodeUSMParameters(codec.USMParameters{
			AuthoritativeEngineID: id, AuthoritativeEngineBoots: 1, AuthoritativeEngineTime: 1,
		})
		raw, _ := codec.Encode(codec.Message{Version: codec.Version3,
			V3: &codec.V3{ID: 1, MaxSize: codec.DefaultMaxSize,
				SecurityModel: codec.SecurityModelUSM, SecurityParameters: params},
			PDU: &codec.PDU{Type: codec.PDUTypeReport, RequestID: 1}})
		return raw
	})
	conn, err := net.Dial("udp", addr)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = conn.Close() }()

	s := v3BareSession()
	// Authentication with no password: a user USM will not derive keys for.
	s.opts.V3 = &V3{User: "signed", Auth: usm.HMACSHA256}
	s.conn = conn
	if err := s.discover(context.Background()); err == nil ||
		!strings.Contains(err.Error(), "user") {
		t.Errorf("= %v, want the user refused", err)
	}
}

func TestV3ReadsAnAuthenticatedReport(t *testing.T) {
	// Our agent answers with unauthenticated Reports, which is what
	// discovery needs. An agent that SIGNS its Reports — RFC 3414 §3.2
	// allows it once keys are agreed — must be read just as well, or
	// the manager sees "cannot open" instead of the reason.
	user := usm.User{Name: "signed", Auth: usm.HMACSHA256, AuthPass: "authpass-authpass"}
	addr, agentEngine := v3AgentUnder(t, 1, user)
	s := v3Session(t, addr, &V3{User: user.Name, Auth: user.Auth, AuthPass: user.AuthPass}, nil)

	sealed, err := agentEngine.Seal(codec.Message{
		Version: codec.Version3,
		V3:      &codec.V3{ID: 5, MaxSize: codec.DefaultMaxSize, ContextEngineID: agentEngine.ID()},
		PDU: &codec.PDU{Type: codec.PDUTypeReport, RequestID: 1,
			VarBinds: []codec.VarBind{{Name: usmStatsUnknownUserNames, Value: codec.Counter32(1)}}},
	}, user.Name)
	if err != nil {
		t.Fatal(err)
	}
	_, aerr := s.acceptable(sealed, 1)
	if aerr == nil || !strings.Contains(aerr.Error(), "no such user") {
		t.Errorf("= %v, want the signed Report read and named", aerr)
	}
}
