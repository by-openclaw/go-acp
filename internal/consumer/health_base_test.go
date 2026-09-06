package consumer

import (
	"context"
	"errors"
	"net"
	"sync/atomic"
	"testing"
	"time"

	"dhs/internal/metrics"
	"dhs/internal/transport"
)

// countingNet is a transport.Net that records how often Dial was called and
// answers with whatever the test wants. Health must only ever dial — the two
// listen methods exist to satisfy the interface and are proof, if they are
// ever reached, that something is wrong.
type countingNet struct {
	dials atomic.Int64
	err   error
}

func (c *countingNet) Dial(ctx context.Context, network, addr string) (net.Conn, error) {
	c.dials.Add(1)
	if c.err != nil {
		return nil, c.err
	}
	left, right := net.Pipe()
	go func() { _ = right.Close() }()
	return left, nil
}

func (c *countingNet) Listen(context.Context, string, string) (net.Listener, error) {
	panic("Health must never listen")
}

func (c *countingNet) ListenPacket(context.Context, string, string) (net.PacketConn, error) {
	panic("Health must never listen")
}

// fixedTimes is a session whose rx/tx instants the test sets directly.
type fixedTimes struct{ rx, tx time.Time }

func (f fixedTimes) LastRx() time.Time { return f.rx }
func (f fixedTimes) LastTx() time.Time { return f.tx }

const testStale = 90 * time.Second

// newTestHealth builds a configured Health the way a factory does.
func newTestHealth(n transport.Net, stale time.Duration) *Health {
	var h Health
	h.Configure(n, stale)
	return &h
}

// The zero value is the one most of this repo's tests get, because they build
// connectors as bare struct literals. It must work rather than panic.
func TestZeroHealthIsUsable(t *testing.T) {
	var h Health
	got := h.SessionHealth(context.Background())
	if got.Connected || got.Live || got.Reachable {
		t.Errorf("an unconfigured Health reports nothing open, got %+v", got)
	}
	if got.StaleAfter != DefaultStaleAfter {
		t.Errorf("StaleAfter = %v, want the default %v", got.StaleAfter, DefaultStaleAfter)
	}

	// And it can still probe, using a process-default transport.
	h.Opened("tcp", "127.0.0.1", 1, fixedTimes{})
	if h.SessionHealth(context.Background()).Reachable {
		t.Error("nothing listens on port 1")
	}
}

// A non-positive window means "unset", not "everything is stale".
func TestConfigureWithNoWindowFallsBackToTheDefault(t *testing.T) {
	h := newTestHealth(&countingNet{}, 0)
	if got := h.SessionHealth(context.Background()); got.StaleAfter != DefaultStaleAfter {
		t.Errorf("StaleAfter = %v, want %v", got.StaleAfter, DefaultStaleAfter)
	}
}

// A connector that never called Opened has no session and no address, so
// there is nothing to report and nothing to probe.
func TestSessionHealthBeforeAnySession(t *testing.T) {
	n := &countingNet{}
	got := newTestHealth(n, testStale).SessionHealth(context.Background())

	if got.Connected || got.Live || got.Reachable {
		t.Errorf("a connector with no session must report all three false, got %+v", got)
	}
	if got.StaleAfter != testStale {
		t.Errorf("StaleAfter = %v, want %v", got.StaleAfter, testStale)
	}
	if n.dials.Load() != 0 {
		t.Error("probed with no address to probe")
	}
}

// The reason Live implies Reachable: a frame that arrived inside the window
// is stronger evidence of a path than a SYN-ACK, so the probe must not run.
func TestLiveSessionIsReachableWithoutProbing(t *testing.T) {
	n := &countingNet{err: errors.New("dial must not happen")}
	h := newTestHealth(n, testStale)
	h.Opened("tcp", "10.0.0.1", 2072, fixedTimes{rx: time.Now()})

	got := h.SessionHealth(context.Background())
	if !got.Live || !got.Reachable || !got.Connected {
		t.Errorf("a live session is connected, live and reachable, got %+v", got)
	}
	if n.dials.Load() != 0 {
		t.Errorf("dialled %d times; traffic is already proof of the path", n.dials.Load())
	}
}

// The bug this type was written to fix: a UDP session answering every request
// used to report Reachable=false, because the probe dialled TCP regardless of
// the transport actually in use.
func TestLiveUDPSessionIsReachable(t *testing.T) {
	h := newTestHealth(&countingNet{}, testStale)
	h.Opened("udp", "10.6.250.105", 2071, fixedTimes{rx: time.Now()})

	if got := h.SessionHealth(context.Background()); !got.Reachable {
		t.Errorf("a UDP session receiving frames is reachable, got %+v", got)
	}
}

// Once the evidence is stale a stream transport falls back to connecting.
func TestStaleStreamSessionFallsBackToTheProbe(t *testing.T) {
	for _, tc := range []struct {
		name string
		err  error
		want bool
	}{
		{"connect succeeds", nil, true},
		{"connect refused", errors.New("refused"), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			n := &countingNet{err: tc.err}
			h := newTestHealth(n, testStale)
			h.Opened("tcp", "10.0.0.1", 2072, fixedTimes{rx: time.Now().Add(-10 * time.Minute)})

			got := h.SessionHealth(context.Background())
			if got.Live {
				t.Error("rx older than StaleAfter is not live")
			}
			if got.Reachable != tc.want {
				t.Errorf("Reachable = %v, want %v", got.Reachable, tc.want)
			}
			if n.dials.Load() != 1 {
				t.Errorf("dialled %d times, want exactly 1", n.dials.Load())
			}
		})
	}
}

// Dialling a datagram socket succeeds whether or not anything is listening,
// so on UDP a stale session reports what it actually knows: nothing.
func TestStaleDatagramSessionDoesNotProbe(t *testing.T) {
	n := &countingNet{}
	h := newTestHealth(n, testStale)
	h.Opened("udp", "10.0.0.1", 2071, fixedTimes{rx: time.Now().Add(-10 * time.Minute)})

	if got := h.SessionHealth(context.Background()); got.Reachable {
		t.Error("a UDP connect proves nothing; Reachable must stay false")
	}
	if n.dials.Load() != 0 {
		t.Error("probed a datagram peer")
	}
}

// Reachable and Connected are separate layers precisely so a dropped session
// on a host that still answers is distinguishable from a host that is gone.
func TestClosedSessionKeepsProbingTheAddress(t *testing.T) {
	n := &countingNet{}
	h := newTestHealth(n, testStale)
	h.Opened("tcp", "10.0.0.1", 2072, fixedTimes{rx: time.Now().Add(-time.Hour)})
	h.Closed()

	got := h.SessionHealth(context.Background())
	if got.Connected {
		t.Error("Closed must clear Connected")
	}
	if !got.Reachable {
		t.Error("the host still answers, so Reachable stays true")
	}
}

// Both instants are surfaced, not just the one Live is derived from.
func TestSessionHealthReportsBothInstants(t *testing.T) {
	rx := time.Now().Add(-time.Second)
	tx := time.Now().Add(-2 * time.Second)
	h := newTestHealth(&countingNet{}, testStale)
	h.Opened("tcp", "10.0.0.1", 2072, fixedTimes{rx: rx, tx: tx})

	got := h.SessionHealth(context.Background())
	if !got.LastRx.Equal(rx) || !got.LastTx.Equal(tx) {
		t.Errorf("LastRx/LastTx = %v/%v, want %v/%v", got.LastRx, got.LastTx, rx, tx)
	}
}

// A session may be open before anything is known about its timestamps.
func TestOpenedWithNoTimeSource(t *testing.T) {
	h := newTestHealth(&countingNet{}, testStale)
	h.Opened("tcp", "10.0.0.1", 2072, nil)

	got := h.SessionHealth(context.Background())
	if !got.Connected || got.Live {
		t.Errorf("connected but with nothing received yet, got %+v", got)
	}
	if !got.LastRx.IsZero() || !got.LastTx.IsZero() {
		t.Error("no time source means zero instants")
	}
}

// A caller that supplies no transport still gets a working probe rather than
// a nil dereference.
func TestNewHealthDefaultsTheTransport(t *testing.T) {
	h := newTestHealth(nil, testStale)
	h.Opened("tcp", "127.0.0.1", 1, fixedTimes{})
	// Port 1 refuses; the point is that it returns rather than panics.
	if got := h.SessionHealth(context.Background()); got.Reachable {
		t.Error("nothing listens on port 1")
	}
}

// The probe must not outlive a caller that is already done.
func TestProbeHonoursACancelledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	h := newTestHealth(transport.New(transport.Config{}), testStale)
	h.Opened("tcp", "10.255.255.1", 2072, fixedTimes{})

	if got := h.SessionHealth(ctx); got.Reachable {
		t.Error("a cancelled probe is not reachability")
	}
}

// The zero-wiring path: a connector that only counts frames through its
// injected Connector gets health without touching its read or write loop.
func TestMetricsTimesReadsTheConnector(t *testing.T) {
	c := metrics.NewConnector()
	c.ObserveRx(8)
	c.ObserveTx(8, 0)

	mt := MetricsTimes{C: c}
	if mt.LastRx().IsZero() || mt.LastTx().IsZero() {
		t.Error("observed traffic must produce instants")
	}

	h := newTestHealth(&countingNet{}, testStale)
	h.Opened("tcp", "10.0.0.1", 2072, mt)
	if got := h.SessionHealth(context.Background()); !got.Live {
		t.Errorf("traffic just observed is live, got %+v", got)
	}
}

// A connector constructed without metrics must not crash health.
func TestMetricsTimesWithNoConnector(t *testing.T) {
	var mt MetricsTimes
	if !mt.LastRx().IsZero() || !mt.LastTx().IsZero() {
		t.Error("a nil Connector reports zero, not a panic")
	}
}

func TestIsStream(t *testing.T) {
	for _, network := range []string{"tcp", "tcp4", "tcp6"} {
		if !isStream(network) {
			t.Errorf("%s is a stream network", network)
		}
	}
	for _, network := range []string{"udp", "udp4", "udp6", "unixgram", ""} {
		if isStream(network) {
			t.Errorf("%s is not a stream network", network)
		}
	}
}
