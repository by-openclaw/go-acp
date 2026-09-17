package consumer

// What a manager does when the agent misbehaves and when the socket
// does. Neither can be provoked from a well-behaved agent over a working
// network, so both are scripted: a UDP peer that answers whatever the
// test says, and a net.Conn that fails where the test says.

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
	"dhs/internal/snmp/mib"
)

// scriptedAgent answers each request with whatever reply returns. A nil
// reply means silence, which is what a device that has gone away does.
func scriptedAgent(t *testing.T, reply func(req codec.Message) *codec.Message) string {
	t.Helper()
	conn, err := net.ListenPacket("udp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })

	go func() {
		buf := make([]byte, 65535)
		for {
			n, from, err := conn.ReadFrom(buf)
			if err != nil {
				return
			}
			req, err := codec.Decode(buf[:n])
			if err != nil {
				continue
			}
			out := reply(req)
			if out == nil {
				continue
			}
			raw, err := codec.Encode(*out)
			if err != nil {
				continue
			}
			_, _ = conn.WriteTo(raw, from)
		}
	}()
	return conn.LocalAddr().String()
}

// respondWith is the shape of a well-formed reply to req.
func respondWith(req codec.Message, status codec.ErrorStatus, binds ...codec.VarBind) *codec.Message {
	return &codec.Message{
		Version: req.Version, Community: req.Community,
		PDU: &codec.PDU{
			Type: codec.PDUTypeResponse, RequestID: req.PDU.RequestID,
			ErrorStatus: status, VarBinds: binds,
		},
	}
}

// RFC 3416 requires GETNEXT to return a strictly greater name. An agent
// that repeats one walks a manager in a circle forever, so the walk
// stops and says so — and counts the deviation.
func TestAnAgentThatDoesNotAdvanceStopsTheWalk(t *testing.T) {
	stuck := mib.SysDescr
	addr := scriptedAgent(t, func(req codec.Message) *codec.Message {
		return respondWith(req, codec.NoError,
			codec.VarBind{Name: stuck, Value: codec.String("the same object, again")})
	})

	prof := &compliance.Profile{}
	s := dial(t, addr, Options{Version: codec.Version2c, Compliance: prof,
		Timeout: time.Second})

	err := s.Walk(context.Background(), mib.System, func(codec.VarBind) error { return nil })
	if err == nil || !strings.Contains(err.Error(), "did not advance") {
		t.Fatalf("= %v, want the walk stopped", err)
	}
	if prof.Snapshot()[TruncatedWalk] == 0 {
		t.Error("the deviation must be counted")
	}
}

// An agent that returns more repetitions than were asked for is
// answering a count it disregarded; the extras are dropped rather than
// trusted, and the deviation is counted.
func TestABulkThatReturnsTooMuch(t *testing.T) {
	addr := scriptedAgent(t, func(req codec.Message) *codec.Message {
		var binds []codec.VarBind
		for i := 1; i <= 5; i++ {
			binds = append(binds, codec.VarBind{
				Name:  mib.System.Append(uint32(i), 0),
				Value: codec.Int(int64(i)),
			})
		}
		return respondWith(req, codec.NoError, binds...)
	})

	prof := &compliance.Profile{}
	s := dial(t, addr, Options{Version: codec.Version2c, MaxRepetitions: 2,
		Compliance: prof, Timeout: time.Second})

	n := 0
	// The walk ends when the agent repeats itself, which it will on the
	// second round; what is asserted is the count taken from the first.
	_ = s.Walk(context.Background(), mib.System, func(codec.VarBind) error {
		n++
		return nil
	})
	if n != 2 {
		t.Errorf("took %d bindings from a bulk of 2", n)
	}
	if prof.Snapshot()[BulkOverrun] == 0 {
		t.Error("the overrun must be counted")
	}
}

// A v1 walk ends on noSuchName, which is the only way v1 can say "there
// is nothing after this" — and it is an end, not a failure.
func TestAV1WalkEndsOnNoSuchName(t *testing.T) {
	addr := scriptedAgent(t, func(req codec.Message) *codec.Message {
		return respondWith(req, codec.NoSuchName, req.PDU.VarBinds...)
	})
	s := dial(t, addr, Options{Version: codec.Version1, Community: "public",
		Timeout: time.Second})

	got, err := s.WalkAll(context.Background(), mib.System, 0)
	if err != nil {
		t.Fatalf("= %v, want a clean end", err)
	}
	if len(got) != 0 {
		t.Errorf("%d objects from an agent that has none", len(got))
	}
}

// Any other error-status ends a v1 walk as a failure, because it is one.
func TestAV1WalkStopsOnARealError(t *testing.T) {
	addr := scriptedAgent(t, func(req codec.Message) *codec.Message {
		return respondWith(req, codec.GenErr, req.PDU.VarBinds...)
	})
	s := dial(t, addr, Options{Version: codec.Version1, Community: "public",
		Timeout: time.Second})

	if _, err := s.WalkAll(context.Background(), mib.System, 0); err == nil ||
		!strings.Contains(err.Error(), "genErr") {
		t.Fatalf("= %v, want the failure reported", err)
	}
}

// A bulk that comes back with an error-status ends the walk with it.
func TestABulkWalkStopsOnAnErrorStatus(t *testing.T) {
	addr := scriptedAgent(t, func(req codec.Message) *codec.Message {
		return respondWith(req, codec.TooBig)
	})
	s := dial(t, addr, Options{Version: codec.Version2c, Timeout: time.Second})

	if _, err := s.WalkAll(context.Background(), mib.System, 0); err == nil ||
		!strings.Contains(err.Error(), "tooBig") {
		t.Fatalf("= %v, want the status reported", err)
	}
}

// A bulk that comes back empty is the end of the tree on an agent that
// says so by saying nothing.
func TestABulkThatComesBackEmpty(t *testing.T) {
	addr := scriptedAgent(t, func(req codec.Message) *codec.Message {
		return respondWith(req, codec.NoError)
	})
	s := dial(t, addr, Options{Version: codec.Version2c, Timeout: time.Second})

	got, err := s.WalkAll(context.Background(), mib.System, 0)
	if err != nil || len(got) != 0 {
		t.Errorf("= %v / %d objects, want a clean empty end", err, len(got))
	}
}

// ---------------------------------------------------------------------
// the socket
// ---------------------------------------------------------------------

// faultyConn fails where a test says. net.Conn is wider than a session
// uses; the rest is never called.
type faultyConn struct {
	deadlineErr error
	writeErr    error
	readErr     error
	reads       [][]byte
}

func (c *faultyConn) Read(b []byte) (int, error) {
	if len(c.reads) > 0 {
		r := c.reads[0]
		c.reads = c.reads[1:]
		return copy(b, r), nil
	}
	if c.readErr != nil {
		return 0, c.readErr
	}
	return 0, net.ErrClosed
}

func (c *faultyConn) Write(b []byte) (int, error) {
	if c.writeErr != nil {
		return 0, c.writeErr
	}
	return len(b), nil
}

func (c *faultyConn) Close() error        { return nil }
func (c *faultyConn) LocalAddr() net.Addr { return &net.UDPAddr{} }
func (c *faultyConn) RemoteAddr() net.Addr {
	return &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 161}
}
func (c *faultyConn) SetDeadline(time.Time) error {
	return c.deadlineErr
}
func (c *faultyConn) SetReadDeadline(time.Time) error  { return nil }
func (c *faultyConn) SetWriteDeadline(time.Time) error { return nil }

func sessionOn(conn net.Conn, opts Options) *Session {
	return &Session{
		opts:   opts.withDefaults(),
		logger: quiet(),
		clk:    clock.System(),
		prof:   &compliance.Profile{},
		conn:   conn,
	}
}

// A socket that will not take a deadline, will not write, or fails a
// read for a reason that is not a timeout: none of those is better on
// the next attempt, so none of them is retried.
func TestSocketFailuresAreNotRetried(t *testing.T) {
	for _, tc := range []struct {
		name string
		conn *faultyConn
		want string
	}{
		{"a deadline that cannot be set",
			&faultyConn{deadlineErr: errors.New("bad fd")}, "set deadline"},
		{"a datagram that cannot be sent",
			&faultyConn{writeErr: errors.New("network unreachable")}, "send"},
		{"a read that fails for its own reasons",
			&faultyConn{readErr: errors.New("connection refused")}, "receive"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := sessionOn(tc.conn, Options{Version: codec.Version2c, Retries: 5})
			_, err := s.Get(context.Background(), mib.SysDescr)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("= %v, want %q", err, tc.want)
			}
			if strings.Contains(err.Error(), "attempt(s)") {
				t.Error("a socket failure must not be retried")
			}
		})
	}
}

// A request this manager cannot even build is refused before a socket is
// touched: GETBULK does not exist in v1.
func TestARequestThatCannotBeEncoded(t *testing.T) {
	s := sessionOn(&faultyConn{}, Options{Version: codec.Version1, Community: "public"})
	_, err := s.roundTrip(context.Background(), &codec.PDU{Type: codec.PDUTypeGetBulk})
	if err == nil || !strings.Contains(err.Error(), "not a v1 PDU") {
		t.Fatalf("= %v, want the refusal", err)
	}
}

// A SET whose round trip fails reports the failure rather than an empty
// result that looks like a write that took.
func TestASetWhoseRoundTripFails(t *testing.T) {
	s := sessionOn(&faultyConn{writeErr: errors.New("no route")},
		Options{Version: codec.Version2c})
	_, err := s.Set(context.Background(),
		codec.VarBind{Name: mib.SysContact, Value: codec.String("x")})
	if err == nil || !strings.Contains(err.Error(), "send") {
		t.Fatalf("= %v, want the send failure", err)
	}
}

// ---------------------------------------------------------------------
// the listener's socket
// ---------------------------------------------------------------------

// A read that fails for a reason other than shutdown is returned: a
// listener whose socket died must not look like one that was stopped.
func TestAListenerReadThatFails(t *testing.T) {
	l := NewListener(ListenerOptions{}, plugin.Deps{Logger: quiet()})
	boom := errors.New("the interface went away")
	l.listen = func(context.Context, string, string) (net.PacketConn, error) {
		return &faultyPacketConn{readErr: boom}, nil
	}
	if err := l.Listen(context.Background(), func(Trap) {}); !errors.Is(err, boom) {
		t.Fatalf("= %v, want the failure reported", err)
	}
}

// A datagram from something that is not a UDP address cannot have come
// from an agent, and the sender's address is the only thing that
// identifies a v2c notification.
func TestADatagramFromSomethingThatIsNotUDP(t *testing.T) {
	prof := &compliance.Profile{}
	l := NewListener(ListenerOptions{Compliance: prof}, plugin.Deps{Logger: quiet()})
	l.listen = func(context.Context, string, string) (net.PacketConn, error) {
		return &faultyPacketConn{reads: []readFrom{{
			b: []byte{0x30, 0x00}, from: &net.UnixAddr{Name: "/tmp/nope"},
		}}}, nil
	}

	delivered := 0
	if err := l.Listen(context.Background(), func(Trap) { delivered++ }); err != nil {
		t.Fatalf("= %v", err)
	}
	if delivered != 0 {
		t.Error("it must not be delivered")
	}
	if prof.Snapshot()[TrapUndecodable] == 0 {
		t.Error("it must be counted")
	}
}

type readFrom struct {
	b    []byte
	from net.Addr
}

// faultyPacketConn scripts a listener's socket.
type faultyPacketConn struct {
	reads   []readFrom
	readErr error
}

func (c *faultyPacketConn) ReadFrom(p []byte) (int, net.Addr, error) {
	if len(c.reads) > 0 {
		r := c.reads[0]
		c.reads = c.reads[1:]
		return copy(p, r.b), r.from, nil
	}
	if c.readErr != nil {
		return 0, nil, c.readErr
	}
	return 0, nil, net.ErrClosed
}

func (c *faultyPacketConn) WriteTo([]byte, net.Addr) (int, error) { return 0, net.ErrClosed }
func (c *faultyPacketConn) Close() error                          { return nil }
func (c *faultyPacketConn) LocalAddr() net.Addr                   { return &net.UDPAddr{} }
func (c *faultyPacketConn) SetDeadline(time.Time) error           { return nil }
func (c *faultyPacketConn) SetReadDeadline(time.Time) error       { return nil }
func (c *faultyPacketConn) SetWriteDeadline(time.Time) error      { return nil }

// A caller's own deadline wins when it is shorter than the session's: a
// poller that gives itself one second and the session two is a poller
// that would otherwise report timeouts it caused.
func TestTheCallersDeadlineWinsWhenItIsShorter(t *testing.T) {
	dead, err := net.ListenPacket("udp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = dead.Close() })

	s := dial(t, dead.LocalAddr().String(), Options{
		Version: codec.Version2c, Timeout: 30 * time.Second, Retries: 0,
	})
	ctx, cancel := context.WithTimeout(context.Background(), 80*time.Millisecond)
	defer cancel()

	start := time.Now()
	if _, err := s.Get(ctx, mib.SysDescr); err == nil {
		t.Fatal("want a failure")
	}
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Errorf("waited %s: the session's timeout won", elapsed)
	}
}

// A datagram that is not ours does not end the wait: the reply we are
// waiting for may still be behind it, and the socket deadline still
// bounds how long we look.
func TestAForeignDatagramDoesNotEndTheWait(t *testing.T) {
	enc := func(m codec.Message) []byte {
		raw, err := codec.Encode(m)
		if err != nil {
			t.Fatal(err)
		}
		return raw
	}
	resp := func(id int32, value string) codec.Message {
		return codec.Message{Version: codec.Version2c, Community: "public",
			PDU: &codec.PDU{Type: codec.PDUTypeResponse, RequestID: id,
				VarBinds: []codec.VarBind{{Name: mib.SysDescr, Value: codec.String(value)}}}}
	}

	conn := &faultyConn{reads: [][]byte{
		enc(resp(999, "somebody else's answer")),
		enc(resp(1, "ours")),
	}}
	s := sessionOn(conn, Options{Version: codec.Version2c, Community: "public"})
	s.nextID = 0 // so the request below is id 1

	binds, err := s.Get(context.Background(), mib.SysDescr)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got := binds[0].Value.String(); got != "ours" {
		t.Errorf("= %q, want the reply that was ours", got)
	}
}

// A socket that fails part way through a walk ends the walk with the
// failure, rather than with a subtree that looks complete.
func TestAWalkThatLosesItsSocket(t *testing.T) {
	s := sessionOn(&faultyConn{writeErr: errors.New("network unreachable")},
		Options{Version: codec.Version2c})
	if _, err := s.WalkAll(context.Background(), mib.System, 0); err == nil ||
		!strings.Contains(err.Error(), "send") {
		t.Fatalf("= %v, want the socket failure", err)
	}
}
