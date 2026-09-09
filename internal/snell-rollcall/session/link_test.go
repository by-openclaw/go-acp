package session

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"net"
	"strings"
	"testing"
	"time"

	"dhs/internal/clock"
	deps "dhs/internal/plugin"
	"dhs/internal/snell-rollcall/codec"
)

// TestHandshake_LearnsBothAddresses covers the first thing a client sends.
//
// A TCP client knows neither address. Its own is assigned by the gateway,
// which stamps a port drawn from its connection slots over the zeroed source
// we send; the gateway's own is wherever the reply comes from. A peer that
// skips this is rejected by real gateways and by the Control Panel.
func TestHandshake_LearnsBothAddresses(t *testing.T) {
	h := newHarness(t, Config{})

	// Before the handshake we have no address and can only reach the peer on
	// the broadcast one.
	if got := h.link.LocalAddress(); got.Unit != 0 || got.Port != 0 {
		t.Errorf("local address starts as %s, want zeros", got)
	}
	if !h.link.RemoteAddress().IsBroadcast() {
		t.Errorf("remote address starts as %s, want the broadcast address",
			h.link.RemoteAddress())
	}

	type result struct {
		info codec.DeviceInfo
		err  error
	}
	done := make(chan result, 1)
	go func() {
		info, err := h.link.Handshake(context.Background())
		done <- result{info, err}
	}()

	req := h.peer.recvType(codec.MsgGetDevInfo)
	if !req.Dst.IsBroadcast() {
		t.Errorf("handshake sent to %s, want the broadcast address", req.Dst)
	}
	if req.Dst.Index != codec.IndexUnknown {
		t.Errorf("handshake index = %d, want %d", req.Dst.Index, codec.IndexUnknown)
	}

	// The gateway answers from its real address, and stamps ours into the
	// destination.
	gateway := codec.Address{Unit: 0x08, Port: 0x00, Index: codec.IndexUnknown}
	assigned := codec.Address{Unit: 0x08, Port: 0xE3, Index: codec.IndexUnknown}

	info := codec.DeviceInfo{
		ProtocolVersion: codec.ProtocolVersion,
		Address:         gateway,
		ID: codec.ID{
			Services: codec.SvcMenus | codec.SvcControl | codec.SvcLongStr,
			TypeID:   636,
			Name:     "Centra",
		},
		Status: codec.UnitStatus{Status: codec.StatusPresent},
	}
	payload, err := info.AppendTo(nil)
	if err != nil {
		t.Fatalf("AppendTo: %v", err)
	}
	h.peer.write(codec.Frame{
		Dst:     assigned,
		Src:     gateway,
		Type:    codec.MsgRetDevInfo,
		Payload: payload,
	})

	r := <-done
	if r.err != nil {
		t.Fatalf("Handshake: %v", r.err)
	}

	if got := h.link.LocalAddress(); got.Unit != 0x08 || got.Port != 0xE3 {
		t.Errorf("local address = %s, want the stamped 0000-08-E3", got)
	}
	if got := h.link.RemoteAddress(); got.Unit != 0x08 || got.Port != 0x00 {
		t.Errorf("remote address = %s, want the gateway 0000-08-00", got)
	}

	// The reply also says which generation the peer can speak, which is what
	// decides whether a 32-bit session may be asked for.
	if !r.info.ID.Services.LongStrings() {
		t.Error("the peer's service mask should advertise long strings")
	}
	if r.info.ID.Name != "Centra" {
		t.Errorf("name = %q", r.info.ID.Name)
	}
}

// TestHandshake_AddressInThePayloadIsNotARoute pins the note in spec 11.3.4.
// The address inside a DeviceInfo is never rewritten as a message crosses a
// bridge, so the route in it is meaningless and the header is what must be
// believed.
func TestHandshake_AddressInThePayloadIsNotARoute(t *testing.T) {
	h := newHarness(t, Config{})

	done := make(chan error, 1)
	go func() {
		_, err := h.link.Handshake(context.Background())
		done <- err
	}()
	h.peer.recvType(codec.MsgGetDevInfo)

	// The payload claims no route; the header says the peer is two bridges
	// away. The header wins.
	info := codec.DeviceInfo{Address: codec.Address{Unit: 0x20, Index: codec.IndexUnknown}}
	payload, err := info.AppendTo(nil)
	if err != nil {
		t.Fatalf("AppendTo: %v", err)
	}
	h.peer.write(codec.Frame{
		Dst:     codec.Address{Unit: 0x08, Port: 0xE0, Index: codec.IndexUnknown},
		Src:     codec.Address{Net: 0x2300, Unit: 0x20, Index: codec.IndexUnknown},
		Type:    codec.MsgRetDevInfo,
		Payload: payload,
	})

	if err := <-done; err != nil {
		t.Fatalf("Handshake: %v", err)
	}
	if got := h.link.RemoteAddress(); got.Net != 0x2300 {
		t.Errorf("remote route = %04X, want the header's 2300", got.Net)
	}
}

func TestHandshake_Refused(t *testing.T) {
	h := newHarness(t, Config{})

	done := make(chan error, 1)
	go func() {
		_, err := h.link.Handshake(context.Background())
		done <- err
	}()
	req := h.peer.recvType(codec.MsgGetDevInfo)
	h.peer.send(req, codec.MsgNack, nil)

	if err := <-done; !errors.Is(err, ErrRefused) {
		t.Fatalf("err = %v, want ErrRefused", err)
	}
}

func TestHandshake_MalformedReply(t *testing.T) {
	h := newHarness(t, Config{})

	done := make(chan error, 1)
	go func() {
		_, err := h.link.Handshake(context.Background())
		done <- err
	}()
	req := h.peer.recvType(codec.MsgGetDevInfo)
	h.peer.send(req, codec.MsgRetDevInfo, []byte{1, 2, 3}) // too short

	if err := <-done; !errors.Is(err, codec.ErrShortBuffer) {
		t.Fatalf("err = %v, want ErrShortBuffer", err)
	}
}

// TestLink_SessionIndexAllocation pins which indices may be used. Zero, 0xFE
// and 0xFF are reserved for blind, logging and unconnected traffic, so a
// session must never be handed one of them.
func TestLink_SessionIndexAllocation(t *testing.T) {
	h := newHarness(t, Config{})

	seen := map[int16]bool{}
	for range 20 {
		s := bareSession(h.link)
		if err := h.link.register(s); err != nil {
			t.Fatalf("register: %v", err)
		}
		idx := s.localIndex
		if idx == codec.IndexBlind || idx == codec.IndexLogging || idx == codec.IndexUnknown {
			t.Fatalf("allocated the reserved index %d", idx)
		}
		if seen[idx] {
			t.Fatalf("index %d allocated twice", idx)
		}
		seen[idx] = true
	}
}

// TestLink_IndexReuse covers a long-lived link opening and closing many
// sessions. Indices wrap, and one still in use must be skipped rather than
// handed out twice.
func TestLink_IndexReuse(t *testing.T) {
	h := newHarness(t, Config{})

	held := h.callSession(t, codec.SvcMenus)
	taken := held.LocalIndex()

	for range 300 {
		s := bareSession(h.link)
		if err := h.link.register(s); err != nil {
			t.Fatalf("register: %v", err)
		}
		if s.localIndex == taken {
			t.Fatalf("allocated index %d, which is already in use", s.localIndex)
		}
		h.link.removeSession(s.localIndex)
	}
}

func TestLink_ClosedRefusesWork(t *testing.T) {
	h := newHarness(t, Config{})
	_ = h.link.Close()

	if err := h.link.register(bareSession(h.link)); !errors.Is(err, ErrLinkClosed) {
		t.Errorf("register err = %v, want ErrLinkClosed", err)
	}
	if _, err := Call(context.Background(), h.link, peerAddr, codec.SvcMenus,
		codec.LevelUser, ClientIdentity("x", codec.SvcMenus)); !errors.Is(err, ErrLinkClosed) {
		t.Errorf("Call err = %v, want ErrLinkClosed", err)
	}
	// Closing twice is harmless.
	if err := h.link.Close(); err != nil {
		t.Errorf("second Close: %v", err)
	}
}

// TestLink_PeerDisconnect covers the far end going away, which is the ordinary
// way a link ends.
func TestLink_PeerDisconnect(t *testing.T) {
	h := newHarness(t, Config{})
	s := h.callSession(t, codec.SvcMenus)

	h.peer.close()

	select {
	case <-h.link.Done():
	case <-time.After(2 * time.Second):
		t.Fatal("the link did not notice the peer disconnecting")
	}
	if _, err := s.Do(context.Background(), codec.MsgGetStat, nil); err == nil {
		t.Error("a session on a dead link should refuse work")
	}
}

// TestLink_WriteFailureClosesTheLink covers the socket dying under a send.
// There is no point keeping a link whose writes fail.
func TestLink_WriteFailureClosesTheLink(t *testing.T) {
	ours, theirs := net.Pipe()
	clk := clock.NewFake(time.Time{})
	l := NewLink(ours, Config{}, testDeps(clk))
	t.Cleanup(func() { _ = l.Close() })

	_ = theirs.Close()

	err := l.send(codec.Frame{Type: codec.MsgKeepAlive})
	if err == nil {
		t.Fatal("a write to a closed pipe should fail")
	}
	select {
	case <-l.Done():
	case <-time.After(2 * time.Second):
		t.Fatal("a failed write should close the link")
	}
}

// TestLink_OversizePayloadIsRefusedBeforeSending covers a caller building a
// frame too big for the wire. It fails locally rather than being truncated
// into a frame the peer will misparse.
func TestLink_OversizePayloadIsRefusedBeforeSending(t *testing.T) {
	h := newHarness(t, Config{})

	err := h.link.send(codec.Frame{Type: codec.MsgRaw, Payload: make([]byte, codec.MaxPayload+1)})
	if !errors.Is(err, codec.ErrPayloadTooLong) {
		t.Fatalf("err = %v, want ErrPayloadTooLong", err)
	}
	h.peer.quiet()
	select {
	case <-h.link.Done():
		t.Error("a rejected frame must not close the link")
	default:
	}
}

// TestLink_Stats covers the counters, including the resync count. A non-zero
// resync means the peer sent something that was not a frame, which is worth a
// compliance event rather than silence.
func TestLink_Stats(t *testing.T) {
	h := newHarness(t, Config{})

	rx, tx, resyncs := h.link.Stats()
	if rx != 0 || tx != 0 || resyncs != 0 {
		t.Errorf("a new link reports %d/%d/%d", rx, tx, resyncs)
	}

	s := h.callSession(t, codec.SvcMenus)
	_ = s

	rx, tx, _ = h.link.Stats()
	if tx == 0 {
		t.Error("no frames counted as sent")
	}
	if rx == 0 {
		t.Error("no frames counted as received")
	}
}

// TestLink_ResyncIsCounted covers junk on the stream. One byte of it puts
// every following frame on an odd offset, and the reader recovers a byte at a
// time; the count is what tells an operator it happened.
func TestLink_ResyncIsCounted(t *testing.T) {
	h := newHarness(t, Config{})

	if _, err := h.peer.conn.Write([]byte{0xAA}); err != nil {
		t.Fatalf("write junk: %v", err)
	}
	h.peer.write(codec.Frame{
		Dst:  codec.Broadcast(),
		Src:  peerAddr,
		Type: codec.MsgIam,
	})

	select {
	case f := <-h.link.Unsolicited():
		if f.Type != codec.MsgIam {
			t.Errorf("= %s, want IAM after the resync", f.Type)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("the reader did not recover from one junk byte")
	}

	if _, _, resyncs := h.link.Stats(); resyncs != 1 {
		t.Errorf("resyncs = %d, want 1", resyncs)
	}
}

// TestLink_UnsolicitedIsLossy covers a caller that never reads announcements.
// They repeat every few seconds, so dropping one costs nothing; stalling the
// read loop would stop every session on the link.
func TestLink_UnsolicitedIsLossy(t *testing.T) {
	h := newHarness(t, Config{})

	for range 100 {
		h.peer.write(codec.Frame{Dst: codec.Broadcast(), Src: peerAddr, Type: codec.MsgIam})
	}

	// The link is still working: a session opens normally.
	s := h.callSession(t, codec.SvcMenus)
	if s == nil {
		t.Fatal("the link stalled on undrained announcements")
	}
}

// TestLink_BlindReplyMatching covers traffic sent outside any session. There is
// no index to correlate by, so replies are matched head-of-queue, which is
// safe precisely because the one-in-flight rule allows only one such request
// at a time.
func TestLink_BlindReplyMatching(t *testing.T) {
	h := newHarness(t, Config{})

	done := make(chan error, 1)
	go func() {
		_, err := h.link.Handshake(context.Background())
		done <- err
	}()
	req := h.peer.recvType(codec.MsgGetDevInfo)

	// A frame that is not a valid blind reply must not be matched to it.
	h.peer.send(req, codec.MsgGetStat, nil)
	select {
	case err := <-done:
		t.Fatalf("a request type was matched as a reply: %v", err)
	case <-time.After(100 * time.Millisecond):
	}

	info, err := codec.DeviceInfo{Address: peerAddr}.AppendTo(nil)
	if err != nil {
		t.Fatalf("AppendTo: %v", err)
	}
	h.peer.send(req, codec.MsgRetDevInfo, info)
	if err := <-done; err != nil {
		t.Fatalf("Handshake: %v", err)
	}
}

func TestNewLink_DefaultsAreUsable(t *testing.T) {
	ours, theirs := net.Pipe()
	t.Cleanup(func() { _ = theirs.Close() })

	// An empty dependency set is filled in rather than panicking, so a caller
	// that only cares about one dependency still gets a working link.
	l := NewLink(ours, Config{}, deps.Deps{})
	t.Cleanup(func() { _ = l.Close() })

	if l.cfg.replyTimeout() != DefaultReplyTimeout {
		t.Errorf("reply timeout = %s, want the default", l.cfg.replyTimeout())
	}
	if l.cfg.maxStrikes() != DefaultMaxStrikes {
		t.Errorf("max strikes = %d, want the default", l.cfg.maxStrikes())
	}
	if l.cfg.pushQueue() != DefaultPushQueue {
		t.Errorf("push queue = %d, want the default", l.cfg.pushQueue())
	}
	if l.cfg.keepaliveInterval() != DefaultKeepaliveInterval {
		t.Errorf("keepalive interval = %s, want the default", l.cfg.keepaliveInterval())
	}
	if l.cfg.maxKeepaliveMisses() != DefaultMaxKeepaliveMisses {
		t.Errorf("max misses = %d, want the default", l.cfg.maxKeepaliveMisses())
	}
}

// TestConfig_KeepaliveCanBeDisabled covers the difference between "unset" and
// "off". Zero takes the default; negative turns probing off, which is only
// right for a link whose peer pushes continuously.
func TestConfig_KeepaliveCanBeDisabled(t *testing.T) {
	if got := (Config{}).keepaliveInterval(); got != DefaultKeepaliveInterval {
		t.Errorf("unset = %s, want the default", got)
	}
	if got := (Config{KeepaliveInterval: -1}).keepaliveInterval(); got != 0 {
		t.Errorf("negative = %s, want probing disabled", got)
	}
	if got := (Config{KeepaliveInterval: time.Second}).keepaliveInterval(); got != time.Second {
		t.Errorf("explicit = %s, want one second", got)
	}
}

// TestConfig_LocalAddress covers a provider that knows its own address, as
// opposed to a client that is assigned one.
func TestConfig_LocalAddress(t *testing.T) {
	h := newHarness(t, Config{Local: Address{Net: 0x1000, Unit: 0x41, Port: 0x02}})

	got := h.link.LocalAddress()
	if got.Net != 0x1000 || got.Unit != 0x41 || got.Port != 0x02 {
		t.Errorf("local address = %s, want 1000-41-02", got)
	}
	if got.Index != codec.IndexUnknown {
		t.Errorf("index = %d, want %d before any session", got.Index, codec.IndexUnknown)
	}
}

func TestTracingEveryFrame(t *testing.T) {
	// A session log names the services a client negotiated and says nothing
	// about what was then said on them, which is enough to see that a client
	// is slow and never enough to see why. The trace is the view that is, and
	// it is off unless the log is turned up to ask for it.
	var buf bytes.Buffer
	lvl := &slog.LevelVar{}
	lvl.Set(LevelTrace)

	ours, theirs := net.Pipe()
	t.Cleanup(func() { _ = theirs.Close() })

	l := NewLink(ours, Config{}, deps.Deps{
		Logger: slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: lvl})),
	})
	t.Cleanup(func() { _ = l.Close() })

	go func() {
		buf := make([]byte, 512)
		for {
			if _, err := theirs.Read(buf); err != nil {
				return
			}
		}
	}()

	if err := l.send(codec.Frame{Type: codec.MsgKeepAlive}); err != nil {
		t.Fatalf("send: %v", err)
	}
	if !strings.Contains(buf.String(), "rollcall: frame") {
		t.Error("nothing was traced at the trace level")
	}
	if !strings.Contains(buf.String(), "dir=tx") {
		t.Error("a sent frame was not traced")
	}

	// And silent at debug, because one line per frame is thousands of lines
	// for a single walk of a large node.
	buf.Reset()
	lvl.Set(slog.LevelDebug)
	if err := l.send(codec.Frame{Type: codec.MsgKeepAlive}); err != nil {
		t.Fatalf("send: %v", err)
	}
	if strings.Contains(buf.String(), "rollcall: frame") {
		t.Error("frames were traced at debug level")
	}
}
