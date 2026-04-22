package metrics

import (
	"runtime"
	"testing"
	"time"
)

func TestProcessSnapshot(t *testing.T) {
	p := NewProcess()
	// NewProcess calls Sample once; values should be populated.
	s := p.Snapshot()
	if s.HeapAllocBytes == 0 {
		t.Errorf("HeapAllocBytes should be non-zero after Sample")
	}
	if s.Goroutines <= 0 {
		t.Errorf("Goroutines should be positive, got %d", s.Goroutines)
	}
	if s.NumCPU != int64(runtime.NumCPU()) {
		t.Errorf("NumCPU = %d, want %d", s.NumCPU, runtime.NumCPU())
	}
	if s.Uptime <= 0 {
		t.Errorf("Uptime should be positive")
	}
	if s.LastSampleAt.IsZero() {
		t.Errorf("LastSampleAt should be set")
	}
}

func TestProcessSample(t *testing.T) {
	p := NewProcess()
	first := p.Snapshot().LastSampleAt

	// Force a heap allocation then re-sample.
	_ = make([]byte, 1<<20)
	time.Sleep(5 * time.Millisecond)
	p.Sample()

	second := p.Snapshot().LastSampleAt
	if !second.After(first) {
		t.Errorf("second sample timestamp %v should be after first %v", second, first)
	}
}

func TestProcessNilSafe(t *testing.T) {
	var p *Process
	p.Sample()
	_ = p.Snapshot()
}
