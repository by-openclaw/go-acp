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
