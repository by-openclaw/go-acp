package metrics

import (
	"bytes"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

// Every Connector method is nil-safe: a connector that was never wired
// (an optional dependency left nil) absorbs observations and answers
// zero, so a caller never guards for nil.
func TestConnectorNilReceiverIsInert(t *testing.T) {
	var c *Connector
	c.RegisterCmd(1, "x")
	c.ObserveRx(1)
	c.ObserveCmdRx(1, 1)
	c.ObserveTx(1, time.Millisecond)
	c.ObserveCmdTx(1, 1, time.Millisecond)
	c.SetTreeBytes(1)
	c.SetPoolBytes(1)
	c.SetInflightBytes(1)
	c.AddDiskBytes(1)
	if c.CPUPercent() != 0 || c.EstimatedBytes() != 0 || c.Uptime() != 0 {
		t.Error("nil connector must answer zero")
	}
}

// The memory estimate is the sum of its four parts, uptime and CPU come
// from the start stamp, and a start stamp in the future (clock skew)
// yields 0% rather than a negative percentage.
func TestConnectorMemoryUptimeAndCPU(t *testing.T) {
	c := NewConnector()
	c.SetTreeBytes(1)
	c.SetPoolBytes(2)
	c.SetInflightBytes(3)
	c.AddDiskBytes(4)
	if got := c.EstimatedBytes(); got != 10 {
		t.Errorf("EstimatedBytes = %d, want 10", got)
	}
	c.ObserveTx(1, 50*time.Millisecond)
	time.Sleep(5 * time.Millisecond) // coarse clocks (Windows) need real elapsed time
	if c.Uptime() <= 0 || c.CPUPercent() <= 0 {
		t.Errorf("uptime %v / cpu %v must be positive after busy time", c.Uptime(), c.CPUPercent())
	}
	skewed := NewConnector()
	skewed.startedAt = time.Now().Add(time.Hour).UnixNano()
	if skewed.CPUPercent() != 0 {
		t.Error("a start stamp in the future must not produce a negative CPU percentage")
	}
}

// Latency buckets are log-linear in µs; percentiles read back the floor
// of the bucket that crosses each threshold, per connector and per
// command, and the top-N command table is ordered and truncated.
func TestLatencyBucketsPercentilesAndTopCmds(t *testing.T) {
	durs := []time.Duration{5 * time.Microsecond, 50 * time.Microsecond, 500 * time.Microsecond,
		5 * time.Millisecond, 50 * time.Millisecond, 500 * time.Millisecond, 5 * time.Second}
	for i, d := range durs {
		if got := bucketFor(d); got != i {
			t.Errorf("bucketFor(%v) = %d, want %d", d, got, i)
		}
	}
	floors := []int64{0, 10, 100, 1_000, 10_000, 100_000, 1_000_000}
	for i, want := range floors {
		if got := bucketFloorUs(i); got != want {
			t.Errorf("bucketFloorUs(%d) = %d, want %d", i, got, want)
		}
	}

	c := NewConnector()
	c.RegisterCmd(7, "seven")
	for i := 0; i < 90; i++ {
		c.ObserveCmdTx(7, 1, 5*time.Microsecond)
	}
	for i := 0; i < 8; i++ {
		c.ObserveCmdTx(7, 1, 5*time.Millisecond)
	}
	c.ObserveCmdTx(7, 1, 5*time.Second)
	c.ObserveCmdTx(7, 1, 5*time.Second)
	c.ObserveCmdRx(9, 3)
	c.ObserveCmdRx(9, 3)
	c.ObserveCmdRx(9, 3)
	c.ObserveCmdTx(11, 1, 0)
	s := c.Snapshot()
	p50, p95, p99 := s.LatencyPercentiles()
	if p50 != 0 || p95 != 1_000 || p99 != 1_000_000 {
		t.Errorf("percentiles = %d/%d/%d, want 0/1000/1000000", p50, p95, p99)
	}
	if c50, c95, c99 := s.CmdLatencyPercentiles(7); c50 != p50 || c95 != p95 || c99 != p99 {
		t.Errorf("cmd percentiles = %d/%d/%d, want the same distribution", c50, c95, c99)
	}
	if a, b, d := s.CmdLatencyPercentiles(9); a != 0 || b != 0 || d != 0 {
		t.Error("a command with no latency samples reports zeros")
	}
	if a, b, d := NewConnector().Snapshot().LatencyPercentiles(); a != 0 || b != 0 || d != 0 {
		t.Error("a connector with no latency samples reports zeros")
	}
	top := s.TopCmdsByHits(2)
	if len(top) != 2 || top[0].ID != 7 || top[0].Name != "seven" || top[1].ID != 9 {
		t.Errorf("TopCmdsByHits(2) = %+v, want cmd 7 then cmd 9", top)
	}
	if all := s.TopCmdsByHits(0); len(all) != 3 {
		t.Errorf("TopCmdsByHits(0) = %d rows, want every command with hits", len(all))
	}
}

// failAfter is a writer that fails on its n-th write, so a sweep over n
// drives every early-return in the exporters.
type failAfter struct {
	n     int
	calls int
}

func (f *failAfter) Write(p []byte) (int, error) {
	f.calls++
	if f.calls > f.n {
		return 0, errors.New("sink closed")
	}
	return len(p), nil
}

func TestExportersReportEveryWriteFailure(t *testing.T) {
	c := NewConnector()
	c.RegisterCmd(1, "one")
	c.ObserveCmdRx(1, 10)
	c.ObserveCmdTx(1, 10, 3*time.Millisecond)
	s := c.Snapshot()
	proc := NewProcess()
	proc.Sample()
	labels := map[string]string{"proto": "acp1", "addr": ":2071"}

	for name, fn := range map[string]func(io.Writer) error{
		"csv":      func(w io.Writer) error { return WriteCSV(w, s, proc.Snapshot(), labels) },
		"markdown": func(w io.Writer) error { return WriteMarkdown(w, s, proc.Snapshot(), labels) },
	} {
		failed := 0
		ok := false
		for n := 0; n < 500; n++ {
			w := &failAfter{n: n}
			if err := fn(w); err != nil {
				failed++
				continue
			}
			ok = true
			break
		}
		if !ok || failed == 0 {
			t.Errorf("%s: sweep saw %d failures, ok=%v — want failures then success", name, failed, ok)
		}
		var buf bytes.Buffer
		if err := fn(&buf); err != nil || buf.Len() == 0 {
			t.Errorf("%s: %v (%d bytes)", name, err, buf.Len())
		}
		if err := WriteMarkdown(&buf, s, proc.Snapshot(), nil); err != nil {
			t.Errorf("markdown without labels: %v", err)
		}
	}
	if formatLabels(nil) != "" {
		t.Error("no labels formats to nothing")
	}
	if got := formatLabels(map[string]string{"b": "2", "a": "1"}); got != `a="1";b="2"` {
		t.Errorf("formatLabels = %s", got)
	}
}

// Process.Run samples on its interval until done; a nil process or a
// non-positive interval returns at once.
func TestProcessRun(t *testing.T) {
	var nilProc *Process
	nilProc.Run(time.Millisecond, nil)
	p := NewProcess()
	p.Run(0, nil)
	initial := p.Snapshot().LastSampleAt
	done := make(chan struct{})
	finished := make(chan struct{})
	go func() { p.Run(time.Millisecond, done); close(finished) }()
	deadline := time.Now().Add(2 * time.Second)
	for !p.Snapshot().LastSampleAt.After(initial) && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	close(done)
	select {
	case <-finished:
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not stop on done")
	}
	if !p.Snapshot().LastSampleAt.After(initial) {
		t.Error("Run never sampled on its tick")
	}
}

// The Prometheus registry refuses nil attachments, exposes its registry,
// the collectors are nil-safe, unregistered commands get a synthetic
// name, and label names are emitted sorted.
func TestPromRegistryContract(t *testing.T) {
	reg := NewPromRegistry()
	if reg.Registry() == nil {
		t.Fatal("Registry() must expose the underlying registry")
	}
	if err := reg.Attach(nil, nil); err == nil {
		t.Error("Attach(nil) must fail")
	}
	if err := reg.AttachProcess(nil); err == nil {
		t.Error("AttachProcess(nil) must fail")
	}
	c := NewConnector()
	c.ObserveCmdRx(0x42, 5) // never registered: synthetic cmd_0x42 name
	if err := reg.Attach(c, map[string]string{"z": "1", "a": "2"}); err != nil {
		t.Fatal(err)
	}
	if err := reg.AttachProcess(NewProcess()); err != nil {
		t.Fatal(err)
	}
	fams, err := reg.Registry().Gather()
	if err != nil {
		t.Fatal(err)
	}
	var sawCmd, sawProc bool
	for _, f := range fams {
		if f.GetName() == "dhs_connector_rx_cmd_hits_total" {
			for _, m := range f.GetMetric() {
				var names []string
				for _, l := range m.GetLabel() {
					names = append(names, l.GetName())
					if l.GetName() == "cmd_name" && l.GetValue() == "cmd_0x42" {
						sawCmd = true
					}
				}
				// Gather sorts label pairs; what must hold is the SET: both
				// base labels and both command labels, nothing dropped.
				if strings.Join(names, ",") != "a,cmd_id,cmd_name,z" {
					t.Errorf("labels = %v, want a, cmd_id, cmd_name, z", names)
				}
			}
		}
		if strings.HasPrefix(f.GetName(), "dhs_process_") {
			sawProc = true
		}
	}
	if !sawCmd || !sawProc {
		t.Errorf("gather: synthetic cmd name seen=%v, process metrics seen=%v", sawCmd, sawProc)
	}

	ch := make(chan prometheus.Metric, 8)
	var nilCC *connectorCollector
	nilCC.Collect(ch)
	(&connectorCollector{}).Collect(ch)
	var nilPC *processCollector
	nilPC.Collect(ch)
	(&processCollector{}).Collect(ch)
	if len(ch) != 0 {
		t.Error("nil collectors must emit nothing")
	}
	if n, v := labelsFor(nil); n != nil || v != nil {
		t.Error("no labels yields nil slices")
	}
}
