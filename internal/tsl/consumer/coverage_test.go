package tsl

import (
	"context"
	"dhs/internal/plugin"
	"encoding/hex"
	"log/slog"
	"net"
	"testing"
	"time"

	"dhs/internal/consumer"
	"dhs/internal/export/canonical"
	"dhs/internal/tsl/codec"
	"dhs/internal/wiretrace"
)

// recvV31 waits for a v3.1 event or fails.
func recvV31(t *testing.T, ch <-chan FrameV31Event) FrameV31Event {
	t.Helper()
	select {
	case ev := <-ch:
		return ev
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for v3.1 event")
		return FrameV31Event{}
	}
}

func recvV40(t *testing.T, ch <-chan FrameV40Event) FrameV40Event {
	t.Helper()
	select {
	case ev := <-ch:
		return ev
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for v4.0 event")
		return FrameV40Event{}
	}
}

func recvV50(t *testing.T, ch <-chan FrameV50Event) FrameV50Event {
	t.Helper()
	select {
	case ev := <-ch:
		return ev
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for v5.0 event")
		return FrameV50Event{}
	}
}

// dialAndSend opens a UDP socket to addr and writes one datagram.
func dialAndSend(t *testing.T, addr *net.UDPAddr, b []byte) {
	t.Helper()
	sock, err := net.DialUDP("udp", nil, addr)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer func() { _ = sock.Close() }()
	if _, err := sock.Write(b); err != nil {
		t.Fatalf("write: %v", err)
	}
}

// --- Version stringers: cover default branches -----------------------------

func TestVersion_Stringers_UnknownDefault(t *testing.T) {
	bad := Version(99)
	if got := bad.name(); got != "tsl-unknown" {
		t.Errorf("name() = %q, want tsl-unknown", got)
	}
	if got := bad.defaultPort(); got != 0 {
		t.Errorf("defaultPort() = %d, want 0", got)
	}
	if got := bad.description(); got != "" {
		t.Errorf("description() = %q, want empty", got)
	}
}

func TestVersion_KnownStringers(t *testing.T) {
	for _, v := range []Version{V31, V40, V50} {
		if v.name() == "tsl-unknown" || v.name() == "" {
			t.Errorf("name(%v) unexpectedly unknown", v)
		}
		if v.description() == "" {
			t.Errorf("description(%v) empty", v)
		}
	}
	if V31.defaultPort() != 4000 || V40.defaultPort() != 4000 || V50.defaultPort() != 8901 {
		t.Errorf("default ports wrong: v31=%d v40=%d v50=%d",
			V31.defaultPort(), V40.defaultPort(), V50.defaultPort())
	}
}

// --- Factory.New + Meta ----------------------------------------------------

func TestFactory_New(t *testing.T) {
	f := &Factory{version: V50}
	p := f.New(plugin.Deps{Logger: slog.Default()})
	plug, ok := p.(*Plugin)
	if !ok {
		t.Fatalf("New returned %T, want *Plugin", p)
	}
	if plug.version != V50 {
		t.Errorf("version = %v, want V50", plug.version)
	}
	if m := f.Meta(); m.Name != "tsl-v50" || m.DefaultPort != 8901 {
		t.Errorf("Meta() = %+v", m)
	}
}

// --- Connect / Disconnect / Bound* / Subscribe error paths -----------------

func TestPlugin_Connect_V40_DispatchAndDisconnect(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	p := NewPluginV40(slog.Default())
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

	ch := make(chan FrameV40Event, 1)
	if err := p.SubscribeV40(func(ev FrameV40Event) { ch <- ev }); err != nil {
		t.Fatalf("SubscribeV40: %v", err)
	}

	f := codec.V40Frame{
		V31:          codec.V31Frame{Address: 7, Tally1: true, Text: "PVW"},
		DisplayLeft:  codec.XByte{LH: codec.TallyGreen},
		DisplayRight: codec.XByte{RH: codec.TallyAmber},
	}
	wire, err := f.Encode()
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	dialAndSend(t, addr, wire)

	ev := recvV40(t, ch)
	if ev.Frame.V31.Address != 7 || !ev.Frame.V31.Tally1 {
		t.Errorf("v3.1 part wrong: %+v", ev.Frame.V31)
	}
	if ev.Frame.DisplayLeft.LH != codec.TallyGreen {
		t.Errorf("DisplayLeft.LH = %v, want green", ev.Frame.DisplayLeft.LH)
	}
	if ev.Frame.DisplayRight.RH != codec.TallyAmber {
		t.Errorf("DisplayRight.RH = %v, want amber", ev.Frame.DisplayRight.RH)
	}

	// Connecting twice must fail.
	if err := p.Connect(ctx, "127.0.0.1", 0); err == nil {
		t.Error("second Connect should fail with already-connected")
	}
}

func TestPlugin_Connect_V50UDP_Dispatch(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	p := NewPluginV50(slog.Default())
	if err := p.Connect(ctx, "127.0.0.1", 0); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	defer func() { _ = p.Disconnect() }()

	ch := make(chan FrameV50Event, 1)
	if err := p.SubscribeV50(func(ev FrameV50Event) { ch <- ev }); err != nil {
		t.Fatalf("SubscribeV50: %v", err)
	}

	pkt := codec.V50Packet{
		Screen: 0,
		DMSGs: []codec.DMSG{
			{Index: 2, LH: codec.TallyRed, Brightness: codec.BrightnessFull, Text: "PGM"},
		},
	}
	wire, err := pkt.Encode()
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	dialAndSend(t, p.BoundAddr(), wire)

	ev := recvV50(t, ch)
	if len(ev.Frame.DMSGs) != 1 || ev.Frame.DMSGs[0].Text != "PGM" {
		t.Fatalf("v5.0 DMSG wrong: %+v", ev.Frame)
	}
	if ev.Frame.DMSGs[0].LH != codec.TallyRed {
		t.Errorf("LH = %v, want red", ev.Frame.DMSGs[0].LH)
	}
	if _, ok := ev.Remote.(*net.UDPAddr); !ok {
		t.Errorf("Remote = %T, want *net.UDPAddr", ev.Remote)
	}
}

func TestPlugin_Connect_UnknownVersion(t *testing.T) {
	p := &Plugin{version: Version(99), logger: slog.Default()}
	err := p.Connect(context.Background(), "127.0.0.1", 0)
	if err == nil {
		t.Fatal("expected error for unknown version")
	}
	_ = p.Disconnect()
}

func TestPlugin_Connect_ListenError(t *testing.T) {
	p := NewPluginV31(slog.Default())
	// Invalid address (bad port) forces lc.ListenPacket to fail.
	err := p.Connect(context.Background(), "127.0.0.1", -1)
	if err == nil {
		t.Fatal("expected listen error for invalid port")
	}
}

func TestPlugin_Disconnect_NoSession(t *testing.T) {
	p := NewPluginV31(slog.Default())
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

func TestPlugin_Subscribe_NotConnected(t *testing.T) {
	if err := NewPluginV31(slog.Default()).SubscribeV31(func(FrameV31Event) {}); err == nil {
		t.Error("SubscribeV31 not connected should fail")
	}
	if err := NewPluginV40(slog.Default()).SubscribeV40(func(FrameV40Event) {}); err == nil {
		t.Error("SubscribeV40 not connected should fail")
	}
	if err := NewPluginV50(slog.Default()).SubscribeV50(func(FrameV50Event) {}); err == nil {
		t.Error("SubscribeV50 not connected (no session) should fail")
	}
}

func TestPlugin_Subscribe_WrongVersion(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// v40 plugin: SubscribeV31 must reject.
	p40 := NewPluginV40(slog.Default())
	if err := p40.Connect(ctx, "127.0.0.1", 0); err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer func() { _ = p40.Disconnect() }()
	if err := p40.SubscribeV31(func(FrameV31Event) {}); err == nil {
		t.Error("SubscribeV31 on v40 plugin should fail")
	}

	// v31 plugin: SubscribeV40 must reject.
	p31 := NewPluginV31(slog.Default())
	if err := p31.Connect(ctx, "127.0.0.1", 0); err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer func() { _ = p31.Disconnect() }()
	if err := p31.SubscribeV40(func(FrameV40Event) {}); err == nil {
		t.Error("SubscribeV40 on v31 plugin should fail")
	}

	// non-v50 plugin: SubscribeV50 must reject (version guard, before session check).
	if err := p31.SubscribeV50(func(FrameV50Event) {}); err == nil {
		t.Error("SubscribeV50 on v31 plugin should fail")
	}
}

func TestPlugin_SubscribeV31_Success(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	p := NewPluginV31(slog.Default())
	if err := p.Connect(ctx, "127.0.0.1", 0); err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer func() { _ = p.Disconnect() }()

	ch := make(chan FrameV31Event, 1)
	if err := p.SubscribeV31(func(ev FrameV31Event) { ch <- ev }); err != nil {
		t.Fatalf("SubscribeV31 success path: %v", err)
	}
	wire, err := (codec.V31Frame{Address: 9, Tally2: true, Text: "X"}).Encode()
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	dialAndSend(t, p.BoundAddr(), wire)
	ev := recvV31(t, ch)
	if ev.Frame.Address != 9 || !ev.Frame.Tally2 {
		t.Errorf("frame = %+v", ev.Frame)
	}
}

func TestTCPSession_BoundAddrNil(t *testing.T) {
	if newTCPSession().boundAddr() != nil {
		t.Error("boundAddr on fresh tcpSession should be nil")
	}
}

// TestTCPSession_AcceptLoop_CtxCancelledAtTop drives acceptLoop with an
// already-cancelled context so the loop-top ctx.Err() guard returns
// before the first Accept. listener is real so the deferred wg.Done
// pairs with the manual wg.Add(1).
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
	s.acceptLoop(ctx) // returns immediately via the top-of-loop guard
	s.wg.Wait()
}

// TestTCPSession_AcceptLoop_ListenerClosed drives the ErrClosed return
// arm: the listener is closed while ctx is still live, so Accept returns
// net.ErrClosed and the loop exits via the errors.Is(net.ErrClosed) arm
// (not the ctx arm).
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

// transientErrListener is an in-memory net.Listener stub: its first
// Accept returns a transient (retryable, non-ErrClosed) error so
// acceptLoop's `continue` retry arm is exercised, then it returns
// net.ErrClosed so the loop exits cleanly.
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
		return nil, tempNetErr{} // transient -> continue (line 73)
	}
	return nil, net.ErrClosed // exit
}
func (l *transientErrListener) Close() error   { return nil }
func (l *transientErrListener) Addr() net.Addr { return &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1)} }

// TestTCPSession_AcceptLoop_TransientErrorContinues drives acceptLoop's
// transient-error `continue` retry arm using an in-memory listener stub
// whose first Accept returns a transient error (not ctx-cancel, not
// net.ErrClosed) and whose second returns net.ErrClosed to terminate.
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
	s := newTCPSession()
	c1, c2 := net.Pipe()
	defer func() { _ = c2.Close() }()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	s.wg.Add(1)
	s.connLoop(ctx, c1) // returns via top guard; also closes c1
	s.wg.Wait()
}

func TestPlugin_Unsubscribe_NoOp(t *testing.T) {
	if err := NewPluginV31(slog.Default()).Unsubscribe(consumer.ValueRequest{}); err != nil {
		t.Errorf("Unsubscribe should be a no-op nil, got %v", err)
	}
}

// --- v5.0 TCP path ---------------------------------------------------------

func TestPlugin_ConnectV50TCP_Dispatch(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	p := NewPluginV50(slog.Default())
	if err := p.ConnectV50TCP(ctx, "127.0.0.1", 0); err != nil {
		t.Fatalf("ConnectV50TCP: %v", err)
	}
	defer func() { _ = p.Disconnect() }()

	tcpAddr := p.BoundTCPAddr()
	if tcpAddr == nil {
		t.Fatal("BoundTCPAddr nil after ConnectV50TCP")
	}
	if p.BoundAddr() != nil {
		t.Error("BoundAddr should be nil in TCP mode")
	}

	ch := make(chan FrameV50Event, 4)
	if err := p.SubscribeV50(func(ev FrameV50Event) { ch <- ev }); err != nil {
		t.Fatalf("SubscribeV50 (tcp): %v", err)
	}

	conn, err := net.Dial("tcp", tcpAddr.String())
	if err != nil {
		t.Fatalf("dial tcp: %v", err)
	}
	defer func() { _ = conn.Close() }()

	pkt := codec.V50Packet{DMSGs: []codec.DMSG{{Index: 3, RH: codec.TallyAmber, Text: "ISO"}}}
	wire, err := pkt.Encode()
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	framed := codec.EncodeDLEFrame(wire)
	if _, err := conn.Write(framed); err != nil {
		t.Fatalf("write framed: %v", err)
	}

	ev := recvV50(t, ch)
	if len(ev.Frame.DMSGs) != 1 || ev.Frame.DMSGs[0].Text != "ISO" {
		t.Fatalf("tcp v5.0 DMSG wrong: %+v", ev.Frame)
	}
	if _, ok := ev.Remote.(*net.TCPAddr); !ok {
		t.Errorf("Remote = %T, want *net.TCPAddr", ev.Remote)
	}
}

func TestPlugin_ConnectV50TCP_DecodeError(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	p := NewPluginV50(slog.Default())
	if err := p.ConnectV50TCP(ctx, "127.0.0.1", 0); err != nil {
		t.Fatalf("ConnectV50TCP: %v", err)
	}
	defer func() { _ = p.Disconnect() }()

	ch := make(chan FrameV50Event, 4)
	if err := p.SubscribeV50(func(ev FrameV50Event) { ch <- ev }); err != nil {
		t.Fatalf("SubscribeV50: %v", err)
	}

	conn, err := net.Dial("tcp", p.BoundTCPAddr().String())
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer func() { _ = conn.Close() }()

	// A well-framed DLE packet (PBC matches body, so ReadFrame succeeds)
	// whose inner v5.0 body carries a truncated DMSG header -> DecodeV50
	// returns ErrV50DMSGTruncated (decode-error branch in connLoop).
	// body = VER(0) FLAGS(0) SCREEN(0,0) + 2 partial DMSG bytes = 6 bytes,
	// PBC = 6.
	inner := []byte{0x06, 0x00, 0x00, 0x00, 0x00, 0x00, 0xAA, 0xBB}
	if _, err := conn.Write(codec.EncodeDLEFrame(inner)); err != nil {
		t.Fatalf("write: %v", err)
	}

	ev := recvV50(t, ch)
	if len(ev.Frame.Notes) == 0 || ev.Frame.Notes[0].Kind != "tsl_decode_error" {
		t.Fatalf("expected tsl_decode_error note, got %+v", ev.Frame.Notes)
	}
}

func TestPlugin_ConnectV50TCP_FramingError(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	p := NewPluginV50(slog.Default())
	if err := p.ConnectV50TCP(ctx, "127.0.0.1", 0); err != nil {
		t.Fatalf("ConnectV50TCP: %v", err)
	}
	defer func() { _ = p.Disconnect() }()

	ch := make(chan FrameV50Event, 4)
	if err := p.SubscribeV50(func(ev FrameV50Event) { ch <- ev }); err != nil {
		t.Fatalf("SubscribeV50: %v", err)
	}

	conn, err := net.Dial("tcp", p.BoundTCPAddr().String())
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer func() { _ = conn.Close() }()

	// First byte not DLE -> ErrDLEMissingStart (framing-error branch).
	if _, err := conn.Write([]byte{0x00, 0x01, 0x02, 0x03}); err != nil {
		t.Fatalf("write: %v", err)
	}

	ev := recvV50(t, ch)
	if len(ev.Frame.Notes) == 0 || ev.Frame.Notes[0].Kind != "tsl_v5_tcp_framing_error" {
		t.Fatalf("expected tsl_v5_tcp_framing_error note, got %+v", ev.Frame.Notes)
	}
}

func TestPlugin_ConnectV50TCP_WrongVersion(t *testing.T) {
	p := NewPluginV31(slog.Default())
	if err := p.ConnectV50TCP(context.Background(), "127.0.0.1", 0); err == nil {
		t.Fatal("ConnectV50TCP on v31 plugin should fail")
	}
}

func TestPlugin_ConnectV50TCP_AlreadyConnected(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	p := NewPluginV50(slog.Default())
	if err := p.Connect(ctx, "127.0.0.1", 0); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	defer func() { _ = p.Disconnect() }()
	if err := p.ConnectV50TCP(ctx, "127.0.0.1", 0); err == nil {
		t.Error("ConnectV50TCP after UDP Connect should fail (already connected)")
	}
}

func TestPlugin_ConnectV50TCP_ListenError(t *testing.T) {
	p := NewPluginV50(slog.Default())
	if err := p.ConnectV50TCP(context.Background(), "127.0.0.1", -1); err == nil {
		t.Fatal("expected listen error for invalid TCP port")
	}
}

// --- UDP decode error / note branches --------------------------------------

func TestDecodeV31Payload_DecodeError(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	sess := newUDPSession()
	if err := sess.listen(ctx, "127.0.0.1:0", decodeV31Payload); err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer func() { _ = sess.close() }()

	ch := make(chan FrameV31Event, 1)
	sess.subscribeV31(func(ev FrameV31Event) { ch <- ev })

	// 18 bytes but HEADER bit 7 clear -> DecodeV31 returns ErrV31HeaderMSB.
	bad := make([]byte, codec.V31FrameSize) // header byte 0x00
	dialAndSend(t, sess.boundAddr(), bad)

	ev := recvV31(t, ch)
	if len(ev.Frame.Notes) == 0 || ev.Frame.Notes[0].Kind != "tsl_decode_error" {
		t.Fatalf("expected tsl_decode_error, got %+v", ev.Frame.Notes)
	}
}

func TestDecodeV40Payload_OkAndError(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	sess := newUDPSession()
	if err := sess.listen(ctx, "127.0.0.1:0", decodeV40Payload); err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer func() { _ = sess.close() }()

	ch := make(chan FrameV40Event, 2)
	sess.subscribeV40(func(ev FrameV40Event) { ch <- ev })

	// Error: too short (< 20 bytes) -> ErrV40FrameSize.
	dialAndSend(t, sess.boundAddr(), make([]byte, 5))
	ev := recvV40(t, ch)
	if len(ev.Frame.Notes) == 0 || ev.Frame.Notes[0].Kind != "tsl_decode_error" {
		t.Fatalf("expected tsl_decode_error, got %+v", ev.Frame.Notes)
	}

	// OK frame.
	f := codec.V40Frame{V31: codec.V31Frame{Address: 1, Text: "OK"}}
	wire, err := f.Encode()
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	dialAndSend(t, sess.boundAddr(), wire)
	ev = recvV40(t, ch)
	if ev.Frame.V31.Address != 1 {
		t.Errorf("address = %d, want 1", ev.Frame.V31.Address)
	}
}

func TestDecodeV50Payload_Error(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	sess := newUDPSession()
	if err := sess.listen(ctx, "127.0.0.1:0", decodeV50Payload); err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer func() { _ = sess.close() }()

	ch := make(chan FrameV50Event, 1)
	sess.subscribeV50(func(ev FrameV50Event) { ch <- ev })

	// Too small (< 6 bytes) -> ErrV50PacketTooSmall.
	dialAndSend(t, sess.boundAddr(), []byte{0x00, 0x01})
	ev := recvV50(t, ch)
	if len(ev.Frame.Notes) == 0 || ev.Frame.Notes[0].Kind != "tsl_decode_error" {
		t.Fatalf("expected tsl_decode_error, got %+v", ev.Frame.Notes)
	}
}

func TestUDPSession_ListenError(t *testing.T) {
	sess := newUDPSession()
	if err := sess.listen(context.Background(), "127.0.0.1:-1", decodeV31Payload); err == nil {
		t.Fatal("expected listen error for invalid port")
	}
	if sess.boundAddr() != nil {
		t.Error("boundAddr should be nil after failed listen")
	}
}

// TestUDPSession_ReadLoop_TransientError exercises the readLoop transient
// error continue path: writing a zero-length datagram followed by a real
// one keeps the loop alive and the real frame is delivered.
func TestUDPSession_ReadLoop_SurvivesEmptyDatagram(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	sess := newUDPSession()
	if err := sess.listen(ctx, "127.0.0.1:0", decodeV31Payload); err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer func() { _ = sess.close() }()

	ch := make(chan FrameV31Event, 2)
	sess.subscribeV31(func(ev FrameV31Event) { ch <- ev })

	// A zero-length UDP datagram is a valid read (n==0) routed as a size
	// mismatch note; then a real frame proves the loop kept running.
	dialAndSend(t, sess.boundAddr(), []byte{})
	f := codec.V31Frame{Address: 5, Text: "AB"}
	wire, err := f.Encode()
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	dialAndSend(t, sess.boundAddr(), wire)

	// Drain up to two events; ensure the real frame (addr 5) arrives.
	deadline := time.After(2 * time.Second)
	gotReal := false
	for !gotReal {
		select {
		case ev := <-ch:
			if ev.Frame.Address == 5 {
				gotReal = true
			}
		case <-deadline:
			t.Fatal("real frame not received after empty datagram")
		}
	}
}

// TestUDPSession_ReadLoop_TransientErrorContinues drives readLoop's
// transient-read-error continue arm (ctx still live). A short read
// deadline makes the first ReadFromUDP return an i/o timeout (a transient
// error, not ctx cancellation); the loop must `continue`. A frame written
// after the deadline window proves the loop kept running.
func TestUDPSession_ReadLoop_TransientErrorContinues(t *testing.T) {
	pc, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	s := &udpSession{conn: pc}
	defer func() { _ = pc.Close() }()

	ch := make(chan FrameV31Event, 1)
	s.subscribeV31(func(ev FrameV31Event) { ch <- ev })

	// Read deadline in the near future: the first blocking read times out
	// (transient error -> line 124 continue), then we clear the deadline
	// and the loop reads the real frame.
	if err := pc.SetReadDeadline(time.Now().Add(150 * time.Millisecond)); err != nil {
		t.Fatalf("set deadline: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go s.readLoop(ctx, decodeV31Payload)

	// Wait past the deadline so at least one read times out + continues,
	// then clear it and send a real frame.
	addr := pc.LocalAddr().(*net.UDPAddr)
	go func() {
		time.Sleep(250 * time.Millisecond)
		_ = pc.SetReadDeadline(time.Time{}) // clear; future reads block
		wire, _ := (codec.V31Frame{Address: 11, Text: "T"}).Encode()
		sock, derr := net.DialUDP("udp", nil, addr)
		if derr != nil {
			return
		}
		defer func() { _ = sock.Close() }()
		_, _ = sock.Write(wire)
	}()

	select {
	case ev := <-ch:
		if ev.Frame.Address != 11 {
			t.Errorf("address = %d, want 11", ev.Frame.Address)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("frame not received after transient read error")
	}
}

// TestPlugin_Disconnect_TCPCloseError drives Disconnect's TCP-close-error
// arm (e != nil && err == nil): no UDP session, a tcpSession whose
// listener is already closed so its close() returns a non-nil error.
func TestPlugin_Disconnect_TCPCloseError(t *testing.T) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	_ = l.Close() // pre-close so tcpSession.close() -> l.Close() errors

	p := NewPluginV50(slog.Default())
	p.tcpSession = &tcpSession{listener: l}

	if derr := p.Disconnect(); derr == nil {
		t.Fatal("expected Disconnect to surface the TCP close error")
	}
	if p.tcpSession != nil {
		t.Error("tcpSession should be cleared after Disconnect")
	}
}

// --- Canonicalize ----------------------------------------------------------

func TestCanonicalize_OK(t *testing.T) {
	p := NewPluginV31(slog.Default())
	exp, err := p.Canonicalize(context.Background())
	if err != nil {
		t.Fatalf("Canonicalize: %v", err)
	}
	root, ok := exp.Root.(*canonical.Node)
	if !ok {
		t.Fatalf("Root = %T, want *canonical.Node", exp.Root)
	}
	if root.Identifier != "tsl-v31" || root.OID != "1" {
		t.Errorf("root header = %+v", root.Header)
	}
	if len(root.Children) != 1 {
		t.Fatalf("root children = %d, want 1", len(root.Children))
	}
	disp, ok := root.Children[0].(*canonical.Node)
	if !ok {
		t.Fatalf("child = %T, want *canonical.Node", root.Children[0])
	}
	if disp.Identifier != "displays" || disp.OID != "1.1" {
		t.Errorf("displays header = %+v", disp.Header)
	}
	if len(disp.Children) != 0 {
		t.Errorf("displays children = %d, want 0", len(disp.Children))
	}
}

func TestCanonicalize_CtxCanceled(t *testing.T) {
	p := NewPluginV50(slog.Default())
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := p.Canonicalize(ctx); err == nil {
		t.Fatal("expected error on cancelled context")
	}
}

// --- EventForNote ----------------------------------------------------------

func TestEventForNote(t *testing.T) {
	mapped := []string{
		ReservedBitSet, VersionMismatch, ChecksumFail, ControlDataUndefined,
		UnknownDisplayIndex, BroadcastReceived, CharsetTranscode,
		LabelLengthMismatch, V31NullPad, V5TcpUnwrapped,
	}
	for _, k := range mapped {
		if got := EventForNote(codec.ComplianceNote{Kind: k}); got != k {
			t.Errorf("EventForNote(%q) = %q, want %q", k, got, k)
		}
	}
	if got := EventForNote(codec.ComplianceNote{Kind: "tsl_label_charset"}); got != "" {
		t.Errorf("EventForNote(unmapped) = %q, want empty", got)
	}
}

// --- Validate / decodeOne / helpers ----------------------------------------

func trame(dir wiretrace.Direction, b []byte, note string) wiretrace.Trame {
	return wiretrace.Trame{
		SchemaVersion: 1,
		Direction:     dir,
		Hex:           hex.EncodeToString(b),
		Note:          note,
	}
}

func TestValidate_V31_AllPaths(t *testing.T) {
	p := NewPluginV31(slog.Default())

	good, err := (codec.V31Frame{Address: 1, Text: "PGM"}).Encode()
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	// Frame with CTRL reserved bit set -> a compliance note (invariant).
	noteFrame := make([]byte, len(good))
	copy(noteFrame, good)
	noteFrame[codec.V31CtrlIdx] |= codec.V31CtrlBitRsvd

	trames := []wiretrace.Trame{
		trame(wiretrace.DirectionRx, good, ""),
		trame(wiretrace.DirectionTx, noteFrame, ""),
		// bad hex string -> hex decode error.
		{SchemaVersion: 1, Direction: wiretrace.DirectionRx, Hex: "zzzz"},
		// 5 bytes -> decode error (wrong size).
		trame(wiretrace.DirectionRx, []byte{1, 2, 3, 4, 5}, ""),
	}

	rep, err := p.Validate(context.Background(), trames, consumer.ValidateOpts{})
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if rep.TramesProcessed != 2 {
		t.Errorf("TramesProcessed = %d, want 2", rep.TramesProcessed)
	}
	if len(rep.Errors) != 2 {
		t.Errorf("Errors = %d, want 2 (hex + decode)", len(rep.Errors))
	}
	if len(rep.Invariants) == 0 {
		t.Error("expected at least one invariant from reserved-bit frame")
	}
	if rep.PerDirection[wiretrace.DirectionRx] != 1 || rep.PerDirection[wiretrace.DirectionTx] != 1 {
		t.Errorf("PerDirection wrong: %+v", rep.PerDirection)
	}
}

func TestValidate_StopAt(t *testing.T) {
	p := NewPluginV31(slog.Default())
	good, err := (codec.V31Frame{Address: 1, Text: "A"}).Encode()
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	trames := []wiretrace.Trame{
		trame(wiretrace.DirectionRx, good, "first"),
		trame(wiretrace.DirectionRx, good, "STOPHERE"),
		trame(wiretrace.DirectionRx, good, "after"),
	}
	rep, err := p.Validate(context.Background(), trames, consumer.ValidateOpts{StopAt: "STOPHERE"})
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if rep.StoppedAt != "STOPHERE" {
		t.Errorf("StoppedAt = %q, want STOPHERE", rep.StoppedAt)
	}
	if rep.TramesProcessed != 2 {
		t.Errorf("TramesProcessed = %d, want 2 (stopped at 2nd)", rep.TramesProcessed)
	}
}

func TestValidate_OutTreeRejected(t *testing.T) {
	p := NewPluginV31(slog.Default())
	if _, err := p.Validate(context.Background(), nil, consumer.ValidateOpts{OutTree: "x.json"}); err == nil {
		t.Error("expected error when OutTree set")
	}
	if _, err := p.Validate(context.Background(), nil, consumer.ValidateOpts{OutParams: "x.csv"}); err == nil {
		t.Error("expected error when OutParams set")
	}
}

func TestValidate_CtxCanceled(t *testing.T) {
	p := NewPluginV31(slog.Default())
	good, err := (codec.V31Frame{Address: 1, Text: "A"}).Encode()
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := p.Validate(ctx, []wiretrace.Trame{trame(wiretrace.DirectionRx, good, "")}, consumer.ValidateOpts{}); err == nil {
		t.Fatal("expected ctx error")
	}
}

func TestValidate_V40_And_V50_And_DLE(t *testing.T) {
	// v4.0
	p40 := NewPluginV40(slog.Default())
	w40, err := (codec.V40Frame{V31: codec.V31Frame{Address: 2, Text: "V4"}}).Encode()
	if err != nil {
		t.Fatalf("v40 encode: %v", err)
	}
	rep, err := p40.Validate(context.Background(),
		[]wiretrace.Trame{trame(wiretrace.DirectionRx, w40, "")}, consumer.ValidateOpts{})
	if err != nil || rep.TramesProcessed != 1 {
		t.Fatalf("v40 validate: err=%v processed=%d", err, rep.TramesProcessed)
	}

	// v5.0 raw UDP-style
	p50 := NewPluginV50(slog.Default())
	w50, err := (codec.V50Packet{DMSGs: []codec.DMSG{{Index: 1, Text: "V5"}}}).Encode()
	if err != nil {
		t.Fatalf("v50 encode: %v", err)
	}
	rep, err = p50.Validate(context.Background(),
		[]wiretrace.Trame{trame(wiretrace.DirectionRx, w50, "")}, consumer.ValidateOpts{})
	if err != nil || rep.TramesProcessed != 1 {
		t.Fatalf("v50 validate: err=%v processed=%d", err, rep.TramesProcessed)
	}

	// v5.0 DLE/STX-wrapped (TCP capture) — exercises the unwrap branch.
	framed := codec.EncodeDLEFrame(w50)
	rep, err = p50.Validate(context.Background(),
		[]wiretrace.Trame{trame(wiretrace.DirectionRx, framed, "")}, consumer.ValidateOpts{})
	if err != nil || rep.TramesProcessed != 1 {
		t.Fatalf("v50 DLE validate: err=%v processed=%d", err, rep.TramesProcessed)
	}
}

func TestValidate_V50_DLE_BadInner(t *testing.T) {
	p50 := NewPluginV50(slog.Default())
	// DLE/STX prefix but truncated body -> ReadFrame error in decodeOne.
	bad := []byte{0xFE, 0x02, 0xFF} // PBC byte starts but no more
	rep, err := p50.Validate(context.Background(),
		[]wiretrace.Trame{trame(wiretrace.DirectionRx, bad, "")}, consumer.ValidateOpts{})
	if err != nil {
		t.Fatalf("Validate returned error: %v", err)
	}
	if len(rep.Errors) != 1 {
		t.Fatalf("expected 1 decode error, got %d", len(rep.Errors))
	}
	if rep.Errors[0].HexPrefix == "" {
		t.Error("expected HexPrefix on decode error")
	}
}

func TestValidate_V50_DLE_BadDecode(t *testing.T) {
	p50 := NewPluginV50(slog.Default())
	// Valid DLE frame (PBC matches body), but the inner v5.0 packet has a
	// truncated DMSG -> DecodeV50 error.
	inner := []byte{0x06, 0x00, 0x00, 0x00, 0x00, 0x00, 0xAA, 0xBB}
	framed := codec.EncodeDLEFrame(inner)
	rep, err := p50.Validate(context.Background(),
		[]wiretrace.Trame{trame(wiretrace.DirectionRx, framed, "")}, consumer.ValidateOpts{})
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if len(rep.Errors) != 1 {
		t.Fatalf("expected 1 decode error, got %d (%+v)", len(rep.Errors), rep.Errors)
	}
}

func TestDecodeOne_UnknownVersion(t *testing.T) {
	p := &Plugin{version: Version(99), logger: slog.Default()}
	if _, err := p.decodeOne([]byte{1, 2, 3}); err == nil {
		t.Fatal("expected error for unknown plugin version")
	}
}

func TestDecodeOne_VersionDecodeErrors(t *testing.T) {
	// v31 decode error (wrong size).
	if _, err := NewPluginV31(slog.Default()).decodeOne([]byte{1, 2}); err == nil {
		t.Error("v31 decodeOne should error on short input")
	}
	// v40 decode error (too short).
	if _, err := NewPluginV40(slog.Default()).decodeOne([]byte{1, 2}); err == nil {
		t.Error("v40 decodeOne should error on short input")
	}
	// v50 raw decode error (too small).
	if _, err := NewPluginV50(slog.Default()).decodeOne([]byte{1, 2}); err == nil {
		t.Error("v50 decodeOne should error on short input")
	}
}

func TestFormatNoteDetail(t *testing.T) {
	if got := formatNoteDetail(""); got != "" {
		t.Errorf("empty detail = %q, want empty", got)
	}
	if got := formatNoteDetail("x"); got != " (x)" {
		t.Errorf("detail = %q, want ' (x)'", got)
	}
}

func TestShortHex(t *testing.T) {
	long := make([]byte, 32)
	for i := range long {
		long[i] = byte(i)
	}
	got := shortHex(long)
	if len(got) != 32 { // 16 bytes -> 32 hex chars
		t.Errorf("shortHex long len = %d, want 32", len(got))
	}
	short := []byte{0xab, 0xcd}
	if shortHex(short) != "abcd" {
		t.Errorf("shortHex short = %q, want abcd", shortHex(short))
	}
}
