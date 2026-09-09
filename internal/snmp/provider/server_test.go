package provider

// The server is the socket half, so these tests use a real one: an agent
// bound to 127.0.0.1:0 and a manager that sends it datagrams. The
// protocol rules are pinned in agent_test.go; what is asserted here is
// what a datagram arriving at a UDP port causes.

import (
	"context"
	"errors"
	"net"
	"strings"
	"testing"
	"time"

	"dhs/internal/plugin"
	"dhs/internal/snmp/codec"
)

// serveAgent starts a server on an ephemeral port and returns its
// address. It is stopped when the test ends.
func serveAgent(t *testing.T, mib *MIB, c Communities) (*Server, *net.UDPAddr) {
	t.Helper()
	s := NewServer(mib, c, plugin.Deps{Logger: quiet()})

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- s.Serve(ctx, "127.0.0.1:0") }()

	t.Cleanup(func() {
		cancel()
		select {
		case err := <-done:
			if err != nil {
				t.Errorf("Serve returned %v", err)
			}
		case <-time.After(5 * time.Second):
			t.Error("Serve did not return")
		}
	})

	addr := waitForAddr(t, s)
	return s, addr
}

// waitForAddr waits for the bind, which happens on Serve's goroutine.
func waitForAddr(t *testing.T, s *Server) *net.UDPAddr {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		if a := s.Addr(); a != nil {
			udp, ok := a.(*net.UDPAddr)
			if !ok {
				t.Fatalf("bound to a %T, want a UDP address", a)
			}
			return udp
		}
		if time.Now().After(deadline) {
			t.Fatal("the server never bound")
		}
		time.Sleep(time.Millisecond)
	}
}

// manager is a socket that talks to the agent the way a real one would.
type manager struct {
	t    *testing.T
	conn *net.UDPConn
}

func dialManager(t *testing.T, addr *net.UDPAddr) *manager {
	t.Helper()
	c, err := net.DialUDP("udp4", nil, addr)
	if err != nil {
		t.Fatalf("dial the agent: %v", err)
	}
	t.Cleanup(func() { _ = c.Close() })
	return &manager{t: t, conn: c}
}

// ask sends raw bytes and returns the reply, or nil if the agent stayed
// silent for the grace period.
func (m *manager) ask(raw []byte) []byte {
	m.t.Helper()
	if _, err := m.conn.Write(raw); err != nil {
		m.t.Fatalf("send: %v", err)
	}
	_ = m.conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	buf := make([]byte, 65535)
	n, err := m.conn.Read(buf)
	if err != nil {
		return nil
	}
	return buf[:n]
}

// silence sends and expects nothing back. The wait is short because the
// assertion is about a datagram that is never sent, and every one of
// these costs the suite that wait.
func (m *manager) silence(raw []byte) bool {
	m.t.Helper()
	if _, err := m.conn.Write(raw); err != nil {
		m.t.Fatalf("send: %v", err)
	}
	_ = m.conn.SetReadDeadline(time.Now().Add(150 * time.Millisecond))
	_, err := m.conn.Read(make([]byte, 65535))
	return err != nil
}

func encode(t *testing.T, m codec.Message) []byte {
	t.Helper()
	raw, err := codec.Encode(m)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	return raw
}

// A manager gets an answer over a real socket: this is the whole agent,
// bind to reply.
func TestTheAgentAnswersOverUDP(t *testing.T) {
	mib, _ := agentTree(t)
	s, addr := serveAgent(t, mib, Communities{Read: "public"})
	mgr := dialManager(t, addr)

	raw := mgr.ask(encode(t, request(codec.Version2c, "public",
		codec.PDUTypeGet, "1.3.6.1.2.1.1.1.0")))
	if raw == nil {
		t.Fatal("no reply")
	}

	resp, err := codec.Decode(raw)
	if err != nil {
		t.Fatalf("the reply does not decode: %v", err)
	}
	if resp.PDU.VarBinds[0].Value.String() != "dhs SNMP agent" {
		t.Errorf("sysDescr.0 = %s", resp.PDU.VarBinds[0].Value)
	}

	// The exchange is counted on both sides, which is what --metrics-addr
	// surfaces and what an operator uses to tell "nobody is polling us"
	// from "we are answering wrong".
	snap := s.Metrics().Snapshot()
	if snap.RxBytes == 0 || snap.TxBytes == 0 {
		t.Errorf("metrics = rx %d / tx %d, want the exchange counted",
			snap.RxBytes, snap.TxBytes)
	}
}

// Rubbish on the port is counted and dropped, never answered: an agent
// that replied to malformed input with an error would be a reflection
// amplifier on a port that takes traffic from anybody.
func TestRubbishOnThePortIsDroppedAndCounted(t *testing.T) {
	mib, _ := agentTree(t)
	s, addr := serveAgent(t, mib, Communities{Read: "public"})
	mgr := dialManager(t, addr)

	if !mgr.silence([]byte{0xFF, 0xFF, 0xFF}) {
		t.Error("the agent answered a datagram that is not SNMP")
	}
	if got := s.Metrics().Snapshot().DecodeErrors; got == 0 {
		t.Error("an undecodable datagram must be counted")
	}
}

// A wrong community over the wire gets the same silence the Agent gives
// in isolation — the socket layer must not turn a refusal into a reply.
func TestAWrongCommunityGetsNothingOverTheWire(t *testing.T) {
	mib, _ := agentTree(t)
	_, addr := serveAgent(t, mib, Communities{Read: "public"})
	mgr := dialManager(t, addr)

	if !mgr.silence(encode(t, request(codec.Version2c, "wrong",
		codec.PDUTypeGet, "1.3.6.1.2.1.1.1.0"))) {
		t.Error("the agent answered a wrong community")
	}
}

// A bind that cannot happen is reported with the address in it, rather
// than leaving the operator to guess which of two agents failed.
func TestABindThatCannotHappen(t *testing.T) {
	mib, _ := agentTree(t)
	s := NewServer(mib, Communities{}, plugin.Deps{Logger: quiet()})

	err := s.Serve(context.Background(), "256.256.256.256:161")
	if err == nil || !strings.Contains(err.Error(), "256.256.256.256:161") {
		t.Fatalf("= %v, want the address in the failure", err)
	}
}

// Stop is idempotent, and stopping a server that never served is not an
// error — a supervisor tearing down calls it either way.
func TestStopIsIdempotent(t *testing.T) {
	mib, _ := agentTree(t)
	s := NewServer(mib, Communities{}, plugin.Deps{Logger: quiet()})
	if err := s.Stop(); err != nil {
		t.Errorf("stopping a server that never served: %v", err)
	}
	if err := s.Stop(); err != nil {
		t.Errorf("second Stop: %v", err)
	}
}

// Serve returns cleanly when its context is cancelled: a closed socket
// is the shutdown path, not a failure to report.
func TestServeReturnsCleanlyOnCancel(t *testing.T) {
	mib, _ := agentTree(t)
	s := NewServer(mib, Communities{}, plugin.Deps{Logger: quiet()})

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- s.Serve(ctx, "127.0.0.1:0") }()
	waitForAddr(t, s)
	cancel()

	select {
	case err := <-done:
		if err != nil {
			t.Errorf("= %v, want a clean return", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Serve did not return")
	}
}

// scriptedConn is a socket that answers from a script, so the reply path
// can be driven where a real socket cannot be made to fail on demand.
type scriptedConn struct {
	reads    [][]byte
	writeErr error

	wrote [][]byte
}

func (c *scriptedConn) ReadFromUDP(b []byte) (int, *net.UDPAddr, error) {
	if len(c.reads) == 0 {
		return 0, nil, net.ErrClosed
	}
	r := c.reads[0]
	c.reads = c.reads[1:]
	return copy(b, r), &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 40000}, nil
}

func (c *scriptedConn) WriteToUDP(b []byte, _ *net.UDPAddr) (int, error) {
	if c.writeErr != nil {
		return 0, c.writeErr
	}
	c.wrote = append(c.wrote, append([]byte(nil), b...))
	return len(b), nil
}

// A reply that cannot be written is this one manager's problem: the
// poller was restarted or the route changed, and an agent that stopped
// serving everybody else over one of those would be worse than one that
// never answered.
func TestAReplyThatCannotBeWrittenDoesNotStopTheAgent(t *testing.T) {
	mib, _ := agentTree(t)
	s := NewServer(mib, Communities{Read: "public"}, plugin.Deps{Logger: quiet()})

	get := encode(t, request(codec.Version2c, "public",
		codec.PDUTypeGet, "1.3.6.1.2.1.1.1.0"))
	conn := &scriptedConn{
		reads:    [][]byte{get, get},
		writeErr: errors.New("host unreachable"),
	}

	// The loop ends because the script runs out, not because a write
	// failed: both datagrams were read.
	if err := s.readLoop(conn); !errors.Is(err, net.ErrClosed) {
		t.Fatalf("= %v, want the loop to end on the closed socket", err)
	}
	if len(conn.reads) != 0 {
		t.Errorf("%d datagrams left unread: a failed write stopped the loop", len(conn.reads))
	}
	// Nothing was counted as sent, because nothing was.
	if got := s.Metrics().Snapshot().TxBytes; got != 0 {
		t.Errorf("TxBytes = %d after two failed writes", got)
	}
}

// The ordinary loop: a datagram in, the answer out, both counted.
func TestTheLoopAnswersAndCounts(t *testing.T) {
	mib, _ := agentTree(t)
	s := NewServer(mib, Communities{Read: "public"}, plugin.Deps{Logger: quiet()})

	conn := &scriptedConn{reads: [][]byte{
		encode(t, request(codec.Version2c, "public", codec.PDUTypeGet, "1.3.6.1.2.1.1.1.0")),
		{0xFF, 0xFF}, // dropped, so it produces no write
	}}
	if err := s.readLoop(conn); !errors.Is(err, net.ErrClosed) {
		t.Fatalf("= %v", err)
	}
	if len(conn.wrote) != 1 {
		t.Fatalf("%d replies, want one", len(conn.wrote))
	}
	snap := s.Metrics().Snapshot()
	if snap.RxBytes == 0 || snap.TxBytes == 0 || snap.DecodeErrors != 1 {
		t.Errorf("metrics = rx %d tx %d decode-errors %d",
			snap.RxBytes, snap.TxBytes, snap.DecodeErrors)
	}
}

// handle is the dispatch, and it answers nothing for each of the reasons
// an agent stays quiet.
func TestHandleStaysQuiet(t *testing.T) {
	mib, _ := agentTree(t)
	s := NewServer(mib, Communities{Read: "public"}, plugin.Deps{Logger: quiet()})
	from := &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 40000}

	if _, ok := s.handle([]byte{0x30, 0x00}, from); ok {
		t.Error("an undecodable datagram must not be answered")
	}
	if _, ok := s.handle(encode(t, request(codec.Version2c, "wrong",
		codec.PDUTypeGet, "1.3.6.1.2.1.1.1.0")), from); ok {
		t.Error("a refused community must not be answered")
	}
	if out, ok := s.handle(encode(t, request(codec.Version2c, "public",
		codec.PDUTypeGet, "1.3.6.1.2.1.1.1.0")), from); !ok || len(out) == 0 {
		t.Error("a good request must be answered")
	}
}

// A request naming a version this package cannot answer in produces no
// datagram at all: not even the empty tooBig response can be encoded, so
// there is nothing useful to send and nothing is sent. Reachable only
// through Respond directly — Decode refuses such a version at the door.
func TestNoResponseCanBeEncoded(t *testing.T) {
	mib, _ := agentTree(t)
	a := NewAgent(mib, Communities{}, quiet())

	_, raw, ok := a.Respond(codec.Message{
		Version:   codec.Version(7),
		Community: DefaultReadCommunity,
		PDU: &codec.PDU{Type: codec.PDUTypeGet, RequestID: 1,
			VarBinds: []codec.VarBind{{Name: oid("1.3.6.1.2.1.1.1.0")}}},
	})
	if ok || raw != nil {
		t.Errorf("= %v / %d bytes, want nothing sent", ok, len(raw))
	}
}

// Agent exposes the request handler so a caller can answer a datagram it
// obtained some other way.
func TestServerExposesItsAgent(t *testing.T) {
	mib, _ := agentTree(t)
	s := NewServer(mib, Communities{Read: "public"}, plugin.Deps{Logger: quiet()})
	if s.Agent() == nil {
		t.Fatal("the server must expose its agent")
	}
	if _, _, ok := s.Agent().Respond(request(codec.Version2c, "public",
		codec.PDUTypeGet, "1.3.6.1.2.1.1.1.0")); !ok {
		t.Error("the exposed agent must answer")
	}
}

// Addr is nil before the bind and after the stop, and the bound port in
// between — a caller given ":0" has no other way to learn it.
func TestAddrReportsTheBoundPort(t *testing.T) {
	mib, _ := agentTree(t)
	s := NewServer(mib, Communities{}, plugin.Deps{Logger: quiet()})
	if s.Addr() != nil {
		t.Error("a server that never served is bound to nothing")
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- s.Serve(ctx, "127.0.0.1:0") }()
	addr := waitForAddr(t, s)
	if addr.Port == 0 {
		t.Error("the OS-chosen port must be reported")
	}

	cancel()
	<-done
	if s.Addr() != nil {
		t.Error("a stopped server is bound to nothing")
	}
}

// A read that failed for a reason other than shutdown IS a failure, and
// has to reach the supervisor: an agent whose socket died must not look
// like one that was asked to stop.
func TestShutdownIsNotAFailure(t *testing.T) {
	if err := shutdownIsNotAFailure(net.ErrClosed); err != nil {
		t.Errorf("a closed socket is how this server stops: %v", err)
	}
	if err := shutdownIsNotAFailure(context.Canceled); err != nil {
		t.Errorf("a cancelled context is a clean stop: %v", err)
	}
	boom := errors.New("the interface went away")
	if err := shutdownIsNotAFailure(boom); !errors.Is(err, boom) {
		t.Errorf("= %v, want the failure reported", err)
	}
}
