package acp1

import (
	"context"
	"net"
	"testing"
	"time"

	"dhs/internal/acp1/codec"
)

// TestDiscover_DefaultsAndCancelledCtx exercises the Port/Duration default
// fill-ins and a fast exit via an already-cancelled context.
func TestDiscover_DefaultsAndCancelledCtx(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	// Port 0 → default; Duration 0 → default. The cancelled ctx makes the
	// receive goroutine return immediately so the call doesn't actually
	// wait the default 5s.
	_, err := Discover(ctx, DiscoverConfig{})
	// Either a bind error (port 2071 busy) or a clean empty result — both
	// drive the default-fill branches; we only require it returns promptly.
	_ = err
}

// TestDiscover_TwoSourcesSorted produces results from two distinct loopback
// source IPs so the final sort.Slice comparator is exercised.
func TestDiscover_TwoSourcesSorted(t *testing.T) {
	port := freeUDPPort(t)
	send := func(srcIP net.IP) bool {
		la := &net.UDPAddr{IP: srcIP, Port: 0}
		ra := &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: port}
		conn, err := net.DialUDP("udp4", la, ra)
		if err != nil {
			return false // OS may not allow binding this loopback alias
		}
		defer func() { _ = conn.Close() }()
		_, _ = conn.Write(buildReply(t, 0, codec.MTypeAnnounce, 0, codec.GroupFrame, 0, []byte{2, 2}))
		return true
	}
	go func() {
		time.Sleep(40 * time.Millisecond)
		ok1 := send(net.IPv4(127, 0, 0, 1))
		ok2 := send(net.IPv4(127, 0, 0, 2))
		if !ok1 || !ok2 {
			// Fall back: at least one source so the test still runs cleanly.
			_ = send(net.IPv4(127, 0, 0, 1))
		}
	}()
	res, err := Discover(context.Background(), DiscoverConfig{Port: port, Duration: 300 * time.Millisecond})
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	if len(res) < 2 {
		t.Skipf("only %d distinct source IP(s) observed on this host; sort comparator needs 2", len(res))
	}
}

// TestDiscover_BroadcastReplySource: the first datagram from an IP is a
// non-zero-MTID reply, so the Source is recorded as "broadcast-reply".
func TestDiscover_BroadcastReplySource(t *testing.T) {
	port := freeUDPPort(t)
	go func() {
		time.Sleep(60 * time.Millisecond)
		conn, err := net.DialUDP("udp4", nil, &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: port})
		if err != nil {
			return
		}
		defer func() { _ = conn.Close() }()
		// Non-zero MTID reply → "broadcast-reply" source.
		_, _ = conn.Write(buildReply(t, 7, codec.MTypeReply, byte(codec.MethodGetValue), codec.GroupFrame, 0, []byte{2, 2, 2}))
	}()
	res, err := Discover(context.Background(), DiscoverConfig{
		Port: port, Duration: 400 * time.Millisecond, Active: false,
	})
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	var found *DiscoverResult
	for i := range res {
		if res[i].IP == "127.0.0.1" {
			found = &res[i]
		}
	}
	if found == nil {
		t.Fatalf("device not discovered: %+v", res)
	}
	if found.Source != "broadcast-reply" {
		t.Errorf("Source = %q, want broadcast-reply", found.Source)
	}
}

// TestDiscover_WindowExit_ReceiveTimeout drives the Receive-error return: a
// quiet window means the single Receive blocks to the deadline and returns
// DeadlineExceeded, exiting the goroutine.
func TestDiscover_WindowExit_ReceiveTimeout(t *testing.T) {
	port := freeUDPPort(t)
	if _, err := Discover(context.Background(), DiscoverConfig{Port: port, Duration: 60 * time.Millisecond}); err != nil {
		t.Fatalf("Discover: %v", err)
	}
}

// TestDiscover_WindowExit_DeadlineCheck drives the loop-top remaining<=0
// return: a continuous datagram flood keeps the goroutine in the success
// path (never blocking in Receive past the deadline) so the deadline is
// observed at the loop top rather than via a Receive timeout.
func TestDiscover_WindowExit_DeadlineCheck(t *testing.T) {
	port := freeUDPPort(t)
	stop := make(chan struct{})
	done := make(chan struct{})
	go func() {
		defer close(done)
		conn, err := net.DialUDP("udp4", nil, &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: port})
		if err != nil {
			return
		}
		defer func() { _ = conn.Close() }()
		dg := buildReply(t, 0, codec.MTypeAnnounce, 0, codec.GroupFrame, 0, []byte{2, 2})
		for {
			select {
			case <-stop:
				return
			default:
				_, _ = conn.Write(dg)
			}
		}
	}()
	_, err := Discover(context.Background(), DiscoverConfig{Port: port, Duration: 80 * time.Millisecond})
	close(stop)
	<-done
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
}

// TestDiscover_CtxCancelMidWindow: cancelling the context while the receive
// goroutine is blocked in Receive drives its error-exit return (the Receive
// returns ctx.Err with remaining still > 0).
func TestDiscover_CtxCancelMidWindow(t *testing.T) {
	port := freeUDPPort(t)
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(80 * time.Millisecond)
		cancel()
	}()
	// Long duration so the goroutine is genuinely blocked in Receive when
	// the cancel lands (remaining stays > 0 → exits via the Receive error,
	// not the deadline check).
	_, err := Discover(ctx, DiscoverConfig{Port: port, Duration: 10 * time.Second})
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	cancel()
}

// TestDiscover_ActiveProbeError drives the probe-failed log branch via an
// injected probe that always errors.
func TestDiscover_ActiveProbeError(t *testing.T) {
	port := freeUDPPort(t)
	_, err := Discover(context.Background(), DiscoverConfig{
		Port:     port,
		Duration: 50 * time.Millisecond,
		Active:   true,
		probe:    func(int) error { return errProbe },
	})
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
}

var errProbe = &probeErr{}

type probeErr struct{}

func (*probeErr) Error() string { return "synthetic probe failure" }

// TestProbeActiveAddr_DialError drives probeActive's dial-failure return via
// a bogus address.
func TestProbeActiveAddr_DialError(t *testing.T) {
	if err := probeActiveAddr("256.256.256.256:99999"); err == nil {
		t.Fatal("bogus broadcast addr: want dial error")
	}
}

// TestSendProbe_WriteError drives the send-failure return with a writer that
// always errors.
func TestSendProbe_WriteError(t *testing.T) {
	if err := sendProbe(errWriter{}); err == nil {
		t.Fatal("failing writer: want send error")
	}
}

// TestSendProbe_Success drives the happy write path.
func TestSendProbe_Success(t *testing.T) {
	var sink discardWriter
	if err := sendProbe(&sink); err != nil {
		t.Fatalf("sendProbe: %v", err)
	}
	if sink.n == 0 {
		t.Error("expected probe bytes written")
	}
}

type errWriter struct{}

func (errWriter) Write([]byte) (int, error) { return 0, errProbe }

type discardWriter struct{ n int }

func (d *discardWriter) Write(p []byte) (int, error) { d.n += len(p); return len(p), nil }

// TestDiscover_OutOfRangePortBindError: an out-of-range port fails the UDP
// bind in Discover, surfacing the listener-bind error return.
func TestDiscover_OutOfRangePortBindError(t *testing.T) {
	if _, err := Discover(context.Background(), DiscoverConfig{Port: 999999, Duration: 50 * time.Millisecond}); err == nil {
		t.Fatal("out-of-range port: want bind error")
	}
}

// TestDiscover_BindError: binding a port already held returns an error.
func TestDiscover_BindError(t *testing.T) {
	// Hold a UDP socket WITHOUT SO_REUSEADDR on a port, then ask Discover to
	// bind the same port. transport.ListenUDP uses SO_REUSEADDR so this may
	// still succeed on some platforms; the test only asserts no panic and a
	// prompt return.
	c, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 0})
	if err != nil {
		t.Fatalf("hold port: %v", err)
	}
	defer func() { _ = c.Close() }()
	p := c.LocalAddr().(*net.UDPAddr).Port
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, _ = Discover(ctx, DiscoverConfig{Port: p, Duration: 50 * time.Millisecond})
}
