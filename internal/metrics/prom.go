// Prometheus exposition wiring. Uses github.com/prometheus/client_golang
// so we get NewGoCollector() + NewProcessCollector() for free — the
// standard go_* / process_* metrics every Grafana Go-service dashboard
// expects, cross-platform, without re-inventing the exposition format.

package metrics

import (
	"fmt"
	"net/http"
	"strconv"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// PromRegistry bundles a dedicated prometheus.Registry with our two
// collector kinds: the stdlib Go/process collectors from client_golang
// and a custom Connector/Process collector bridging our internal
// metrics types. One PromRegistry per dhs process (or dhs-srv); plug
// it into an HTTP mux via its Handler.
type PromRegistry struct {
	reg *prometheus.Registry
}

// NewPromRegistry builds a PromRegistry wired to:
//   - Go runtime metrics (go_memstats_*, go_goroutines, go_gc_*, …)
//   - Process metrics (process_cpu_seconds_total, process_resident_memory_bytes, …)
//
// Callers must also call Attach(*Connector, labels…) for each live
// connector so their per-cmd counters end up scraped.
func NewPromRegistry() *PromRegistry {
	r := prometheus.NewRegistry()
	r.MustRegister(collectors.NewGoCollector())
	r.MustRegister(collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}))
	return &PromRegistry{reg: r}
}

// Registry returns the underlying prometheus.Registry for callers
// that want to register additional collectors (e.g. application-level
// gauges).
func (p *PromRegistry) Registry() *prometheus.Registry {
	return p.reg
}

// Attach registers a Connector under the given label set. Each call
// adds one more scrape target to /metrics. labels keys are used as
// Prom label names; typical set: {"proto": "probel-sw08p", "role":
// "provider", "addr": "127.0.0.1:2008"}.
func (p *PromRegistry) Attach(c *Connector, labels map[string]string) error {
	if c == nil {
		return fmt.Errorf("metrics: Attach nil Connector")
	}
	col := &connectorCollector{c: c, labels: labels}
	return p.reg.Register(col)
}

// AttachProcess registers a Process under no labels. Usually called
// once per dhs process to surface NumGoroutine and a couple of
// runtime values we track separately from go_*. The standard Go
// collectors already provide heap + gc details.
func (p *PromRegistry) AttachProcess(proc *Process) error {
	if proc == nil {
		return fmt.Errorf("metrics: AttachProcess nil Process")
	}
	return p.reg.Register(&processCollector{p: proc})
}

// Handler returns an http.Handler that serves /metrics in Prometheus
// text exposition format.
func (p *PromRegistry) Handler() http.Handler {
	return promhttp.HandlerFor(p.reg, promhttp.HandlerOpts{
		EnableOpenMetrics: true,
		Registry:          p.reg,
	})
}

// connectorCollector bridges one Connector → Prom scrape.
type connectorCollector struct {
	c      *Connector
	labels map[string]string
}

func (cc *connectorCollector) Describe(ch chan<- *prometheus.Desc) {
	prometheus.DescribeByCollect(cc, ch)
}

func (cc *connectorCollector) Collect(ch chan<- prometheus.Metric) {
	if cc == nil || cc.c == nil {
		return
	}
	s := cc.c.Snapshot()
	lblNames, lblValues := labelsFor(cc.labels)

	counter := func(name, help string, v uint64, extraNames []string, extraVals []string) {
		desc := prometheus.NewDesc("dhs_connector_"+name, help,
			append(append([]string{}, lblNames...), extraNames...), nil)
		ch <- prometheus.MustNewConstMetric(desc, prometheus.CounterValue, float64(v),
			append(append([]string{}, lblValues...), extraVals...)...)
	}
	gauge := func(name, help string, v float64, extraNames []string, extraVals []string) {
		desc := prometheus.NewDesc("dhs_connector_"+name, help,
			append(append([]string{}, lblNames...), extraNames...), nil)
		ch <- prometheus.MustNewConstMetric(desc, prometheus.GaugeValue, v,
			append(append([]string{}, lblValues...), extraVals...)...)
	}

	// Aggregate counters / gauges.
	counter("rx_frames_total", "Frames received", s.RxFrames, nil, nil)
	counter("tx_frames_total", "Frames transmitted", s.TxFrames, nil, nil)
	counter("rx_bytes_total", "Bytes received", s.RxBytes, nil, nil)
	counter("tx_bytes_total", "Bytes transmitted", s.TxBytes, nil, nil)
	counter("decode_errors_total", "Frame decode errors", s.DecodeErrors, nil, nil)
	counter("naks_total", "NAKs received from peer", s.NAKs, nil, nil)
	counter("timeouts_total", "ACK timeouts", s.Timeouts, nil, nil)
	counter("retries_total", "Frame re-send attempts (after NAK or ACK timeout)", s.Retries, nil, nil)
	counter("reconnects_total", "Session reconnects", s.Reconnects, nil, nil)
	gauge("cpu_percent", "Handler busy time as percentage of uptime", s.CPUPercent, nil, nil)
	gauge("memory_bytes", "Estimated memory attributed to this connector", float64(s.EstimatedBytes), nil, nil)
	gauge("tree_bytes", "Tree memory estimate", float64(s.TreeBytes), nil, nil)
	gauge("pool_bytes", "Buffer pool memory estimate", float64(s.PoolBytes), nil, nil)
	gauge("inflight_bytes", "In-flight frame buffer bytes", float64(s.InflightBytes), nil, nil)
	gauge("disk_bytes", "Bytes written to capture file", float64(s.DiskBytes), nil, nil)
	gauge("last_rx_timestamp_seconds", "Unix time of last rx frame", float64(s.LastRxAt.Unix()), nil, nil)
	gauge("last_tx_timestamp_seconds", "Unix time of last tx frame", float64(s.LastTxAt.Unix()), nil, nil)
	gauge("uptime_seconds", "Connector uptime", s.Uptime.Seconds(), nil, nil)

	// Aggregate latency histogram — one metric per bucket.
	for i, count := range s.LatencyBuckets {
		counter("handler_latency_us_bucket_total",
			"Count of handler latencies falling in a log-linear µs bucket",
			count, []string{"le_us"},
			[]string{strconv.FormatInt(bucketCeilingUs(i), 10)})
	}

	// Per-command counters.
	for i := 0; i < 256; i++ {
		if s.RxHitsByCmd[i] == 0 && s.TxHitsByCmd[i] == 0 {
			continue
		}
		cmdID := strconv.Itoa(i)
		cmdName := s.CmdNames[i]
		if cmdName == "" {
			cmdName = fmt.Sprintf("cmd_0x%02x", i)
		}
		extraNames := []string{"cmd_id", "cmd_name"}
		extraVals := []string{cmdID, cmdName}

		counter("rx_cmd_hits_total", "Per-command rx hit count",
			s.RxHitsByCmd[i], extraNames, extraVals)
		counter("tx_cmd_hits_total", "Per-command tx hit count",
			s.TxHitsByCmd[i], extraNames, extraVals)
		counter("rx_cmd_bytes_total", "Per-command rx bytes",
			s.RxBytesByCmd[i], extraNames, extraVals)
		counter("tx_cmd_bytes_total", "Per-command tx bytes",
			s.TxBytesByCmd[i], extraNames, extraVals)
	}
}

// processCollector bridges one Process → Prom scrape. Only exposes
// things NOT already covered by client_golang's GoCollector.
type processCollector struct {
	p *Process
}

func (pc *processCollector) Describe(ch chan<- *prometheus.Desc) {
	prometheus.DescribeByCollect(pc, ch)
}

func (pc *processCollector) Collect(ch chan<- prometheus.Metric) {
	if pc == nil || pc.p == nil {
		return
	}
	s := pc.p.Snapshot()
	gauge := func(name, help string, v float64) {
		desc := prometheus.NewDesc("dhs_process_"+name, help, nil, nil)
		ch <- prometheus.MustNewConstMetric(desc, prometheus.GaugeValue, v)
	}
	gauge("uptime_seconds", "Process uptime", s.Uptime.Seconds())
	gauge("goroutines", "runtime.NumGoroutine", float64(s.Goroutines))
	gauge("gomaxprocs", "runtime.GOMAXPROCS", float64(s.GOMAXPROCS))
	gauge("num_cpu", "runtime.NumCPU", float64(s.NumCPU))
}

// labelsFor returns sorted label-name + matching-value slices.
func labelsFor(m map[string]string) (names, values []string) {
	if len(m) == 0 {
		return nil, nil
	}
	names = make([]string, 0, len(m))
	for k := range m {
		names = append(names, k)
	}
	// Sort for deterministic ordering (Prom expects consistent order
	// across Collect calls within a scrape).
	for i := 0; i < len(names); i++ {
		for j := i + 1; j < len(names); j++ {
			if names[i] > names[j] {
				names[i], names[j] = names[j], names[i]
			}
		}
	}
	values = make([]string, len(names))
	for i, n := range names {
		values[i] = m[n]
	}
	return names, values
}

// bucketCeilingUs returns the upper bound of latency bucket i in µs.
// The final "overflow" bucket is reported as +Inf.
func bucketCeilingUs(i int) int64 {
	switch i {
	case 0:
		return 10
	case 1:
		return 100
	case 2:
		return 1_000
	case 3:
		return 10_000
	case 4:
		return 100_000
	case 5:
		return 1_000_000
	default:
		return 1<<62 - 1 // effectively +Inf for Prom label value
	}
}
