package provider

// A trap is the only notice a manager gets that something happened, and
// nothing acknowledges it. So the two things worth pinning hard are the
// shape on the wire — which differs completely between v1 and v2c — and
// the fan-out, which must not let one unreachable NMS silence the rest.

import (
	"context"
	"errors"
	"net"
	"strings"
	"sync"
	"testing"
	"time"

	"dhs/internal/snmp/codec"
)

// alarm is the notification the IRDs actually emit: enterprise-specific
// trap 1 in the shared alarm namespace, "Signal Lock lost".
func alarm() Notification {
	return Notification{
		Enterprise: oid("1.3.6.1.4.1.1773.1.3.200"),
		AgentAddr:  net.IPv4(10, 6, 255, 110),
		Generic:    codec.EnterpriseSpecific,
		Specific:   1,
		Uptime:     360000,
		VarBinds: []codec.VarBind{
			{Name: oid("1.3.6.1.4.1.1773.1.1.3.1"), Value: codec.Int(1)},
		},
	}
}

// A v1 trap carries the enterprise, the agent address and the two trap
// numbers in the PDU itself.
func TestAV1TrapCarriesItsFieldsInThePDU(t *testing.T) {
	m, err := alarm().Message(codec.Version1, "public", 1)
	if err != nil {
		t.Fatal(err)
	}
	if m.Type() != codec.PDUTypeTrapV1 || m.TrapV1 == nil {
		t.Fatalf("built a %s", m.Type())
	}
	tr := m.TrapV1
	if tr.Enterprise.String() != "1.3.6.1.4.1.1773.1.3.200" {
		t.Errorf("enterprise = %s", tr.Enterprise)
	}
	if !tr.AgentAddr.Equal(net.IPv4(10, 6, 255, 110)) {
		t.Errorf("agent-addr = %s", tr.AgentAddr)
	}
	if tr.Generic != codec.EnterpriseSpecific || tr.Specific != 1 {
		t.Errorf("trap = %s/%d", tr.Generic, tr.Specific)
	}
	if tr.Timestamp != 360000 {
		t.Errorf("time-stamp = %d", tr.Timestamp)
	}
	if len(tr.VarBinds) != 1 {
		t.Errorf("%d varbinds, want the sender's own", len(tr.VarBinds))
	}

	// And it survives the wire.
	raw, err := codec.Encode(m)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := codec.Decode(raw); err != nil {
		t.Errorf("the trap does not decode: %v", err)
	}
}

// A v2c notification MUST open with sysUpTime.0 and snmpTrapOID.0, in
// that order (RFC 3416 §4.2.6). A conforming receiver drops one that
// does not — the failure that looks like a network problem and is not.
func TestAV2cNotificationOpensWithTheTwoMandatoryBindings(t *testing.T) {
	m, err := alarm().Message(codec.Version2c, "public", 42)
	if err != nil {
		t.Fatal(err)
	}
	if m.Type() != codec.PDUTypeTrapV2 {
		t.Fatalf("built a %s", m.Type())
	}
	if m.PDU.RequestID != 42 {
		t.Errorf("request-id = %d", m.PDU.RequestID)
	}

	vbs := m.PDU.VarBinds
	if len(vbs) != 3 {
		t.Fatalf("%d varbinds, want the two mandatory ones plus the sender's", len(vbs))
	}
	if vbs[0].Name.Compare(SysUpTimeInstance) != 0 {
		t.Errorf("varbind 0 = %s, want sysUpTime.0", vbs[0].Name)
	}
	if vbs[0].Value.Type != codec.TypeTimeTicks || vbs[0].Value.Uint != 360000 {
		t.Errorf("sysUpTime.0 = %s (%s)", vbs[0].Value, vbs[0].Value.Type)
	}
	if vbs[1].Name.Compare(SNMPTrapOID) != 0 {
		t.Errorf("varbind 1 = %s, want snmpTrapOID.0", vbs[1].Name)
	}
	if vbs[1].Value.Type != codec.TypeOID {
		t.Errorf("snmpTrapOID.0 is a %s", vbs[1].Value.Type)
	}
	// The sender's own bindings FOLLOW rather than replace them.
	if vbs[2].Name.String() != "1.3.6.1.4.1.1773.1.1.3.1" {
		t.Errorf("varbind 2 = %s", vbs[2].Name)
	}
}

// The v2c identity of a generic event is the RFC 3418 well-known OID;
// of an enterprise-specific one, RFC 3584 §3.1's enterprise + 0 +
// specific. The zero arc is what keeps a vendor's trap 1 from colliding
// with the vendor's object 1.
func TestTrapOIDMapping(t *testing.T) {
	for _, tc := range []struct {
		generic codec.GenericTrap
		want    string
	}{
		{codec.ColdStart, "1.3.6.1.6.3.1.1.5.1"},
		{codec.WarmStart, "1.3.6.1.6.3.1.1.5.2"},
		{codec.LinkDown, "1.3.6.1.6.3.1.1.5.3"},
		{codec.LinkUp, "1.3.6.1.6.3.1.1.5.4"},
		{codec.AuthenticationFailure, "1.3.6.1.6.3.1.1.5.5"},
		{codec.EGPNeighborLoss, "1.3.6.1.6.3.1.1.5.6"},
	} {
		n := Notification{Enterprise: oid("1.3.6.1.4.1.1773"), Generic: tc.generic}
		if got := n.TrapOID().String(); got != tc.want {
			t.Errorf("%s = %s, want %s", tc.generic, got, tc.want)
		}
	}

	n := Notification{Enterprise: oid("1.3.6.1.4.1.1773.1.3.200"),
		Generic: codec.EnterpriseSpecific, Specific: 1}
	if got := n.TrapOID().String(); got != "1.3.6.1.4.1.1773.1.3.200.0.1" {
		t.Errorf("enterprise-specific = %s, want the RFC 3584 mapping", got)
	}
}

// A notification cannot be sent as a version this package does not
// build, and says so rather than emitting nothing and returning nil.
func TestANotificationInAVersionWeCannotSend(t *testing.T) {
	if _, err := alarm().Message(codec.Version3, "public", 1); err == nil ||
		!strings.Contains(err.Error(), "v3") {
		t.Fatalf("= %v, want the refusal", err)
	}
}

// ---------------------------------------------------------------------
// the fan-out
// ---------------------------------------------------------------------

// fakeSocket records what was written to one destination.
type fakeSocket struct {
	mu       sync.Mutex
	writes   [][]byte
	writeErr error
	closed   bool
}

func (f *fakeSocket) Write(b []byte) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.writeErr != nil {
		return 0, f.writeErr
	}
	f.writes = append(f.writes, append([]byte(nil), b...))
	return len(b), nil
}

func (f *fakeSocket) Close() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.closed = true
	return nil
}

func (f *fakeSocket) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.writes)
}

func (f *fakeSocket) last() []byte {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.writes) == 0 {
		return nil
	}
	return f.writes[len(f.writes)-1]
}

// net.Conn is wider than a write and a close; the rest is never called.
type fakeConn struct {
	*fakeSocket
}

func (fakeConn) Read([]byte) (int, error)         { return 0, net.ErrClosed }
func (fakeConn) LocalAddr() net.Addr              { return nil }
func (fakeConn) RemoteAddr() net.Addr             { return nil }
func (fakeConn) SetDeadline(_ time.Time) error    { return nil }
func (fakeConn) SetReadDeadline(time.Time) error  { return nil }
func (fakeConn) SetWriteDeadline(time.Time) error { return nil }

// fakeDialer hands out one socket per address and records dial failures.
type fakeDialer struct {
	mu      sync.Mutex
	sockets map[string]*fakeSocket
	failing map[string]error
	dials   int
}

func newDialer() *fakeDialer {
	return &fakeDialer{sockets: map[string]*fakeSocket{}, failing: map[string]error{}}
}

func (d *fakeDialer) dial(_, addr string) (net.Conn, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.dials++
	if err, ok := d.failing[addr]; ok {
		return nil, err
	}
	s, ok := d.sockets[addr]
	if !ok {
		s = &fakeSocket{}
		d.sockets[addr] = s
	}
	return fakeConn{s}, nil
}

func (d *fakeDialer) socket(addr string) *fakeSocket {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.sockets[addr]
}

func senderOn(t *testing.T, d *fakeDialer, dests ...TrapDestination) *TrapSender {
	t.Helper()
	s := NewTrapSender(dests, quiet())
	s.dial = d.dial
	t.Cleanup(func() { _ = s.Close() })
	return s
}

// A plant mid-migration has both: the old NMS listening for v1 traps and
// the new one for v2c notifications, from the same device at the same
// time. So the version is per destination.
func TestOneEventReachesBothKindsOfManager(t *testing.T) {
	d := newDialer()
	s := senderOn(t, d,
		TrapDestination{Addr: "10.6.250.5:162", Version: codec.Version2c, Community: "public"},
		TrapDestination{Addr: "10.6.255.9:162", Version: codec.Version1, Community: "trapc"})

	if err := s.Send(context.Background(), alarm()); err != nil {
		t.Fatalf("Send: %v", err)
	}

	v2 := decodeTrap(t, d.socket("10.6.250.5:162").last())
	if v2.Type() != codec.PDUTypeTrapV2 || v2.Community != "public" {
		t.Errorf("the v2c manager got a %s/%q", v2.Type(), v2.Community)
	}
	v1 := decodeTrap(t, d.socket("10.6.255.9:162").last())
	if v1.Type() != codec.PDUTypeTrapV1 || v1.Community != "trapc" {
		t.Errorf("the v1 manager got a %s/%q", v1.Type(), v1.Community)
	}
}

func decodeTrap(t *testing.T, raw []byte) codec.Message {
	t.Helper()
	if raw == nil {
		t.Fatal("nothing was sent")
	}
	m, err := codec.Decode(raw)
	if err != nil {
		t.Fatalf("the trap does not decode: %v", err)
	}
	return m
}

// One unreachable NMS must not silence the others: a trap is the only
// notice a manager gets, and there is no retry behind it.
func TestOneBadDestinationDoesNotSilenceTheRest(t *testing.T) {
	d := newDialer()
	d.failing["10.6.0.1:162"] = errors.New("no route to host")
	s := senderOn(t, d,
		TrapDestination{Addr: "10.6.0.1:162", Version: codec.Version2c},
		TrapDestination{Addr: "10.6.250.5:162", Version: codec.Version2c})

	err := s.Send(context.Background(), alarm())
	if err == nil || !strings.Contains(err.Error(), "10.6.0.1:162") {
		t.Fatalf("= %v, want the first failure named", err)
	}
	if d.socket("10.6.250.5:162").count() != 1 {
		t.Error("the reachable manager was not told")
	}
}

// A write that fails drops the socket, so the next trap redials rather
// than inheriting a dead one — on a dialed UDP socket the error being
// reported is the PREVIOUS datagram's ICMP refusal.
func TestAFailedWriteDropsTheSocket(t *testing.T) {
	d := newDialer()
	s := senderOn(t, d, TrapDestination{Addr: "10.6.250.5:162", Version: codec.Version2c})

	// First send dials and fails the write.
	sock := &fakeSocket{writeErr: errors.New("connection refused")}
	d.mu.Lock()
	d.sockets["10.6.250.5:162"] = sock
	d.mu.Unlock()

	if err := s.Send(context.Background(), alarm()); err == nil {
		t.Fatal("a failed write must be reported")
	}
	if !sock.closed {
		t.Error("the socket must be dropped so the next trap redials")
	}

	// The next send dials again rather than reusing what failed.
	before := d.dials
	_ = s.Send(context.Background(), alarm())
	if d.dials <= before {
		t.Error("the next trap must redial")
	}
}

// A cancelled context stops the fan-out rather than sending to whichever
// destinations happen to sort late.
func TestACancelledContextStopsTheFanOut(t *testing.T) {
	d := newDialer()
	s := senderOn(t, d,
		TrapDestination{Addr: "10.6.250.5:162", Version: codec.Version2c},
		TrapDestination{Addr: "10.6.250.6:162", Version: codec.Version2c})

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if err := s.Send(ctx, alarm()); !errors.Is(err, context.Canceled) {
		t.Fatalf("= %v, want the cancellation", err)
	}
	if d.dials != 0 {
		t.Errorf("%d dials after cancellation", d.dials)
	}
}

// A version no destination can be sent in is reported per destination,
// not by refusing the whole event.
func TestADestinationWithAVersionWeCannotSend(t *testing.T) {
	d := newDialer()
	s := senderOn(t, d,
		TrapDestination{Addr: "10.6.250.5:162", Version: codec.Version3},
		TrapDestination{Addr: "10.6.250.6:162", Version: codec.Version2c})

	if err := s.Send(context.Background(), alarm()); err == nil ||
		!strings.Contains(err.Error(), "v3") {
		t.Fatalf("= %v, want the version refusal", err)
	}
	if d.socket("10.6.250.6:162").count() != 1 {
		t.Error("the destination that CAN be sent to was not")
	}
}

// A notification too large for a datagram is reported rather than
// silently dropped by the socket.
func TestANotificationTooLargeToSend(t *testing.T) {
	d := newDialer()
	s := senderOn(t, d, TrapDestination{Addr: "10.6.250.5:162", Version: codec.Version2c})

	n := alarm()
	for i := 0; i < 4096; i++ {
		n.VarBinds = append(n.VarBinds, codec.VarBind{
			Name: oid("1.3.6.1.4.1.1773.1.1.3.1"), Value: codec.Bytes(make([]byte, 32))})
	}
	if err := s.Send(context.Background(), n); err == nil ||
		!strings.Contains(err.Error(), "datagram limit") {
		t.Fatalf("= %v, want the size refusal", err)
	}
}

// The socket is dialed once and kept: a trap burst during an alarm storm
// must not be a socket setup per packet.
func TestTheSocketIsDialedOnceAndKept(t *testing.T) {
	d := newDialer()
	s := senderOn(t, d, TrapDestination{Addr: "10.6.250.5:162", Version: codec.Version2c})

	for i := 0; i < 5; i++ {
		if err := s.Send(context.Background(), alarm()); err != nil {
			t.Fatalf("Send %d: %v", i, err)
		}
	}
	if d.dials != 1 {
		t.Errorf("%d dials for 5 traps", d.dials)
	}
	if got := d.socket("10.6.250.5:162").count(); got != 5 {
		t.Errorf("%d traps written, want 5", got)
	}
}

// v2c notifications are numbered, so a receiver correlating an inform's
// acknowledgement has something to correlate on.
func TestNotificationsAreNumbered(t *testing.T) {
	d := newDialer()
	s := senderOn(t, d, TrapDestination{Addr: "10.6.250.5:162", Version: codec.Version2c})

	var ids []int32
	for i := 0; i < 3; i++ {
		if err := s.Send(context.Background(), alarm()); err != nil {
			t.Fatal(err)
		}
		ids = append(ids, decodeTrap(t, d.socket("10.6.250.5:162").last()).PDU.RequestID)
	}
	if ids[0] == ids[1] || ids[1] == ids[2] {
		t.Errorf("request-ids = %v, want them to move", ids)
	}
}

// Destinations answers "who would hear about this", and hands back a
// copy — a caller that appended to it would be reconfiguring the sender.
func TestDestinationsIsACopy(t *testing.T) {
	d := newDialer()
	s := senderOn(t, d, TrapDestination{Addr: "10.6.250.5:162", Version: codec.Version2c})

	got := s.Destinations()
	if len(got) != 1 || got[0].Addr != "10.6.250.5:162" {
		t.Fatalf("= %v", got)
	}
	got[0].Addr = "somewhere else"
	if s.Destinations()[0].Addr != "10.6.250.5:162" {
		t.Error("the caller's edit reached the sender")
	}
}

// Close releases the sockets, is idempotent, and a sender that has been
// closed refuses to send rather than dialing again.
func TestCloseEndsTheSender(t *testing.T) {
	d := newDialer()
	s := NewTrapSender([]TrapDestination{
		{Addr: "10.6.250.5:162", Version: codec.Version2c}}, quiet())
	s.dial = d.dial

	if err := s.Send(context.Background(), alarm()); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if !d.socket("10.6.250.5:162").closed {
		t.Error("Close must release the socket")
	}
	if err := s.Close(); err != nil {
		t.Errorf("second Close: %v", err)
	}
	if err := s.Send(context.Background(), alarm()); !errors.Is(err, net.ErrClosed) {
		t.Errorf("= %v, want net.ErrClosed", err)
	}
}

// A Close racing a dial must not leave a socket nobody owns.
func TestACloseThatRacesADial(t *testing.T) {
	d := newDialer()
	s := NewTrapSender(nil, quiet())

	// inDial fires once connFor is PAST its own closed check and inside
	// the dial, which is the only window this arm lives in.
	inDial := make(chan struct{})
	release := make(chan struct{})
	s.dial = func(network, addr string) (net.Conn, error) {
		close(inDial)
		<-release
		return d.dial(network, addr)
	}

	done := make(chan error, 1)
	go func() {
		_, err := s.connFor("10.6.250.5:162")
		done <- err
	}()

	<-inDial
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	close(release)

	if err := <-done; !errors.Is(err, net.ErrClosed) {
		t.Errorf("= %v, want net.ErrClosed", err)
	}
	if sock := d.socket("10.6.250.5:162"); sock != nil && !sock.closed {
		t.Error("the socket the race produced was left open")
	}
}

// Two sends racing to the same destination keep ONE socket: the loser's
// is closed rather than leaked.
func TestTwoDialsForOneDestinationKeepOne(t *testing.T) {
	d := newDialer()
	s := NewTrapSender(nil, quiet())
	t.Cleanup(func() { _ = s.Close() })

	// Hold BOTH callers inside the dial until each is there, so both are
	// past the map check with neither having recorded a socket — the
	// window connFor closes.
	entered := make(chan struct{}, 2)
	proceed := make(chan struct{})
	s.dial = func(string, string) (net.Conn, error) {
		entered <- struct{}{}
		<-proceed
		d.mu.Lock()
		d.dials++
		d.mu.Unlock()
		return fakeConn{&fakeSocket{}}, nil
	}

	var wg sync.WaitGroup
	conns := make([]net.Conn, 2)
	for i := range conns {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			c, err := s.connFor("10.6.250.5:162")
			if err != nil {
				t.Errorf("connFor: %v", err)
				return
			}
			conns[i] = c
		}(i)
	}
	<-entered
	<-entered
	close(proceed)
	wg.Wait()

	if conns[0] != conns[1] {
		t.Error("two callers got two sockets for one destination")
	}
}

// A sender with no destinations sends nothing and reports nothing — an
// agent with no trap receiver configured is a normal state, not an
// error at every event.
func TestNoDestinationsIsNotAnError(t *testing.T) {
	d := newDialer()
	s := senderOn(t, d)
	if err := s.Send(context.Background(), alarm()); err != nil {
		t.Errorf("= %v, want nothing to happen", err)
	}
	if d.dials != 0 {
		t.Errorf("%d dials with no destinations", d.dials)
	}
}

// The default dialer is a real UDP socket, which nothing else here
// exercises because every test replaces it. A trap sent to a listener on
// loopback proves the sender works as shipped.
func TestTheDefaultDialerSendsARealDatagram(t *testing.T) {
	rx, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatalf("a trap receiver on loopback: %v", err)
	}
	t.Cleanup(func() { _ = rx.Close() })

	s := NewTrapSender([]TrapDestination{{
		Addr: rx.LocalAddr().String(), Version: codec.Version2c, Community: "public",
	}}, quiet())
	t.Cleanup(func() { _ = s.Close() })

	if err := s.Send(context.Background(), alarm()); err != nil {
		t.Fatalf("Send: %v", err)
	}

	_ = rx.SetReadDeadline(time.Now().Add(3 * time.Second))
	buf := make([]byte, 65535)
	n, _, err := rx.ReadFromUDP(buf)
	if err != nil {
		t.Fatalf("the trap never arrived: %v", err)
	}
	if got := decodeTrap(t, buf[:n]); got.Type() != codec.PDUTypeTrapV2 {
		t.Errorf("received a %s", got.Type())
	}

	// A destination nothing is listening on still dials; the datagram
	// simply goes nowhere, which is what UDP is.
	if _, err := s.connFor("127.0.0.1:1"); err != nil {
		t.Errorf("dialing an unused port: %v", err)
	}
	// And a destination that cannot be resolved at all is reported with
	// the address in it.
	if _, err := s.connFor("not a host:162"); err == nil ||
		!strings.Contains(err.Error(), "not a host:162") {
		t.Errorf("= %v, want the destination named", err)
	}
}

// A sender that has been closed does not dial: Send checks first, but
// connFor is also reachable directly and has to refuse on its own.
func TestConnForRefusesAfterClose(t *testing.T) {
	d := newDialer()
	s := NewTrapSender(nil, quiet())
	s.dial = d.dial

	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := s.connFor("10.6.250.5:162"); !errors.Is(err, net.ErrClosed) {
		t.Errorf("= %v, want net.ErrClosed", err)
	}
	if d.dials != 0 {
		t.Errorf("%d dials after Close", d.dials)
	}
}
