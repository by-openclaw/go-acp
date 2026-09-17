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

// fakeController is a frame whose segment holds several units — a Centra, in
// shape: a controller at unit 08 whose map lists matrices and panels on units
// of their own. It answers the handshake, a map session and a map walk, and
// can be told to misbehave at each step, which is how the proxy's handling of
// a frame that will not list its map is reached.
type fakeController struct {
	t     *testing.T
	units []codec.DeviceInfo

	refuseMap bool // refuse the map session
	oddItem   bool // answer an item with something other than a device record
	badItem   bool // answer an item with a device record that does not decode

	ln net.Listener
}

func (f *fakeController) identity() codec.DeviceInfo {
	return codec.DeviceInfo{
		ProtocolVersion: codec.ProtocolVersion,
		Address:         codec.Address{Unit: 0x08, Index: codec.IndexUnknown},
		ID: codec.ID{
			Services: codec.SvcMenus | codec.SvcControl | codec.SvcFile | codec.SvcMap,
			TypeID:   605,
			Version:  codec.Version{Major: 3, Minor: 0, Alpha: ' ', CmdSet: 12},
			Name:     codec.TruncateFixed("<not set>", codec.MaxTextSize),
		},
		Status: codec.UnitStatus{Status: codec.StatusPresent},
	}
}

// serve listens on a port the system picks and answers every connection.
func (f *fakeController) serve() string {
	f.t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		f.t.Fatalf("listen: %v", err)
	}
	f.ln = ln
	f.t.Cleanup(func() { _ = ln.Close() })
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			l := session.NewLink(conn, session.Config{
				Handler:           f,
				Local:             session.Address{Unit: 0x08},
				KeepaliveInterval: -1,
			}, testDeps(clock.NewFake(time.Time{})))
			f.t.Cleanup(func() { _ = l.Close() })
		}
	}()
	return ln.Addr().String()
}

func (f *fakeController) Call(l *session.Link, req codec.Frame, conn codec.Connect) error {
	if f.refuseMap && conn.Services.Has(codec.SvcMap) {
		return session.RefuseNack("no map for you")
	}
	_, err := session.Accept(l, req, conn)
	return err
}

func (f *fakeController) Request(s *session.Session, req codec.Frame) {
	switch req.Type {
	case codec.MsgTerm:
		_ = s.Answer(codec.MsgAck, nil)
		s.Terminate(nil)
	case codec.MsgGetLocDevMap, codec.MsgGetDevList:
		hdr := codec.BlockHeader{PktType: req.Type, Count: uint16(len(f.units)), MaxSize: codec.MaxPayload}
		_ = s.Answer(codec.MsgBlockHeader, hdr.AppendTo(nil))
	case codec.MsgGetNextPkt:
		next, err := codec.DecodeGetNext(req.Payload)
		if err != nil || int(next.Index) >= len(f.units) {
			_ = s.Refuse(session.RefuseNack("no such item"))
			return
		}
		switch {
		case f.oddItem:
			_ = s.Answer(codec.MsgAck, nil)
		case f.badItem:
			_ = s.Answer(codec.MsgRetDevInfo, []byte{1, 2, 3})
		default:
			payload, _ := f.units[next.Index].AppendTo(nil)
			_ = s.Answer(codec.MsgRetDevInfo, payload)
		}
	default:
		_ = s.Answer(codec.MsgAck, nil)
	}
}

func (f *fakeController) Unsolicited(l *session.Link, req codec.Frame) {
	if req.Type != codec.MsgGetDevInfo {
		return
	}
	payload, _ := f.identity().AppendTo(nil)
	_ = l.SendFrame(codec.Frame{
		Dst:     req.Src,
		Src:     codec.Address{Unit: 0x08, Index: codec.IndexUnknown},
		Type:    codec.MsgRetDevInfo,
		Payload: payload,
	})
}

// centraUnits is the shape the Centra emulator measured on 2026-09-07 answers:
// the controller, two matrices, a tielines engine and a panel, each on its own
// unit, plus the controller's own client port, which a map must not list.
func centraUnits() []codec.DeviceInfo {
	unit := func(u uint8, port uint8, typeID uint16, name string, svc codec.Service) codec.DeviceInfo {
		return codec.DeviceInfo{
			ProtocolVersion: codec.ProtocolVersion,
			Address:         codec.Address{Unit: u, Port: port},
			ID:              codec.ID{Services: svc, TypeID: typeID, Name: codec.TruncateFixed(name, codec.MaxTextSize)},
			Status:          codec.UnitStatus{Status: codec.StatusPresent},
		}
	}
	return []codec.DeviceInfo{
		unit(0x08, 0, 605, "<not set>", codec.SvcMenus|codec.SvcControl|codec.SvcFile|codec.SvcMap),
		unit(0x11, 0, 636, "Matrix 1", codec.SvcMenus|codec.SvcControl|codec.SvcFile|codec.SvcPorts),
		unit(0x12, 0, 636, "Matrix 2", codec.SvcMenus|codec.SvcControl|codec.SvcFile|codec.SvcPorts),
		unit(0x80, 0, 640, "TIELINES", codec.SvcMenus|codec.SvcControl|codec.SvcFile),
		unit(0x81, 0, 641, "XY Panel", codec.SvcMenus|codec.SvcControl|codec.SvcFile),
		unit(0x08, 0x8E, 500, "ControlPanel", 0),
	}
}

// frontFake fronts a fake controller behind subnet 3000 with one client
// connected, and returns the served harness.
func frontFake(t *testing.T, f *fakeController) *served {
	t.Helper()
	addr := f.serve()

	clk := clock.NewFake(time.Time{})
	deps := testDeps(clk)
	p := New(deps, nil)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := p.SetProxy(ctx, ProxyConfig{Unit: 0xFF, Subnet: 0x3000, Upstream: addr}); err != nil {
		t.Fatalf("SetProxy: %v", err)
	}

	ours, theirs := net.Pipe()
	p.serveConn(theirs)
	cl := session.NewLink(ours, session.Config{}, deps)
	if _, err := cl.Handshake(ctx); err != nil {
		t.Fatalf("handshake: %v", err)
	}
	s := &served{t: t, p: p, cl: cl, clk: clk}
	t.Cleanup(func() {
		_ = cl.Close()
		_ = p.Stop()
	})
	return s
}

func TestTheFarSideIsTheFramesWholeSegment(t *testing.T) {
	f := &fakeController{t: t, units: centraUnits()}
	s := frontFake(t, f)

	// The last virtual node lists every unit of the controller's map, routed,
	// with each unit's own identity — and not the controller's client port.
	sess, err := s.openAddr(codec.Address{Unit: 0x03, Index: codec.IndexUnknown}, codec.SvcNet)
	if err != nil {
		t.Fatalf("open net session: %v", err)
	}
	far := walkDevices(t, sess)
	if len(far) != 5 {
		t.Fatalf("the far side lists %d units, want the controller's five: %+v", len(far), far)
	}
	for i, want := range []struct {
		unit uint8
		name string
	}{{0x08, "<not set>"}, {0x11, "Matrix 1"}, {0x12, "Matrix 2"}, {0x80, "TIELINES"}, {0x81, "XY Panel"}} {
		got := far[i]
		if got.Address.Net != 0x3000 || got.Address.Unit != want.unit || got.Address.Port != 0 {
			t.Errorf("entry %d at %s, want 3000-%02X-00", i, got.Address, want.unit)
		}
		if got.ID.Name != want.name {
			t.Errorf("entry %d named %q, want %q", i, got.ID.Name, want.name)
		}
	}

	// And a matrix on its own unit is reached through the relay.
	m, err := s.openAddr(codec.Address{Net: 0x3000, Unit: 0x11, Index: codec.IndexUnknown}, codec.SvcPorts)
	if err != nil {
		t.Fatalf("open the matrix through the relay: %v", err)
	}
	if reply := do(t, m, codec.MsgKeepAlive, nil); reply.Type != codec.MsgAck {
		t.Errorf("the matrix answered %s", reply.Type)
	}
}

func TestAFrameThatWillNotListItsMapIsItsGatewayAlone(t *testing.T) {
	for _, tc := range []struct {
		name string
		f    *fakeController
	}{
		{"map session refused", &fakeController{units: centraUnits(), refuseMap: true}},
		{"an item that is not a device record", &fakeController{units: centraUnits(), oddItem: true}},
		{"an item that does not decode", &fakeController{units: centraUnits(), badItem: true}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tc.f.t = t
			s := frontFake(t, tc.f)
			sess, err := s.openAddr(codec.Address{Unit: 0x03, Index: codec.IndexUnknown}, codec.SvcNet)
			if err != nil {
				t.Fatalf("open net session: %v", err)
			}
			// An item that is not a device record is skipped rather than fatal,
			// so that map came back empty; a refused session and an item that
			// does not decode fail the read. All three list the gateway alone.
			far := walkDevices(t, sess)
			if len(far) != 1 || far[0].Address.Unit != 0x08 || far[0].Address.Net != 0x3000 {
				t.Errorf("the far side lists %+v, want the gateway at 3000-08 alone", far)
			}
		})
	}
}
