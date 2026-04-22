package metrics

import (
	"testing"
	"time"
)

func TestConnectorObserve(t *testing.T) {
	c := NewConnector()
	c.ObserveRx(10)
	c.ObserveRx(20)
	c.ObserveTx(5, 5*time.Microsecond)
	c.ObserveTx(7, 500*time.Microsecond)
	c.ObserveTx(3, 2*time.Millisecond)
	c.ObserveDecodeError()
	c.ObserveNAK()
	c.ObserveNAK()
	c.ObserveTimeout()
	c.ObserveReconnect()

	s := c.Snapshot()
	if s.RxFrames != 2 || s.RxBytes != 30 {
		t.Errorf("rx counts wrong: frames=%d bytes=%d", s.RxFrames, s.RxBytes)
	}
	if s.TxFrames != 3 || s.TxBytes != 15 {
		t.Errorf("tx counts wrong: frames=%d bytes=%d", s.TxFrames, s.TxBytes)
	}
	if s.DecodeErrors != 1 || s.NAKs != 2 || s.Timeouts != 1 || s.Reconnects != 1 {
		t.Errorf("error counts wrong: decode=%d naks=%d to=%d rec=%d",
			s.DecodeErrors, s.NAKs, s.Timeouts, s.Reconnects)
	}
	if s.LatencyBuckets[0] != 1 { // 5µs < 10µs bucket 0
		t.Errorf("bucket 0 (<10µs) want 1 got %d", s.LatencyBuckets[0])
	}
	if s.LatencyBuckets[2] != 1 { // 500µs in [100,1000)
		t.Errorf("bucket 2 ([100,1k)µs) want 1 got %d", s.LatencyBuckets[2])
	}
	if s.LatencyBuckets[3] != 1 { // 2ms = 2000µs in [1k,10k)
		t.Errorf("bucket 3 ([1k,10k)µs) want 1 got %d", s.LatencyBuckets[3])
	}
}

func TestConnectorNilSafe(t *testing.T) {
	var c *Connector
	c.ObserveRx(1)
	c.ObserveTx(1, time.Microsecond)
	c.ObserveDecodeError()
	c.ObserveNAK()
	c.ObserveTimeout()
	c.ObserveReconnect()
	_ = c.Snapshot()
}

func TestSummaryFormat(t *testing.T) {
	c := NewConnector()
	c.ObserveRx(123)
	c.ObserveTx(45, 50*time.Microsecond)
	got := c.Summary()
	if len(got) < 50 {
		t.Errorf("summary too short: %q", got)
	}
}

func TestObserveCmdRxAndTx(t *testing.T) {
	c := NewConnector()
	c.RegisterCmd(0x02, "rx 002 Crosspoint Connect")
	c.RegisterCmd(0x04, "tx 004 Crosspoint Connected")

	c.ObserveCmdRx(0x02, 11)
	c.ObserveCmdRx(0x02, 11)
	c.ObserveCmdRx(0x02, 11)
	c.ObserveCmdTx(0x04, 12, 50*time.Microsecond)
	c.ObserveCmdTx(0x04, 12, 60*time.Microsecond)

	s := c.Snapshot()
	if s.RxFrames != 3 || s.TxFrames != 2 {
		t.Errorf("aggregate wrong: rx=%d tx=%d", s.RxFrames, s.TxFrames)
	}
	if s.RxHitsByCmd[0x02] != 3 {
		t.Errorf("cmd 0x02 rx hits = %d, want 3", s.RxHitsByCmd[0x02])
	}
	if s.TxHitsByCmd[0x04] != 2 {
		t.Errorf("cmd 0x04 tx hits = %d, want 2", s.TxHitsByCmd[0x04])
	}
	if s.RxBytesByCmd[0x02] != 33 {
		t.Errorf("cmd 0x02 rx bytes = %d, want 33", s.RxBytesByCmd[0x02])
	}
	if s.TxBytesByCmd[0x04] != 24 {
		t.Errorf("cmd 0x04 tx bytes = %d, want 24", s.TxBytesByCmd[0x04])
	}
	if s.CmdNames[0x02] != "rx 002 Crosspoint Connect" {
		t.Errorf("name lookup failed: %q", s.CmdNames[0x02])
	}

	top := s.TopCmdsByHits(5)
	if len(top) != 2 {
		t.Fatalf("TopCmdsByHits len = %d, want 2 (only registered cmds with hits)", len(top))
	}
	if top[0].ID != 0x02 || top[0].Hits != 3 {
		t.Errorf("top[0] = %+v, want id=0x02 hits=3", top[0])
	}
}

func TestTaskManagerFields(t *testing.T) {
	c := NewConnector()
	c.SetTreeBytes(1024)
	c.SetPoolBytes(2048)
	c.AddDiskBytes(512)
	// busyNanos is accumulated via ObserveCmdTx's handlerElapsed.
	c.ObserveCmdTx(0x01, 7, 500*time.Microsecond)
	c.ObserveCmdTx(0x01, 7, 500*time.Microsecond)

	s := c.Snapshot()
	if s.TreeBytes != 1024 || s.PoolBytes != 2048 || s.DiskBytes != 512 {
		t.Errorf("memory fields wrong: tree=%d pool=%d disk=%d",
			s.TreeBytes, s.PoolBytes, s.DiskBytes)
	}
	if s.EstimatedBytes != 1024+2048+512 {
		t.Errorf("EstimatedBytes = %d, want %d", s.EstimatedBytes, 1024+2048+512)
	}
	if s.CPUPercent < 0 || s.CPUPercent > 100 {
		t.Errorf("CPU%% out of range: %f", s.CPUPercent)
	}
	// busyNanos should have accumulated 2 × 500 µs = 1 ms.
	if c.busyNanos.Load() < 1_000_000-1000 {
		t.Errorf("busyNanos = %d, want ~1ms", c.busyNanos.Load())
	}
}

func TestCmdLatencyPercentiles(t *testing.T) {
	c := NewConnector()
	for i := 0; i < 100; i++ {
		c.ObserveCmdTx(0x20, 5, 50*time.Microsecond) // bucket 1 ([10,100))
	}
	s := c.Snapshot()
	p50, p95, p99 := s.CmdLatencyPercentiles(0x20)
	if p50 != 10 || p95 != 10 || p99 != 10 {
		t.Errorf("per-cmd percentiles: p50=%d p95=%d p99=%d, want 10/10/10", p50, p95, p99)
	}

	// Unregistered cmd: all zero.
	p50z, p95z, p99z := s.CmdLatencyPercentiles(0xFF)
	if p50z != 0 || p95z != 0 || p99z != 0 {
		t.Errorf("unobserved cmd percentiles should be 0, got %d/%d/%d", p50z, p95z, p99z)
	}
}

func TestLatencyPercentiles(t *testing.T) {
	c := NewConnector()
	for i := 0; i < 90; i++ {
		c.ObserveTx(1, 5*time.Microsecond) // bucket 0
	}
	for i := 0; i < 9; i++ {
		c.ObserveTx(1, 500*time.Microsecond) // bucket 2
	}
	c.ObserveTx(1, 50*time.Millisecond) // bucket 4
	s := c.Snapshot()
	p50, p95, p99 := s.LatencyPercentiles()
	if p50 != 0 {
		t.Errorf("p50 should be 0 (bucket 0 floor), got %d", p50)
	}
	if p95 != 100 && p95 != 1_000 {
		t.Errorf("p95 expected 100 (bucket 2) or 1k, got %d", p95)
	}
	if p99 < 100 {
		t.Errorf("p99 expected >=100, got %d", p99)
	}
}
