package rollcall

import (
	"context"
	"io"
	"log/slog"
	"net"
	"sync/atomic"
	"testing"
	"time"

	"dhs/internal/clock"
	"dhs/internal/export/canonical"
	"dhs/internal/metrics"
	"dhs/internal/plugin"
	"dhs/internal/snell-rollcall/codec"
	"dhs/internal/snell-rollcall/session"
)

// The provider is driven by a real client: a session.Link over net.Pipe that
// speaks the wire format. It is deliberately not the consumer plugin — a
// provider and a consumer that share an assumption agree with each other and
// with nothing else — and it is deliberately not a mock of the session layer,
// because then a wrong byte would be a rewritten expectation rather than a
// failure.
//
// Nothing sleeps. The clock is injected and fake, so a timeout only happens
// where a test asks for one.

// served is a provider with one client connected to it.
type served struct {
	t    *testing.T
	p    *Provider
	cl   *session.Link
	info codec.DeviceInfo
	clk  *clock.Fake
}

// testDeps is the dependency set every test uses.
func testDeps(clk *clock.Fake) plugin.Deps {
	return plugin.Deps{
		Logger:  slog.New(slog.NewTextHandler(io.Discard, nil)),
		Clock:   clk,
		Metrics: metrics.NewConnector(),
	}
}

// newServed builds a provider over a pipe and completes the handshake.
func newServed(t *testing.T, tree *canonical.Export) *served {
	t.Helper()
	s, _ := newServedWith(t, tree, false)
	return s
}

// newServedFaulty is the same, over a connection whose writes can be made to
// fail without the read side noticing. It is how the paths that answer into a
// connection that has gone are reached without a race: closing the socket ends
// the link from the read side first, and the handler never runs at all.
func newServedFaulty(t *testing.T, tree *canonical.Export) (*served, *blockedConn) {
	t.Helper()
	return newServedWith(t, tree, true)
}

func newServedWith(t *testing.T, tree *canonical.Export, faulty bool) (*served, *blockedConn) {
	t.Helper()

	clk := clock.NewFake(time.Time{})
	deps := testDeps(clk)
	p := New(deps, tree)

	ours, theirs := net.Pipe()

	var blocked *blockedConn
	if faulty {
		blocked = &blockedConn{Conn: theirs}
		p.serveConn(blocked)
	} else {
		p.serveConn(theirs)
	}

	cl := session.NewLink(ours, session.Config{}, deps)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	info, err := cl.Handshake(ctx)
	if err != nil {
		t.Fatalf("handshake: %v", err)
	}

	s := &served{t: t, p: p, cl: cl, info: info, clk: clk}
	t.Cleanup(func() {
		_ = cl.Close()
		_ = p.Stop()
	})
	return s, blocked
}

// blockedConn is a connection whose writes can be made to fail while its reads
// go on working, which is a socket that has gone away without our end having
// noticed yet.
type blockedConn struct {
	net.Conn
	failing atomic.Bool
}

func (c *blockedConn) Write(b []byte) (int, error) {
	if c.failing.Load() {
		return 0, io.ErrClosedPipe
	}
	return c.Conn.Write(b)
}

// callerID is what the client says about itself when it opens a session.
func callerID() codec.DeviceInfo {
	return codec.DeviceInfo{
		ProtocolVersion: codec.ProtocolVersion,
		ID:              codec.ID{Name: "test client"},
	}
}

// open opens a session on a slot with the services a test needs.
func (s *served) open(port uint8, svc codec.Service) *session.Session {
	s.t.Helper()
	return s.openLevel(port, svc, codec.LevelSupervisor)
}

func (s *served) openLevel(port uint8, svc codec.Service, level codec.UserLevel) *session.Session {
	s.t.Helper()

	peer := s.cl.RemoteAddress()
	peer.Port = port

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	sess, err := session.Call(ctx, s.cl, peer, svc, level, callerID())
	if err != nil {
		s.t.Fatalf("call port %02X: %v", port, err)
	}
	s.t.Cleanup(func() { _ = sess.Close() })
	return sess
}

// tryOpen opens a session and returns the error rather than failing.
func (s *served) tryOpen(port uint8, svc codec.Service, level codec.UserLevel) (*session.Session, error) {
	peer := s.cl.RemoteAddress()
	peer.Port = port

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	return session.Call(ctx, s.cl, peer, svc, level, callerID())
}

// do sends a request and returns the reply, failing on a transport error.
func do(t *testing.T, s *session.Session, typ codec.PacketType, payload []byte) codec.Frame {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	reply, err := s.Do(ctx, typ, payload)
	if err != nil {
		t.Fatalf("%s: %v", typ, err)
	}
	return reply
}

// refused sends a request that is expected to be refused, and returns the
// reply frame the refusal arrived in.
func refused(t *testing.T, s *session.Session, typ codec.PacketType, payload []byte) codec.Frame {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	reply, err := s.Do(ctx, typ, payload)
	if err == nil {
		t.Fatalf("%s: expected a refusal, got %s", typ, reply.Type)
	}
	return reply
}

// testTree is a small frame: two cards, one with a nested menu covering every
// parameter kind that maps differently, one with a single line.
func testTree() *canonical.Export {
	factor := int64(10)
	unit := "dB"

	gain := &canonical.Parameter{
		Header: canonical.Header{
			Number: 1, Identifier: "gain", Path: "frame.card1.video.gain",
			Access: canonical.AccessReadWrite,
		},
		Type: canonical.ParamReal, Value: 0.0,
		Minimum: -60.0, Maximum: 6.0, Step: 0.5,
		Unit: &unit, Factor: &factor,
	}
	enable := &canonical.Parameter{
		Header: canonical.Header{
			Number: 2, Identifier: "enable", Path: "frame.card1.video.enable",
			Access: canonical.AccessReadWrite,
		},
		Type: canonical.ParamBoolean, Value: true,
	}
	name := &canonical.Parameter{
		Header: canonical.Header{
			Number: 3, Identifier: "name", Path: "frame.card1.video.name",
			Access: canonical.AccessReadWrite,
		},
		Type: canonical.ParamString, Value: "SDI 1",
	}
	status := &canonical.Parameter{
		Header: canonical.Header{
			Number: 4, Identifier: "status", Path: "frame.card1.status",
			Access: canonical.AccessRead,
		},
		Type: canonical.ParamInteger, Value: int64(3), Minimum: 0.0, Maximum: 9.0,
	}

	video := &canonical.Node{Header: canonical.Header{
		Number: 1, Identifier: "video", Path: "frame.card1.video",
		Access:   canonical.AccessRead,
		Children: []canonical.Element{gain, enable, name},
	}}
	card1 := &canonical.Node{Header: canonical.Header{
		Number: 1, Identifier: "card1", Path: "frame.card1",
		Access:   canonical.AccessRead,
		Children: []canonical.Element{video, status},
	}}

	level := &canonical.Parameter{
		Header: canonical.Header{
			Number: 1, Identifier: "level", Path: "frame.card2.level",
			Access: canonical.AccessReadWrite,
		},
		Type: canonical.ParamInteger, Value: int64(5), Minimum: 0.0, Maximum: 10.0,
	}
	card2 := &canonical.Node{Header: canonical.Header{
		Number: 2, Identifier: "card2", Path: "frame.card2",
		Access:   canonical.AccessRead,
		Children: []canonical.Element{level},
	}}

	root := &canonical.Node{Header: canonical.Header{
		Identifier: "frame", Path: "frame", Access: canonical.AccessRead,
		Children: []canonical.Element{card1, card2},
	}}
	return &canonical.Export{Root: root}
}

// commandOf finds the command number behind a path, which a test needs before
// it can read or write anything.
func commandOf(t *testing.T, p *Provider, path string) (uint8, uint32) {
	t.Helper()

	slot, command, _, ok := p.resolve(path)
	if !ok {
		t.Fatalf("no object at %q", path)
	}
	return slot, command
}

func TestHandshakeAssignsAnAddress(t *testing.T) {
	s := newServed(t, testTree())

	// The gateway names itself, and the reply's destination is the address it
	// assigned. A client with a zeroed address cannot open a session at all.
	if got := s.cl.LocalAddress(); got.Unit != defaultUnit || got.Port != firstClientPort {
		t.Errorf("assigned address = %s, want unit %d port %02X",
			got, defaultUnit, firstClientPort)
	}
	if got := s.cl.RemoteAddress(); got.Unit != defaultUnit {
		t.Errorf("gateway address = %s, want unit %d", got, defaultUnit)
	}
	if s.info.ID.Name != "dhs rollcall" {
		t.Errorf("gateway name = %q", s.info.ID.Name)
	}
	if !s.info.ID.Services.LongStrings() {
		t.Error("a gateway that serves both generations must advertise long strings")
	}
}

func TestSetUnitChangesWhatTheGatewayAnswersAs(t *testing.T) {
	p := New(testDeps(clock.NewFake(time.Time{})), testTree())
	p.SetUnit(8)

	if got := p.deviceInfoFor(0); len(got) == 0 {
		t.Fatal("no device info")
	}
	info, err := codec.DecodeDeviceInfo(p.deviceInfoFor(0))
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if info.Address.Unit != 8 {
		t.Errorf("unit = %d, want 8", info.Address.Unit)
	}
}

func TestServeAcceptsAndStops(t *testing.T) {
	clk := clock.NewFake(time.Time{})
	p := New(testDeps(clk), testTree())

	errs := make(chan error, 1)
	go func() { errs <- p.Serve(context.Background(), "127.0.0.1:0") }()

	addr := waitForAddr(t, p)

	conn, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	cl := session.NewLink(conn, session.Config{}, testDeps(clk))

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, err := cl.Handshake(ctx); err != nil {
		t.Fatalf("handshake over tcp: %v", err)
	}
	_ = cl.Close()

	if err := p.Stop(); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	if err := <-errs; err != nil {
		t.Fatalf("Serve returned %v, want nil after Stop", err)
	}
	if p.Addr() != "" && addr == "" {
		t.Fatal("unreachable")
	}
}

func TestServeEndsWithTheContext(t *testing.T) {
	p := New(testDeps(clock.NewFake(time.Time{})), testTree())

	ctx, cancel := context.WithCancel(context.Background())
	errs := make(chan error, 1)
	go func() { errs <- p.Serve(ctx, "127.0.0.1:0") }()

	waitForAddr(t, p)
	cancel()

	if err := <-errs; err != context.Canceled {
		t.Fatalf("Serve returned %v, want context.Canceled", err)
	}
}

func TestServeRefusesAnAddressItCannotBind(t *testing.T) {
	p := New(testDeps(clock.NewFake(time.Time{})), testTree())

	err := p.Serve(context.Background(), "127.0.0.1:99999")
	if err == nil {
		t.Fatal("binding an impossible port should fail")
	}
	if got := p.Addr(); got != "" {
		t.Errorf("Addr after a failed bind = %q, want empty", got)
	}
}

// waitForAddr spins until Serve has bound, which it does on its own goroutine.
func waitForAddr(t *testing.T, p *Provider) string {
	t.Helper()

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if addr := p.Addr(); addr != "" {
			return addr
		}
	}
	t.Fatal("Serve never bound")
	return ""
}
