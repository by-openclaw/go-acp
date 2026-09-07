package rollcall

import (
	"context"
	"io"
	"log/slog"
	"net"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"dhs/internal/clock"
	"dhs/internal/metrics"
	"dhs/internal/plugin"
	"dhs/internal/snell-rollcall/codec"
	"dhs/internal/transport"
)

// The consumer is tested against a fake RollCall device rather than a mock of
// its own session layer. That is the point: the device speaks the wire format,
// so a test failure means the bytes are wrong, not that an expectation was
// written differently from the code.
//
// It runs over net.Pipe with an injected clock, so there is no socket, no port
// and no sleeping anywhere.

// gatewayAddr is the unit the fake device presents as.
var gatewayAddr = codec.Address{Unit: 0x08, Port: 0x00, Index: codec.IndexUnknown}

// device is a RollCall server good enough to drive the consumer.
type device struct {
	t *testing.T

	mu sync.Mutex

	// services is what the gateway advertises, which decides which
	// generation the consumer will ask for.
	services codec.Service

	// menus is the menu each port serves.
	menus map[uint8][]codec.MenuItem

	// values is the current value of each command, per port.
	values map[uint8]map[uint32]codec.Value

	// files is what the file service serves.
	files map[string][]byte

	// identity is what each port reports.
	identity map[uint8]codec.ID

	// ports is how many ports the device list reports.
	ports int

	// refuseLongStrings makes the device advertise long strings and then
	// refuse a call that asks for them, which is a real deviation.
	refuseLongStrings bool

	// blockSize is what an open reports as its maximum read size.
	blockSize int32

	// sessions maps our index to the port it was opened on.
	sessions map[int16]uint8
	nextIdx  int16

	// backChannel records which sessions asked for pushes.
	backChannel map[int16]bool

	// blocks is the state a multi-packet transfer needs, per session. The
	// 16-bit form is stateful by design: a fetch names an offset from the
	// base of the request that opened the transfer.
	blocks map[int16]*blockState

	// openFiles is what the file service has handed out handles for.
	openFiles map[int16][]byte

	// refuse makes the device answer a message type with Nack, so a test can
	// see what the connector does when a device says no.
	refuse map[codec.PacketType]bool

	// garble makes the device answer a message type with a payload too short
	// to decode, which is what a firmware bug looks like from outside.
	garble map[codec.PacketType]bool

	// gateCall holds every Call until a test releases it, so two callers can
	// be made to race for the same port deterministically.
	gateCall chan struct{}

	// callsSeen counts the Calls that arrived, so a test can wait for both
	// racers to be in flight before releasing them.
	callsSeen int

	// oddMenuItem makes one item of a 16-bit menu transfer come back as
	// something other than a menu line, which a walker should skip rather
	// than abandon the menu for. Negative disables it.
	oddMenuItem int

	// oddListItem makes one item of a device or port list come back as
	// something other than a device record, which a walker should skip.
	oddListItem int

	// routers are the ports that serve a routing interface instead of a menu,
	// which is what a router controller node is.
	routers map[uint8]*fakeRouter

	// refuseMap makes the device refuse a call asking for the map service,
	// which is what a gateway that will not open one looks like.
	refuseMap bool

	// emptySlots are the ports that report nothing fitted, which is what a
	// frame with a slot left out looks like.
	emptySlots map[uint8]bool

	// emptyDeviceMap makes the gateway report having heard nothing announce
	// itself, which is what a freshly started one does.
	emptyDeviceMap bool

	// badMapEntry makes the device map answer with a device record too short
	// to decode.
	badMapEntry bool

	// oddDirItem makes one entry of a directory listing come back as
	// something other than a directory record.
	oddDirItem int

	// failReadAfter makes a file read report an error once that many chunks
	// have been handed over. Negative disables it.
	failReadAfter int

	// shortReadCount makes a read reply claim fewer bytes than it actually
	// carries, which a client must believe over the payload it can see.
	shortReadCount bool

	// endlessFile makes every read return data and never end, which is what a
	// device stuck in a loop looks like.
	endlessFile bool

	// garbleDirEntry truncates the entries of a directory listing. It is
	// separate from garble because the items of a multi-packet transfer all
	// arrive as answers to the same request type, so the type alone cannot
	// say which transfer to spoil.
	garbleDirEntry bool

	conn net.Conn
	w    sync.Mutex
	done chan struct{}
	once sync.Once
}

func newDevice(t *testing.T, conn net.Conn) *device {
	d := &device{
		t:             t,
		services: codec.SvcMenus | codec.SvcControl | codec.SvcDisplay |
			codec.SvcFile | codec.SvcMap | codec.SvcLongStr,
		menus:         make(map[uint8][]codec.MenuItem),
		values:        make(map[uint8]map[uint32]codec.Value),
		files:         make(map[string][]byte),
		identity:      make(map[uint8]codec.ID),
		ports:         2,
		blockSize:     0,
		sessions:      make(map[int16]uint8),
		nextIdx:       0x30,
		backChannel:   make(map[int16]bool),
		oddMenuItem:   -1,
		oddListItem:   -1,
		oddDirItem:    -1,
		emptySlots:    map[uint8]bool{},
		routers:       map[uint8]*fakeRouter{},
		failReadAfter: -1,
		refuse:        make(map[codec.PacketType]bool),
		garble:        make(map[codec.PacketType]bool),
		conn:          conn,
		done:          make(chan struct{}),
	}
	go d.serve()
	return d
}

// setMenu gives a port a menu and seeds a value for every command in it.
func (d *device) setMenu(port uint8, items []codec.MenuItem) {
	d.mu.Lock()
	defer d.mu.Unlock()

	d.menus[port] = items
	if d.values[port] == nil {
		d.values[port] = make(map[uint32]codec.Value)
	}
	for _, m := range items {
		if m.Command == 0 || m.Style.Container() {
			continue
		}
		if _, seen := d.values[port][m.Command]; seen {
			continue
		}
		d.values[port][m.Command] = codec.Value{
			Command: m.Command,
			Mode:    codec.ModeValue,
			Val:     m.MinRange,
		}
	}
}

func (d *device) setValue(port uint8, v codec.Value) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.values[port] == nil {
		d.values[port] = make(map[uint32]codec.Value)
	}
	d.values[port][v.Command] = v
}

func (d *device) setFile(path string, body []byte) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.files[path] = body
}

func (d *device) close() {
	d.once.Do(func() {
		close(d.done)
		_ = d.conn.Close()
	})
}

// serve reads frames and answers them.
func (d *device) serve() {
	r := codec.NewReader(d.conn)
	for {
		f, err := r.ReadFrame()
		if err != nil {
			return
		}
		f.Payload = append([]byte(nil), f.Payload...)
		d.handle(f)
	}
}

func (d *device) reply(req codec.Frame, typ codec.PacketType, payload []byte) {
	d.send(codec.Frame{
		Dst:     req.Src,
		Src:     req.Dst,
		Type:    typ,
		Payload: payload,
	})
}

func (d *device) send(f codec.Frame) {
	b, err := f.Encode()
	if err != nil {
		return
	}
	d.w.Lock()
	defer d.w.Unlock()
	select {
	case <-d.done:
		return
	default:
	}
	if _, err := d.conn.Write(b); err != nil && err != io.ErrClosedPipe {
		return
	}
}

// push sends a back-channel message to whoever is subscribed on a port.
func (d *device) push(port uint8, typ codec.PacketType, payload []byte) {
	d.mu.Lock()
	var targets []int16
	for idx, p := range d.sessions {
		if p == port && d.backChannel[idx] {
			targets = append(targets, idx)
		}
	}
	d.mu.Unlock()

	for _, idx := range targets {
		d.send(codec.Frame{
			Dst:     codec.Address{Unit: 0, Port: 0xE0, Index: idx},
			Src:     codec.Address{Unit: gatewayAddr.Unit, Port: port, Index: idx},
			Type:    typ,
			Flags:   codec.FlagBackChannel,
			Payload: payload,
		})
	}
}

// harness is a plugin connected to a fake device.
type harness struct {
	plugin *Plugin
	device *device
	clk    *clock.Fake
}

// pipeNet is a transport whose Dial hands back one end of a pipe. It is what
// keeps the consumer from opening a socket in a test while still going through
// the same code path it uses in service.
type pipeNet struct {
	t  *testing.T
	mu sync.Mutex
	fn func() net.Conn

	// blocked, when set, receives a wrapper whose writes a test can make fail
	// while its reads go on working: a socket that has gone away without our
	// end having noticed.
	blocked **blockedConn
}

// blockedConn fails writes on demand and leaves reads alone.
type blockedConn struct {
	net.Conn
	failing atomic.Bool
}

func (c *blockedConn) Write(b []byte) (int, error) {
	if c.failing.Load() {
		return 0, io.ErrClosedPipe
	}
	return c.Conn.Write(b)
}

func (p *pipeNet) Dial(ctx context.Context, network, addr string) (net.Conn, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	conn := p.fn()
	if p.blocked != nil {
		*p.blocked = &blockedConn{Conn: conn}
		return *p.blocked, nil
	}
	return conn, nil
}

func (p *pipeNet) Listen(context.Context, string, string) (net.Listener, error) {
	p.t.Fatal("the consumer must never listen")
	return nil, nil
}

func (p *pipeNet) ListenPacket(context.Context, string, string) (net.PacketConn, error) {
	p.t.Fatal("the consumer must never bind a datagram socket")
	return nil, nil
}

var _ transport.Net = (*pipeNet)(nil)

// newHarness builds a plugin wired to a fake device, and connects it.
func newHarness(t *testing.T, setup func(*device)) *harness {
	t.Helper()
	h, _ := newHarnessWith(t, setup, false)
	return h
}

// newBlockedHarness is a harness whose own writes can be made to fail while
// what the device sends still arrives.
func newBlockedHarness(t *testing.T, setup func(*device)) (*harness, **blockedConn) {
	t.Helper()
	return newHarnessWith(t, setup, true)
}

func newHarnessWith(t *testing.T, setup func(*device), blocked bool) (*harness, **blockedConn) {
	t.Helper()

	clk := clock.NewFake(time.Time{})
	var dev *device

	var block *blockedConn
	dialer := &pipeNet{t: t}
	if blocked {
		dialer.blocked = &block
	}
	dialer.fn = func() net.Conn {
		ours, theirs := net.Pipe()
		dev = newDevice(t, theirs)
		if setup != nil {
			setup(dev)
		}
		return ours
	}

	p := New(plugin.Deps{
		Logger:  slog.New(slog.NewTextHandler(io.Discard, nil)),
		Net:     dialer,
		Clock:   clk,
		Metrics: metrics.NewConnector(),
	})

	if err := p.Connect(context.Background(), "10.6.250.105", DefaultPort); err != nil {
		t.Fatalf("Connect: %v", err)
	}

	h := &harness{plugin: p, device: dev, clk: clk}
	t.Cleanup(func() {
		_ = p.Disconnect()
		if h.device != nil {
			h.device.close()
		}
	})
	return h, &block
}

// menu builds a small but realistic menu: a container with three lines under
// it, which is enough to exercise the tree spans, and one line of each kind
// that maps differently.
func testMenu() []codec.MenuItem {
	return []codec.MenuItem{
		{MenuIndex: 0, Style: codec.StyleList, Step: 3, Text: "Video"},
		{MenuIndex: 1, Style: codec.StyleNumber, Command: 0x0113,
			MinRange: -60, MaxRange: 6, Step: 1, DivScale: 10, Text: "Gain", Param: "%0.1f dB"},
		{MenuIndex: 2, Style: codec.StyleCheckbox, Command: 0x0114,
			MinRange: 1, Text: "Enable"},
		{MenuIndex: 3, Style: codec.StyleEditString, Command: 0x0115,
			MaxRange: 32, Text: "Name"},
		{MenuIndex: 4, Style: codec.StyleDisplay, Command: 0x0116, Text: "Status"},
	}
}

// timeoutAfter is the wait for something that should already have happened.
// It bounds a test rather than pacing one: nothing under test is waiting on
// protocol time here.
func timeoutAfter() <-chan time.Time { return time.After(2 * time.Second) }

// testDeps is the dependency set for a plugin with no device behind it.
func testDeps() plugin.Deps {
	return plugin.Deps{
		Logger:  slog.New(slog.NewTextHandler(io.Discard, nil)),
		Clock:   clock.NewFake(time.Time{}),
		Metrics: metrics.NewConnector(),
	}
}
