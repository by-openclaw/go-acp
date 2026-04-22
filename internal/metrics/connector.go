// Package metrics holds the neutral ConnectorMetrics surface every
// protocol plugin (consumer or provider) must expose. Stdlib-only on
// purpose so codec packages can embed the Sink interface without
// breaking library independence.
//
// Usage:
//
//	c := metrics.NewConnector()
//	c.ObserveRx(len(frame))
//	c.ObserveTx(len(reply), handlerElapsed)
//	c.ObserveError()
//	...
//	fmt.Println(c.Summary())
//
// All methods are safe from multiple goroutines. Reads use
// Snapshot() which returns a frozen point-in-time view.
package metrics

import (
	"fmt"
	"sync/atomic"
	"time"
)

// Connector is the metrics surface attached to one session (consumer
// or provider). Zero value is NOT usable — use NewConnector.
type Connector struct {
	rxFrames atomic.Uint64
	txFrames atomic.Uint64
	rxBytes  atomic.Uint64
	txBytes  atomic.Uint64

	decodeErrors atomic.Uint64
	naks         atomic.Uint64
	timeouts     atomic.Uint64
	reconnects   atomic.Uint64

	startedAt int64 // nanos since unix epoch, set in NewConnector
	lastRxAt  atomic.Int64
	lastTxAt  atomic.Int64

	// Log-linear latency buckets over µs:
	//   0: [0,10)  1: [10,100)  2: [100,1k)  3: [1k,10k)
	//   4: [10k,100k)  5: [100k,1M)  6: [1M,∞)
	latency [7]atomic.Uint64
}

// NewConnector returns a Connector with the clock started at now.
func NewConnector() *Connector {
	return &Connector{startedAt: time.Now().UnixNano()}
}

// ObserveRx records receipt of one frame of n bytes.
func (c *Connector) ObserveRx(n int) {
	if c == nil {
		return
	}
	c.rxFrames.Add(1)
	c.rxBytes.Add(uint64(n))
	c.lastRxAt.Store(time.Now().UnixNano())
}

// ObserveTx records emission of one frame of n bytes. handlerElapsed
// is the rx→tx turnaround inside the handler (pass 0 if not measured;
// the bucket stays untouched).
func (c *Connector) ObserveTx(n int, handlerElapsed time.Duration) {
	if c == nil {
		return
	}
	c.txFrames.Add(1)
	c.txBytes.Add(uint64(n))
	c.lastTxAt.Store(time.Now().UnixNano())
	if handlerElapsed > 0 {
		c.latency[bucketFor(handlerElapsed)].Add(1)
	}
}

// ObserveDecodeError bumps the framing / decode error counter.
func (c *Connector) ObserveDecodeError() {
	if c == nil {
		return
	}
	c.decodeErrors.Add(1)
}

// ObserveNAK bumps the peer-NAK counter.
func (c *Connector) ObserveNAK() {
	if c == nil {
		return
	}
	c.naks.Add(1)
}

// ObserveTimeout bumps the ACK-timeout counter.
func (c *Connector) ObserveTimeout() {
	if c == nil {
		return
	}
	c.timeouts.Add(1)
}

// ObserveReconnect bumps the reconnect counter.
func (c *Connector) ObserveReconnect() {
	if c == nil {
		return
	}
	c.reconnects.Add(1)
}

// Snapshot is a frozen point-in-time read of every counter. Safe to
// pass around — no pointers into the Connector.
type Snapshot struct {
	RxFrames, TxFrames uint64
	RxBytes, TxBytes   uint64

	DecodeErrors, NAKs, Timeouts, Reconnects uint64

	// LatencyBuckets gives the count of handler turnarounds that fell
	// into each of the 7 log-linear µs buckets (see Connector.latency).
	LatencyBuckets [7]uint64

	StartedAt time.Time
	LastRxAt  time.Time // zero if no rx yet
	LastTxAt  time.Time // zero if no tx yet
	Uptime    time.Duration
}

// Snapshot takes a read-only view of this Connector's counters.
func (c *Connector) Snapshot() Snapshot {
	if c == nil {
		return Snapshot{}
	}
	var s Snapshot
	s.RxFrames = c.rxFrames.Load()
	s.TxFrames = c.txFrames.Load()
	s.RxBytes = c.rxBytes.Load()
	s.TxBytes = c.txBytes.Load()
	s.DecodeErrors = c.decodeErrors.Load()
	s.NAKs = c.naks.Load()
	s.Timeouts = c.timeouts.Load()
	s.Reconnects = c.reconnects.Load()
	for i := range c.latency {
		s.LatencyBuckets[i] = c.latency[i].Load()
	}
	s.StartedAt = time.Unix(0, c.startedAt)
	if v := c.lastRxAt.Load(); v > 0 {
		s.LastRxAt = time.Unix(0, v)
	}
	if v := c.lastTxAt.Load(); v > 0 {
		s.LastTxAt = time.Unix(0, v)
	}
	s.Uptime = time.Since(s.StartedAt)
	return s
}

// Summary formats a concise one-line session summary suitable for log
// output at session close.
func (c *Connector) Summary() string {
	s := c.Snapshot()
	p50, p95, p99 := s.LatencyPercentiles()
	return fmt.Sprintf(
		"uptime=%s rx=%d/%dB tx=%d/%dB errs=decode:%d nak:%d to:%d rec:%d lat_us p50~%d p95~%d p99~%d",
		s.Uptime.Round(time.Millisecond),
		s.RxFrames, s.RxBytes,
		s.TxFrames, s.TxBytes,
		s.DecodeErrors, s.NAKs, s.Timeouts, s.Reconnects,
		p50, p95, p99,
	)
}

// LatencyPercentiles returns bucket-floor estimates for p50, p95, p99
// in µs. Uses the log-linear buckets: imprecise but allocation-free
// and dependency-free. Returns 0 when no latency has been observed.
func (s Snapshot) LatencyPercentiles() (p50, p95, p99 int64) {
	var total uint64
	for _, v := range s.LatencyBuckets {
		total += v
	}
	if total == 0 {
		return 0, 0, 0
	}
	t50 := total / 2
	t95 := total - total/20
	t99 := total - total/100
	var (
		acc            uint64
		have50, have95 bool
	)
	for i, v := range s.LatencyBuckets {
		acc += v
		floor := bucketFloorUs(i)
		if !have50 && acc >= t50 {
			p50 = floor
			have50 = true
		}
		if !have95 && acc >= t95 {
			p95 = floor
			have95 = true
		}
		if acc >= t99 {
			p99 = floor
			break
		}
	}
	return p50, p95, p99
}

func bucketFor(d time.Duration) int {
	us := d.Microseconds()
	switch {
	case us < 10:
		return 0
	case us < 100:
		return 1
	case us < 1_000:
		return 2
	case us < 10_000:
		return 3
	case us < 100_000:
		return 4
	case us < 1_000_000:
		return 5
	default:
		return 6
	}
}

func bucketFloorUs(i int) int64 {
	switch i {
	case 0:
		return 0
	case 1:
		return 10
	case 2:
		return 100
	case 3:
		return 1_000
	case 4:
		return 10_000
	case 5:
		return 100_000
	default:
		return 1_000_000
	}
}
