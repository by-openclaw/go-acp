package rollcall

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net"
	"sync"
	"testing"
	"time"

	"dhs/internal/clock"
	"dhs/internal/consumer"
	"dhs/internal/metrics"
	"dhs/internal/plugin"
	"dhs/internal/snell-rollcall/codec"
)

// TestConcurrentSessionOpenKeepsOne covers two callers reaching for the same
// port at once.
//
// A session costs a round trip, so the check for an existing one happens
// before the call rather than under a lock held across it. That leaves a
// window where both callers see none and both open one, and the loser has to
// close its own: a port with two sessions wastes one of the few a unit has,
// and a unit that runs out stops answering anybody.
//
// The device holds both calls until each has arrived, so the race is forced
// rather than hoped for.
func TestConcurrentSessionOpenKeepsOne(t *testing.T) {
	gate := make(chan struct{})
	h := newHarness(t, func(d *device) {
		d.setMenu(1, testMenu())
	})

	// Enumeration first, before the gate exists. A slot number is resolved
	// against the device's own node list, so the session that walks it would
	// otherwise be one of the calls the gate holds — and it would hold it
	// forever, because nothing else can arrive until it is through.
	if _, err := h.plugin.nodes(context.Background()); err != nil {
		t.Fatalf("enumerate: %v", err)
	}

	h.device.mu.Lock()
	h.device.gateCall = gate
	h.device.mu.Unlock()

	h.plugin.mu.RLock()
	l0 := h.plugin.link
	h.plugin.mu.RUnlock()
	l0.mu.Lock()
	before := len(l0.sessions)
	l0.mu.Unlock()

	h.device.mu.Lock()
	deviceBefore := len(h.device.sessions)
	h.device.callsSeen = 0
	h.device.mu.Unlock()

	// Two callers reach for port 1 together.
	var wg sync.WaitGroup
	results := make([]error, 2)
	for i := range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := h.plugin.session(context.Background(), 1)
			results[i] = err
		}()
	}

	// Wait until both calls are in flight, then let them through together.
	deadline := time.Now().Add(2 * time.Second)
	for {
		h.device.mu.Lock()
		seen := h.device.callsSeen
		h.device.mu.Unlock()
		if seen >= 2 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("only %d calls arrived; the race was not set up", seen)
		}
		time.Sleep(time.Millisecond)
	}
	close(gate)
	wg.Wait()

	for i, err := range results {
		if err != nil {
			t.Errorf("caller %d: %v", i, err)
		}
	}

	// Exactly one session was added for the port, and both callers got it.
	h.plugin.mu.RLock()
	l := h.plugin.link
	h.plugin.mu.RUnlock()

	l.mu.Lock()
	kept := len(l.sessions) - before
	l.mu.Unlock()
	if kept != 1 {
		t.Errorf("the race added %d sessions for one port, want 1", kept)
	}

	// And the loser closed its own, so the device is not left holding two.
	deadline = time.Now().Add(2 * time.Second)
	for {
		h.device.mu.Lock()
		open := len(h.device.sessions) - deviceBefore
		h.device.mu.Unlock()
		if open == 1 {
			break
		}
		if time.Now().After(deadline) {
			t.Errorf("the device kept %d of the two sessions, want 1", open)
			break
		}
		time.Sleep(time.Millisecond)
	}
}

// TestConcurrentFileSessionKeepsOne covers the same race on the file service,
// which opens its own session because services are all-or-nothing.
func TestConcurrentFileSessionKeepsOne(t *testing.T) {
	gate := make(chan struct{})
	h := newHarness(t, func(d *device) {
		d.gateCall = gate
		d.setFile("A.BIN", []byte("x"))
	})

	var wg sync.WaitGroup
	for range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _ = h.plugin.fileSession(context.Background(), 0)
		}()
	}

	deadline := time.Now().Add(2 * time.Second)
	for {
		h.device.mu.Lock()
		seen := h.device.callsSeen
		h.device.mu.Unlock()
		if seen >= 2 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("only %d calls arrived", seen)
		}
		time.Sleep(time.Millisecond)
	}
	close(gate)
	wg.Wait()

	h.plugin.mu.RLock()
	l := h.plugin.link
	h.plugin.mu.RUnlock()

	l.mu.Lock()
	kept := len(l.fileSessions)
	l.mu.Unlock()
	if kept != 1 {
		t.Errorf("the link holds %d file sessions, want 1", kept)
	}
}

// TestDisconnectDuringSessionOpen covers a caller disconnecting while a
// session is being opened. The session must not be left registered on a link
// that is already gone, and the caller must be told rather than handed one.
func TestDisconnectDuringSessionOpen(t *testing.T) {
	gate := make(chan struct{})
	h := newHarness(t, func(d *device) { d.gateCall = gate })

	done := make(chan error, 1)
	go func() {
		_, err := h.plugin.session(context.Background(), 1)
		done <- err
	}()

	deadline := time.Now().Add(2 * time.Second)
	for {
		h.device.mu.Lock()
		seen := h.device.callsSeen
		h.device.mu.Unlock()
		if seen >= 1 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the call never arrived")
		}
		time.Sleep(time.Millisecond)
	}

	// Disconnect while the call is in flight, then let it complete.
	_ = h.plugin.Disconnect()
	close(gate)

	select {
	case err := <-done:
		if err == nil {
			t.Error("a session opened on a link that was closed should not be handed out")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("the caller was left waiting")
	}
}

// TestDisconnectDuringFileOpen covers the same for the file service.
func TestDisconnectDuringFileOpen(t *testing.T) {
	gate := make(chan struct{})
	h := newHarness(t, func(d *device) { d.gateCall = gate })

	done := make(chan error, 1)
	go func() {
		_, err := h.plugin.fileSession(context.Background(), 0)
		done <- err
	}()

	deadline := time.Now().Add(2 * time.Second)
	for {
		h.device.mu.Lock()
		seen := h.device.callsSeen
		h.device.mu.Unlock()
		if seen >= 1 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the call never arrived")
		}
		time.Sleep(time.Millisecond)
	}

	_ = h.plugin.Disconnect()
	close(gate)

	select {
	case err := <-done:
		if err == nil {
			t.Error("a file session opened on a closed link should not be handed out")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("the caller was left waiting")
	}
}

// failingNet refuses to dial, which is what an unreachable device looks like.
type failingNet struct{ err error }

func (f failingNet) Dial(context.Context, string, string) (net.Conn, error) {
	return nil, f.err
}
func (failingNet) Listen(context.Context, string, string) (net.Listener, error) { return nil, nil }
func (failingNet) ListenPacket(context.Context, string, string) (net.PacketConn, error) {
	return nil, nil
}

func TestConnect_DialFails(t *testing.T) {
	want := errors.New("no route to host")
	p := New(plugin.Deps{
		Logger:  slog.New(slog.NewTextHandler(io.Discard, nil)),
		Net:     failingNet{err: want},
		Clock:   clock.NewFake(time.Time{}),
		Metrics: metrics.NewConnector(),
	})

	err := p.Connect(context.Background(), "10.0.0.1", DefaultPort)
	if !errors.Is(err, want) {
		t.Fatalf("err = %v, want the dialler's own reason", err)
	}
	// A failed connection leaves nothing behind.
	if _, err := p.GetDeviceInfo(context.Background()); !errors.Is(err, consumer.ErrNotConnected) {
		t.Errorf("err = %v, want ErrNotConnected", err)
	}
}

// TestConnect_HandshakeFails covers a peer that accepts the connection and
// then refuses the first message, which is what a wrong port looks like.
func TestConnect_HandshakeFails(t *testing.T) {
	dialer := &pipeNet{t: t}
	dialer.fn = func() net.Conn {
		ours, theirs := net.Pipe()
		d := newDevice(t, theirs)
		d.refuse[codec.MsgGetDevInfo] = true
		return ours
	}

	p := New(plugin.Deps{
		Logger:  slog.New(slog.NewTextHandler(io.Discard, nil)),
		Net:     dialer,
		Clock:   clock.NewFake(time.Time{}),
		Metrics: metrics.NewConnector(),
	})

	if err := p.Connect(context.Background(), "10.0.0.1", DefaultPort); err == nil {
		t.Fatal("a refused handshake should fail the connection")
	}
	if _, err := p.GetDeviceInfo(context.Background()); !errors.Is(err, consumer.ErrNotConnected) {
		t.Errorf("err = %v, want ErrNotConnected", err)
	}
}

// TestConnect_DefaultPort covers a caller that names no port. The IPShare port
// is what a gateway listens on, so it is the sensible default rather than an
// error.
func TestConnect_DefaultPort(t *testing.T) {
	dialer := &pipeNet{t: t}
	dialer.fn = func() net.Conn {
		ours, theirs := net.Pipe()
		newDevice(t, theirs)
		return ours
	}

	p := New(plugin.Deps{
		Logger:  slog.New(slog.NewTextHandler(io.Discard, nil)),
		Net:     dialer,
		Clock:   clock.NewFake(time.Time{}),
		Metrics: metrics.NewConnector(),
	})
	t.Cleanup(func() { _ = p.Disconnect() })

	if err := p.Connect(context.Background(), "10.0.0.1", 0); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	if p.port != DefaultPort {
		t.Errorf("port = %d, want the default %d", p.port, DefaultPort)
	}
}

// TestPumpStopsWithTheLink covers the announcement pump ending when the device
// goes away, rather than spinning on a dead link.
func TestPumpStopsWithTheLink(t *testing.T) {
	h := newHarness(t, nil)

	// An announcement arrives and is absorbed by the pump.
	info, err := codec.DeviceInfo{ID: codec.ID{Name: "Vega"}}.AppendTo(nil)
	if err != nil {
		t.Fatalf("AppendTo: %v", err)
	}
	h.device.send(codec.Frame{
		Dst:     codec.Broadcast(),
		Src:     gatewayAddr,
		Type:    codec.MsgIam,
		Payload: info,
	})

	// An announcement that will not decode must not stop the pump either.
	h.device.send(codec.Frame{
		Dst:     codec.Broadcast(),
		Src:     gatewayAddr,
		Type:    codec.MsgIam,
		Payload: []byte{0x01},
	})

	// Wait for the pump to have taken both before the link dies underneath it,
	// so this tests what the pump does with an announcement rather than which
	// of the two goroutines won.
	deadline := time.Now().Add(2 * time.Second)
	for h.plugin.Announcements() < 2 {
		if time.Now().After(deadline) {
			t.Fatalf("the pump absorbed %d announcements, want 2", h.plugin.Announcements())
		}
		time.Sleep(time.Millisecond)
	}

	h.device.close()

	// The link notices and the plugin refuses further work rather than
	// hanging.
	deadline = time.Now().Add(2 * time.Second)
	for {
		if _, err := h.plugin.GetSlotInfo(context.Background(), 1); err != nil {
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("the plugin kept working after the device went away")
		}
		time.Sleep(time.Millisecond)
	}
}
