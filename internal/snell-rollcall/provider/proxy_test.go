package rollcall

import (
	"context"
	"net"
	"testing"
	"time"

	"dhs/internal/clock"
	"dhs/internal/snell-rollcall/codec"
	"dhs/internal/snell-rollcall/session"
)

// A proxy fronts the test frame at subnet 0x1100, unit 0x0C, presenting as unit
// 0xFF — the addresses measured behind the vendor RollProxy.
func testProxyConfig() ProxyConfig {
	return ProxyConfig{Unit: 0xFF, Subnet: 0x1100, Frame: 0x0C}
}

func TestRouteNibbles(t *testing.T) {
	for _, tc := range []struct {
		net  uint16
		want []uint8
	}{
		{0x0000, nil},
		{0x1100, []uint8{1, 1}},
		{0x2340, []uint8{2, 3, 4}},
		{0x1234, []uint8{1, 2, 3, 4}},
	} {
		got := routeNibbles(tc.net)
		if len(got) != len(tc.want) {
			t.Errorf("routeNibbles(%04X) = %v, want %v", tc.net, got, tc.want)
			continue
		}
		for i := range got {
			if got[i] != tc.want[i] {
				t.Errorf("routeNibbles(%04X) = %v, want %v", tc.net, got, tc.want)
				break
			}
		}
	}
}

func TestBuildProxyTopology(t *testing.T) {
	tp, err := buildProxyTopology(testProxyConfig())
	if err != nil {
		t.Fatalf("buildProxyTopology: %v", err)
	}
	if len(tp.nodes) != 2 {
		t.Fatalf("%d virtual nodes, want 2 for subnet 1100", len(tp.nodes))
	}
	// Node 0 is listed at its own net-zero address and reached there; node 1 is
	// reached one hop out, its far side the frame gateway.
	if got := tp.nodes[0].dialed; got.Net != 0 || got.Unit != 0x01 {
		t.Errorf("node 0 dialed at %s, want 0000-01", got)
	}
	if got := tp.nodes[1].dialed; got.Net != 0x1000 || got.Unit != 0x01 {
		t.Errorf("node 1 dialed at %s, want 1000-01", got)
	}
	if got := tp.nodes[1].far; got.Net != 0 || got.Unit != 0x0C {
		t.Errorf("node 1 far side at %s, want the gateway local 0000-0C", got)
	}
}

func TestBuildProxyTopologyRejects(t *testing.T) {
	for _, tc := range []struct {
		name string
		cfg  ProxyConfig
	}{
		{"no route", ProxyConfig{Unit: 0xFF, Subnet: 0x0000, Frame: 0x0C}},
		{"invalid route", ProxyConfig{Unit: 0xFF, Subnet: 0x1010, Frame: 0x0C}},
		{"zero proxy unit", ProxyConfig{Unit: 0x00, Subnet: 0x1100, Frame: 0x0C}},
		{"zero frame unit", ProxyConfig{Unit: 0xFF, Subnet: 0x1100, Frame: 0x00}},
		{"frame is the proxy", ProxyConfig{Unit: 0x0C, Subnet: 0x1100, Frame: 0x0C}},
		{"proxy unit is a hop", ProxyConfig{Unit: 0x01, Subnet: 0x1100, Frame: 0x0C}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := buildProxyTopology(tc.cfg); err == nil {
				t.Errorf("%s was accepted", tc.name)
			}
		})
	}
}

func TestProxyResolve(t *testing.T) {
	tp, err := buildProxyTopology(testProxyConfig())
	if err != nil {
		t.Fatalf("buildProxyTopology: %v", err)
	}
	for _, tc := range []struct {
		name string
		dst  codec.Address
		role proxyRole
		port uint8
	}{
		{"proxy unit", codec.Address{Unit: 0xFF, Index: codec.IndexUnknown}, roleProxy, 0},
		{"virtual node 0", codec.Address{Unit: 0x01, Index: codec.IndexUnknown}, roleVirtual, 0},
		{"virtual node 1", codec.Address{Net: 0x1000, Unit: 0x01, Index: codec.IndexUnknown}, roleVirtual, 0},
		{"frame gateway", codec.Address{Net: 0x1100, Unit: 0x0C, Index: codec.IndexUnknown}, roleFrame, 0},
		{"a card", codec.Address{Net: 0x1100, Unit: 0x0C, Port: 0x03, Index: codec.IndexUnknown}, roleFrame, 0x03},
		{"unknown route", codec.Address{Net: 0x2200, Unit: 0x05, Index: codec.IndexUnknown}, roleNone, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			role, _, port, ok := tp.resolve(tc.dst)
			if role != tc.role {
				t.Errorf("resolve(%s) role = %d, want %d", tc.dst, role, tc.role)
			}
			if tc.role == roleNone && ok {
				t.Errorf("resolve(%s) reported ok for an unknown route", tc.dst)
			}
			if tc.role != roleNone && !ok {
				t.Errorf("resolve(%s) reported not-ok for a known node", tc.dst)
			}
			if role == roleFrame && port != tc.port {
				t.Errorf("resolve(%s) port = %02X, want %02X", tc.dst, port, tc.port)
			}
		})
	}
}

func TestServedAtAddr(t *testing.T) {
	p := New(testDeps(clock.NewFake(time.Time{})), testTree())
	// Base mode: by port, unchanged.
	if got := p.servedAtAddr(codec.Address{Unit: p.unit}); got != p.served() {
		t.Errorf("base mode servedAtAddr = %s, want %s", got, p.served())
	}

	if err := p.SetProxy(context.Background(), testProxyConfig()); err != nil {
		t.Fatalf("SetProxy: %v", err)
	}
	cases := []struct {
		name string
		dst  codec.Address
		want codec.Service
	}{
		{"proxy", codec.Address{Unit: 0xFF}, codec.SvcMap},
		{"virtual", codec.Address{Unit: 0x01}, codec.SvcNet},
		{"unknown", codec.Address{Net: 0x2200, Unit: 0x05}, 0},
	}
	for _, tc := range cases {
		if got := p.servedAtAddr(tc.dst); got != tc.want {
			t.Errorf("%s servedAtAddr = %s, want %s", tc.name, got, tc.want)
		}
	}
	// The frame gateway offers what the frame's port zero offers.
	if got := p.servedAtAddr(codec.Address{Net: 0x1100, Unit: 0x0C}); got != p.servedAt(0) {
		t.Errorf("frame gateway servedAtAddr = %s, want %s", got, p.servedAt(0))
	}
}

func TestSetProxyRejectedAfterServe(t *testing.T) {
	p := New(testDeps(clock.NewFake(time.Time{})), testTree())
	errs := make(chan error, 1)
	go func() { errs <- p.Serve(context.Background(), "127.0.0.1:0") }()
	t.Cleanup(func() {
		_ = p.Stop()
		<-errs
	})

	deadline := time.Now().Add(5 * time.Second)
	for p.Addr() == "" {
		if time.Now().After(deadline) {
			t.Fatal("the provider never bound")
		}
		time.Sleep(5 * time.Millisecond)
	}
	if err := p.SetProxy(context.Background(), testProxyConfig()); err == nil {
		t.Error("the proxy was configured after the frame was already served")
	}
}

// newProxyServed builds a provider fronting the test frame behind the proxy, with
// one client connected and handshaken.
func newProxyServed(t *testing.T) *served {
	t.Helper()

	clk := clock.NewFake(time.Time{})
	deps := testDeps(clk)
	p := New(deps, testTree())
	if err := p.SetProxy(context.Background(), testProxyConfig()); err != nil {
		t.Fatalf("SetProxy: %v", err)
	}

	ours, theirs := net.Pipe()
	p.serveConn(theirs)
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
	return s
}

// openAddr opens a session to a full address through the proxy.
func (s *served) openAddr(addr codec.Address, svc codec.Service) (*session.Session, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	sess, err := session.Call(ctx, s.cl, addr, svc, codec.LevelSupervisor, callerID())
	if err == nil {
		s.t.Cleanup(func() { _ = sess.Close() })
	}
	return sess, err
}

func TestProxyHandshakePresentsTheProxy(t *testing.T) {
	s := newProxyServed(t)
	if s.info.ID.TypeID != proxyTypeService {
		t.Errorf("the connected unit is type %d, want %d (RollProxy Service)", s.info.ID.TypeID, proxyTypeService)
	}
	if s.info.ID.Services != codec.SvcMap {
		t.Errorf("the proxy advertises %s, want just the map service", s.info.ID.Services)
	}
}

func TestProxyMapListsTheVirtualNode(t *testing.T) {
	s := newProxyServed(t)
	sess, err := s.openAddr(codec.Address{Unit: 0xFF, Index: codec.IndexUnknown}, codec.SvcMap)
	if err != nil {
		t.Fatalf("open map session: %v", err)
	}
	devs := walkDevices(t, sess)
	if len(devs) != 1 {
		t.Fatalf("the map listed %d nodes, want the one virtual node", len(devs))
	}
	if devs[0].ID.TypeID != proxyTypeNode || !devs[0].ID.Services.Has(codec.SvcNet) {
		t.Errorf("the map's node is type %d %s, want %d Net", devs[0].ID.TypeID, devs[0].ID.Services, proxyTypeNode)
	}
	if devs[0].Address.Net != 0 || devs[0].Address.Unit != 0x01 {
		t.Errorf("the map's node is at %s, want the net-zero 0000-01", devs[0].Address)
	}
}

func TestProxyVirtualNodeFarSides(t *testing.T) {
	s := newProxyServed(t)

	// Node 0's far side is the next virtual node, still net zero.
	sess, err := s.openAddr(codec.Address{Unit: 0x01, Index: codec.IndexUnknown}, codec.SvcNet)
	if err != nil {
		t.Fatalf("open net session to node 0: %v", err)
	}
	near := walkDevices(t, sess)
	if len(near) != 1 || near[0].ID.TypeID != proxyTypeNode {
		t.Fatalf("node 0 far side = %+v, want one virtual node", near)
	}

	// Node 1's far side is the frame gateway, carrying the frame's own identity.
	sess2, err := s.openAddr(codec.Address{Net: 0x1000, Unit: 0x01, Index: codec.IndexUnknown}, codec.SvcNet)
	if err != nil {
		t.Fatalf("open net session to node 1: %v", err)
	}
	far := walkDevices(t, sess2)
	if len(far) != 1 {
		t.Fatalf("node 1 far side listed %d, want the gateway", len(far))
	}
	gw, _ := s.p.identityOf(0)
	// Routed, as the vendor proxy lists the frame at its last hop.
	if far[0].Address.Net != 0x1100 || far[0].Address.Unit != 0x0C {
		t.Errorf("the gateway far entry is at %s, want the routed 1100-0C", far[0].Address)
	}
	// And the virtual node before it at net zero with index zero.
	if near[0].Address.Net != 0 || near[0].Address.Index != 0 {
		t.Errorf("the virtual node entry is at %s, want net zero with index 0", near[0].Address)
	}
	if far[0].ID.TypeID != gw.TypeID {
		t.Errorf("the gateway far entry is type %d, want the frame's %d", far[0].ID.TypeID, gw.TypeID)
	}
}

func TestProxyNodeIdentityAndStatus(t *testing.T) {
	s := newProxyServed(t)

	// The proxy unit, in a session: identity, status and its own device info.
	proxy, err := s.openAddr(codec.Address{Unit: 0xFF, Index: codec.IndexUnknown}, codec.SvcMap)
	if err != nil {
		t.Fatalf("open proxy session: %v", err)
	}
	id := decodeID(t, do(t, proxy, codec.MsgGetID, nil))
	if id.TypeID != proxyTypeService {
		t.Errorf("proxy GetID type %d, want %d", id.TypeID, proxyTypeService)
	}
	if reply := do(t, proxy, codec.MsgGetStat, nil); reply.Type != codec.MsgRetStat {
		t.Errorf("proxy GetStat answered %s", reply.Type)
	}
	if reply := do(t, proxy, codec.MsgGetDevInfo, nil); reply.Type != codec.MsgRetDevInfo {
		t.Errorf("proxy GetDevInfo answered %s", reply.Type)
	}
	if reply := do(t, proxy, codec.MsgKeepAlive, nil); reply.Type != codec.MsgAck {
		t.Errorf("proxy KeepAlive answered %s", reply.Type)
	}
	// A message the proxy does not implement is InvCmd.
	if reply := refused(t, proxy, codec.MsgGetValue, []byte{0, 0, 0, 1}); reply.Type != codec.MsgInvCmd {
		t.Errorf("proxy answered a value read with %s, want InvCmd", reply.Type)
	}
	// The back channel on a map session is acknowledged: a Control Panel enables
	// it after reading the map and gives up on InvCmd.
	if reply := do(t, proxy, codec.MsgBkChnReady, []byte{codec.BackChannelEnable}); reply.Type != codec.MsgAck {
		t.Errorf("proxy BkChnReady answered %s, want Ack", reply.Type)
	}
	if reply := do(t, proxy, codec.MsgRepFChg, []byte{0xFF, 0xFF}); reply.Type != codec.MsgAck {
		t.Errorf("proxy RepFChg answered %s, want Ack", reply.Type)
	}
	if reply := refused(t, proxy, codec.MsgBkChnReady, nil); reply.Type != codec.MsgNack {
		t.Errorf("proxy answered a malformed BkChnReady with %s, want Nack", reply.Type)
	}

	// A virtual node, in a session: identity, status, device info, keepalive and
	// the same InvCmd for what it does not serve.
	v, err := s.openAddr(codec.Address{Unit: 0x01, Index: codec.IndexUnknown}, codec.SvcNet)
	if err != nil {
		t.Fatalf("open virtual session: %v", err)
	}
	vid := decodeID(t, do(t, v, codec.MsgGetID, nil))
	if vid.TypeID != proxyTypeNode {
		t.Errorf("virtual GetID type %d, want %d", vid.TypeID, proxyTypeNode)
	}
	if reply := do(t, v, codec.MsgGetStat, nil); reply.Type != codec.MsgRetStat {
		t.Errorf("virtual GetStat answered %s", reply.Type)
	}
	if reply := do(t, v, codec.MsgGetDevInfo, nil); reply.Type != codec.MsgRetDevInfo {
		t.Errorf("virtual GetDevInfo answered %s", reply.Type)
	}
	if reply := do(t, v, codec.MsgKeepAlive, nil); reply.Type != codec.MsgAck {
		t.Errorf("virtual KeepAlive answered %s", reply.Type)
	}
	if reply := refused(t, v, codec.MsgGetValue, []byte{0, 0, 0, 1}); reply.Type != codec.MsgInvCmd {
		t.Errorf("virtual answered a value read with %s, want InvCmd", reply.Type)
	}
	if reply := do(t, v, codec.MsgBkChnReady, []byte{codec.BackChannelEnable}); reply.Type != codec.MsgAck {
		t.Errorf("virtual BkChnReady answered %s, want Ack", reply.Type)
	}
}

func TestFrameThroughProxy(t *testing.T) {
	s := newProxyServed(t)

	// The gateway, reached at the subnet route: its port list is the frame's own,
	// stamped at the frame unit and net zero so a client composes the route.
	gwSvc := s.p.servedAt(0)
	gw, err := s.openAddr(codec.Address{Net: 0x1100, Unit: 0x0C, Index: codec.IndexUnknown}, gwSvc&^codec.SvcLongStr)
	if err != nil {
		t.Fatalf("open gateway session: %v", err)
	}
	cards := walkDevices(t, gw)
	if len(cards) < 2 {
		t.Fatalf("the gateway listed %d ports, want the gateway and at least one card", len(cards))
	}
	for _, d := range cards {
		if d.Address.Net != 0 || d.Address.Unit != 0x0C {
			t.Errorf("port entry at %s, want net-zero on the frame unit 0000-0C", d.Address)
		}
	}

	// A card, reached at its port on the subnet route: its identity is the card's.
	cardPort := s.p.model.portNumbers()[0]
	card, err := s.openAddr(codec.Address{Net: 0x1100, Unit: 0x0C, Port: cardPort, Index: codec.IndexUnknown}, codec.SvcMenus|codec.SvcControl)
	if err != nil {
		t.Fatalf("open card session: %v", err)
	}
	if reply := do(t, card, codec.MsgGetID, nil); reply.Type != codec.MsgRetID {
		t.Errorf("card GetID answered %s", reply.Type)
	}
	if reply := do(t, card, codec.MsgGetMenuCount, codec.MenuReq{}.AppendTo(nil)); reply.Type != codec.MsgRetMenuCount {
		t.Errorf("card menu count answered %s", reply.Type)
	}
}

func TestABadRouteRefusesTheSession(t *testing.T) {
	s := newProxyServed(t)
	// An address the proxy fronts nothing at offers no service, so the session is
	// refused rather than opened onto a node that is not there.
	if _, err := s.openAddr(codec.Address{Net: 0x2200, Unit: 0x05, Index: codec.IndexUnknown}, codec.SvcMenus); err == nil {
		t.Error("a session opened on a route the proxy does not front")
	}
}

// walkDevices drives a device-list walk on a session and returns the entries.
func walkDevices(t *testing.T, s *session.Session) []codec.DeviceInfo {
	t.Helper()
	var out []codec.DeviceInfo
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	err := session.Walk(ctx, s, codec.MsgGetLocDevMap, nil, func(_ int, f codec.Frame) error {
		if f.Type != codec.MsgRetDevInfo {
			return nil
		}
		d, err := codec.DecodeDeviceInfo(f.Payload)
		if err != nil {
			return err
		}
		out = append(out, d)
		return nil
	})
	if err != nil {
		t.Fatalf("device-list walk: %v", err)
	}
	return out
}

// decodeID reads an identity reply.
func decodeID(t *testing.T, reply codec.Frame) codec.ID {
	t.Helper()
	if reply.Type != codec.MsgRetID {
		t.Fatalf("wanted an identity, got %s", reply.Type)
	}
	id, err := codec.DecodeID(reply.Payload)
	if err != nil {
		t.Fatalf("decode identity: %v", err)
	}
	return id
}
