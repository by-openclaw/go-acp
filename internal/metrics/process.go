package metrics

import (
	"runtime"
	"sync/atomic"
	"time"
)

// Process captures process-wide runtime metrics — heap, goroutines,
// GC, scheduler — to complement per-connector Connector counters.
// There is one Process instance per dhs process; dhs-srv shares a
// single Process across all connectors.
//
// Uses only stdlib (runtime / runtime/metrics). Sampling is cheap
// (~5 µs) so Snapshot() is safe to call every second; periodic
// sampling via Run keeps the last values fresh for lock-free reads
// between calls.
type Process struct {
	startedAt int64 // unix nano, set once in NewProcess

	// Cached runtime snapshot updated by Sample / Run. Reads are
	// lock-free atomic loads; writers serialise via a compareAndSwap
	// on the monotonic timestamp.
	lastSampleAt atomic.Int64

	// Go heap / GC metrics (subset of runtime.MemStats).
	heapAlloc      atomic.Uint64
	heapSys        atomic.Uint64
	heapIdle       atomic.Uint64
	heapInuse      atomic.Uint64
	heapReleased   atomic.Uint64
	stackInuse     atomic.Uint64
	totalAlloc     atomic.Uint64
	mallocs        atomic.Uint64
	frees          atomic.Uint64
	numGC          atomic.Uint64
	pauseTotalNs   atomic.Uint64
	gcCPUFraction  atomic.Uint64 // stored as ppm (parts-per-million) since atomic.Float64 is Go 1.25+
	nextGC         atomic.Uint64
	goroutines     atomic.Int64
	osThreads      atomic.Int64
	numCPU         atomic.Int64
}

// NewProcess returns a Process with one initial sample.
func NewProcess() *Process {
	p := &Process{startedAt: time.Now().UnixNano()}
	p.Sample()
	return p
}

// Sample refreshes the cached values from runtime.ReadMemStats and
// runtime.NumGoroutine / NumCPU. Safe to call from multiple
// goroutines but expected to be called by a single "sampler"
// goroutine at Run cadence.
func (p *Process) Sample() {
	if p == nil {
		return
	}
	var ms runtime.MemStats
	runtime.ReadMemStats(&ms)
	p.heapAlloc.Store(ms.HeapAlloc)
	p.heapSys.Store(ms.HeapSys)
	p.heapIdle.Store(ms.HeapIdle)
	p.heapInuse.Store(ms.HeapInuse)
	p.heapReleased.Store(ms.HeapReleased)
	p.stackInuse.Store(ms.StackInuse)
	p.totalAlloc.Store(ms.TotalAlloc)
	p.mallocs.Store(ms.Mallocs)
	p.frees.Store(ms.Frees)
	p.numGC.Store(uint64(ms.NumGC))
	p.pauseTotalNs.Store(ms.PauseTotalNs)
	p.gcCPUFraction.Store(uint64(ms.GCCPUFraction * 1_000_000))
	p.nextGC.Store(ms.NextGC)
	p.goroutines.Store(int64(runtime.NumGoroutine()))
	p.osThreads.Store(int64(runtime.GOMAXPROCS(0)))
	p.numCPU.Store(int64(runtime.NumCPU()))
	p.lastSampleAt.Store(time.Now().UnixNano())
}

// Run samples at the given interval until ctx.Done fires. Typical
// cadence: 1-10 seconds. Non-blocking start: caller launches this in
// a goroutine. Pass nil done to run indefinitely (e.g. for a
// long-lived dhs-srv); always recover the goroutine on shutdown.
func (p *Process) Run(interval time.Duration, done <-chan struct{}) {
	if p == nil || interval <= 0 {
		return
	}
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		select {
		case <-done:
			return
		case <-t.C:
			p.Sample()
		}
	}
}

// ProcessSnapshot is a frozen point-in-time view of process-wide
// runtime metrics.
type ProcessSnapshot struct {
	StartedAt    time.Time
	LastSampleAt time.Time
	Uptime       time.Duration

	HeapAllocBytes    uint64 // live heap bytes
	HeapSysBytes      uint64 // OS-reserved heap
	HeapIdleBytes     uint64 // reserved but unused
	HeapInuseBytes    uint64
	HeapReleasedBytes uint64 // returned to OS
	StackInuseBytes   uint64
	TotalAllocBytes   uint64 // cumulative mallocs (monotonic)
	Mallocs           uint64 // cumulative malloc count
	Frees             uint64 // cumulative free count
	NumGC             uint64 // cumulative GC cycles
	PauseTotalNs      uint64 // cumulative stop-the-world pause
	GCCPUFraction     float64
	NextGCBytes       uint64
	Goroutines        int64
	GOMAXPROCS        int64
	NumCPU            int64
}

// Snapshot returns the latest cached process metrics. If Sample has
// not run since the last atomic write, the values are slightly stale
// (at most one interval).
func (p *Process) Snapshot() ProcessSnapshot {
	if p == nil {
		return ProcessSnapshot{}
	}
	var s ProcessSnapshot
	s.StartedAt = time.Unix(0, p.startedAt)
	if v := p.lastSampleAt.Load(); v > 0 {
		s.LastSampleAt = time.Unix(0, v)
	}
	s.Uptime = time.Since(s.StartedAt)
	s.HeapAllocBytes = p.heapAlloc.Load()
	s.HeapSysBytes = p.heapSys.Load()
	s.HeapIdleBytes = p.heapIdle.Load()
	s.HeapInuseBytes = p.heapInuse.Load()
	s.HeapReleasedBytes = p.heapReleased.Load()
	s.StackInuseBytes = p.stackInuse.Load()
	s.TotalAllocBytes = p.totalAlloc.Load()
	s.Mallocs = p.mallocs.Load()
	s.Frees = p.frees.Load()
	s.NumGC = p.numGC.Load()
	s.PauseTotalNs = p.pauseTotalNs.Load()
	s.GCCPUFraction = float64(p.gcCPUFraction.Load()) / 1_000_000
	s.NextGCBytes = p.nextGC.Load()
	s.Goroutines = p.goroutines.Load()
	s.GOMAXPROCS = p.osThreads.Load()
	s.NumCPU = p.numCPU.Load()
	return s
}
