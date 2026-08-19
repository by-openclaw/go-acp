package acp1

import (
	"context"
	"net"
	"testing"
	"time"

	"dhs/internal/acp1/codec"
	"dhs/internal/export/canonical"
)

// TestNewServer_NilLoggerAndBadExport covers the nil-logger default and the
// tree-build-failure branch (newServer falls back to an empty tree).
func TestNewServer_NilLoggerAndBadExport(t *testing.T) {
	// nil logger + nil export → tree build fails → empty-tree fallback.
	s := newServer(nil, nil)
	if s.logger == nil {
		t.Error("nil logger should default")
	}
	if s.tree == nil || len(s.tree.entries) != 0 {
		t.Error("bad export should yield empty tree fallback")
	}

	// A valid (non-nil) export builds a real tree.
	root := &canonical.Node{Header: canonical.Header{Identifier: "device", Access: canonical.AccessRead}}
	s2 := newServer(nil, &canonical.Export{Root: root})
	if s2.tree == nil {
		t.Error("valid export should build a tree")
	}
}

// TestServe_AddrErrors covers Serve's resolve and listen failure returns.
func TestServe_AddrErrors(t *testing.T) {
	s := newTestServer(t)
	if err := s.Serve(context.Background(), "not-an-addr:::"); err == nil {
		t.Error("bad addr: want resolve error")
	}
	if err := s.Serve(context.Background(), "127.0.0.1:999999"); err == nil {
		t.Error("out-of-range port: want listen error")
	}
}

// TestServe_ReadLoopHardError: a pre-read hook sets a past read deadline so
// readLoop's first ReadFromUDP returns a non-closed i/o-timeout error, which
// Serve surfaces (the hard-error return arm).
func TestServe_ReadLoopHardError(t *testing.T) {
	probe, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 0})
	if err != nil {
		t.Fatalf("probe: %v", err)
	}
	addr := probe.LocalAddr().String()
	_ = probe.Close()

	s := newTestServer(t)
	s.preReadHook = func(c *net.UDPConn) { _ = c.SetReadDeadline(time.Now().Add(-time.Hour)) }
	err = s.Serve(context.Background(), addr)
	if err == nil {
		t.Fatal("read deadline in the past: Serve should return the read error")
	}
}

// TestServe_BroadcastDialDisabled drives the broadcast-disabled warn arm via
// the injected bcast-dial-error hook.
func TestServe_BroadcastDialDisabled(t *testing.T) {
	probe, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 0})
	if err != nil {
		t.Fatalf("probe: %v", err)
	}
	addr := probe.LocalAddr().String()
	_ = probe.Close()

	s := newTestServer(t)
	s.bcastDialErrHook = func() bool { return true }
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- s.Serve(ctx, addr) }()
	time.Sleep(80 * time.Millisecond)
	cancel()
	<-done
}

// TestServe_BoundToLoopback binds to an explicit 127.0.0.1 address (non-
// unspecified) so the broadcast-dial path pins LocalAddr. On hosts where a
// broadcast dial from a loopback source is rejected, this also drives the
// broadcast-disabled warn branch; otherwise it exercises the bound path.
func TestServe_BoundToLoopback(t *testing.T) {
	// Find a free UDP port on 127.0.0.1.
	probe, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 0})
	if err != nil {
		t.Fatalf("probe: %v", err)
	}
	addr := probe.LocalAddr().String()
	_ = probe.Close()

	s := newTestServer(t)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- s.Serve(ctx, addr) }()
	time.Sleep(80 * time.Millisecond)
	cancel()
	<-done
}

// TestServe_ListenPacketError: a syntactically-valid but unbindable address
// (a non-local IP) makes ListenPacket fail after ResolveUDPAddr succeeds.
func TestServe_ListenPacketError(t *testing.T) {
	s := newTestServer(t)
	// 192.0.2.1 is TEST-NET-1 (RFC 5737), never a local interface address,
	// so binding it fails — exercising the ListenPacket error return.
	if err := s.Serve(context.Background(), "192.0.2.1:2071"); err == nil {
		t.Error("non-local bind: want listen error")
	}
}

// TestSetValue_AccessAndMutationErrors covers SetValue's no-read-access,
// no-write-access, and applyMutation-error arms.
func TestSetValue_AccessAndMutationErrors(t *testing.T) {
	s := newTestServer(t)

	// No-read-access object.
	noRead := objectKey{slot: 1, group: codec.GroupControl, id: 210}
	s.tree.mu.Lock()
	s.tree.entries[noRead] = &entry{key: noRead, acpType: codec.TypeInteger,
		access: codec.AccessWrite, // write only, no read
		param:  &canonical.Parameter{Value: int64(0)}}
	// No-write-access object.
	noWrite := objectKey{slot: 1, group: codec.GroupControl, id: 211}
	s.tree.entries[noWrite] = &entry{key: noWrite, acpType: codec.TypeInteger,
		access: codec.AccessRead, // read only, no write
		param:  &canonical.Parameter{Value: int64(0)}}
	// RW object whose stored Value is the wrong type → applyMutation fails.
	badVal := objectKey{slot: 1, group: codec.GroupControl, id: 212}
	s.tree.entries[badVal] = &entry{key: badVal, acpType: codec.TypeInteger,
		access: codec.AccessRead | codec.AccessWrite,
		param:  &canonical.Parameter{Value: "not-an-int"}}
	s.tree.mu.Unlock()

	if _, err := s.SetValue(context.Background(), "1.2.2.210", int64(1)); err == nil {
		t.Error("no-read-access: want error")
	}
	if _, err := s.SetValue(context.Background(), "1.2.2.211", int64(1)); err == nil {
		t.Error("no-write-access: want error")
	}
	if _, err := s.SetValue(context.Background(), "1.2.2.212", int64(1)); err == nil {
		t.Error("applyMutation error: want error")
	}
}

// TestBroadcastAnnounce_EncodeError: an announce whose Value exceeds the
// ACP1 MDATA budget fails Encode in broadcastAnnounceSkip.
func TestBroadcastAnnounce_EncodeError(t *testing.T) {
	s := newTestServer(t)
	huge := make([]byte, 300)
	s.broadcastAnnounce(&codec.Message{
		MType: codec.MTypeReply, MCode: byte(codec.MethodSetValue),
		ObjGroup: codec.GroupControl, ObjID: 0, Value: huge,
	})
	// No assertion: the encode-error path just logs and returns.
}

// TestBroadcastAnnounce_WriteToClosedBcast drives the broadcast write-error
// arm: a closed bcast socket yields net.ErrClosed on Write.
func TestBroadcastAnnounce_WriteToClosedBcast(t *testing.T) {
	s := newTestServer(t)
	bc, err := net.DialUDP("udp4", nil, &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 9}) // discard port
	if err != nil {
		t.Skipf("dial: %v", err)
	}
	_ = bc.Close() // close so the next Write fails with net.ErrClosed
	s.mu.Lock()
	s.bcast = bc
	s.mu.Unlock()
	s.broadcastAnnounce(&codec.Message{
		MType: codec.MTypeReply, MCode: byte(codec.MethodSetValue),
		ObjGroup: codec.GroupControl, ObjID: 0, Value: []byte{0x00, 0x01},
	})
}

// TestReadLoop_CtxAlreadyCancelled: readLoop returns immediately via the
// pre-read ctx.Err() check when its context is already cancelled.
func TestReadLoop_CtxAlreadyCancelled(t *testing.T) {
	s := newTestServer(t)
	conn, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 0})
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer func() { _ = conn.Close() }()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := s.readLoop(ctx, conn); err == nil {
		t.Fatal("cancelled ctx: readLoop should return ctx error")
	}
}

// TestBroadcastAnnounce_WriteNonClosedError drives the non-ErrClosed write
// warn: a bcast socket with a write deadline already in the past returns an
// i/o timeout (not ErrClosed) on Write.
func TestBroadcastAnnounce_WriteNonClosedError(t *testing.T) {
	s := newTestServer(t)
	bc, err := net.DialUDP("udp4", nil, &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 9})
	if err != nil {
		t.Skipf("dial: %v", err)
	}
	defer func() { _ = bc.Close() }()
	_ = bc.SetWriteDeadline(time.Now().Add(-time.Hour)) // past → Write times out
	s.mu.Lock()
	s.bcast = bc
	s.mu.Unlock()
	s.broadcastAnnounce(&codec.Message{
		MType: codec.MTypeReply, MCode: byte(codec.MethodSetValue),
		ObjGroup: codec.GroupControl, ObjID: 0, Value: []byte{0x00, 0x01},
	})
}

// TestReadLoop_ZeroByteDatagram sends an empty UDP packet so readLoop's n==0
// continue arm runs.
func TestReadLoop_ZeroByteDatagram(t *testing.T) {
	s := newTestServer(t)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	// Serve on an ephemeral port directly and read the bound address off
	// the server — no probe-socket close-then-rebind (the freed port can
	// be stolen by a parallel test process, #694 flake class).
	go func() { done <- s.Serve(ctx, "127.0.0.1:0") }()
	var addr string
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		s.mu.Lock()
		if s.conn != nil {
			addr = s.conn.LocalAddr().String()
		}
		s.mu.Unlock()
		if addr != "" {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if addr == "" {
		t.Fatal("server socket never bound")
	}
	c, err := net.Dial("udp4", addr)
	if err == nil {
		_, _ = c.Write([]byte{})
		_ = c.Close()
	}
	time.Sleep(40 * time.Millisecond)
	cancel()
	<-done
}

// TestSetValue_BadPathAndMissing covers the parsePath and lookup-miss arms.
func TestSetValue_BadPathAndMissing(t *testing.T) {
	s := newTestServer(t)
	if _, err := s.SetValue(context.Background(), "bogus", 1); err == nil {
		t.Error("bad path: want error")
	}
	if _, err := s.SetValue(context.Background(), "1.30.2.99", 1); err == nil {
		t.Error("missing object: want error")
	}
}
