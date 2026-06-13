package acp2

import (
	"context"
	"encoding/binary"
	"io"
	"log/slog"
	"net"
	"strconv"
	"sync"
	"testing"
	"time"

	"dhs/internal/acp2/codec"
	"dhs/internal/export/canonical"
)

// serve_loopback_test.go drives the production server.Serve accept loop
// over a real 127.0.0.1:0 TCP listener — the mirror of the consumer's
// loopback harness, but with OUR provider under test and a fake client.
//
// It exercises the otherwise 0%-covered server.go surface (newServer,
// Serve, Stop, registerSession, unregisterSession, broadcastAnnounce)
// plus the request handlers (handleGetProperty, handleSetProperty) end
// to end. Bytes are built/decoded with the production codec; no fixed
// sleeps gate any assert (waitFor polls), and every listener/conn is
// deferred-closed so no goroutine leaks.

// quietLogger discards all provider output so test runs stay clean.
func quietLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, &slog.HandlerOptions{Level: slog.LevelError}))
}

// waitFor polls cond until true or the deadline elapses. Keeps
// announce-delivery + session-registration assertions deterministic
// without a fixed sleep on the assert path.
func waitFor(t *testing.T, d time.Duration, cond func() bool) bool {
	t.Helper()
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		if cond() {
			return true
		}
		time.Sleep(time.Millisecond)
	}
	return cond()
}

// buildServeExport returns a canonical export with a single populated
// slot-1 subtree covering one of every leaf object type plus a node, so
// the loopback client can walk + get + set across the type matrix.
//
//	slot-1
//	└── ROOT_NODE_V2 (1, node)
//	    ├── Gain      (10, integer s32, RW, min=-10 max=10)
//	    ├── Mode      (11, enum Off/On/Auto, RW)
//	    ├── Address   (12, string ipv4, RW)
//	    ├── Name      (13, string maxLen=8, RW)
//	    └── Locked    (14, integer u8, R only)
func buildServeExport() *canonical.Export {
	str := func(s string) *string { return &s }
	gain := &canonical.Parameter{
		Header: canonical.Header{Number: 10, Identifier: "Gain",
			Access: canonical.AccessReadWrite, Children: canonical.EmptyChildren()},
		Type:    canonical.ParamInteger,
		Value:   int64(0),
		Minimum: int64(-10),
		Maximum: int64(10),
		Default: int64(0),
		Format:  str("s32"),
	}
	enumVal := "Off,On,Auto"
	mode := &canonical.Parameter{
		Header: canonical.Header{Number: 11, Identifier: "Mode",
			Access: canonical.AccessReadWrite, Children: canonical.EmptyChildren()},
		Type:        canonical.ParamEnum,
		Value:       int64(0),
		Enumeration: &enumVal,
		EnumMap: []canonical.EnumEntry{
			{Key: "Off", Value: 0},
			{Key: "On", Value: 1},
			{Key: "Auto", Value: 2},
		},
	}
	addr := &canonical.Parameter{
		Header: canonical.Header{Number: 12, Identifier: "Address",
			Access: canonical.AccessReadWrite, Children: canonical.EmptyChildren()},
		Type:   canonical.ParamString,
		Value:  "10.0.0.1",
		Format: str("ipv4"),
	}
	name := &canonical.Parameter{
		Header: canonical.Header{Number: 13, Identifier: "Name",
			Access: canonical.AccessReadWrite, Children: canonical.EmptyChildren()},
		Type:   canonical.ParamString,
		Value:  "card",
		Format: str("maxLen=8"),
	}
	locked := &canonical.Parameter{
		Header: canonical.Header{Number: 14, Identifier: "Locked",
			Access: canonical.AccessRead, Children: canonical.EmptyChildren()},
		Type:   canonical.ParamInteger,
		Value:  int64(1),
		Format: str("u8"),
	}
	root := &canonical.Node{Header: canonical.Header{
		Number: 1, Identifier: "ROOT_NODE_V2", Access: canonical.AccessRead,
		Children: []canonical.Element{gain, mode, addr, name, locked},
	}}
	slot1 := &canonical.Node{Header: canonical.Header{
		Number: 1, Identifier: "slot-1", Access: canonical.AccessRead,
		Children: []canonical.Element{root},
	}}
	return &canonical.Export{Root: &canonical.Node{Header: canonical.Header{
		Number: 1, Identifier: "device", Access: canonical.AccessRead,
		Children: []canonical.Element{slot1},
	}}}
}

// fakeClient is a minimal AN2/ACP2 client over a live TCP conn. It
// serialises writes, reads frames on a background goroutine, and routes
// ACP2 data frames into typed channels (replies vs announces).
type fakeClient struct {
	t    *testing.T
	conn net.Conn

	mu        sync.Mutex
	announces []*codec.ACP2Message
	replies   chan *codec.ACP2Message
	an2       chan *codec.AN2Frame
}

func dialFakeClient(t *testing.T, addr string) *fakeClient {
	t.Helper()
	conn, err := net.DialTimeout("tcp4", addr, 2*time.Second)
	if err != nil {
		t.Fatalf("dial %s: %v", addr, err)
	}
	c := &fakeClient{
		t:       t,
		conn:    conn,
		replies: make(chan *codec.ACP2Message, 16),
		an2:     make(chan *codec.AN2Frame, 16),
	}
	go c.readLoop()
	return c
}

func (c *fakeClient) close() { _ = c.conn.Close() }

func (c *fakeClient) readLoop() {
	for {
		f, err := codec.ReadAN2Frame(c.conn)
		if err != nil {
			return
		}
		switch f.Proto {
		case codec.AN2ProtoInternal:
			select {
			case c.an2 <- f:
			default:
			}
		case codec.AN2ProtoACP2:
			msg, err := codec.DecodeACP2Message(f.Payload)
			if err != nil {
				continue
			}
			// Parse obj-id/idx for reply + announce bodies so callers can
			// assert addressing (decoder leaves them set for these types).
			if msg.Type == codec.ACP2TypeAnnounce {
				c.mu.Lock()
				c.announces = append(c.announces, msg)
				c.mu.Unlock()
				continue
			}
			select {
			case c.replies <- msg:
			default:
			}
		}
	}
}

func (c *fakeClient) writeFrame(f *codec.AN2Frame) {
	raw, err := codec.EncodeAN2Frame(f)
	if err != nil {
		c.t.Fatalf("encode frame: %v", err)
	}
	if _, err := c.conn.Write(raw); err != nil {
		c.t.Fatalf("client write: %v", err)
	}
}

// an2Request sends an AN2 internal request and waits for its reply.
func (c *fakeClient) an2Request(slot, mtid uint8, payload []byte) *codec.AN2Frame {
	c.writeFrame(&codec.AN2Frame{
		Proto: codec.AN2ProtoInternal, Slot: slot, MTID: mtid,
		Type: codec.AN2TypeRequest, Payload: payload,
	})
	select {
	case f := <-c.an2:
		return f
	case <-time.After(time.Second):
		c.t.Fatal("timeout waiting for AN2 reply")
		return nil
	}
}

// handshake runs the standard AN2 bring-up so ACP2 traffic is accepted,
// optionally enabling ACP2 protocol events for announce delivery.
func (c *fakeClient) handshake(enableEvents bool) {
	c.an2Request(0, 1, []byte{codec.AN2FuncGetVersion})
	c.an2Request(0, 2, []byte{codec.AN2FuncGetDeviceInfo})
	c.an2Request(1, 3, []byte{codec.AN2FuncGetSlotInfo})
	if enableEvents {
		rep := c.an2Request(1, 4, []byte{codec.AN2FuncEnableProtocolEvents, 1, uint8(codec.AN2ProtoACP2)})
		if len(rep.Payload) != 1 || rep.Payload[0] != codec.AN2FuncEnableProtocolEvents {
			c.t.Fatalf("EnableProtocolEvents reply=%x", rep.Payload)
		}
	}
}

// acp2Request sends one ACP2 request frame and waits for its reply.
func (c *fakeClient) acp2Request(slot uint8, msg *codec.ACP2Message) *codec.ACP2Message {
	body, err := codec.EncodeACP2Message(msg)
	if err != nil {
		c.t.Fatalf("encode acp2: %v", err)
	}
	c.writeFrame(&codec.AN2Frame{
		Proto: codec.AN2ProtoACP2, Slot: slot, MTID: 0,
		Type: codec.AN2TypeData, Payload: body,
	})
	select {
	case m := <-c.replies:
		return m
	case <-time.After(time.Second):
		c.t.Fatal("timeout waiting for ACP2 reply")
		return nil
	}
}

func (c *fakeClient) announceCount() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.announces)
}

func (c *fakeClient) lastAnnounce() *codec.ACP2Message {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.announces) == 0 {
		return nil
	}
	return c.announces[len(c.announces)-1]
}

// startServe boots the provider on an ephemeral port and returns the
// server, the dial address, and a stop func that cancels Serve + Stops.
func startServe(t *testing.T, exp *canonical.Export) (*server, string, func()) {
	t.Helper()
	srv := newServer(quietLogger(), exp)

	// Bind ephemeral ourselves to read back the port, then hand the
	// listener to Serve via a tiny shim: Serve binds its own listener,
	// so instead we let Serve bind ":0" and discover the port from the
	// server's listener field once it's set.
	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() { errCh <- srv.Serve(ctx, "127.0.0.1:0") }()

	// Wait until Serve has published its listener.
	var addr string
	ok := waitFor(t, 2*time.Second, func() bool {
		srv.mu.Lock()
		defer srv.mu.Unlock()
		if srv.listener != nil {
			addr = srv.listener.Addr().String()
			return true
		}
		return false
	})
	if !ok {
		cancel()
		t.Fatal("Serve never bound a listener")
	}

	stop := func() {
		cancel()
		_ = srv.Stop()
		select {
		case <-errCh:
		case <-time.After(time.Second):
		}
	}
	return srv, addr, stop
}

func decodeProp(t *testing.T, msg *codec.ACP2Message, pid uint8) (codec.Property, bool) {
	t.Helper()
	// Reply body = obj-id(4) + idx(4) + property headers.
	if len(msg.Body) < 8 {
		return codec.Property{}, false
	}
	props, err := codec.DecodeProperties(msg.Body[8:])
	if err != nil {
		t.Fatalf("decode props: %v", err)
	}
	for _, p := range props {
		if p.PID == pid {
			return p, true
		}
	}
	return codec.Property{}, false
}

// TestServe_FullHandshakeAndWalk drives the whole accept loop: handshake,
// get_version, get_object on the root, get_property on a leaf — over a
// real TCP socket against the production Serve path.
func TestServe_FullHandshakeAndWalk(t *testing.T) {
	_, addr, stop := startServe(t, buildServeExport())
	defer stop()

	c := dialFakeClient(t, addr)
	defer c.close()
	c.handshake(false)

	// ACP2 get_version → reply pid byte = acp2Version (2).
	ver := c.acp2Request(1, &codec.ACP2Message{
		Type: codec.ACP2TypeRequest, MTID: 5, Func: codec.ACP2FuncGetVersion,
	})
	if ver.Type != codec.ACP2TypeReply || ver.PID != acp2Version {
		t.Fatalf("get_version reply type=%d pid=%d want reply/%d", ver.Type, ver.PID, acp2Version)
	}

	// get_object on root (obj-id 1) → must carry pid 14 children listing
	// the five leaves.
	obj := c.acp2Request(1, &codec.ACP2Message{
		Type: codec.ACP2TypeRequest, MTID: 6, Func: codec.ACP2FuncGetObject, ObjID: 1,
	})
	if obj.Func != codec.ACP2FuncGetObject {
		t.Fatalf("get_object func=%d", obj.Func)
	}
	ch, ok := decodeProp(t, obj, codec.PIDChildren)
	if !ok {
		t.Fatal("get_object root: no pid 14 children")
	}
	if len(ch.Data) != 5*4 {
		t.Fatalf("children data len=%d want 20 (5 child ids)", len(ch.Data))
	}

	// get_property pid 8 (value) on Gain (obj 10) → s32 = 0.
	gp := c.acp2Request(1, &codec.ACP2Message{
		Type: codec.ACP2TypeRequest, MTID: 7, Func: codec.ACP2FuncGetProperty,
		ObjID: 10, PID: codec.PIDValue,
	})
	if gp.Func != codec.ACP2FuncGetProperty {
		t.Fatalf("get_property func=%d", gp.Func)
	}
	if _, ok := decodeProp(t, gp, codec.PIDValue); !ok {
		t.Fatal("get_property: no pid 8 value")
	}
}

// TestServe_SetProperty_AnnounceFanout verifies set_property mutates the
// tree, replies with the confirmed post-state, and fans an announce out
// to a SECOND client that subscribed via EnableProtocolEvents — while a
// third, unsubscribed client receives nothing (spec gate).
func TestServe_SetProperty_AnnounceFanout(t *testing.T) {
	srv, addr, stop := startServe(t, buildServeExport())
	defer stop()

	setter := dialFakeClient(t, addr)
	defer setter.close()
	setter.handshake(false)

	listener := dialFakeClient(t, addr)
	defer listener.close()
	listener.handshake(true) // subscribes to ACP2 announces

	unsub := dialFakeClient(t, addr)
	defer unsub.close()
	unsub.handshake(false) // never subscribes

	// Wait until the server has registered all three sessions.
	if !waitFor(t, time.Second, func() bool {
		srv.mu.Lock()
		defer srv.mu.Unlock()
		return len(srv.sessions) == 3
	}) {
		t.Fatal("server never registered 3 sessions")
	}

	// set Gain (obj 10) to 7 (within [-10,10]).
	val := make([]byte, 4)
	binary.BigEndian.PutUint32(val, uint32(int32(7)))
	in := codec.Property{PID: codec.PIDValue, VType: uint8(codec.NumTypeS32), PLen: 8, Data: val}
	setMsg := &codec.ACP2Message{
		Type: codec.ACP2TypeRequest, MTID: 9, Func: codec.ACP2FuncSetProperty,
		ObjID: 10, PID: codec.PIDValue, Properties: []codec.Property{in},
	}
	rep := setter.acp2Request(1, setMsg)
	if rep.Func != codec.ACP2FuncSetProperty || rep.Type != codec.ACP2TypeReply {
		t.Fatalf("set reply type=%d func=%d", rep.Type, rep.Func)
	}
	post, ok := decodeProp(t, rep, codec.PIDValue)
	if !ok {
		t.Fatal("set reply: no pid 8")
	}
	iv, _, _, err := codec.DecodeNumericValue(codec.NumTypeS32, post.Data)
	if err != nil || iv != 7 {
		t.Fatalf("post value iv=%d err=%v want 7", iv, err)
	}

	// The subscribed listener must receive exactly one announce.
	if !waitFor(t, time.Second, func() bool { return listener.announceCount() >= 1 }) {
		t.Fatal("subscribed listener got no announce")
	}
	ann := listener.lastAnnounce()
	if ann.Type != codec.ACP2TypeAnnounce || ann.Func != 0 {
		t.Fatalf("announce type=%d stat=%d want announce/0", ann.Type, ann.Func)
	}
	if ann.PID != codec.PIDValue {
		t.Fatalf("announce pid=%d want %d", ann.PID, codec.PIDValue)
	}
	// The unsubscribed client must receive zero announces.
	if n := unsub.announceCount(); n != 0 {
		t.Fatalf("unsubscribed client got %d announces, want 0", n)
	}

	// Tree was actually mutated.
	e, _ := srv.tree.lookup(1, 10)
	if e.param.Value != int64(7) {
		t.Fatalf("tree value=%v want 7", e.param.Value)
	}
}

// TestServe_ErrorStatCodes walks every reachable error stat code (1-5)
// over the live serve path so the error branches in the handlers are
// covered end to end with spec-correct stat bytes.
func TestServe_ErrorStatCodes(t *testing.T) {
	_, addr, stop := startServe(t, buildServeExport())
	defer stop()

	c := dialFakeClient(t, addr)
	defer c.close()
	c.handshake(false)

	expectErr := func(name string, msg *codec.ACP2Message, want codec.ACP2ErrStatus) {
		rep := c.acp2Request(1, msg)
		if rep.Type != codec.ACP2TypeError {
			t.Fatalf("%s: type=%d want error(3)", name, rep.Type)
		}
		if codec.ACP2ErrStatus(rep.Func) != want {
			t.Fatalf("%s: stat=%d want %d", name, rep.Func, want)
		}
	}

	// stat=1 invalid-obj-id: get_object on a non-existent obj.
	expectErr("invalid_obj", &codec.ACP2Message{
		Type: codec.ACP2TypeRequest, MTID: 11, Func: codec.ACP2FuncGetObject, ObjID: 9999,
	}, codec.ErrInvalidObjID)

	// stat=2 invalid-idx: get_property with idx!=0 on a non-preset object.
	expectErr("invalid_idx", &codec.ACP2Message{
		Type: codec.ACP2TypeRequest, MTID: 12, Func: codec.ACP2FuncGetProperty,
		ObjID: 10, Idx: 5, PID: codec.PIDValue,
	}, codec.ErrInvalidIdx)

	// stat=3 invalid-pid: get_property for a pid the object doesn't carry.
	expectErr("invalid_pid", &codec.ACP2Message{
		Type: codec.ACP2TypeRequest, MTID: 13, Func: codec.ACP2FuncGetProperty,
		ObjID: 10, PID: 99,
	}, codec.ErrInvalidPID)

	// stat=4 no-access: set_property on read-only Locked (obj 14).
	roVal := make([]byte, 4)
	expectErr("no_access", &codec.ACP2Message{
		Type: codec.ACP2TypeRequest, MTID: 14, Func: codec.ACP2FuncSetProperty,
		ObjID: 14, PID: codec.PIDValue,
		Properties: []codec.Property{{PID: codec.PIDValue, VType: uint8(codec.NumTypeU8), PLen: 8, Data: roVal}},
	}, codec.ErrNoAccess)

	// stat=5 invalid-value: set Mode (enum obj 11) to an out-of-set idx.
	badEnum := make([]byte, 4)
	binary.BigEndian.PutUint32(badEnum, 99)
	expectErr("invalid_value", &codec.ACP2Message{
		Type: codec.ACP2TypeRequest, MTID: 15, Func: codec.ACP2FuncSetProperty,
		ObjID: 11, PID: codec.PIDValue,
		Properties: []codec.Property{{PID: codec.PIDValue, VType: uint8(codec.NumTypeU32), PLen: 8, Data: badEnum}},
	}, codec.ErrInvalidValue)
}

// TestServe_UnknownFuncProtocolError pins stat=0 protocol-error for an
// ACP2 request carrying an unknown func id.
func TestServe_UnknownFuncProtocolError(t *testing.T) {
	_, addr, stop := startServe(t, buildServeExport())
	defer stop()

	c := dialFakeClient(t, addr)
	defer c.close()
	c.handshake(false)

	rep := c.acp2Request(1, &codec.ACP2Message{
		Type: codec.ACP2TypeRequest, MTID: 20, Func: codec.ACP2Func(99),
	})
	if rep.Type != codec.ACP2TypeError || codec.ACP2ErrStatus(rep.Func) != codec.ErrProtocol {
		t.Fatalf("unknown func: type=%d stat=%d want error/protocol", rep.Type, rep.Func)
	}
}

// TestServe_StopClosesSessions verifies Stop tears down the listener and
// active sessions, and is idempotent (safe to call twice).
func TestServe_StopClosesSessions(t *testing.T) {
	srv, addr, stop := startServe(t, buildServeExport())
	defer stop()

	c := dialFakeClient(t, addr)
	c.handshake(false)

	if !waitFor(t, time.Second, func() bool {
		srv.mu.Lock()
		defer srv.mu.Unlock()
		return len(srv.sessions) == 1
	}) {
		t.Fatal("session never registered")
	}

	if err := srv.Stop(); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	// Idempotent.
	if err := srv.Stop(); err != nil {
		t.Fatalf("second Stop: %v", err)
	}

	// Client reads should now hit EOF/closed.
	_ = c.conn.SetReadDeadline(time.Now().Add(time.Second))
	_, err := codec.ReadAN2Frame(c.conn)
	if err == nil {
		t.Fatal("expected client read error after Stop")
	}
	c.close()
}

// TestServe_ListenError checks Serve surfaces a bind failure (port
// already held) rather than blocking.
func TestServe_ListenError(t *testing.T) {
	ln, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("pre-bind: %v", err)
	}
	defer func() { _ = ln.Close() }()
	addr := ln.Addr().String()

	srv := newServer(quietLogger(), buildServeExport())
	err = srv.Serve(context.Background(), addr)
	if err == nil {
		t.Fatal("Serve on an occupied port should error")
	}
}

// TestProviderSetValue_NotImplemented pins the documented Step-2e stub:
// Provider.SetValue returns an error until that step ships.
func TestProviderSetValue_NotImplemented(t *testing.T) {
	srv := newServer(quietLogger(), buildServeExport())
	_, err := srv.SetValue(context.Background(), "/some/path", 1)
	if err == nil {
		t.Fatal("SetValue should return the not-implemented error")
	}
}

// silence unused import if strconv ever drops out of use.
var _ = strconv.Itoa
