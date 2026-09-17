package osc

import (
	"context"
	"dhs/internal/plugin"
	"errors"
	"io"
	"log/slog"
	"net"
	"strings"
	"testing"
	"time"

	"dhs/internal/export/canonical"
	"dhs/internal/osc/codec"
	"dhs/internal/provider"
)

// discardLogger returns a logger that drops all output — tests assert on
// behaviour, not log lines.
func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// --- Version metadata (name / defaultPort / description fall-through) -------

func TestVersion_Metadata(t *testing.T) {
	cases := []struct {
		v       Version
		name    string
		port    int
		descSub string
	}{
		{V10, "osc-v10", 8000, "1.0"},
		{V11, "osc-v11", 8000, "1.1"},
	}
	for _, c := range cases {
		if got := c.v.name(); got != c.name {
			t.Errorf("name(%d)=%q want %q", c.v, got, c.name)
		}
		if got := c.v.defaultPort(); got != c.port {
			t.Errorf("defaultPort(%d)=%d want %d", c.v, got, c.port)
		}
		if got := c.v.description(); !strings.Contains(got, c.descSub) {
			t.Errorf("description(%d)=%q want substr %q", c.v, got, c.descSub)
		}
	}
}

// TestVersion_UnknownFallthrough drives the trailing fall-through returns in
// name/description (the default arms for an out-of-range Version).
func TestVersion_UnknownFallthrough(t *testing.T) {
	unknown := Version(99)
	if got := unknown.name(); got != "osc-unknown" {
		t.Errorf("name(unknown)=%q want osc-unknown", got)
	}
	// defaultPort is version-independent (always 8000) — assert it too.
	if got := unknown.defaultPort(); got != 8000 {
		t.Errorf("defaultPort(unknown)=%d want 8000", got)
	}
	if got := unknown.description(); got != "" {
		t.Errorf("description(unknown)=%q want empty", got)
	}
}

// TestVersion_FramerForVersion covers both framing arms (and the default).
func TestVersion_FramerForVersion(t *testing.T) {
	if k := (&Server{version: V11}).framerForVersion(); k != framerSLIP {
		t.Errorf("V11 framer=%d want framerSLIP", k)
	}
	if k := (&Server{version: V10}).framerForVersion(); k != framerLenPrefix {
		t.Errorf("V10 framer=%d want framerLenPrefix", k)
	}
	if k := (&Server{version: Version(99)}).framerForVersion(); k != framerLenPrefix {
		t.Errorf("unknown framer=%d want default framerLenPrefix", k)
	}
}

// --- Factory.Meta + Factory.New -------------------------------------------

func TestFactory_MetaAndNew(t *testing.T) {
	f := &Factory{version: V11}
	meta := f.Meta()
	if meta.Name != "osc-v11" || meta.DefaultPort != 8000 {
		t.Fatalf("meta=%+v", meta)
	}
	if !strings.Contains(meta.Description, "1.1") {
		t.Errorf("meta description=%q", meta.Description)
	}

	tree := &canonical.Export{}
	p := f.New(plugin.Deps{Logger: discardLogger()}, tree)
	srv, ok := p.(*Server)
	if !ok {
		t.Fatalf("New returned %T, want *Server", p)
	}
	if srv.version != V11 {
		t.Errorf("server version=%d want V11", srv.version)
	}
	if srv.tree != tree {
		t.Errorf("server tree not threaded through")
	}
	// Confirm the package registered factories satisfying the iface.
	var _ provider.Provider = srv
}

// --- Direct constructors --------------------------------------------------

func TestNewServerConstructors(t *testing.T) {
	lg := discardLogger()
	if s := NewServerV10(lg); s.version != V10 {
		t.Errorf("NewServerV10 version=%d", s.version)
	}
	if s := NewServerV11(lg); s.version != V11 {
		t.Errorf("NewServerV11 version=%d", s.version)
	}
}

// --- SetValue: not-implemented sentinel -----------------------------------

func TestServer_SetValue_NotImplemented(t *testing.T) {
	s := NewServerV10(discardLogger())
	got, err := s.SetValue(context.Background(), "/mixer/ch/1", 7)
	if got != nil {
		t.Errorf("SetValue value=%v want nil", got)
	}
	if !errors.Is(err, errNotImplemented) {
		t.Errorf("SetValue err=%v want errNotImplemented", err)
	}
}

// --- Bind / BoundAddr / AddDestination (lazy sender allocation) -----------

func TestServer_BindBoundAddrAddDest(t *testing.T) {
	s := NewServerV10(discardLogger())
	if got := s.BoundAddr(); got != nil {
		t.Errorf("BoundAddr before bind=%v want nil", got)
	}
	if err := s.Bind("127.0.0.1:0"); err != nil {
		t.Fatalf("Bind: %v", err)
	}
	defer func() { _ = s.Stop() }()
	addr := s.BoundAddr()
	if addr == nil || addr.Port == 0 {
		t.Fatalf("BoundAddr after bind=%v", addr)
	}
	if err := s.AddDestination("127.0.0.1", 8000); err != nil {
		t.Fatalf("AddDestination: %v", err)
	}
}

// TestServer_AddDestination_LazyAlloc drives the lazy ensureSender path in
// AddDestination (no prior Bind/Serve).
func TestServer_AddDestination_LazyAlloc(t *testing.T) {
	s := NewServerV10(discardLogger())
	if err := s.AddDestination("127.0.0.1", 8000); err != nil {
		t.Fatalf("AddDestination lazy: %v", err)
	}
	if s.senderRef() == nil {
		t.Fatalf("AddDestination did not allocate sender")
	}
}

func TestServer_AddDestination_ResolveError(t *testing.T) {
	s := NewServerV10(discardLogger())
	if err := s.AddDestination("no such host at all", -1); err == nil {
		t.Fatalf("want resolve error for bad dest")
	}
}

// --- Serve: binds + blocks until ctx cancel -------------------------------

func TestServer_Serve_BlocksUntilCancel(t *testing.T) {
	s := NewServerV10(discardLogger())
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- s.Serve(ctx, "127.0.0.1:0") }()

	// Wait for the bind to land before asserting BoundAddr.
	deadline := time.Now().Add(2 * time.Second)
	for s.BoundAddr() == nil {
		if time.Now().After(deadline) {
			t.Fatalf("Serve never bound")
		}
		time.Sleep(time.Millisecond)
	}
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Serve returned %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("Serve did not return after cancel")
	}
	if err := s.Stop(); err != nil {
		t.Fatalf("Stop: %v", err)
	}
}

// TestServer_Serve_ReusesBoundSender exercises serveBlock's "already bound"
// path (s.conn != nil) — Bind first, then Serve must not rebind.
func TestServer_Serve_ReusesBoundSender(t *testing.T) {
	s := NewServerV11(discardLogger())
	if err := s.Bind("127.0.0.1:0"); err != nil {
		t.Fatalf("Bind: %v", err)
	}
	bound := s.BoundAddr()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- s.Serve(ctx, "127.0.0.1:0") }()
	// Serve must reuse the existing socket, never rebind. The bound port is
	// fixed once Bind succeeded, so any later read must equal it. Poll a few
	// times to give the Serve goroutine a chance to (incorrectly) rebind.
	for i := 0; i < 100; i++ {
		if got := s.BoundAddr(); got == nil || got.Port != bound.Port {
			t.Fatalf("Serve rebound or lost addr: got %v want port %d", got, bound.Port)
		}
	}
	cancel()
	if err := <-done; err != nil {
		t.Fatalf("Serve: %v", err)
	}
	if err := s.Stop(); err != nil {
		t.Fatalf("Stop: %v", err)
	}
}

func TestServer_Serve_BindError(t *testing.T) {
	s := NewServerV10(discardLogger())
	err := s.Serve(context.Background(), "256.256.256.256:99999")
	if err == nil {
		t.Fatalf("want bind error for invalid addr")
	}
}

// --- SendMessage / SendBundle: not-bound + happy path ---------------------

func TestSendMessage_NotBound(t *testing.T) {
	s := NewServerV10(discardLogger())
	if err := s.SendMessage(codec.Message{Address: "/x"}); err == nil {
		t.Fatalf("SendMessage unbound should error")
	}
}

func TestSendBundle_NotBound(t *testing.T) {
	s := NewServerV11(discardLogger())
	if err := s.SendBundle(codec.Bundle{Timetag: 1}); err == nil {
		t.Fatalf("SendBundle unbound should error")
	}
}

// udpRecv binds a UDP listener and returns it + its dest addr.
func udpRecv(t *testing.T) (*net.UDPConn, *net.UDPAddr) {
	t.Helper()
	l, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 0})
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	return l, l.LocalAddr().(*net.UDPAddr)
}

func TestSendMessage_RoundTrip(t *testing.T) {
	l, dest := udpRecv(t)
	defer func() { _ = l.Close() }()

	s := NewServerV10(discardLogger())
	if err := s.Bind("127.0.0.1:0"); err != nil {
		t.Fatalf("Bind: %v", err)
	}
	defer func() { _ = s.Stop() }()
	if err := s.AddDestination(dest.IP.String(), dest.Port); err != nil {
		t.Fatalf("AddDestination: %v", err)
	}

	m := codec.Message{Address: "/mixer/ch/1/fader", Args: []codec.Arg{codec.Float32(0.5), codec.Int32(7)}}
	if err := s.SendMessage(m); err != nil {
		t.Fatalf("SendMessage: %v", err)
	}

	buf := make([]byte, 2048)
	if err := l.SetReadDeadline(time.Now().Add(2 * time.Second)); err != nil {
		t.Fatalf("deadline: %v", err)
	}
	n, _, err := l.ReadFromUDP(buf)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	got, err := codec.DecodeMessage(buf[:n])
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.Address != "/mixer/ch/1/fader" || len(got.Args) != 2 ||
		got.Args[0].Float32 != 0.5 || got.Args[1].Int32 != 7 {
		t.Errorf("message round-trip wrong: %+v", got)
	}
}

func TestSendBundle_RoundTrip(t *testing.T) {
	l, dest := udpRecv(t)
	defer func() { _ = l.Close() }()

	s := NewServerV11(discardLogger())
	if err := s.Bind("127.0.0.1:0"); err != nil {
		t.Fatalf("Bind: %v", err)
	}
	defer func() { _ = s.Stop() }()
	if err := s.AddDestination(dest.IP.String(), dest.Port); err != nil {
		t.Fatalf("AddDestination: %v", err)
	}

	b := codec.Bundle{
		Timetag: 0x0000000000000001,
		Elements: []codec.Packet{
			codec.Message{Address: "/a", Args: []codec.Arg{codec.Int32(1)}},
			codec.Message{Address: "/b", Args: []codec.Arg{codec.True()}},
		},
	}
	if err := s.SendBundle(b); err != nil {
		t.Fatalf("SendBundle: %v", err)
	}

	buf := make([]byte, 2048)
	if err := l.SetReadDeadline(time.Now().Add(2 * time.Second)); err != nil {
		t.Fatalf("deadline: %v", err)
	}
	n, _, err := l.ReadFromUDP(buf)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	got, err := codec.DecodeBundle(buf[:n])
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.Timetag != 1 || len(got.Elements) != 2 {
		t.Errorf("bundle round-trip wrong: %+v", got)
	}
}

// --- encode-error fan-out (sendMessage / sendBundle error arms) -----------

func TestSendMessage_EncodeError(t *testing.T) {
	s := NewServerV10(discardLogger())
	if err := s.Bind("127.0.0.1:0"); err != nil {
		t.Fatalf("Bind: %v", err)
	}
	defer func() { _ = s.Stop() }()
	if err := s.AddDestination("127.0.0.1", 8000); err != nil {
		t.Fatalf("AddDestination: %v", err)
	}
	// Empty address makes Message.Encode fail before any network write.
	if err := s.SendMessage(codec.Message{Address: ""}); err == nil {
		t.Fatalf("want encode error for empty address")
	}
}

func TestSendBundle_EncodeError(t *testing.T) {
	s := NewServerV11(discardLogger())
	if err := s.Bind("127.0.0.1:0"); err != nil {
		t.Fatalf("Bind: %v", err)
	}
	defer func() { _ = s.Stop() }()
	if err := s.AddDestination("127.0.0.1", 8000); err != nil {
		t.Fatalf("AddDestination: %v", err)
	}
	// A bundle element with an empty address fails at element Encode.
	bad := codec.Bundle{Elements: []codec.Packet{codec.Message{Address: ""}}}
	if err := s.SendBundle(bad); err == nil {
		t.Fatalf("want encode error for bundle with bad element")
	}
}

// --- sendBytes write-error fan-out (closed conn) --------------------------

func TestSendBytes_WriteError(t *testing.T) {
	s := newUDPSender(nil)
	if err := s.bind("127.0.0.1:0"); err != nil {
		t.Fatalf("bind: %v", err)
	}
	if err := s.addDest("127.0.0.1", 9999); err != nil {
		t.Fatalf("addDest: %v", err)
	}
	// Close the socket out from under the send so WriteToUDP errors.
	if err := s.close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	if err := s.sendMessage(codec.Message{Address: "/x"}); err == nil {
		t.Fatalf("want write error on closed conn")
	}
}

// --- udpSender.bind: already-bound guard + empty-addr default + nil addr ---

func TestUDPSender_BindTwice(t *testing.T) {
	s := newUDPSender(nil)
	if err := s.bind("127.0.0.1:0"); err != nil {
		t.Fatalf("bind: %v", err)
	}
	defer func() { _ = s.close() }()
	if err := s.bind("127.0.0.1:0"); err == nil {
		t.Fatalf("second bind should error")
	}
}

// TestUDPSender_BoundAddr_Nil drives boundAddr's conn==nil arm on a
// freshly-constructed (unbound) sender.
func TestUDPSender_BoundAddr_Nil(t *testing.T) {
	s := newUDPSender(nil)
	if got := s.boundAddr(); got != nil {
		t.Fatalf("boundAddr on unbound sender=%v want nil", got)
	}
}

// TestUDPSender_Bind_EmptyAddrDefault drives the addr=="" -> ":0" default
// branch in bind.
func TestUDPSender_Bind_EmptyAddrDefault(t *testing.T) {
	s := newUDPSender(nil)
	if err := s.bind(""); err != nil {
		t.Fatalf("bind empty addr: %v", err)
	}
	defer func() { _ = s.close() }()
	if a := s.boundAddr(); a == nil || a.Port == 0 {
		t.Fatalf("bind empty addr did not resolve an ephemeral port: %v", a)
	}
}

func TestUDPSender_SendBytes_NotBound(t *testing.T) {
	s := newUDPSender(nil)
	if err := s.sendMessage(codec.Message{Address: "/x"}); err == nil {
		t.Fatalf("sendBytes unbound should error")
	}
}

// TestUDPSender_BindError drives bind's ListenPacket-error arm.
func TestUDPSender_BindError(t *testing.T) {
	s := newUDPSender(nil)
	if err := s.bind("256.256.256.256:99999"); err == nil {
		t.Fatalf("want bind error for invalid addr")
	}
}

// --- bind defensive branches via the transparent seam --------------------
//
// These branches cannot fire on a live UDP socket: the SO_REUSEADDR /
// SO_BROADCAST setsockopt calls and the RawConn.Control dispatch succeed on
// every supported OS, and ListenPacket(ctx,"udp",...) always yields a
// *net.UDPConn. The seam (bind_seam.go) is nil in production — these tests
// install a hook, prove bind propagates the defensive error, then restore
// nil. The guards in bind are untouched.

// --- Stop: aggregates sender + dialer errors; nil-safe ---------------------

func TestServer_Stop_NoResources(t *testing.T) {
	s := NewServerV10(discardLogger())
	if err := s.Stop(); err != nil {
		t.Fatalf("Stop with no resources: %v", err)
	}
}

func TestServer_Stop_ClosesSenderAndDialer(t *testing.T) {
	// Stand up a real TCP consumer so the dialer has a live conn to close.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer func() { _ = ln.Close() }()
	accepted := make(chan net.Conn, 1)
	go func() {
		c, aerr := ln.Accept()
		if aerr == nil {
			accepted <- c
		}
	}()
	host, port := splitHostPort(t, ln.Addr().String())

	s := NewServerV10(discardLogger())
	if err := s.Bind("127.0.0.1:0"); err != nil {
		t.Fatalf("Bind: %v", err)
	}
	if err := s.SendMessageTCP(host, port, codec.Message{Address: "/a", Args: []codec.Arg{codec.Int32(1)}}); err != nil {
		t.Fatalf("SendMessageTCP: %v", err)
	}
	srvConn := <-accepted
	defer func() { _ = srvConn.Close() }()

	if err := s.Stop(); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	// Second Stop must be safe (close-once on sender, empty dialer map).
	if err := s.Stop(); err != nil {
		t.Fatalf("second Stop: %v", err)
	}
}

// TestServer_Stop_SenderError drives Stop's sender-error arm: the sender's
// UDP conn is closed out-of-band (bypassing closeOnce) so the close-once
// body's conn.Close() inside Stop returns an error.
func TestServer_Stop_SenderError(t *testing.T) {
	s := NewServerV10(discardLogger())
	if err := s.Bind("127.0.0.1:0"); err != nil {
		t.Fatalf("Bind: %v", err)
	}
	// Close the underlying conn directly; closeOnce has not fired yet, so
	// Stop -> sender.close() -> conn.Close() will hit an already-closed
	// socket and return an error.
	sender := s.senderRef()
	if err := sender.conn.Close(); err != nil {
		t.Fatalf("pre-close conn: %v", err)
	}
	if err := s.Stop(); err == nil {
		t.Fatalf("Stop should surface sender close error")
	}
}

// TestServer_Stop_DialerError drives Stop's dialer-error aggregation arm:
// sender closes cleanly (first==nil) and the TCP dialer close() then returns
// the first error, so Stop surfaces it.
func TestServer_Stop_DialerError(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer func() { _ = ln.Close() }()
	host, port := splitHostPort(t, ln.Addr().String())

	accepted := make(chan net.Conn, 1)
	go func() {
		c, aerr := ln.Accept()
		if aerr == nil {
			accepted <- c
		}
	}()

	s := NewServerV11(discardLogger())
	if err := s.Bind("127.0.0.1:0"); err != nil {
		t.Fatalf("Bind: %v", err)
	}
	if err := s.SendMessageTCP(host, port, codec.Message{Address: "/a"}); err != nil {
		t.Fatalf("SendMessageTCP: %v", err)
	}
	srvConn := <-accepted
	defer func() { _ = srvConn.Close() }()

	// Pre-close the dialer's cached conn so tcpDialer.close() inside Stop
	// observes a close error while the sender closes cleanly.
	dialer := s.tcpDialerRef()
	key := destKey(host, port)
	dialer.mu.Lock()
	c := dialer.conns[key]
	dialer.mu.Unlock()
	if err := c.Close(); err != nil {
		t.Fatalf("pre-close cached conn: %v", err)
	}

	if err := s.Stop(); err == nil {
		t.Fatalf("Stop should surface dialer close error")
	}
}

// --- tcpDialer: full lifecycle (dial / reuse / send / write-error / close) -

func splitHostPort(t *testing.T, addr string) (string, int) {
	t.Helper()
	host, portStr, err := net.SplitHostPort(addr)
	if err != nil {
		t.Fatalf("split %q: %v", addr, err)
	}
	a, err := net.ResolveTCPAddr("tcp", net.JoinHostPort(host, portStr))
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	return host, a.Port
}

// readLenPrefixFrame reads one OSC 1.0 length-prefixed frame from c.
func readLenPrefixFrame(t *testing.T, c net.Conn) []byte {
	t.Helper()
	if err := c.SetReadDeadline(time.Now().Add(2 * time.Second)); err != nil {
		t.Fatalf("deadline: %v", err)
	}
	r := codec.NewLenPrefixReader(c, 1<<20)
	pkt, err := r.ReadPacket()
	if err != nil {
		t.Fatalf("len-prefix read: %v", err)
	}
	return pkt
}

// readSLIPFrame reads one OSC 1.1 SLIP frame from c.
func readSLIPFrame(t *testing.T, c net.Conn) []byte {
	t.Helper()
	if err := c.SetReadDeadline(time.Now().Add(2 * time.Second)); err != nil {
		t.Fatalf("deadline: %v", err)
	}
	r := codec.NewSLIPReader(c, 1<<20)
	pkt, err := r.ReadPacket()
	if err != nil {
		t.Fatalf("slip read: %v", err)
	}
	return pkt
}

// TestSendMessageTCP_V10_RoundTrip drives the length-prefix framer and decodes
// the emitted frame back.
func TestSendMessageTCP_V10_RoundTrip(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer func() { _ = ln.Close() }()
	host, port := splitHostPort(t, ln.Addr().String())

	type acc struct {
		c   net.Conn
		err error
	}
	accepted := make(chan acc, 1)
	go func() {
		c, aerr := ln.Accept()
		accepted <- acc{c, aerr}
	}()

	s := NewServerV10(discardLogger())
	defer func() { _ = s.Stop() }()

	m := codec.Message{Address: "/mixer/ch/2/mute", Args: []codec.Arg{codec.True()}}
	if err := s.SendMessageTCP(host, port, m); err != nil {
		t.Fatalf("SendMessageTCP #1: %v", err)
	}
	a := <-accepted
	if a.err != nil {
		t.Fatalf("accept: %v", a.err)
	}
	srvConn := a.c
	defer func() { _ = srvConn.Close() }()

	frame := readLenPrefixFrame(t, srvConn)
	got, err := codec.DecodeMessage(frame)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.Address != "/mixer/ch/2/mute" || len(got.Args) != 1 || got.Args[0].Tag != codec.TagTrue {
		t.Errorf("tcp v1.0 round-trip wrong: %+v", got)
	}

	// Second send must REUSE the cached conn (dial hits the map-hit branch).
	dialer := s.tcpDialerRef()
	if dialer == nil {
		t.Fatalf("dialer not allocated")
	}
	dialer.mu.Lock()
	before := len(dialer.conns)
	dialer.mu.Unlock()
	if err := s.SendMessageTCP(host, port, m); err != nil {
		t.Fatalf("SendMessageTCP #2: %v", err)
	}
	_ = readLenPrefixFrame(t, srvConn)
	dialer.mu.Lock()
	after := len(dialer.conns)
	dialer.mu.Unlock()
	if after != before {
		t.Errorf("second send changed conn count %d -> %d (no reuse)", before, after)
	}
}

// TestSendBundleTCP_V11_RoundTrip drives the SLIP framer + bundle encode path.
func TestSendBundleTCP_V11_RoundTrip(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer func() { _ = ln.Close() }()
	host, port := splitHostPort(t, ln.Addr().String())

	type acc struct {
		c   net.Conn
		err error
	}
	accepted := make(chan acc, 1)
	go func() {
		c, aerr := ln.Accept()
		accepted <- acc{c, aerr}
	}()

	s := NewServerV11(discardLogger())
	defer func() { _ = s.Stop() }()

	b := codec.Bundle{
		Timetag: 1,
		Elements: []codec.Packet{
			codec.Message{Address: "/tally/1", Args: []codec.Arg{codec.Int32(0xC0)}}, // 0xC0 = SLIP END, must be escaped
		},
	}
	if err := s.SendBundleTCP(host, port, b); err != nil {
		t.Fatalf("SendBundleTCP: %v", err)
	}
	a := <-accepted
	if a.err != nil {
		t.Fatalf("accept: %v", a.err)
	}
	srvConn := a.c
	defer func() { _ = srvConn.Close() }()

	frame := readSLIPFrame(t, srvConn)
	got, err := codec.DecodeBundle(frame)
	if err != nil {
		t.Fatalf("decode bundle: %v", err)
	}
	if got.Timetag != 1 || len(got.Elements) != 1 {
		t.Errorf("tcp v1.1 bundle round-trip wrong: %+v", got)
	}
	msg, ok := got.Elements[0].(codec.Message)
	if !ok || msg.Address != "/tally/1" || msg.Args[0].Int32 != 0xC0 {
		t.Errorf("bundle element wrong: %+v", got.Elements[0])
	}
}

func TestSendMessageTCP_EncodeError(t *testing.T) {
	s := NewServerV10(discardLogger())
	defer func() { _ = s.Stop() }()
	// Encode fails (empty address) before any dial — no listener needed.
	if err := s.SendMessageTCP("127.0.0.1", 1, codec.Message{Address: ""}); err == nil {
		t.Fatalf("want encode error for empty address over TCP")
	}
}

func TestSendBundleTCP_EncodeError(t *testing.T) {
	s := NewServerV11(discardLogger())
	defer func() { _ = s.Stop() }()
	bad := codec.Bundle{Elements: []codec.Packet{codec.Message{Address: ""}}}
	if err := s.SendBundleTCP("127.0.0.1", 1, bad); err == nil {
		t.Fatalf("want encode error for bundle with bad element over TCP")
	}
}

func TestSendMessageTCP_DialError(t *testing.T) {
	// Reserve a port then close it so the dial is refused.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	host, port := splitHostPort(t, ln.Addr().String())
	_ = ln.Close()

	s := NewServerV10(discardLogger())
	defer func() { _ = s.Stop() }()
	if err := s.SendMessageTCP(host, port, codec.Message{Address: "/a"}); err == nil {
		t.Fatalf("want dial error to closed port")
	}
}

// TestSendBundleTCP_LazyDialerAndDialError also covers SendBundleTCP's lazy
// ensureTCPDialer path with no prior bind.
func TestSendBundleTCP_DialError(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	host, port := splitHostPort(t, ln.Addr().String())
	_ = ln.Close()

	s := NewServerV11(discardLogger())
	defer func() { _ = s.Stop() }()
	if err := s.SendBundleTCP(host, port, codec.Bundle{Timetag: 1}); err == nil {
		t.Fatalf("want dial error to closed port for bundle")
	}
}

// TestTCPDialer_WriteError forces the write-failure arm: dial a live
// listener, then close the cached conn so the next Write errors and the
// dialer drops the dead conn from its map.
func TestTCPDialer_WriteError(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer func() { _ = ln.Close() }()
	host, port := splitHostPort(t, ln.Addr().String())

	accepted := make(chan net.Conn, 1)
	go func() {
		c, aerr := ln.Accept()
		if aerr == nil {
			accepted <- c
		}
	}()

	d := newTCPDialer(framerLenPrefix, nil)
	defer func() { _ = d.close() }()
	m := codec.Message{Address: "/a", Args: []codec.Arg{codec.Int32(1)}}
	if err := d.sendMessage(host, port, m); err != nil {
		t.Fatalf("first send: %v", err)
	}
	srvConn := <-accepted
	defer func() { _ = srvConn.Close() }()

	// Close the cached client conn directly; the next Write must fail.
	key := destKey(host, port)
	d.mu.Lock()
	c := d.conns[key]
	d.mu.Unlock()
	if c == nil {
		t.Fatalf("expected cached conn for %s", key)
	}
	if err := c.Close(); err != nil {
		t.Fatalf("close cached conn: %v", err)
	}

	if err := d.sendMessage(host, port, m); err == nil {
		t.Fatalf("want write error after conn closed")
	}
	// Dead conn must have been dropped from the map.
	d.mu.Lock()
	_, still := d.conns[key]
	d.mu.Unlock()
	if still {
		t.Errorf("dead conn not dropped from dialer map")
	}
}

// TestTCPDialer_SendBundle_SLIP_WriteError exercises the bundle write-error
// path through the SLIP framer.
func TestTCPDialer_SendBundle_SLIP_WriteError(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer func() { _ = ln.Close() }()
	host, port := splitHostPort(t, ln.Addr().String())

	accepted := make(chan net.Conn, 1)
	go func() {
		c, aerr := ln.Accept()
		if aerr == nil {
			accepted <- c
		}
	}()

	d := newTCPDialer(framerSLIP, nil)
	defer func() { _ = d.close() }()
	b := codec.Bundle{Timetag: 1, Elements: []codec.Packet{codec.Message{Address: "/a"}}}
	if err := d.sendBundle(host, port, b); err != nil {
		t.Fatalf("first send: %v", err)
	}
	srvConn := <-accepted
	defer func() { _ = srvConn.Close() }()

	key := destKey(host, port)
	d.mu.Lock()
	c := d.conns[key]
	d.mu.Unlock()
	if err := c.Close(); err != nil {
		t.Fatalf("close cached conn: %v", err)
	}
	if err := d.sendBundle(host, port, b); err == nil {
		t.Fatalf("want write error after conn closed")
	}
}

// TestTCPDialer_CloseError drives close()'s first-error capture: put a conn
// whose Close errors (already-closed) into the map and close the dialer.
func TestTCPDialer_CloseError(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer func() { _ = ln.Close() }()
	host, port := splitHostPort(t, ln.Addr().String())

	accepted := make(chan net.Conn, 1)
	go func() {
		c, aerr := ln.Accept()
		if aerr == nil {
			accepted <- c
		}
	}()

	d := newTCPDialer(framerLenPrefix, nil)
	c, err := d.dial(host, port)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	srvConn := <-accepted
	defer func() { _ = srvConn.Close() }()

	// Pre-close the conn so dialer.close() observes an error from c.Close().
	if err := c.Close(); err != nil {
		t.Fatalf("pre-close: %v", err)
	}
	if err := d.close(); err == nil {
		t.Fatalf("want first error from closing an already-closed conn")
	}
	// Map must be drained regardless.
	d.mu.Lock()
	n := len(d.conns)
	d.mu.Unlock()
	if n != 0 {
		t.Errorf("dialer map not drained after close: %d", n)
	}
}

// TestTCPDialer_DestKey is a direct check of the key helper (covers destKey
// independently of the dial path).
func TestTCPDialer_DestKey(t *testing.T) {
	if got := destKey("10.0.0.5", 8001); got != "10.0.0.5:8001" {
		t.Errorf("destKey=%q", got)
	}
}

// --- Race-safety: concurrent lazy-init + read across goroutines -----------
//
// Mirrors the scenario that broke the tsl provider under `go test -race`:
// one goroutine drives Serve (which lazily creates + binds the sender) while
// others call BoundAddr / SendMessage / Stop concurrently. With the additive
// Server.mu + udpSender conn-locking, this is race-free. Run with -race in CI.
func TestServer_ConcurrentServeAndReaders(t *testing.T) {
	l, dest := udpRecv(t)
	defer func() { _ = l.Close() }()

	s := NewServerV10(discardLogger())
	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan error, 1)
	go func() { done <- s.Serve(ctx, "127.0.0.1:0") }()

	// Hammer the read/create paths concurrently with Serve's lazy bind.
	const workers = 8
	start := make(chan struct{})
	finished := make(chan struct{}, workers)
	for i := 0; i < workers; i++ {
		go func() {
			<-start
			for j := 0; j < 50; j++ {
				_ = s.BoundAddr()
				_ = s.AddDestination(dest.IP.String(), dest.Port)
				_ = s.SendMessage(codec.Message{Address: "/x", Args: []codec.Arg{codec.Int32(int32(j))}})
			}
			finished <- struct{}{}
		}()
	}
	close(start)
	for i := 0; i < workers; i++ {
		<-finished
	}

	cancel()
	if err := <-done; err != nil {
		t.Fatalf("Serve: %v", err)
	}
	if err := s.Stop(); err != nil {
		t.Fatalf("Stop: %v", err)
	}
}
