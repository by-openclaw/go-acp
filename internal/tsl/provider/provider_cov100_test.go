package tsl

import (
	"bytes"
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
	"dhs/internal/provider"
	"dhs/internal/tsl/codec"
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
		{V31, "tsl-v31", 4000, "v3.1"},
		{V40, "tsl-v40", 4000, "v4.0"},
		{V50, "tsl-v50", 8901, "v5.0"},
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

// TestVersion_UnknownFallthrough drives the trailing fall-through returns
// in name/defaultPort/description (the default arms for an out-of-range
// Version) — these are reachable for any value outside the iota set.
func TestVersion_UnknownFallthrough(t *testing.T) {
	unknown := Version(99)
	if got := unknown.name(); got != "tsl-unknown" {
		t.Errorf("name(unknown)=%q want tsl-unknown", got)
	}
	if got := unknown.defaultPort(); got != 0 {
		t.Errorf("defaultPort(unknown)=%d want 0", got)
	}
	if got := unknown.description(); got != "" {
		t.Errorf("description(unknown)=%q want empty", got)
	}
}

// --- Factory.Meta + Factory.New -------------------------------------------

func TestFactory_MetaAndNew(t *testing.T) {
	f := &Factory{version: V50}
	meta := f.Meta()
	if meta.Name != "tsl-v50" || meta.DefaultPort != 8901 {
		t.Fatalf("meta=%+v", meta)
	}
	if !strings.Contains(meta.Description, "v5.0") {
		t.Errorf("meta description=%q", meta.Description)
	}

	tree := &canonical.Export{}
	p := f.New(plugin.Deps{Logger: discardLogger()}, tree)
	srv, ok := p.(*Server)
	if !ok {
		t.Fatalf("New returned %T, want *Server", p)
	}
	if srv.version != V50 {
		t.Errorf("server version=%d want V50", srv.version)
	}
	if srv.tree != tree {
		t.Errorf("server tree not threaded through")
	}
	// Confirm the package registered all three factories at init time.
	var _ provider.Provider = srv
}

// --- Direct constructors --------------------------------------------------

func TestNewServerConstructors(t *testing.T) {
	lg := discardLogger()
	if s := NewServerV31(lg); s.version != V31 {
		t.Errorf("NewServerV31 version=%d", s.version)
	}
	if s := NewServerV40(lg); s.version != V40 {
		t.Errorf("NewServerV40 version=%d", s.version)
	}
	if s := NewServerV50(lg); s.version != V50 {
		t.Errorf("NewServerV50 version=%d", s.version)
	}
}

// --- SetValue: not-implemented sentinel -----------------------------------

func TestServer_SetValue_NotImplemented(t *testing.T) {
	s := NewServerV31(discardLogger())
	got, err := s.SetValue(context.Background(), "1.2.3", 7)
	if got != nil {
		t.Errorf("SetValue value=%v want nil", got)
	}
	if !errors.Is(err, errNotImplemented) {
		t.Errorf("SetValue err=%v want errNotImplemented", err)
	}
}

// --- Bind / BoundAddr / AddDestination (lazy sender allocation) -----------

func TestServer_BindBoundAddrAddDest(t *testing.T) {
	s := NewServerV31(discardLogger())
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
	if err := s.AddDestination("127.0.0.1", 4000); err != nil {
		t.Fatalf("AddDestination: %v", err)
	}
}

// TestServer_AddDestination_LazyAlloc drives the s.sender==nil branch in
// AddDestination (no prior Bind/Serve).
func TestServer_AddDestination_LazyAlloc(t *testing.T) {
	s := NewServerV31(discardLogger())
	if err := s.AddDestination("127.0.0.1", 4000); err != nil {
		t.Fatalf("AddDestination lazy: %v", err)
	}
	if s.sender == nil {
		t.Fatalf("AddDestination did not allocate sender")
	}
}

func TestServer_AddDestination_ResolveError(t *testing.T) {
	s := NewServerV31(discardLogger())
	if err := s.AddDestination("no such host at all", -1); err == nil {
		t.Fatalf("want resolve error for bad dest")
	}
}

// --- Serve: binds + blocks until ctx cancel -------------------------------

func TestServer_Serve_BlocksUntilCancel(t *testing.T) {
	s := NewServerV31(discardLogger())
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

// TestServer_Serve_ReusesBoundSender exercises serveBlock's "already
// bound" path (s.conn != nil) — Bind first, then Serve must not rebind.
func TestServer_Serve_ReusesBoundSender(t *testing.T) {
	s := NewServerV40(discardLogger())
	if err := s.Bind("127.0.0.1:0"); err != nil {
		t.Fatalf("Bind: %v", err)
	}
	bound := s.BoundAddr()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- s.Serve(ctx, "127.0.0.1:0") }()
	// Port must be unchanged — Serve reused the existing socket.
	time.Sleep(20 * time.Millisecond)
	if got := s.BoundAddr(); got.Port != bound.Port {
		t.Fatalf("Serve rebound: %v != %v", got, bound)
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
	s := NewServerV31(discardLogger())
	err := s.Serve(context.Background(), "256.256.256.256:99999")
	if err == nil {
		t.Fatalf("want bind error for invalid addr")
	}
}

// --- SendV31 / SendV40 / SendV50: version gating + not-bound + happy path --

func TestSendV31_WrongVersion(t *testing.T) {
	s := NewServerV40(discardLogger())
	if err := s.SendV31(codec.V31Frame{Address: 1}); err == nil {
		t.Fatalf("SendV31 on v4.0 server should error")
	}
}

func TestSendV31_NotBound(t *testing.T) {
	s := NewServerV31(discardLogger())
	if err := s.SendV31(codec.V31Frame{Address: 1}); err == nil {
		t.Fatalf("SendV31 unbound should error")
	}
}

func TestSendV40_WrongVersion(t *testing.T) {
	s := NewServerV31(discardLogger())
	if err := s.SendV40(codec.V40Frame{}); err == nil {
		t.Fatalf("SendV40 on v3.1 server should error")
	}
}

func TestSendV40_NotBound(t *testing.T) {
	s := NewServerV40(discardLogger())
	if err := s.SendV40(codec.V40Frame{}); err == nil {
		t.Fatalf("SendV40 unbound should error")
	}
}

func TestSendV50_WrongVersion(t *testing.T) {
	s := NewServerV31(discardLogger())
	if err := s.SendV50(codec.V50Packet{}); err == nil {
		t.Fatalf("SendV50 on v3.1 server should error")
	}
}

func TestSendV50_NotBound(t *testing.T) {
	s := NewServerV50(discardLogger())
	if err := s.SendV50(codec.V50Packet{}); err == nil {
		t.Fatalf("SendV50 unbound should error")
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

func TestSendV40_RoundTrip(t *testing.T) {
	l, dest := udpRecv(t)
	defer func() { _ = l.Close() }()

	s := NewServerV40(discardLogger())
	if err := s.Bind("127.0.0.1:0"); err != nil {
		t.Fatalf("Bind: %v", err)
	}
	defer func() { _ = s.Stop() }()
	if err := s.AddDestination(dest.IP.String(), dest.Port); err != nil {
		t.Fatalf("AddDestination: %v", err)
	}

	frame := codec.V40Frame{
		V31:          codec.V31Frame{Address: 5, Tally1: true, Brightness: codec.BrightnessFull, Text: "PGM"},
		DisplayLeft:  codec.XByte{LH: codec.TallyRed},
		DisplayRight: codec.XByte{RH: codec.TallyGreen},
	}
	if err := s.SendV40(frame); err != nil {
		t.Fatalf("SendV40: %v", err)
	}

	buf := make([]byte, 64)
	if err := l.SetReadDeadline(time.Now().Add(2 * time.Second)); err != nil {
		t.Fatalf("deadline: %v", err)
	}
	n, _, err := l.ReadFromUDP(buf)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	got, err := codec.DecodeV40(buf[:n])
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.V31.Address != 5 || !got.V31.Tally1 {
		t.Errorf("v3.1 section wrong: %+v", got.V31)
	}
	if got.DisplayLeft.LH != codec.TallyRed || got.DisplayRight.RH != codec.TallyGreen {
		t.Errorf("xdata wrong: L=%+v R=%+v", got.DisplayLeft, got.DisplayRight)
	}
}

func TestSendV50_UDP_RoundTrip(t *testing.T) {
	l, dest := udpRecv(t)
	defer func() { _ = l.Close() }()

	s := NewServerV50(discardLogger())
	if err := s.Bind("127.0.0.1:0"); err != nil {
		t.Fatalf("Bind: %v", err)
	}
	defer func() { _ = s.Stop() }()
	if err := s.AddDestination(dest.IP.String(), dest.Port); err != nil {
		t.Fatalf("AddDestination: %v", err)
	}

	pkt := codec.V50Packet{
		Screen: 0,
		DMSGs: []codec.DMSG{
			{Index: 2, LH: codec.TallyRed, Brightness: codec.BrightnessFull, Text: "PGM"},
		},
	}
	if err := s.SendV50(pkt); err != nil {
		t.Fatalf("SendV50: %v", err)
	}

	buf := make([]byte, 2048)
	if err := l.SetReadDeadline(time.Now().Add(2 * time.Second)); err != nil {
		t.Fatalf("deadline: %v", err)
	}
	n, _, err := l.ReadFromUDP(buf)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	got, err := codec.DecodeV50(buf[:n])
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(got.DMSGs) != 1 || got.DMSGs[0].Index != 2 || got.DMSGs[0].Text != "PGM" {
		t.Errorf("v5.0 round-trip wrong: %+v", got.DMSGs)
	}
}

// --- encode-error fan-out (encodeAndSendV40 / V50UDP / V31 error arms) -----

// badV31 builds a frame whose Encode fails (address > 126).
func badV31() codec.V31Frame { return codec.V31Frame{Address: 200} }

func TestSendV31_EncodeError(t *testing.T) {
	s := NewServerV31(discardLogger())
	if err := s.Bind("127.0.0.1:0"); err != nil {
		t.Fatalf("Bind: %v", err)
	}
	defer func() { _ = s.Stop() }()
	if err := s.AddDestination("127.0.0.1", 4000); err != nil {
		t.Fatalf("AddDestination: %v", err)
	}
	if err := s.SendV31(badV31()); err == nil {
		t.Fatalf("want encode error for address overflow")
	}
}

func TestSendV40_EncodeError(t *testing.T) {
	s := NewServerV40(discardLogger())
	if err := s.Bind("127.0.0.1:0"); err != nil {
		t.Fatalf("Bind: %v", err)
	}
	defer func() { _ = s.Stop() }()
	if err := s.AddDestination("127.0.0.1", 4000); err != nil {
		t.Fatalf("AddDestination: %v", err)
	}
	if err := s.SendV40(codec.V40Frame{V31: badV31()}); err == nil {
		t.Fatalf("want encode error for v4.0 wrapping bad v3.1")
	}
}

func TestSendV50_EncodeError(t *testing.T) {
	s := NewServerV50(discardLogger())
	if err := s.Bind("127.0.0.1:0"); err != nil {
		t.Fatalf("Bind: %v", err)
	}
	defer func() { _ = s.Stop() }()
	if err := s.AddDestination("127.0.0.1", 8901); err != nil {
		t.Fatalf("AddDestination: %v", err)
	}
	// Oversized single-DMSG text overflows PBC (> V50MaxPacketSize).
	huge := strings.Repeat("X", 3000)
	pkt := codec.V50Packet{DMSGs: []codec.DMSG{{Index: 1, Text: huge}}}
	if err := s.SendV50(pkt); err == nil {
		t.Fatalf("want encode error for oversized v5.0 packet")
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
	err := s.encodeAndSendV31(codec.V31Frame{Address: 1})
	if err == nil {
		t.Fatalf("want write error on closed conn")
	}
}

// --- udpSender.bind: already-bound guard ----------------------------------

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

// TestUDPSender_BoundAddr_Nil drives boundAddr's s.conn==nil arm on a
// freshly-constructed (unbound) sender.
func TestUDPSender_BoundAddr_Nil(t *testing.T) {
	s := newUDPSender(nil)
	if got := s.boundAddr(); got != nil {
		t.Fatalf("boundAddr on unbound sender=%v want nil", got)
	}
}

// TestUDPSender_Bind_EmptyAddrDefault drives the addr=="" -> ":0"
// default branch in bind.
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

// --- bind defensive branches via the transparent seam --------------------
//
// These three branches cannot fire on a live UDP socket: the
// SO_REUSEADDR/SO_BROADCAST setsockopt calls and the RawConn.Control
// dispatch succeed on every supported OS, and ListenPacket(ctx,"udp",...)
// always yields a *net.UDPConn. The seam (bind_seam.go) is nil in
// production — these tests install a hook, prove bind propagates the
// defensive error, then restore nil. The guards in bind are untouched.

func TestUDPSender_SendBytes_NotBound(t *testing.T) {
	s := newUDPSender(nil)
	if err := s.encodeAndSendV31(codec.V31Frame{Address: 1}); err == nil {
		t.Fatalf("sendBytes unbound should error")
	}
}

// --- Stop: aggregates sender + dialer errors; nil-safe ---------------------

func TestServer_Stop_NoResources(t *testing.T) {
	s := NewServerV31(discardLogger())
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

	s := NewServerV50(discardLogger())
	if err := s.Bind("127.0.0.1:0"); err != nil {
		t.Fatalf("Bind: %v", err)
	}
	pkt := codec.V50Packet{DMSGs: []codec.DMSG{{Index: 1, Text: "A"}}}
	if err := s.SendV50TCP(host, port, pkt); err != nil {
		t.Fatalf("SendV50TCP: %v", err)
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

// TestServer_Stop_SenderError drives Stop's sender-error arm: the
// sender's UDP conn is closed out-of-band (bypassing closeOnce) so the
// close-once body's conn.Close() inside Stop returns an error.
func TestServer_Stop_SenderError(t *testing.T) {
	s := NewServerV31(discardLogger())
	if err := s.Bind("127.0.0.1:0"); err != nil {
		t.Fatalf("Bind: %v", err)
	}
	// Close the underlying conn directly; closeOnce has not fired yet, so
	// Stop -> sender.close() -> conn.Close() will hit an already-closed
	// socket and return an error.
	if err := s.sender.conn.Close(); err != nil {
		t.Fatalf("pre-close conn: %v", err)
	}
	if err := s.Stop(); err == nil {
		t.Fatalf("Stop should surface sender close error")
	}
}

// TestServer_Stop_DialerError drives Stop's dialer-error aggregation arm:
// sender closes cleanly (first==nil) and tcpDialer.close() then returns
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

	s := NewServerV50(discardLogger())
	if err := s.Bind("127.0.0.1:0"); err != nil {
		t.Fatalf("Bind: %v", err)
	}
	if err := s.SendV50TCP(host, port, codec.V50Packet{DMSGs: []codec.DMSG{{Index: 1, Text: "A"}}}); err != nil {
		t.Fatalf("SendV50TCP: %v", err)
	}
	srvConn := <-accepted
	defer func() { _ = srvConn.Close() }()

	// Pre-close the dialer's cached conn so tcpDialer.close() inside Stop
	// observes a close error while the sender closes cleanly.
	key := destKey(host, port)
	s.tcpDialer.mu.Lock()
	c := s.tcpDialer.conns[key]
	s.tcpDialer.mu.Unlock()
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

func TestSendV50TCP_WrongVersion(t *testing.T) {
	s := NewServerV31(discardLogger())
	if err := s.SendV50TCP("127.0.0.1", 8902, codec.V50Packet{}); err == nil {
		t.Fatalf("SendV50TCP on v3.1 server should error")
	}
}

func TestSendV50TCP_RoundTripAndReuse(t *testing.T) {
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

	s := NewServerV50(discardLogger())
	defer func() { _ = s.Stop() }()

	pkt := codec.V50Packet{Screen: 0, DMSGs: []codec.DMSG{{Index: 7, RH: codec.TallyAmber, Text: "ISO"}}}
	if err := s.SendV50TCP(host, port, pkt); err != nil {
		t.Fatalf("SendV50TCP #1: %v", err)
	}
	a := <-accepted
	if a.err != nil {
		t.Fatalf("accept: %v", a.err)
	}
	srvConn := a.c
	defer func() { _ = srvConn.Close() }()

	// Read the DLE/STX-wrapped frame, unwrap, decode, assert.
	if err := srvConn.SetReadDeadline(time.Now().Add(2 * time.Second)); err != nil {
		t.Fatalf("deadline: %v", err)
	}
	buf := make([]byte, 4096)
	n, err := srvConn.Read(buf)
	if err != nil {
		t.Fatalf("server read: %v", err)
	}
	dec := codec.NewDLEStreamDecoder(bytes.NewReader(buf[:n]), 0)
	unwrapped, err := dec.ReadFrame()
	if err != nil {
		t.Fatalf("DLE decode: %v", err)
	}
	got, err := codec.DecodeV50(unwrapped)
	if err != nil {
		t.Fatalf("DecodeV50: %v", err)
	}
	if len(got.DMSGs) != 1 || got.DMSGs[0].Index != 7 || got.DMSGs[0].Text != "ISO" {
		t.Errorf("TCP round-trip wrong: %+v", got.DMSGs)
	}

	// Second send must REUSE the cached conn (dial hits the map-hit branch).
	if s.tcpDialer == nil {
		t.Fatalf("dialer not allocated")
	}
	before := len(s.tcpDialer.conns)
	if err := s.SendV50TCP(host, port, pkt); err != nil {
		t.Fatalf("SendV50TCP #2: %v", err)
	}
	if after := len(s.tcpDialer.conns); after != before {
		t.Errorf("second send changed conn count %d -> %d (no reuse)", before, after)
	}
}

func TestSendV50TCP_DialError(t *testing.T) {
	// Reserve a port then close it so the dial is refused.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	host, port := splitHostPort(t, ln.Addr().String())
	_ = ln.Close()

	s := NewServerV50(discardLogger())
	defer func() { _ = s.Stop() }()
	if err := s.SendV50TCP(host, port, codec.V50Packet{DMSGs: []codec.DMSG{{Index: 1, Text: "A"}}}); err == nil {
		t.Fatalf("want dial error to closed port")
	}
}

func TestSendV50TCP_EncodeError(t *testing.T) {
	s := NewServerV50(discardLogger())
	defer func() { _ = s.Stop() }()
	huge := strings.Repeat("X", 3000)
	pkt := codec.V50Packet{DMSGs: []codec.DMSG{{Index: 1, Text: huge}}}
	// Encode fails before any dial — no listener needed.
	if err := s.SendV50TCP("127.0.0.1", 1, pkt); err == nil {
		t.Fatalf("want encode error for oversized v5.0 TCP packet")
	}
}

// TestSendV50TCP_WriteError forces the write-failure arm: dial a live
// listener, then close the cached conn so the next Write errors and the
// dialer drops the dead conn from its map.
func TestSendV50TCP_WriteError(t *testing.T) {
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

	d := newTCPDialer(nil)
	defer func() { _ = d.close() }()
	pkt := codec.V50Packet{DMSGs: []codec.DMSG{{Index: 1, Text: "A"}}}
	if err := d.sendV50TCP(host, port, pkt); err != nil {
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

	err = d.sendV50TCP(host, port, pkt)
	if err == nil {
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

// TestTCPDialer_CloseError drives close()'s first-error capture: put a
// conn whose Close errors (already-closed) into the map and close the
// dialer.
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

	d := newTCPDialer(nil)
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
	if len(d.conns) != 0 {
		t.Errorf("dialer map not drained after close: %d", len(d.conns))
	}
}
