package rollcall

import (
	"context"
	"net"
	"strings"
	"testing"
	"time"

	"dhs/internal/clock"
	"dhs/internal/snell-rollcall/codec"
	"dhs/internal/snell-rollcall/session"
)

// The relay's address transforms, pinned to the vendor library's own (IPShare.c
// ipshSendMessage and the receive path, IPShClient.c): what a frame sees of a
// client's message, and what a client sees of the frame's.
func TestRelayTransforms(t *testing.T) {
	for _, tc := range []struct {
		name   string
		subnet uint16
		// A client's frame, and what reaches the frame.
		clientDst, clientSrc codec.Address
		wantDst, wantSrc     codec.Address
	}{
		{
			name:      "two hops: the route is consumed and the source zeroed",
			subnet:    0x2100,
			clientDst: codec.Address{Net: 0x2100, Unit: 0x0C, Port: 0x01, Index: 5},
			clientSrc: codec.Address{Unit: 0xFF, Port: 0xE0, Index: 3},
			wantDst:   codec.Address{Unit: 0x0C, Port: 0x01, Index: 5},
			wantSrc:   codec.Address{Index: 3},
		},
		{
			name:      "one hop",
			subnet:    0x3000,
			clientDst: codec.Address{Net: 0x3000, Unit: 0x0C, Index: codec.IndexUnknown},
			clientSrc: codec.Address{Unit: 0xFF, Port: 0xE0, Index: 1},
			wantDst:   codec.Address{Unit: 0x0C, Index: codec.IndexUnknown},
			wantSrc:   codec.Address{Index: 1},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tp, err := buildProxyTopology(ProxyConfig{Unit: 0xFF, Subnet: tc.subnet, Frame: 0x0C})
			if err != nil {
				t.Fatalf("buildProxyTopology: %v", err)
			}
			out := tp.outbound(codec.Frame{Dst: tc.clientDst, Src: tc.clientSrc, Type: codec.MsgGetID})
			if out.Dst != tc.wantDst {
				t.Errorf("outbound dst = %s, want %s", out.Dst, tc.wantDst)
			}
			if out.Src != tc.wantSrc {
				t.Errorf("outbound src = %s, want %s", out.Src, tc.wantSrc)
			}
		})
	}
}

func TestRelayInboundComposesTheRouteAndAddressesTheClient(t *testing.T) {
	tp, err := buildProxyTopology(ProxyConfig{Unit: 0xFF, Subnet: 0x2100, Frame: 0x0C})
	if err != nil {
		t.Fatalf("buildProxyTopology: %v", err)
	}
	// A client of the proxy is the zero address: the vendor proxy assigns none
	// and neither does this one.
	client := codec.Address{}

	// A reply: the frame zeroes the destination device and answers from its own
	// net-zero address. The client sees it from the subnet route, to itself,
	// with both session indices as they were.
	in := tp.inbound(codec.Frame{
		Dst:  codec.Address{Index: 3},
		Src:  codec.Address{Unit: 0x0C, Port: 0x01, Index: 7},
		Type: codec.MsgRetID,
	}, client)
	if want := (codec.Address{Net: 0x2100, Unit: 0x0C, Port: 0x01, Index: 7}); in.Src != want {
		t.Errorf("inbound src = %s, want %s", in.Src, want)
	}
	if want := (codec.Address{Index: 3}); in.Dst != want {
		t.Errorf("inbound dst = %s, want %s", in.Dst, want)
	}

	// A frame that echoes the spoofed source instead of zeroing is addressed
	// the same way: whatever it wrote, the client's address goes back in.
	in = tp.inbound(codec.Frame{
		Dst:  codec.Address{Unit: 0x0C, Port: 0x8E, Index: 3},
		Src:  codec.Address{Unit: 0x0C, Index: codec.IndexUnknown},
		Type: codec.MsgRetDevInfo,
	}, client)
	if want := (codec.Address{Index: 3}); in.Dst != want {
		t.Errorf("inbound dst (echoed) = %s, want %s", in.Dst, want)
	}

	// A broadcast keeps the broadcast address: that is how a client knows it
	// for an announcement.
	in = tp.inbound(codec.Frame{
		Dst:  codec.Broadcast(),
		Src:  codec.Address{Unit: 0x0C, Index: codec.IndexUnknown},
		Type: codec.MsgIam,
	}, client)
	if in.Dst != codec.Broadcast() {
		t.Errorf("a relayed announcement is addressed to %s, want the broadcast address", in.Dst)
	}
	if in.Src.Net != 0x2100 {
		t.Errorf("a relayed announcement comes from %s, want the subnet route", in.Src)
	}

	// A device behind the frame's own bridge keeps its route beneath ours.
	in = tp.inbound(codec.Frame{
		Dst:  codec.Address{Index: 1},
		Src:  codec.Address{Net: 0x1000, Unit: 0x20, Index: 2},
		Type: codec.MsgRetID,
	}, client)
	if in.Src.Net != 0x2110 {
		t.Errorf("a deeper source came through as %s, want route 2110", in.Src)
	}
}

// serveFrame serves the test tree directly as a frame of its own unit on a
// port the system picks, the way the real IQ frame answers at 0000-0C.
func serveFrame(t *testing.T, unit uint8) (*Provider, string) {
	t.Helper()
	p := New(testDeps(clock.NewFake(time.Time{})), testTree())
	p.SetUnit(unit)
	errs := make(chan error, 1)
	go func() { errs <- p.Serve(context.Background(), "127.0.0.1:0") }()
	t.Cleanup(func() {
		_ = p.Stop()
		<-errs
	})
	deadline := time.Now().Add(5 * time.Second)
	for p.Addr() == "" {
		if time.Now().After(deadline) {
			t.Fatal("the frame never bound")
		}
		time.Sleep(5 * time.Millisecond)
	}
	return p, p.Addr()
}

// newRelayServed fronts a served frame behind our proxy at subnet 2100, with
// one client connected and handshaken.
func newRelayServed(t *testing.T) (*served, *Provider) {
	t.Helper()
	frame, addr := serveFrame(t, 0x0C)

	clk := clock.NewFake(time.Time{})
	deps := testDeps(clk)
	// No tree of its own: what it fronts is the frame.
	p := New(deps, nil)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := p.SetProxy(ctx, ProxyConfig{Unit: 0xFF, Subnet: 0x2100, Upstream: addr}); err != nil {
		t.Fatalf("SetProxy: %v", err)
	}

	ours, theirs := net.Pipe()
	p.serveConn(theirs)
	cl := session.NewLink(ours, session.Config{}, deps)
	info, err := cl.Handshake(ctx)
	if err != nil {
		t.Fatalf("handshake: %v", err)
	}
	s := &served{t: t, p: p, cl: cl, info: info, clk: clk}
	t.Cleanup(func() {
		_ = cl.Close()
		_ = p.Stop()
	})
	return s, frame
}

func TestRelayLearnsTheFrameFromTheProbe(t *testing.T) {
	s, frame := newRelayServed(t)
	if s.p.frameUnit != 0x0C {
		t.Errorf("the frame unit was learned as %02X, want 0C from the probe", s.p.frameUnit)
	}
	gw, _ := frame.identityOf(0)
	if s.p.frameIdentity().TypeID != gw.TypeID || s.p.frameIdentity().Name != gw.Name {
		t.Errorf("the far side identity is %+v, want the frame's %+v", s.p.frameIdentity(), gw)
	}

	// The last virtual node lists the real frame's gateway as its far side.
	sess, err := s.openAddr(codec.Address{Net: 0x2000, Unit: 0x01, Index: codec.IndexUnknown}, codec.SvcNet)
	if err != nil {
		t.Fatalf("open net session to the last node: %v", err)
	}
	far := walkDevices(t, sess)
	if len(far) != 1 || far[0].Address.Net != 0x2100 || far[0].Address.Unit != 0x0C || far[0].ID.TypeID != gw.TypeID {
		t.Errorf("the far side is %+v, want the frame gateway routed at 2100-0C", far)
	}
	if far[0].Status != frame.statusOf(0) {
		t.Errorf("the far side status is %v, want the frame's own %v", far[0].Status, frame.statusOf(0))
	}
}

func TestRelayCarriesASessionToTheFrame(t *testing.T) {
	s, frame := newRelayServed(t)

	// A session to the frame's gateway at the subnet route is opened on the
	// frame itself: the acknowledgement comes back from the routed address, and
	// the port list is the frame's own.
	gw, err := s.openAddr(codec.Address{Net: 0x2100, Unit: 0x0C, Index: codec.IndexUnknown}, codec.SvcPorts)
	if err != nil {
		t.Fatalf("open gateway session through the relay: %v", err)
	}
	if got := gw.Peer().Device(); got != (codec.Address{Net: 0x2100, Unit: 0x0C, Index: codec.IndexUnknown}) {
		t.Errorf("the session peer is %s, want 2100-0C-00", got)
	}
	ports := walkDevices(t, gw)
	want := 1 + len(frame.model.portNumbers())
	if len(ports) != want {
		t.Fatalf("the frame listed %d ports through the relay, want %d", len(ports), want)
	}
	for _, d := range ports {
		if d.Address.Net != 0 || d.Address.Unit != 0x0C {
			t.Errorf("port entry at %s, want the frame's own net-zero 0000-0C", d.Address)
		}
	}

	// A card, at its port on the route: its menu is the frame's.
	cardPort := frame.model.portNumbers()[0]
	card, err := s.openAddr(codec.Address{Net: 0x2100, Unit: 0x0C, Port: cardPort, Index: codec.IndexUnknown},
		codec.SvcMenus|codec.SvcControl)
	if err != nil {
		t.Fatalf("open card session through the relay: %v", err)
	}
	if reply := do(t, card, codec.MsgGetMenuCount, codec.MenuReq{}.AppendTo(nil)); reply.Type != codec.MsgRetMenuCount {
		t.Errorf("card menu count answered %s", reply.Type)
	}

	// The frame saw one client for this one, on a connection of its own.
	if n := linkCount(frame); n != 1 {
		t.Errorf("the frame holds %d connections, want the relay's one", n)
	}
}

func TestRelayCarriesABlindRequest(t *testing.T) {
	s, frame := newRelayServed(t)

	// A card's identity asked outside any session, the way a Control Panel asks
	// when a node is selected: answered by the frame, relayed back from the
	// routed address to the client's own.
	cardPort := frame.model.portNumbers()[0]
	req := codec.Frame{
		Dst:  codec.Address{Net: 0x2100, Unit: 0x0C, Port: cardPort, Index: codec.IndexUnknown},
		Src:  codec.Address{Unit: s.cl.LocalAddress().Unit, Port: s.cl.LocalAddress().Port, Index: codec.IndexUnknown},
		Type: codec.MsgGetID,
	}
	if err := s.cl.SendFrame(req); err != nil {
		t.Fatalf("send: %v", err)
	}
	// The frame's own announcements cross the relay too, ahead of the answer.
	var reply codec.Frame
	deadline := time.After(5 * time.Second)
	for reply.Type == 0 || reply.Type.Broadcastable() {
		select {
		case reply = <-s.cl.Unsolicited():
		case <-deadline:
			t.Fatal("no answer through the relay")
		}
	}
	{
		if reply.Type != codec.MsgRetID {
			t.Fatalf("answered %s, want RetID", reply.Type)
		}
		if reply.Src.Device() != req.Dst.Device() {
			t.Errorf("the answer comes from %s, want the card's routed address %s", reply.Src, req.Dst)
		}
		if reply.Dst.Device() != s.cl.LocalAddress().Device() {
			t.Errorf("the answer is addressed to %s, want the client's own %s", reply.Dst, s.cl.LocalAddress())
		}
		id, err := codec.DecodeID(reply.Payload)
		if err != nil {
			t.Fatalf("decode: %v", err)
		}
		want, _ := frame.identityOf(cardPort)
		if id.TypeID != want.TypeID {
			t.Errorf("the card answered as type %d, want %d", id.TypeID, want.TypeID)
		}
	}
}

func TestRelayRefusesWhenTheFrameIsGone(t *testing.T) {
	s, frame := newRelayServed(t)
	_ = frame.Stop()

	// The frame stopped: a session asked of it is refused now, with a reason,
	// rather than left to time out.
	start := time.Now()
	_, err := s.openAddr(codec.Address{Net: 0x2100, Unit: 0x0C, Index: codec.IndexUnknown}, codec.SvcPorts)
	if err == nil {
		t.Fatal("a session was opened on a frame that is not there")
	}
	if time.Since(start) > 4*time.Second {
		t.Errorf("the refusal took %s, want a Nack rather than a timeout", time.Since(start))
	}

	// The proxy itself still answers.
	if _, err := s.openAddr(codec.Address{Unit: 0xFF, Index: codec.IndexUnknown}, codec.SvcMap); err != nil {
		t.Errorf("the proxy stopped answering with its frame gone: %v", err)
	}
}

func TestRelayClosesWithItsClient(t *testing.T) {
	s, frame := newRelayServed(t)
	if _, err := s.openAddr(codec.Address{Net: 0x2100, Unit: 0x0C, Index: codec.IndexUnknown}, codec.SvcPorts); err != nil {
		t.Fatalf("open gateway session through the relay: %v", err)
	}
	if n := linkCount(frame); n != 1 {
		t.Fatalf("the frame holds %d connections, want 1", n)
	}

	// The client goes: the relay's connection to the frame goes with it, which
	// is what gives the frame its slot back.
	_ = s.cl.Close()
	deadline := time.Now().Add(5 * time.Second)
	for linkCount(frame) != 0 {
		if time.Now().After(deadline) {
			t.Fatalf("the frame still holds %d connections after the client left", linkCount(frame))
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestSetProxyRefusesAnUnreachableFrame(t *testing.T) {
	// A port nothing listens on.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	addr := ln.Addr().String()
	_ = ln.Close()

	p := New(testDeps(clock.NewFake(time.Time{})), nil)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := p.SetProxy(ctx, ProxyConfig{Unit: 0xFF, Subnet: 0x2100, Upstream: addr}); err == nil {
		t.Error("a proxy was configured in front of a frame that does not answer")
	}
}

// linkCount is how many client connections a provider holds.
func linkCount(p *Provider) int {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return len(p.links)
}

// The relay's edges: what it does when the frame, the client or the connection
// between them is not where it was.

func TestSetProxyRefusesAnUpstreamOnABadRoute(t *testing.T) {
	_, addr := serveFrame(t, 0x0C)
	p := New(testDeps(clock.NewFake(time.Time{})), nil)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	// The probe answers; the route it would be served at is malformed.
	if err := p.SetProxy(ctx, ProxyConfig{Unit: 0xFF, Subnet: 0x1010, Upstream: addr}); err == nil {
		t.Error("a proxy was configured at a route that is not one")
	}
}

func TestSetProxyRefusesAFrameThatAcceptsAndSaysNothing(t *testing.T) {
	// The wedged frame: TCP accepts, RollCall never answers. Here the accept
	// closes at once, which ends the handshake the same way without the wait.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			_ = c.Close()
		}
	}()

	p := New(testDeps(clock.NewFake(time.Time{})), nil)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := p.SetProxy(ctx, ProxyConfig{Unit: 0xFF, Subnet: 0x2100, Upstream: ln.Addr().String()}); err == nil {
		t.Error("a proxy was configured in front of a frame that never answered")
	}
}

// relayOf is the relay serving the one client a served proxy has.
func relayOf(t *testing.T, p *Provider) (*relayLink, *session.Link) {
	t.Helper()
	p.mu.RLock()
	defer p.mu.RUnlock()
	for l, st := range p.links {
		if st.relay != nil {
			return st.relay, l
		}
	}
	t.Fatal("the proxy has no relaying client")
	return nil, nil
}

func TestRelayCopesWithAClientThatIsNotThere(t *testing.T) {
	s, _ := newRelayServed(t)

	// A frame from the frame before any client frame set the relay's client.
	r := &relayLink{p: s.p, addr: s.p.relay}
	if !r.fromFrame(nil, codec.Frame{Type: codec.MsgIam}) {
		t.Error("a frame with no client to go to was not taken")
	}
}

func TestRelayReportsAFrameItCannotWriteToTheClient(t *testing.T) {
	s, _ := newRelayServed(t)
	if _, err := s.openAddr(codec.Address{Net: 0x2100, Unit: 0x0C, Index: codec.IndexUnknown}, codec.SvcPorts); err != nil {
		t.Fatalf("open gateway session through the relay: %v", err)
	}
	r, _ := relayOf(t, s.p)

	// A frame too large to encode cannot be written; the relay logs and goes on.
	if !r.fromFrame(nil, codec.Frame{Type: codec.MsgRetID, Payload: make([]byte, codec.MaxPayload+1)}) {
		t.Error("an unwritable frame was not taken")
	}
	// The client is still served.
	if _, err := s.openAddr(codec.Address{Unit: 0xFF, Index: codec.IndexUnknown}, codec.SvcMap); err != nil {
		t.Errorf("the proxy stopped answering: %v", err)
	}
}

// deadWriteConn fails every write and blocks every read: a socket that has
// gone away without our end having noticed.
type deadWriteConn struct{ net.Conn }

func (deadWriteConn) Write([]byte) (int, error) { return 0, net.ErrClosed }

func TestRelayRefusesWhenTheFrameConnectionBreaks(t *testing.T) {
	s, _ := newRelayServed(t)
	if _, err := s.openAddr(codec.Address{Net: 0x2100, Unit: 0x0C, Index: codec.IndexUnknown}, codec.SvcPorts); err != nil {
		t.Fatalf("open gateway session through the relay: %v", err)
	}
	r, down := relayOf(t, s.p)

	// Swap the frame connection for one whose writes fail.
	ours, theirs := net.Pipe()
	t.Cleanup(func() { _ = ours.Close(); _ = theirs.Close() })
	broken := session.NewLink(deadWriteConn{ours}, session.Config{KeepaliveInterval: -1}, s.p.deps)
	t.Cleanup(func() { _ = broken.Close() })
	r.mu.Lock()
	old := r.up
	r.up = broken
	r.mu.Unlock()
	t.Cleanup(func() { _ = old.Close() })

	req := codec.Frame{
		Dst:  codec.Address{Net: 0x2100, Unit: 0x0C, Port: 0x01, Index: codec.IndexUnknown},
		Src:  codec.Address{Unit: s.cl.LocalAddress().Unit, Port: s.cl.LocalAddress().Port, Index: codec.IndexUnknown},
		Type: codec.MsgGetID,
	}
	r.toFrame(down, req)

	// A broadcast that cannot be carried is not answered; the request after it
	// is, so the first refusal the client sees names the request.
	r.refuse(down, codec.Frame{Dst: req.Dst, Src: req.Src, Type: codec.MsgIam}, "iam")
	r.refuse(down, req, "getid")

	var seen []string
	deadline := time.After(5 * time.Second)
	for len(seen) < 2 {
		select {
		case f := <-s.cl.Unsolicited():
			if f.Type == codec.MsgNack {
				seen = append(seen, strings.TrimRight(string(f.Payload), "\x00"))
			}
		case <-deadline:
			t.Fatalf("saw refusals %v, want two", seen)
		}
	}
	if seen[0] != "frame connection lost" || seen[1] != "getid" {
		t.Errorf("refusals = %v, want the broken connection then the request, never the broadcast", seen)
	}
}

func TestRelayRedialsAFrameThatDropped(t *testing.T) {
	s, frame := newRelayServed(t)

	// A frame connection that has already ended is replaced on the next request.
	ours, theirs := net.Pipe()
	_ = theirs.Close()
	dead := session.NewLink(ours, session.Config{KeepaliveInterval: -1}, s.p.deps)
	<-dead.Done()
	r := &relayLink{p: s.p, addr: s.p.relay, up: dead}
	up, err := r.upstream()
	if err != nil {
		t.Fatalf("redial: %v", err)
	}
	if up == dead {
		t.Error("the dead connection was handed back")
	}
	r.close()

	// Closed, a routed frame is dropped rather than refused: the client whose
	// last frames these are is gone.
	_, down := relayOf(t, s.p)
	r.toFrame(down, codec.Frame{Type: codec.MsgTerm})

	// And the relay forgets a connection the frame ends, so the next request
	// dials again rather than writing into a closed one.
	if _, err := s.openAddr(codec.Address{Net: 0x2100, Unit: 0x0C, Index: codec.IndexUnknown}, codec.SvcPorts); err != nil {
		t.Fatalf("open gateway session through the relay: %v", err)
	}
	rl, _ := relayOf(t, s.p)
	_ = frame.Stop()
	deadline := time.Now().Add(5 * time.Second)
	for {
		rl.mu.Lock()
		gone := rl.up == nil
		rl.mu.Unlock()
		if gone {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the relay kept a frame connection the frame had ended")
		}
		time.Sleep(10 * time.Millisecond)
	}
}
