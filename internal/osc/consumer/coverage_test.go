package osc

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"testing"
	"time"

	"dhs/internal/consumer"
	"dhs/internal/osc/codec"
)

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

// recvEvent waits for one PacketEvent or fails the test.
func recvEvent(t *testing.T, ch <-chan PacketEvent) PacketEvent {
	t.Helper()
	select {
	case ev := <-ch:
		return ev
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for PacketEvent")
		return PacketEvent{}
	}
}

// dialUDPSend opens a UDP socket to addr and writes one datagram.
func dialUDPSend(t *testing.T, addr *net.UDPAddr, b []byte) {
	t.Helper()
	sock, err := net.DialUDP("udp", nil, addr)
	if err != nil {
		t.Fatalf("dial udp: %v", err)
	}
	defer func() { _ = sock.Close() }()
	if _, err := sock.Write(b); err != nil {
		t.Fatalf("write udp: %v", err)
	}
}

// encoder is satisfied by codec.Message and codec.Bundle (Encode is on the
// concrete types, not the codec.Packet interface).
type encoder interface {
	Encode() ([]byte, error)
}

func mustEncode(t *testing.T, p encoder) []byte {
	t.Helper()
	b, err := p.Encode()
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	return b
}

// ---------------------------------------------------------------------------
// Version stringers — default branches
// ---------------------------------------------------------------------------

func TestVersion_Stringers_UnknownDefault(t *testing.T) {
	bad := Version(99)
	if got := bad.name(); got != "osc-unknown" {
		t.Errorf("name() = %q, want osc-unknown", got)
	}
	if got := bad.description(); got != "" {
		t.Errorf("description() = %q, want empty", got)
	}
}

func TestVersion_KnownStringers(t *testing.T) {
	for _, v := range []Version{V10, V11} {
		if v.name() == "osc-unknown" || v.name() == "" {
			t.Errorf("name(%v) unexpectedly unknown", v)
		}
		if v.description() == "" {
			t.Errorf("description(%v) empty", v)
		}
		if v.defaultPort() != 8000 {
			t.Errorf("defaultPort(%v) = %d, want 8000", v, v.defaultPort())
		}
	}
}

// ---------------------------------------------------------------------------
// Factory.New + Meta
// ---------------------------------------------------------------------------

func TestFactory_New_Meta(t *testing.T) {
	for _, tc := range []struct {
		v        Version
		wantName string
	}{
		{V10, "osc-v10"},
		{V11, "osc-v11"},
	} {
		f := &Factory{version: tc.v}
		p := f.New(slog.Default())
		plug, ok := p.(*Plugin)
		if !ok {
			t.Fatalf("New returned %T, want *Plugin", p)
		}
		if plug.version != tc.v {
			t.Errorf("version = %v, want %v", plug.version, tc.v)
		}
		m := f.Meta()
		if m.Name != tc.wantName || m.DefaultPort != 8000 || m.Description == "" {
			t.Errorf("Meta() = %+v", m)
		}
	}
}

func TestNewPluginConstructors(t *testing.T) {
	if p := NewPluginV10(slog.Default()); p.version != V10 {
		t.Errorf("NewPluginV10 version = %v, want V10", p.version)
	}
	if p := NewPluginV11(slog.Default()); p.version != V11 {
		t.Errorf("NewPluginV11 version = %v, want V11", p.version)
	}
}

// ---------------------------------------------------------------------------
// Plugin UDP: Connect / dispatch / Bound* / Subscribe / Disconnect
// ---------------------------------------------------------------------------

func TestPlugin_Connect_UDP_DispatchAndDisconnect(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	p := NewPluginV10(slog.Default())
	if err := p.Connect(ctx, "127.0.0.1", 0); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	defer func() { _ = p.Disconnect() }()

	addr := p.BoundAddr()
	if addr == nil {
		t.Fatal("BoundAddr nil after Connect")
	}
	if p.BoundTCPAddr() != nil {
		t.Error("BoundTCPAddr should be nil for UDP plugin")
	}

	ch := make(chan PacketEvent, 1)
	if err := p.SubscribePattern("/x/y", func(ev PacketEvent) { ch <- ev }); err != nil {
		t.Fatalf("SubscribePattern: %v", err)
	}

	wire := mustEncode(t, codec.Message{Address: "/x/y", Args: []codec.Arg{codec.Int32(7)}})
	dialUDPSend(t, addr, wire)

	ev := recvEvent(t, ch)
	if ev.Msg.Address != "/x/y" || ev.Msg.Args[0].Int32 != 7 {
		t.Errorf("event msg = %+v", ev.Msg)
	}
	if _, ok := ev.Remote.(*net.UDPAddr); !ok {
		t.Errorf("Remote = %T, want *net.UDPAddr", ev.Remote)
	}

	// Connecting twice must fail (UDP already connected).
	if err := p.Connect(ctx, "127.0.0.1", 0); err == nil {
		t.Error("second Connect should fail with already-connected")
	}
	// ConnectTCP after UDP Connect must also fail.
	if err := p.ConnectTCP(ctx, "127.0.0.1", 0); err == nil {
		t.Error("ConnectTCP after UDP Connect should fail")
	}
}

func TestPlugin_Connect_ListenError(t *testing.T) {
	p := NewPluginV10(slog.Default())
	if err := p.Connect(context.Background(), "127.0.0.1", -1); err == nil {
		t.Fatal("expected listen error for invalid port")
	}
}

func TestPlugin_Disconnect_NoSession(t *testing.T) {
	p := NewPluginV10(slog.Default())
	if err := p.Disconnect(); err != nil {
		t.Errorf("Disconnect on fresh plugin: %v", err)
	}
	if p.BoundAddr() != nil {
		t.Error("BoundAddr should be nil when not connected")
	}
	if p.BoundTCPAddr() != nil {
		t.Error("BoundTCPAddr should be nil when not connected")
	}
}

func TestPlugin_SubscribePattern_NotConnected(t *testing.T) {
	p := NewPluginV10(slog.Default())
	if err := p.SubscribePattern("/x", func(PacketEvent) {}); err == nil {
		t.Error("SubscribePattern not connected should fail")
	}
}

// ---------------------------------------------------------------------------
// Plugin TCP: both framings (v1.0 length-prefix, v1.1 SLIP)
// ---------------------------------------------------------------------------

func TestPlugin_ConnectTCP_V10_LenPrefix_Dispatch(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	p := NewPluginV10(slog.Default())
	if err := p.ConnectTCP(ctx, "127.0.0.1", 0); err != nil {
		t.Fatalf("ConnectTCP: %v", err)
	}
	defer func() { _ = p.Disconnect() }()

	tcpAddr := p.BoundTCPAddr()
	if tcpAddr == nil {
		t.Fatal("BoundTCPAddr nil after ConnectTCP")
	}
	if p.BoundAddr() != nil {
		t.Error("BoundAddr should be nil in TCP mode")
	}

	ch := make(chan PacketEvent, 4)
	if err := p.SubscribePattern("/mix/*", func(ev PacketEvent) { ch <- ev }); err != nil {
		t.Fatalf("SubscribePattern (tcp): %v", err)
	}

	conn, err := net.Dial("tcp", tcpAddr.String())
	if err != nil {
		t.Fatalf("dial tcp: %v", err)
	}
	defer func() { _ = conn.Close() }()

	msg := mustEncode(t, codec.Message{Address: "/mix/ch1", Args: []codec.Arg{codec.Float32(0.5)}})
	if _, err := conn.Write(codec.EncodeLenPrefix(msg)); err != nil {
		t.Fatalf("write framed: %v", err)
	}

	ev := recvEvent(t, ch)
	if ev.Msg.Address != "/mix/ch1" || ev.Matched != "/mix/ch1" {
		t.Fatalf("tcp lenprefix msg wrong: %+v", ev.Msg)
	}
	if _, ok := ev.Remote.(*net.TCPAddr); !ok {
		t.Errorf("Remote = %T, want *net.TCPAddr", ev.Remote)
	}

	// Already connected guards.
	if err := p.ConnectTCP(ctx, "127.0.0.1", 0); err == nil {
		t.Error("second ConnectTCP should fail with already-connected")
	}
	if err := p.Connect(ctx, "127.0.0.1", 0); err == nil {
		t.Error("Connect after ConnectTCP should fail")
	}
}

func TestPlugin_ConnectTCP_V11_SLIP_DispatchBundle(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	p := NewPluginV11(slog.Default())
	if err := p.ConnectTCP(ctx, "127.0.0.1", 0); err != nil {
		t.Fatalf("ConnectTCP: %v", err)
	}
	defer func() { _ = p.Disconnect() }()

	ch := make(chan PacketEvent, 8)
	if err := p.SubscribePattern("", func(ev PacketEvent) { ch <- ev }); err != nil {
		t.Fatalf("SubscribePattern: %v", err)
	}

	conn, err := net.Dial("tcp", p.BoundTCPAddr().String())
	if err != nil {
		t.Fatalf("dial tcp: %v", err)
	}
	defer func() { _ = conn.Close() }()

	// A bundle with two messages + a nested bundle (exercises fireBundle
	// recursion on the TCP session).
	bundle := codec.Bundle{
		Timetag: 1,
		Elements: []codec.Packet{
			codec.Message{Address: "/a", Args: []codec.Arg{codec.Int32(1)}},
			codec.Message{Address: "/b", Args: []codec.Arg{codec.Int32(2)}},
			codec.Bundle{
				Timetag: 2,
				Elements: []codec.Packet{
					codec.Message{Address: "/c", Args: []codec.Arg{codec.Int32(3)}},
				},
			},
		},
	}
	wire := mustEncode(t, bundle)
	if _, err := conn.Write(codec.EncodeSLIP(wire)); err != nil {
		t.Fatalf("write slip: %v", err)
	}

	seen := map[string]bool{}
	deadline := time.After(2 * time.Second)
	for len(seen) < 3 {
		select {
		case ev := <-ch:
			seen[ev.Msg.Address] = true
		case <-deadline:
			t.Fatalf("got %v, want /a /b /c", seen)
		}
	}
	for _, want := range []string{"/a", "/b", "/c"} {
		if !seen[want] {
			t.Errorf("missing %s", want)
		}
	}
}

func TestPlugin_ConnectTCP_ListenError(t *testing.T) {
	p := NewPluginV10(slog.Default())
	if err := p.ConnectTCP(context.Background(), "127.0.0.1", -1); err == nil {
		t.Fatal("expected listen error for invalid TCP port")
	}
}

// TestPlugin_Disconnect_TCPCloseError drives Disconnect's TCP-close-error
// arm (e != nil && err == nil): no UDP session, a tcpSession whose listener
// is already closed so its close() returns a non-nil error.
func TestPlugin_Disconnect_TCPCloseError(t *testing.T) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	_ = l.Close() // pre-close so tcpSession.close() -> l.Close() errors

	p := NewPluginV10(slog.Default())
	p.tcp = &tcpSession{listener: l}

	if derr := p.Disconnect(); derr == nil {
		t.Fatal("expected Disconnect to surface the TCP close error")
	}
	if p.tcp != nil {
		t.Error("tcp should be cleared after Disconnect")
	}
}

// TestPlugin_Disconnect_UDPAndTCP exercises Disconnect closing both a UDP
// and a TCP session in one call (both nil-out arms).
func TestPlugin_Disconnect_UDPAndTCP(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	p := NewPluginV10(slog.Default())
	// Inject both sessions directly (Connect/ConnectTCP guard against it).
	u := newUDPSession()
	if err := u.listen(ctx, "127.0.0.1:0"); err != nil {
		t.Fatalf("udp listen: %v", err)
	}
	tcp := newTCPSession(framerLenPrefix)
	if err := tcp.listen(ctx, "127.0.0.1:0"); err != nil {
		t.Fatalf("tcp listen: %v", err)
	}
	p.udp = u
	p.tcp = tcp
	if err := p.Disconnect(); err != nil {
		t.Fatalf("Disconnect: %v", err)
	}
	if p.udp != nil || p.tcp != nil {
		t.Error("both sessions should be cleared")
	}
}

// TestPlugin_SubscribePattern_TCP covers the tcp arm of SubscribePattern.
func TestPlugin_SubscribePattern_TCP(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	p := NewPluginV11(slog.Default())
	if err := p.ConnectTCP(ctx, "127.0.0.1", 0); err != nil {
		t.Fatalf("ConnectTCP: %v", err)
	}
	defer func() { _ = p.Disconnect() }()
	if err := p.SubscribePattern("/x", func(PacketEvent) {}); err != nil {
		t.Errorf("SubscribePattern tcp arm: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Protocol stub methods (ErrNotImplemented / no-op)
// ---------------------------------------------------------------------------

func TestPlugin_StubMethods(t *testing.T) {
	p := NewPluginV10(slog.Default())
	ctx := context.Background()

	if _, err := p.GetDeviceInfo(ctx); !errors.Is(err, consumer.ErrNotImplemented) {
		t.Errorf("GetDeviceInfo err = %v", err)
	}
	if _, err := p.GetSlotInfo(ctx, 0); !errors.Is(err, consumer.ErrNotImplemented) {
		t.Errorf("GetSlotInfo err = %v", err)
	}
	if _, err := p.Walk(ctx, 0); !errors.Is(err, consumer.ErrNotImplemented) {
		t.Errorf("Walk err = %v", err)
	}
	if _, err := p.GetValue(ctx, consumer.ValueRequest{}); !errors.Is(err, consumer.ErrNotImplemented) {
		t.Errorf("GetValue err = %v", err)
	}
	if _, err := p.SetValue(ctx, consumer.ValueRequest{}, consumer.Value{}); !errors.Is(err, consumer.ErrNotImplemented) {
		t.Errorf("SetValue err = %v", err)
	}
	if err := p.Subscribe(consumer.ValueRequest{}, nil); !errors.Is(err, consumer.ErrNotImplemented) {
		t.Errorf("Subscribe err = %v", err)
	}
	if err := p.Unsubscribe(consumer.ValueRequest{}); err != nil {
		t.Errorf("Unsubscribe should be a no-op nil, got %v", err)
	}
}

// ---------------------------------------------------------------------------
// udpSession: fireAll via decode error, boundAddr nil, readLoop branches
// ---------------------------------------------------------------------------

// TestUDPSession_DecodeError_FiresAll sends an undecodable packet; the
// match-all ("") subscriber must receive a synthetic osc_decode_error note
// (drives dispatch's error arm + fireAll).
func TestUDPSession_DecodeError_FiresAll(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s := newUDPSession()
	if err := s.listen(ctx, "127.0.0.1:0"); err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer func() { _ = s.close() }()

	all := make(chan PacketEvent, 1)
	s.subscribe("", func(ev PacketEvent) { all <- ev })
	// A pattern subscriber that must NOT receive the decode-error event.
	s.subscribe("/never", func(ev PacketEvent) { t.Errorf("pattern sub fired on decode error: %+v", ev) })

	// First byte not '/' or '#' -> DecodePacket returns ErrBundleNotBundle.
	dialUDPSend(t, s.boundAddr(), []byte{0x01, 0x02, 0x03, 0x04})

	ev := recvEvent(t, all)
	m, ok := ev.Packet.(codec.Message)
	if !ok || len(m.Notes) == 0 || m.Notes[0].Kind != "osc_decode_error" {
		t.Fatalf("expected osc_decode_error note, got %+v", ev.Packet)
	}
}

func TestUDPSession_BoundAddrNil(t *testing.T) {
	if newUDPSession().boundAddr() != nil {
		t.Error("boundAddr on fresh udpSession should be nil")
	}
}

func TestUDPSession_ListenError(t *testing.T) {
	s := newUDPSession()
	if err := s.listen(context.Background(), "127.0.0.1:-1"); err == nil {
		t.Fatal("expected listen error for invalid port")
	}
	if s.boundAddr() != nil {
		t.Error("boundAddr should be nil after failed listen")
	}
}

// TestUDPSession_ReadLoop_CtxCancelledAtTop drives readLoop's loop-top
// ctx.Err() guard with an already-cancelled context.
func TestUDPSession_ReadLoop_CtxCancelledAtTop(t *testing.T) {
	pc, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer func() { _ = pc.Close() }()
	s := &udpSession{conn: pc}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	s.readLoop(ctx) // returns immediately via top guard
}

// TestUDPSession_ReadLoop_TransientErrorContinues drives readLoop's
// transient-read-error continue arm (ctx still live): a near-future read
// deadline makes the first ReadFromUDP time out (transient), the loop
// continues, then a real packet after the deadline is cleared proves the
// loop kept running.
func TestUDPSession_ReadLoop_TransientErrorContinues(t *testing.T) {
	pc, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	s := &udpSession{conn: pc}
	defer func() { _ = pc.Close() }()

	ch := make(chan PacketEvent, 1)
	s.subscribe("", func(ev PacketEvent) { ch <- ev })

	if err := pc.SetReadDeadline(time.Now().Add(150 * time.Millisecond)); err != nil {
		t.Fatalf("set deadline: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go s.readLoop(ctx)

	addr := pc.LocalAddr().(*net.UDPAddr)
	go func() {
		time.Sleep(250 * time.Millisecond)
		_ = pc.SetReadDeadline(time.Time{}) // clear; future reads block
		wire, _ := codec.Message{Address: "/late", Args: []codec.Arg{codec.Int32(1)}}.Encode()
		sock, derr := net.DialUDP("udp", nil, addr)
		if derr != nil {
			return
		}
		defer func() { _ = sock.Close() }()
		_, _ = sock.Write(wire)
	}()

	ev := recvEvent(t, ch)
	if ev.Msg.Address != "/late" {
		t.Errorf("address = %q, want /late", ev.Msg.Address)
	}
}

// TestUDPSession_ReadLoop_ErrAfterCancel drives readLoop's "ReadFromUDP
// errored AND ctx already cancelled" return arm: the conn is closed (so
// ReadFromUDP errors) while ctx is cancelled.
func TestUDPSession_ReadLoop_ErrAfterCancel(t *testing.T) {
	pc, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	s := &udpSession{conn: pc}
	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan struct{})
	go func() {
		s.readLoop(ctx)
		close(done)
	}()
	// Cancel, then close the conn so the blocked ReadFromUDP returns an
	// error and the loop hits the `if ctx.Err() != nil { return }` arm
	// inside the error branch.
	cancel()
	_ = pc.Close()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("readLoop did not return after cancel+close")
	}
}

// ---------------------------------------------------------------------------
// tcpSession: lifecycle + branches
// ---------------------------------------------------------------------------

func TestTCPSession_BoundAddrNil(t *testing.T) {
	if newTCPSession(framerLenPrefix).boundAddr() != nil {
		t.Error("boundAddr on fresh tcpSession should be nil")
	}
}

func TestTCPSession_ListenError(t *testing.T) {
	s := newTCPSession(framerSLIP)
	if err := s.listen(context.Background(), "127.0.0.1:-1"); err == nil {
		t.Fatal("expected listen error for invalid port")
	}
}

// TestTCPSession_AcceptLoop_CtxCancelledAtTop drives acceptLoop with an
// already-cancelled context so the loop-top ctx.Err() guard returns before
// the first Accept.
func TestTCPSession_AcceptLoop_CtxCancelledAtTop(t *testing.T) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer func() { _ = l.Close() }()
	s := &tcpSession{listener: l}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	s.wg.Add(1)
	s.acceptLoop(ctx)
	s.wg.Wait()
}

// TestTCPSession_AcceptLoop_ListenerClosed drives the ErrClosed return arm.
func TestTCPSession_AcceptLoop_ListenerClosed(t *testing.T) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	_ = l.Close() // close before accept; ctx stays live
	s := &tcpSession{listener: l}
	s.wg.Add(1)
	s.acceptLoop(context.Background())
	s.wg.Wait()
}

// transientErrListener: first Accept returns a transient (retryable,
// non-ErrClosed) error to drive acceptLoop's `continue`; second returns
// net.ErrClosed to terminate.
type transientErrListener struct {
	calls int
}

type tempNetErr struct{}

func (tempNetErr) Error() string   { return "temporary accept error" }
func (tempNetErr) Timeout() bool   { return true }
func (tempNetErr) Temporary() bool { return true }

func (l *transientErrListener) Accept() (net.Conn, error) {
	l.calls++
	if l.calls == 1 {
		return nil, tempNetErr{}
	}
	return nil, net.ErrClosed
}
func (l *transientErrListener) Close() error   { return nil }
func (l *transientErrListener) Addr() net.Addr { return &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1)} }

func TestTCPSession_AcceptLoop_TransientErrorContinues(t *testing.T) {
	l := &transientErrListener{}
	s := &tcpSession{listener: l}
	s.wg.Add(1)
	s.acceptLoop(context.Background())
	s.wg.Wait()
	if l.calls < 2 {
		t.Errorf("Accept called %d times, want >= 2 (continue then exit)", l.calls)
	}
}

// TestTCPSession_ConnLoop_CtxCancelledAtTop drives connLoop's loop-top
// ctx.Err() guard with an already-cancelled context.
func TestTCPSession_ConnLoop_CtxCancelledAtTop(t *testing.T) {
	s := newTCPSession(framerLenPrefix)
	c1, c2 := net.Pipe()
	defer func() { _ = c2.Close() }()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	s.wg.Add(1)
	s.connLoop(ctx, c1) // returns via top guard; also closes c1
	s.wg.Wait()
}

// TestTCPSession_ConnLoop_CleanEOF: a length-prefix conn that closes between
// packets makes ReadPacket return io.EOF -> clean return (no decode-error
// note). Verified by writing one good packet, asserting it, then closing.
func TestTCPSession_ConnLoop_CleanEOF(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s := newTCPSession(framerLenPrefix)
	if err := s.listen(ctx, "127.0.0.1:0"); err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer func() { _ = s.close() }()

	ch := make(chan PacketEvent, 1)
	s.subscribe("", func(ev PacketEvent) { ch <- ev })

	conn, err := net.Dial("tcp", s.boundAddr().String())
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	msg := mustEncode(t, codec.Message{Address: "/ok", Args: []codec.Arg{codec.Int32(1)}})
	if _, err := conn.Write(codec.EncodeLenPrefix(msg)); err != nil {
		t.Fatalf("write: %v", err)
	}
	ev := recvEvent(t, ch)
	if ev.Msg.Address != "/ok" {
		t.Fatalf("address = %q, want /ok", ev.Msg.Address)
	}
	// Clean close between packets -> connLoop returns via io.EOF arm.
	_ = conn.Close()
	// Give the conn goroutine a moment; nothing else should arrive.
	select {
	case extra := <-ch:
		t.Errorf("unexpected extra event after clean close: %+v", extra)
	case <-time.After(150 * time.Millisecond):
	}
}

// TestTCPSession_ConnLoop_FramingError: a length-prefix stream with a
// negative size triggers a framing error -> fireDecodeError on "" subs then
// connLoop closes.
func TestTCPSession_ConnLoop_FramingError(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s := newTCPSession(framerLenPrefix)
	if err := s.listen(ctx, "127.0.0.1:0"); err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer func() { _ = s.close() }()

	ch := make(chan PacketEvent, 1)
	s.subscribe("", func(ev PacketEvent) { ch <- ev })

	conn, err := net.Dial("tcp", s.boundAddr().String())
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer func() { _ = conn.Close() }()

	// 0xFFFFFFFF length-prefix -> int32 negative size -> ErrLenPrefixNegative.
	if _, err := conn.Write([]byte{0xFF, 0xFF, 0xFF, 0xFF}); err != nil {
		t.Fatalf("write: %v", err)
	}

	ev := recvEvent(t, ch)
	m, ok := ev.Packet.(codec.Message)
	if !ok || len(m.Notes) == 0 || m.Notes[0].Kind != "osc_decode_error" {
		t.Fatalf("expected osc_decode_error note (framing), got %+v", ev.Packet)
	}
}

// TestTCPSession_ConnLoop_SLIPFramingError: SLIP stream with an invalid
// escape sequence triggers ErrSLIPBadEscape -> fireDecodeError.
func TestTCPSession_ConnLoop_SLIPFramingError(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s := newTCPSession(framerSLIP)
	if err := s.listen(ctx, "127.0.0.1:0"); err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer func() { _ = s.close() }()

	ch := make(chan PacketEvent, 1)
	s.subscribe("", func(ev PacketEvent) { ch <- ev })

	conn, err := net.Dial("tcp", s.boundAddr().String())
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer func() { _ = conn.Close() }()

	// END, ESC, <bad>, END : ESC not followed by ESC_END/ESC_ESC.
	if _, err := conn.Write([]byte{codec.SLIPEnd, codec.SLIPEsc, 0x00, codec.SLIPEnd}); err != nil {
		t.Fatalf("write: %v", err)
	}

	ev := recvEvent(t, ch)
	m, ok := ev.Packet.(codec.Message)
	if !ok || len(m.Notes) == 0 || m.Notes[0].Kind != "osc_decode_error" {
		t.Fatalf("expected osc_decode_error note (slip), got %+v", ev.Packet)
	}
}

// TestTCPSession_DispatchPacket_DecodeError: a well-framed packet whose
// body is undecodable (first byte not '/' or '#') drives dispatchPacket's
// DecodePacket error arm -> fireDecodeError.
func TestTCPSession_DispatchPacket_DecodeError(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s := newTCPSession(framerLenPrefix)
	if err := s.listen(ctx, "127.0.0.1:0"); err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer func() { _ = s.close() }()

	ch := make(chan PacketEvent, 1)
	s.subscribe("", func(ev PacketEvent) { ch <- ev })

	conn, err := net.Dial("tcp", s.boundAddr().String())
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer func() { _ = conn.Close() }()

	// Length-prefixed body of 4 junk bytes -> ReadPacket OK, DecodePacket err.
	if _, err := conn.Write(codec.EncodeLenPrefix([]byte{0x01, 0x02, 0x03, 0x04})); err != nil {
		t.Fatalf("write: %v", err)
	}

	ev := recvEvent(t, ch)
	m, ok := ev.Packet.(codec.Message)
	if !ok || len(m.Notes) == 0 || m.Notes[0].Kind != "osc_decode_error" {
		t.Fatalf("expected osc_decode_error note (dispatch), got %+v", ev.Packet)
	}
}

// TestTCPSession_Close_Idempotent confirms close() is safe to call twice
// (closeOnce) and returns the same result.
func TestTCPSession_Close_Idempotent(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s := newTCPSession(framerLenPrefix)
	if err := s.listen(ctx, "127.0.0.1:0"); err != nil {
		t.Fatalf("listen: %v", err)
	}
	if err := s.close(); err != nil {
		t.Errorf("first close: %v", err)
	}
	if err := s.close(); err != nil {
		t.Errorf("second close: %v", err)
	}
}

// TestTCPSession_Close_Fresh covers close() on a session that never
// listened (cancel nil, listener nil).
func TestTCPSession_Close_Fresh(t *testing.T) {
	if err := newTCPSession(framerLenPrefix).close(); err != nil {
		t.Errorf("close on fresh tcpSession: %v", err)
	}
}

func TestUDPSession_Close_Fresh(t *testing.T) {
	if err := newUDPSession().close(); err != nil {
		t.Errorf("close on fresh udpSession: %v", err)
	}
}

// ---------------------------------------------------------------------------
// pattern.go: unbalanced bracket / brace literal-fallthrough arms
// ---------------------------------------------------------------------------

func TestSegmentMatches_UnbalancedBracketsAndBraces(t *testing.T) {
	cases := []struct {
		name    string
		pattern string
		addr    string
		want    bool
	}{
		// '[' with no closing ']' -> treated as literal '['. The match
		// case must differ from the addr so addressMatches' fast-path
		// equality check (pattern == addr) does NOT short-circuit before
		// matchHere runs — a trailing '?' after the literal '[' keeps the
		// strings unequal while still matching the '[' literally.
		{"open bracket literal match", "/a[?", "/a[b", true},
		{"open bracket literal mismatch", "/a[b", "/axb", false},
		{"open bracket literal short", "/a[", "/a", false},
		// closing ']' present but segment already exhausted at the class.
		{"class but segment exhausted", "/a[bc]", "/a", false},
		// '{' with no closing '}' -> treated as literal '{' (trailing '?'
		// keeps pattern != addr to bypass the equality fast-path).
		{"open brace literal match", "/a{?", "/a{b", true},
		{"open brace literal mismatch", "/a{b", "/ayb", false},
		{"open brace literal short", "/a{", "/a", false},

		// '*' in the middle followed by a literal that never matches ->
		// the split-point loop exhausts and returns false.
		{"star middle no split matches", "/a*z", "/abc", false},
		// trailing literal after '*' that requires more chars than remain.
		{"star then literal too long", "/*xyz", "/ab", false},
		// consecutive '*' collapse then match the rest.
		{"double star collapses", "/a**b", "/axyzb", true},
		// '?' that runs past the end of the segment in a mid position.
		{"question past end mid", "/a?c", "/ac", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := addressMatches(c.pattern, c.addr); got != c.want {
				t.Errorf("match(%q, %q) = %v, want %v", c.pattern, c.addr, got, c.want)
			}
		})
	}
}
