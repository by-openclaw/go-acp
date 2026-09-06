package acp1

import (
	"context"
	"sync/atomic"
	"time"

	"dhs/internal/metrics"
)

// acp1StaleAfter is the rolling-window threshold past which the device
// is judged not Live. Per spec p.20 + design discussion: 90 seconds
// matches the slow announce cadence Synapse rack controllers use.
const acp1StaleAfter = 90 * time.Second

// timestampSink stores the most recent rx/tx wall-clock timestamps as
// UnixNano values. Lock-free reads via atomic.Int64; writers update
// once per wire event.
type timestampSink struct {
	lastRxNS atomic.Int64
	lastTxNS atomic.Int64
}

// recordRx must be called from the transport read path.
func (t *timestampSink) recordRx() { t.lastRxNS.Store(time.Now().UnixNano()) }

// recordTx must be called from the transport write path.
func (t *timestampSink) recordTx() { t.lastTxNS.Store(time.Now().UnixNano()) }

func (t *timestampSink) lastRx() time.Time {
	ns := t.lastRxNS.Load()
	if ns == 0 {
		return time.Time{}
	}
	return time.Unix(0, ns)
}

func (t *timestampSink) lastTx() time.Time {
	ns := t.lastTxNS.Load()
	if ns == 0 {
		return time.Time{}
	}
	return time.Unix(0, ns)
}

// timestampingTransport wraps a Transport and taps every Send/Receive: it
// stamps the timestampSink for liveness and counts the frame on the metrics
// connector. Cheap — one atomic store and two counter adds per wire event.
//
// This is the UDP path's equivalent of the TCP and AN2 clients' OnRx/OnTx
// hooks; all three end up feeding the same connector, so the counters mean
// the same thing whichever transport a session resolved to.
type timestampingTransport struct {
	inner interface {
		Send(ctx context.Context, payload []byte) error
		Receive(ctx context.Context, maxSize int) ([]byte, error)
		Close() error
	}
	sink *timestampSink
	met  *metrics.Connector
}

func (t *timestampingTransport) Send(ctx context.Context, payload []byte) error {
	if err := t.inner.Send(ctx, payload); err != nil {
		return err
	}
	t.sink.recordTx()
	if t.met != nil {
		t.met.ObserveTx(len(payload), 0)
	}
	return nil
}

func (t *timestampingTransport) Receive(ctx context.Context, maxSize int) ([]byte, error) {
	data, err := t.inner.Receive(ctx, maxSize)
	if err == nil && len(data) > 0 {
		t.sink.recordRx()
		if t.met != nil {
			t.met.ObserveRx(len(data))
		}
	}
	return data, err
}

func (t *timestampingTransport) Close() error { return t.inner.Close() }

// LastRx and LastTx expose the sink as a consumer.RxTxTimes, which is all
// the shared Health needs to decide Live.
func (t *timestampSink) LastRx() time.Time { return t.lastRx() }
func (t *timestampSink) LastTx() time.Time { return t.lastTx() }

// Plugin reports health through the embedded *consumer.Health (see
// plugin.go). What lives here is only ACP1's time source.
//
// The SessionHealth method and the private probeReachable that used to
// follow were byte-for-byte identical to ACP2's, and both dialled TCP to
// decide Reachable regardless of the transport in use — so a live UDP
// session against a Synapse rack reported reachable=false while it was
// answering every request. Connect now passes the real network.
