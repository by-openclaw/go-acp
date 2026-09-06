package acp1

import (
	"context"
	"sync/atomic"
	"time"
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

// timestampingTransport wraps a Transport and updates a timestampSink on
// every Send/Receive. Cheap (one atomic store per wire event).
type timestampingTransport struct {
	inner interface {
		Send(ctx context.Context, payload []byte) error
		Receive(ctx context.Context, maxSize int) ([]byte, error)
		Close() error
	}
	sink *timestampSink
}

func (t *timestampingTransport) Send(ctx context.Context, payload []byte) error {
	t.sink.recordTx()
	return t.inner.Send(ctx, payload)
}

func (t *timestampingTransport) Receive(ctx context.Context, maxSize int) ([]byte, error) {
	data, err := t.inner.Receive(ctx, maxSize)
	if err == nil && len(data) > 0 {
		t.sink.recordRx()
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
