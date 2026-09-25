package provider

import (
	"context"
	"errors"
	"net"
	"strings"
	"sync"
	"testing"
	"time"

	"dhs/internal/clock"
	"dhs/internal/plugin"
	"dhs/internal/snmp/codec"
	"dhs/internal/snmp/usm"
)

// An inform is a notification that is acknowledged, so everything worth
// asserting about one is about the acknowledgement: that it is waited
// for, that a missing one is retried, that the retry carries the same
// request-id, and that giving up says which manager did not answer.

// informReceiver is a manager that answers informs, or does not.
// answered counts what it acknowledged; seen counts every datagram,
// including the retries it deliberately ignored.
type informReceiver struct {
	addr string

	mu       sync.Mutex
	seen     []codec.PDU
	ignore   int // ignore this many datagrams before answering
	engine   *usm.Engine
	sealAs   string
	answered int
}

func newInformReceiver(t *testing.T, ignore int) *informReceiver {
	t.Helper()
	pc, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	r := &informReceiver{addr: pc.LocalAddr().String(), ignore: ignore}
	t.Cleanup(func() { _ = pc.Close() })
	go func() {
		buf := make([]byte, 64*1024)
		for {
			n, from, rerr := pc.ReadFrom(buf)
			if rerr != nil {
				return
			}
			raw := append([]byte(nil), buf[:n]...)
			m, derr := codec.Decode(raw)
			if derr != nil {
				continue
			}
			r.mu.Lock()
			user := ""
			if m.Version == codec.Version3 && r.engine != nil {
				opened, u, oerr := r.engine.Open(raw)
				if oerr != nil {
					r.mu.Unlock()
					continue
				}
				m, user = opened, u.Name
			}
			if m.PDU != nil {
				r.seen = append(r.seen, *m.PDU)
			}
			skip := r.ignore > 0
			if skip {
				r.ignore--
			}
			engine, sealAs := r.engine, r.sealAs
			if !skip {
				r.answered++
			}
			r.mu.Unlock()
			if skip || m.PDU == nil {
				continue
			}
			resp := codec.PDU{Type: codec.PDUTypeResponse,
				RequestID: m.PDU.RequestID, VarBinds: m.PDU.VarBinds}
			var out []byte
			if m.Version == codec.Version3 && engine != nil {
				if sealAs == "" {
					sealAs = user
				}
				out, _ = engine.Seal(codec.Message{Version: codec.Version3,
					V3:  &codec.V3{ID: m.V3.ID, MaxSize: codec.DefaultMaxSize},
					PDU: &resp}, sealAs)
			} else {
				out, _ = codec.Encode(codec.Message{Version: m.Version,
					Community: m.Community, PDU: &resp})
			}
			_, _ = pc.WriteTo(out, from)
		}
	}()
	return r
}

func (r *informReceiver) pdus() []codec.PDU {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]codec.PDU(nil), r.seen...)
}

func testNotification() Notification {
	return Notification{
		Enterprise: codec.MustParseOID("1.3.6.1.4.1.54981.1.1"),
		Generic:    codec.EnterpriseSpecific,
		Specific:   1,
		Uptime:     1234,
		VarBinds: []codec.VarBind{
			{Name: codec.MustParseOID("1.3.6.1.2.1.1.5.0"), Value: codec.String("agent")},
		},
	}
}

func TestAnInformIsWaitedForAndAcknowledged(t *testing.T) {
	r := newInformReceiver(t, 0)
	s := NewTrapSender([]TrapDestination{
		{Addr: r.addr, Version: codec.Version2c, Community: "public"},
	}, plugin.Deps{Logger: quiet(), Clock: clock.System()})
	defer func() { _ = s.Close() }()

	results := s.SendInform(context.Background(), testNotification())
	if len(results) != 1 {
		t.Fatalf("results = %d, want one per destination", len(results))
	}
	if results[0].Err != nil {
		t.Fatalf("an acknowledged inform must not report an error: %v", results[0].Err)
	}
	if results[0].Attempts != 1 {
		t.Errorf("attempts = %d, want 1 — the first was answered", results[0].Attempts)
	}
	pdus := r.pdus()
	if len(pdus) != 1 {
		t.Fatalf("the receiver saw %d datagrams, want 1", len(pdus))
	}
	if pdus[0].Type != codec.PDUTypeInform {
		t.Errorf("PDU = %v, want an InformRequest", pdus[0].Type)
	}
	// The two mandatory bindings are still there: an inform is a
	// notification, and RFC 3416 §4.2.6 applies to it too.
	if len(pdus[0].VarBinds) < 3 ||
		pdus[0].VarBinds[0].Name.Compare(SysUpTimeInstance) != 0 ||
		pdus[0].VarBinds[1].Name.Compare(SNMPTrapOID) != 0 {
		t.Errorf("varbinds = %+v, want sysUpTime.0 and snmpTrapOID.0 first", pdus[0].VarBinds)
	}
}

func TestAnUnansweredInformIsRetriedWithTheSameRequestID(t *testing.T) {
	// The retry is the whole point. And it carries the same request-id,
	// so a receiver that answers the second copy is still answering
	// THIS inform — and one that got both can tell they were one event.
	r := newInformReceiver(t, 1)
	s := NewTrapSender([]TrapDestination{
		{Addr: r.addr, Version: codec.Version2c, Community: "public"},
	}, plugin.Deps{Logger: quiet(), Clock: clock.System()})
	defer func() { _ = s.Close() }()

	results := s.SendInform(context.Background(), testNotification())
	if results[0].Err != nil {
		t.Fatalf("the retry must be acknowledged: %v", results[0].Err)
	}
	if results[0].Attempts != 2 {
		t.Errorf("attempts = %d, want 2", results[0].Attempts)
	}
	pdus := r.pdus()
	if len(pdus) != 2 {
		t.Fatalf("the receiver saw %d datagrams, want 2", len(pdus))
	}
	if pdus[0].RequestID != pdus[1].RequestID {
		t.Errorf("request ids %d and %d — a retry is the same inform",
			pdus[0].RequestID, pdus[1].RequestID)
	}
}

func TestAnInformNobodyAnswersSaysWhichManager(t *testing.T) {
	// "A notification failed" is not actionable. Which manager did not
	// answer is.
	r := newInformReceiver(t, 99)
	s := NewTrapSender([]TrapDestination{
		{Addr: r.addr, Version: codec.Version2c, Community: "public"},
	}, plugin.Deps{Logger: quiet(), Clock: clock.System()})
	defer func() { _ = s.Close() }()

	start := time.Now()
	results := s.SendInform(context.Background(), testNotification())
	if results[0].Err == nil {
		t.Fatal("an unanswered inform must not report success")
	}
	if !errors.Is(results[0].Err, ErrInformNotAcknowledged) {
		t.Errorf("err = %v, want ErrInformNotAcknowledged", results[0].Err)
	}
	if got := results[0].Err.Error(); !strings.Contains(got, r.addr) {
		t.Errorf("err = %q, want it to name %s", got, r.addr)
	}
	if results[0].Attempts != DefaultInformRetries+1 {
		t.Errorf("attempts = %d, want %d", results[0].Attempts, DefaultInformRetries+1)
	}
	// It gave up rather than retrying forever.
	if waited := time.Since(start); waited > 30*time.Second {
		t.Errorf("waited %s before giving up", waited)
	}
}

func TestAV1DestinationCannotBeInformed(t *testing.T) {
	// v1 has no InformRequest-PDU. Sending a trap instead and letting
	// the caller believe it was acknowledged is the one thing that
	// must not happen.
	s := NewTrapSender([]TrapDestination{
		{Addr: "127.0.0.1:1", Version: codec.Version1, Community: "public"},
	}, plugin.Deps{Logger: quiet(), Clock: clock.System()})
	defer func() { _ = s.Close() }()

	results := s.SendInform(context.Background(), testNotification())
	if results[0].Err == nil || !strings.Contains(results[0].Err.Error(), "v1 has no InformRequest") {
		t.Errorf("err = %v, want v1 refused with the reason", results[0].Err)
	}
}

func TestAV3InformIsSealedAndItsAcknowledgementOpened(t *testing.T) {
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
	r := newInformReceiver(t, 0)
	// The receiver answers with the sender's own engine, which is what
	// a real one does: a notification's authoritative engine is the
	// SENDER's, and the acknowledgement is scoped to it.
	r.mu.Lock()
	r.engine, r.sealAs = engine, user.Name
	r.mu.Unlock()

	s := NewTrapSenderV3([]TrapDestination{
		{Addr: r.addr, Version: codec.Version3, User: user.Name},
	}, engine, plugin.Deps{Logger: quiet(), Clock: clock.System()})
	defer func() { _ = s.Close() }()

	results := s.SendInform(context.Background(), testNotification())
	if results[0].Err != nil {
		t.Fatalf("a v3 inform must be acknowledged: %v", results[0].Err)
	}
	pdus := r.pdus()
	if len(pdus) != 1 || pdus[0].Type != codec.PDUTypeInform {
		t.Fatalf("the receiver saw %+v", pdus)
	}
}

func TestAV3InformWithoutAnEngineIsRefused(t *testing.T) {
	s := NewTrapSender([]TrapDestination{
		{Addr: "127.0.0.1:1", Version: codec.Version3, User: "operator"},
	}, plugin.Deps{Logger: quiet(), Clock: clock.System()})
	defer func() { _ = s.Close() }()

	results := s.SendInform(context.Background(), testNotification())
	if results[0].Err == nil || !strings.Contains(results[0].Err.Error(), "no USM engine") {
		t.Errorf("err = %v, want the missing engine named", results[0].Err)
	}
}

func TestSendInformStopsOnACancelledContext(t *testing.T) {
	r := newInformReceiver(t, 99)
	s := NewTrapSender([]TrapDestination{
		{Addr: r.addr, Version: codec.Version2c, Community: "public"},
	}, plugin.Deps{Logger: quiet(), Clock: clock.System()})
	defer func() { _ = s.Close() }()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	results := s.SendInform(ctx, testNotification())
	if results[0].Err == nil {
		t.Error("a cancelled context must not report a delivered inform")
	}
}


// The paths a good night never takes.

func TestAnInformForAVersionNobodyCanSendIsRefused(t *testing.T) {
	// A destination whose version this sender cannot render at all.
	s := NewTrapSender([]TrapDestination{
		{Addr: "127.0.0.1:1", Version: codec.Version(9), Community: "public"},
	}, plugin.Deps{Logger: quiet(), Clock: clock.System()})
	defer func() { _ = s.Close() }()
	results := s.SendInform(context.Background(), testNotification())
	if results[0].Err == nil {
		t.Error("a version this sender cannot build must be refused")
	}
}

func TestAnInformToAnAddressThatCannotBeDialledFails(t *testing.T) {
	s := NewTrapSender([]TrapDestination{
		{Addr: "no.such.host.invalid:162", Version: codec.Version2c, Community: "public"},
	}, plugin.Deps{Logger: quiet(), Clock: clock.System()})
	defer func() { _ = s.Close() }()
	results := s.SendInform(context.Background(), testNotification())
	if results[0].Err == nil {
		t.Error("a destination that does not resolve must not report delivery")
	}
}

func TestAnInformStopsWhenTheSocketDies(t *testing.T) {
	// Not a timeout, so not a retry: the socket is dropped so the next
	// notification redials rather than inheriting the error.
	r := newInformReceiver(t, 99)
	s := NewTrapSender([]TrapDestination{
		{Addr: r.addr, Version: codec.Version2c, Community: "public"},
	}, plugin.Deps{Logger: quiet(), Clock: clock.System()})
	defer func() { _ = s.Close() }()

	conn, err := s.connFor(context.Background(), r.addr)
	if err != nil {
		t.Fatal(err)
	}
	_ = conn.Close()
	results := s.SendInform(context.Background(), testNotification())
	if results[0].Err == nil {
		t.Error("a dead socket must not look like an acknowledgement")
	}
	if errors.Is(results[0].Err, ErrInformNotAcknowledged) {
		t.Error("a dead socket is a send failure, not an unanswered inform")
	}
}

func TestAnInformIgnoresDatagramsThatAreNotItsAcknowledgement(t *testing.T) {
	s := NewTrapSender(nil, plugin.Deps{Logger: quiet(), Clock: clock.System()})
	defer func() { _ = s.Close() }()
	d := TrapDestination{Addr: "127.0.0.1:1", Version: codec.Version2c, Community: "public"}

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
	if s.acknowledges([]byte("not snmp"), 1, d) {
		t.Error("rubbish is not an acknowledgement")
	}
	if s.acknowledges(enc(resp(999)), 1, d) {
		t.Error("an answer to another inform is not this one's")
	}
	if !s.acknowledges(enc(resp(1)), 1, d) {
		t.Error("the matching Response IS the acknowledgement")
	}
	// v3: a datagram this sender's engine cannot open is not an
	// acknowledgement, however well it is formed.
	id, err := usm.NewEngineID(usm.Enterprise, "sender")
	if err != nil {
		t.Fatal(err)
	}
	engine, err := usm.NewEngine(id, 1, clock.System())
	if err != nil {
		t.Fatal(err)
	}
	v3Sender := NewTrapSenderV3(nil, engine, plugin.Deps{Logger: quiet(), Clock: clock.System()})
	defer func() { _ = v3Sender.Close() }()
	if v3Sender.acknowledges(enc(resp(1)), 1,
		TrapDestination{Addr: "127.0.0.1:1", Version: codec.Version3, User: "operator"}) {
		t.Error("an unopenable datagram is not a v3 acknowledgement")
	}
}

func TestAnInformHonoursAContextDeadline(t *testing.T) {
	// The per-attempt timeout is a ceiling: a caller that gave the
	// whole thing 50ms does not wait a second per attempt.
	r := newInformReceiver(t, 99)
	s := NewTrapSender([]TrapDestination{
		{Addr: r.addr, Version: codec.Version2c, Community: "public"},
	}, plugin.Deps{Logger: quiet(), Clock: clock.System()})
	defer func() { _ = s.Close() }()

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	start := time.Now()
	results := s.SendInform(ctx, testNotification())
	if results[0].Err == nil {
		t.Error("an unanswered inform must not report success")
	}
	if waited := time.Since(start); waited > 2*time.Second {
		t.Errorf("waited %s — the context deadline was ignored", waited)
	}
}

// informConn hands informAttempt exactly the datagrams a test wants
// it to read, and can fail the write the way a downed interface does.
type informConn struct {
	net.Conn
	writeErr error
	reads    [][]byte
}

func (c *informConn) SetDeadline(time.Time) error { return nil }
func (c *informConn) Write([]byte) (int, error) {
	if c.writeErr != nil {
		return 0, c.writeErr
	}
	return 0, nil
}
func (c *informConn) Read(p []byte) (int, error) {
	if len(c.reads) == 0 {
		return 0, errors.New("nothing left to read")
	}
	next := c.reads[0]
	c.reads = c.reads[1:]
	return copy(p, next), nil
}
func (c *informConn) Close() error { return nil }

func TestAnInformAttemptReportsAWriteThatCannotLeave(t *testing.T) {
	s := NewTrapSender(nil, plugin.Deps{Logger: quiet(), Clock: clock.System()})
	defer func() { _ = s.Close() }()
	d := TrapDestination{Addr: "127.0.0.1:1", Version: codec.Version2c, Community: "public"}

	acked, err := s.informAttempt(context.Background(),
		&informConn{writeErr: errors.New("network is down")}, []byte{0x30, 0x00}, 1, d)
	if acked || err == nil {
		t.Errorf("acked=%v err=%v, want the write failure reported", acked, err)
	}
}

func TestAnInformWaitsPastSomebodyElsesDatagram(t *testing.T) {
	// A trap port carries everybody's traffic. A datagram that is not
	// this inform's acknowledgement must not end the wait for one.
	s := NewTrapSender(nil, plugin.Deps{Logger: quiet(), Clock: clock.System()})
	defer func() { _ = s.Close() }()
	d := TrapDestination{Addr: "127.0.0.1:1", Version: codec.Version2c, Community: "public"}

	other, err := codec.Encode(codec.Message{Version: codec.Version2c, Community: "public",
		PDU: &codec.PDU{Type: codec.PDUTypeResponse, RequestID: 777}})
	if err != nil {
		t.Fatal(err)
	}
	ours, err := codec.Encode(codec.Message{Version: codec.Version2c, Community: "public",
		PDU: &codec.PDU{Type: codec.PDUTypeResponse, RequestID: 42}})
	if err != nil {
		t.Fatal(err)
	}
	acked, err := s.informAttempt(context.Background(),
		&informConn{reads: [][]byte{other, ours}}, []byte{0x30, 0x00}, 42, d)
	if !acked || err != nil {
		t.Errorf("acked=%v err=%v, want the second datagram recognised", acked, err)
	}
}

func TestAnInformAttemptReportsAReadThatIsNotATimeout(t *testing.T) {
	// A socket that fails for a reason other than "nothing yet" is not
	// a retry: the next attempt would fail the same way.
	s := NewTrapSender(nil, plugin.Deps{Logger: quiet(), Clock: clock.System()})
	defer func() { _ = s.Close() }()
	d := TrapDestination{Addr: "127.0.0.1:1", Version: codec.Version2c, Community: "public"}

	acked, err := s.informAttempt(context.Background(),
		&informConn{}, []byte{0x30, 0x00}, 1, d)
	if acked || err == nil || errors.Is(err, errInformTimeout) {
		t.Errorf("acked=%v err=%v, want a real read failure", acked, err)
	}
}
