// Package metrics holds the neutral ConnectorMetrics surface every
// protocol plugin (consumer or provider) must expose. Stdlib-only on
// purpose so codec packages can embed it without breaking library
// independence.
//
// Typical usage:
//
//	c := metrics.NewConnector()
//	c.RegisterCmd(0x02, "rx 002 Crosspoint Connect")
//	c.ObserveCmdRx(0x02, n)
//	c.ObserveCmdTx(0x04, m, handlerElapsed)
//	c.ObserveNAK()
//	...
//	fmt.Println(c.Summary())
//
// Counters are all atomic.Uint64; timestamps are atomic.Int64 (unix
// nanos). No plain int / uint on any hot path. All methods are safe
// from multiple goroutines; nil receivers are no-ops.
package metrics

import (
	"fmt"
	"sort"
	"sync"
	"sync/atomic"
	"time"
)

// Connector is the metrics surface attached to one session (consumer
// or provider). Zero value is NOT usable — use NewConnector.
type Connector struct {
	// Aggregate frame + byte counters.
	rxFrames atomic.Uint64
	txFrames atomic.Uint64
	rxBytes  atomic.Uint64
	txBytes  atomic.Uint64

	// Aggregate error counters.
	decodeErrors atomic.Uint64
	naks         atomic.Uint64
	timeouts     atomic.Uint64
	reconnects   atomic.Uint64

	// Timestamps (unix nano).
	startedAt int64 // set once in NewConnector
	lastRxAt  atomic.Int64
	lastTxAt  atomic.Int64

	// Aggregate latency buckets over µs (log-linear):
	//   0: [0,10)  1: [10,100)  2: [100,1k)  3: [1k,10k)
	//   4: [10k,100k)  5: [100k,1M)  6: [1M,∞)
	latency [7]atomic.Uint64

	// Per-command drill-down. Command byte is a natural uint8 so the
	// fixed [256] arrays are lock-free, O(1), ~14 KiB total. Rx and tx
	// hits are tracked separately because SW-P-08 (and most protocols)
	// use direction-specific command IDs.
	rxHitsByCmd  [256]atomic.Uint64
	txHitsByCmd  [256]atomic.Uint64
	rxBytesByCmd [256]atomic.Uint64
	txBytesByCmd [256]atomic.Uint64
	latencyByCmd [256][7]atomic.Uint64

	// Human-readable command names. Written via RegisterCmd (typically
	// once per plugin init before the first Observe call); read by
	// Snapshot for display. Protected by a RWMutex — off hot path.
	cmdNamesMu sync.RWMutex
	cmdNames   [256]string

	// Task-Manager-style per-connector fields.
	//
	// busyNanos accumulates handler wall time (Tx's handlerElapsed). Ratio
	// against uptime gives "CPU %" for this connector.
	//
	// treeBytes / poolBytes / inflightBytes / diskBytes are application-
	// reported memory attributions. Go's GC is process-wide so exact
	// per-connector heap accounting is impossible; these counters are an
	// approximation updated by the owning layer (tree impl, buffer pool,
	// capture recorder). Their sum is EstimatedBytes().
	busyNanos     atomic.Uint64
	treeBytes     atomic.Uint64
	poolBytes     atomic.Uint64
	inflightBytes atomic.Uint64
	diskBytes     atomic.Uint64
}

// NewConnector returns a Connector with the clock started at now.
func NewConnector() *Connector {
	return &Connector{startedAt: time.Now().UnixNano()}
}

// RegisterCmd assigns a human-readable name to command id. Safe to
// call multiple times (last writer wins). Call before the first
// Observe* for clean reporting.
func (c *Connector) RegisterCmd(id uint8, name string) {
	if c == nil {
		return
	}
	c.cmdNamesMu.Lock()
	c.cmdNames[id] = name
	c.cmdNamesMu.Unlock()
}

// ObserveRx records receipt of one frame of n bytes without cmd
// attribution (used for raw-byte observers like DLE ACK / NAK that
// don't carry a command id).
func (c *Connector) ObserveRx(n int) {
	if c == nil {
		return
	}
	c.rxFrames.Add(1)
	c.rxBytes.Add(uint64(n))
	c.lastRxAt.Store(time.Now().UnixNano())
}

// ObserveCmdRx records frame receipt with command id attribution.
// Increments the per-cmd rx counters AND the aggregate counters in one
// call — do not call ObserveRx for the same frame.
func (c *Connector) ObserveCmdRx(id uint8, n int) {
	if c == nil {
		return
	}
	c.rxFrames.Add(1)
	c.rxBytes.Add(uint64(n))
	c.lastRxAt.Store(time.Now().UnixNano())
	c.rxHitsByCmd[id].Add(1)
	c.rxBytesByCmd[id].Add(uint64(n))
}

// ObserveTx records emission of one frame of n bytes without cmd
// attribution. handlerElapsed accumulates into busyNanos and the
// aggregate latency histogram; pass 0 if not measured.
func (c *Connector) ObserveTx(n int, handlerElapsed time.Duration) {
	if c == nil {
		return
	}
	c.txFrames.Add(1)
	c.txBytes.Add(uint64(n))
	c.lastTxAt.Store(time.Now().UnixNano())
	if handlerElapsed > 0 {
		c.busyNanos.Add(uint64(handlerElapsed.Nanoseconds()))
		c.latency[bucketFor(handlerElapsed)].Add(1)
	}
}

// ObserveCmdTx records frame emission with command id attribution.
// Increments per-cmd tx counters AND the aggregate counters including
// the latency histogram. Do not call ObserveTx for the same frame.
func (c *Connector) ObserveCmdTx(id uint8, n int, handlerElapsed time.Duration) {
	if c == nil {
		return
	}
	c.txFrames.Add(1)
	c.txBytes.Add(uint64(n))
	c.lastTxAt.Store(time.Now().UnixNano())
	c.txHitsByCmd[id].Add(1)
	c.txBytesByCmd[id].Add(uint64(n))
	if handlerElapsed > 0 {
		c.busyNanos.Add(uint64(handlerElapsed.Nanoseconds()))
		b := bucketFor(handlerElapsed)
		c.latency[b].Add(1)
		c.latencyByCmd[id][b].Add(1)
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

// SetTreeBytes stores the caller's latest estimate of tree memory
// usage. Called by the provider's tree on mutation.
func (c *Connector) SetTreeBytes(v uint64) {
	if c == nil {
		return
	}
	c.treeBytes.Store(v)
}

// SetPoolBytes stores the caller's latest estimate of buffer pool
// memory usage.
func (c *Connector) SetPoolBytes(v uint64) {
	if c == nil {
		return
	}
	c.poolBytes.Store(v)
}

// SetInflightBytes stores the caller's latest estimate of in-flight
// frame buffer bytes.
func (c *Connector) SetInflightBytes(v uint64) {
	if c == nil {
		return
	}
	c.inflightBytes.Store(v)
}

// AddDiskBytes accumulates bytes written to disk (e.g. --capture).
func (c *Connector) AddDiskBytes(n uint64) {
	if c == nil {
		return
	}
	c.diskBytes.Add(n)
}

// CPUPercent returns busyNanos / uptimeNanos × 100. 0.0 when no work
// has been observed yet.
func (c *Connector) CPUPercent() float64 {
	if c == nil {
		return 0
	}
	uptime := time.Now().UnixNano() - c.startedAt
	if uptime <= 0 {
		return 0
	}
	return float64(c.busyNanos.Load()) / float64(uptime) * 100
}

// EstimatedBytes is the sum of the four memory attributions
// (treeBytes + poolBytes + inflightBytes + diskBytes).
func (c *Connector) EstimatedBytes() uint64 {
	if c == nil {
		return 0
	}
	return c.treeBytes.Load() + c.poolBytes.Load() +
		c.inflightBytes.Load() + c.diskBytes.Load()
}

// Uptime returns wall-clock time since NewConnector.
func (c *Connector) Uptime() time.Duration {
	if c == nil {
		return 0
	}
	return time.Since(time.Unix(0, c.startedAt))
}

// Snapshot is a frozen point-in-time read of every counter. Safe to
// pass around — no pointers into the Connector.
type Snapshot struct {
	// Aggregate.
	RxFrames, TxFrames                       uint64
	RxBytes, TxBytes                         uint64
	DecodeErrors, NAKs, Timeouts, Reconnects uint64

	// Aggregate latency: count per µs log-linear bucket.
	LatencyBuckets [7]uint64

	// Per-command. Index == command id. CmdNames carries the display
	// label set via RegisterCmd (empty string for unregistered ids).
	RxHitsByCmd  [256]uint64
	TxHitsByCmd  [256]uint64
	RxBytesByCmd [256]uint64
	TxBytesByCmd [256]uint64
	LatencyByCmd [256][7]uint64
	CmdNames     [256]string

	// Task-Manager-style.
	CPUPercent     float64
	TreeBytes      uint64
	PoolBytes      uint64
	InflightBytes  uint64
	DiskBytes      uint64
	EstimatedBytes uint64

	// Timestamps.
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
	for i := 0; i < 256; i++ {
		s.RxHitsByCmd[i] = c.rxHitsByCmd[i].Load()
		s.TxHitsByCmd[i] = c.txHitsByCmd[i].Load()
		s.RxBytesByCmd[i] = c.rxBytesByCmd[i].Load()
		s.TxBytesByCmd[i] = c.txBytesByCmd[i].Load()
		for b := 0; b < 7; b++ {
			s.LatencyByCmd[i][b] = c.latencyByCmd[i][b].Load()
		}
	}
	c.cmdNamesMu.RLock()
	s.CmdNames = c.cmdNames
	c.cmdNamesMu.RUnlock()
	s.CPUPercent = c.CPUPercent()
	s.TreeBytes = c.treeBytes.Load()
	s.PoolBytes = c.poolBytes.Load()
	s.InflightBytes = c.inflightBytes.Load()
	s.DiskBytes = c.diskBytes.Load()
	s.EstimatedBytes = s.TreeBytes + s.PoolBytes + s.InflightBytes + s.DiskBytes
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
// output at session close. Includes aggregate counters + Task-Manager
// style metrics; per-cmd detail is too wide for one line — call
// TopCmdsByHits for that.
func (c *Connector) Summary() string {
	s := c.Snapshot()
	p50, p95, p99 := s.LatencyPercentiles()
	return fmt.Sprintf(
		"uptime=%s cpu=%.2f%% mem=%dB rx=%d/%dB tx=%d/%dB errs=decode:%d nak:%d to:%d rec:%d lat_us p50~%d p95~%d p99~%d",
		s.Uptime.Round(time.Millisecond),
		s.CPUPercent,
		s.EstimatedBytes,
		s.RxFrames, s.RxBytes,
		s.TxFrames, s.TxBytes,
		s.DecodeErrors, s.NAKs, s.Timeouts, s.Reconnects,
		p50, p95, p99,
	)
}

// CmdStat is one per-command row surfaced by TopCmdsByHits. Hits is
// the sum of RxHits + TxHits (total activity on this cmd id); the
// split is available via the fields.
type CmdStat struct {
	ID      uint8
	Name    string // from RegisterCmd; "" if unregistered
	Hits    uint64 // RxHits + TxHits
	RxHits  uint64
	TxHits  uint64
	RxBytes uint64
	TxBytes uint64
}

// TopCmdsByHits returns up to n per-command rows sorted by total hit
// count (rx + tx) descending. Rows with 0 hits both directions are
// excluded. Use for CLI / dashboard "top commands" displays.
func (s Snapshot) TopCmdsByHits(n int) []CmdStat {
	rows := make([]CmdStat, 0, 256)
	for i := 0; i < 256; i++ {
		total := s.RxHitsByCmd[i] + s.TxHitsByCmd[i]
		if total == 0 {
			continue
		}
		rows = append(rows, CmdStat{
			ID:      uint8(i),
			Name:    s.CmdNames[i],
			Hits:    total,
			RxHits:  s.RxHitsByCmd[i],
			TxHits:  s.TxHitsByCmd[i],
			RxBytes: s.RxBytesByCmd[i],
			TxBytes: s.TxBytesByCmd[i],
		})
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].Hits > rows[j].Hits })
	if n > 0 && len(rows) > n {
		rows = rows[:n]
	}
	return rows
}

// LatencyPercentiles returns bucket-floor estimates for p50, p95, p99
// in µs from the aggregate histogram. Imprecise (bucketed) but
// allocation-free. Returns 0,0,0 when no latency has been observed.
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

// CmdLatencyPercentiles returns bucket-floor estimates for p50, p95,
// p99 µs from the per-command histogram of command id. Returns 0,0,0
// when no latency for that command has been observed.
func (s Snapshot) CmdLatencyPercentiles(id uint8) (p50, p95, p99 int64) {
	var total uint64
	for _, v := range s.LatencyByCmd[id] {
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
	for i, v := range s.LatencyByCmd[id] {
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
