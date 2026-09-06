package acp1

import (
	"context"
	"math/rand"
	"sync/atomic"
	"testing"
	"time"

	"dhs/internal/acp1/codec"
)

// announceCounter wraps a server's broadcast path so tests can count
// announces without a real socket. We swap broadcastTCPAnnounce via
// the registry; the UDP path returns early with no socket.
func announceCounter(t *testing.T, s *server) *atomic.Int64 {
	t.Helper()
	var n atomic.Int64
	reg := newTCPSessionRegistry(s.logger)
	send := make(chan []byte, 4096)
	reg.register("10.0.0.1", nil, send)
	s.tcpRegistry = reg
	go func() {
		for range send {
			n.Add(1)
		}
	}()
	t.Cleanup(func() { close(send) })
	return &n
}

func TestRunFuzz_NoTargets(t *testing.T) {
	s := newTestServer(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	cfg := FuzzConfig{
		Seed: 42,
		Rate: 10,
		Slot: 99, // no such slot
		ID:   -1,
	}
	err := s.RunFuzz(ctx, cfg)
	if err == nil {
		t.Fatal("expected error for no eligible targets")
	}
}

func TestRunFuzz_DurationStopsLoop(t *testing.T) {
	s := newTestServer(t)
	announceCounter(t, s)

	cfg := FuzzConfig{
		Seed:     42,
		Rate:     50,
		Duration: 200 * time.Millisecond,
		Slot:     -1,
		ID:       -1,
	}
	start := time.Now()
	if err := s.RunFuzz(context.Background(), cfg); err != nil {
		t.Fatalf("RunFuzz: %v", err)
	}
	elapsed := time.Since(start)
	if elapsed < 150*time.Millisecond || elapsed > 600*time.Millisecond {
		t.Fatalf("duration not honoured: ran for %v (want ~200ms)", elapsed)
	}
}

func TestRunFuzz_ContextCancelStopsLoop(t *testing.T) {
	s := newTestServer(t)
	announceCounter(t, s)

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(150 * time.Millisecond)
		cancel()
	}()
	cfg := FuzzConfig{
		Seed: 42,
		Rate: 50,
		Slot: -1,
		ID:   -1,
	}
	start := time.Now()
	if err := s.RunFuzz(ctx, cfg); err != nil {
		t.Fatalf("RunFuzz: %v", err)
	}
	elapsed := time.Since(start)
	if elapsed > 600*time.Millisecond {
		t.Fatalf("ctx cancel not honoured: ran for %v", elapsed)
	}
}

func TestRunFuzz_ProducesAnnounces(t *testing.T) {
	s := newTestServer(t)
	count := announceCounter(t, s)

	cfg := FuzzConfig{
		Seed:     1,
		Rate:     100,
		Duration: 200 * time.Millisecond,
		Slot:     -1,
		ID:       -1,
	}
	if err := s.RunFuzz(context.Background(), cfg); err != nil {
		t.Fatalf("RunFuzz: %v", err)
	}
	// Allow time for the consumer goroutine to drain.
	time.Sleep(50 * time.Millisecond)
	if got := count.Load(); got < 5 {
		t.Fatalf("announces emitted = %d, want at least 5 over 200ms at rate 100", got)
	}
}

func TestRunFuzz_GroupFilter(t *testing.T) {
	s := newTestServer(t)
	announceCounter(t, s)

	// Filter to control group only — slot 1 control 0 is writable in
	// the test fixture; identity is read-only.
	cfg := FuzzConfig{
		Seed:     1,
		Rate:     100,
		Duration: 100 * time.Millisecond,
		Slot:     1,
		Group:    codec.GroupControl,
		GroupSet: true,
		ID:       -1,
	}
	if err := s.RunFuzz(context.Background(), cfg); err != nil {
		t.Fatalf("RunFuzz: %v", err)
	}
}

func TestRunFuzz_ReadOnlyTargetsExcluded(t *testing.T) {
	s := newTestServer(t)

	// Identity (group=1) in the test fixture is read-only. A fuzz
	// scoped to identity should report "no eligible targets".
	cfg := FuzzConfig{
		Seed:     1,
		Rate:     10,
		Slot:     1,
		Group:    codec.GroupIdentity,
		GroupSet: true,
		ID:       -1,
	}
	err := s.RunFuzz(context.Background(), cfg)
	if err == nil {
		t.Fatal("expected error: identity is read-only, no eligible targets")
	}
}

func TestSynthInteger_RespectsMinMax(t *testing.T) {
	// Synth 200 random values for a fixture entry; every one must
	// fall within [min, max].
	s := newTestServer(t)
	s.tree.mu.RLock()
	e := s.tree.entries[objectKey{slot: 1, group: codec.GroupControl, id: 0}]
	s.tree.mu.RUnlock()
	if e == nil {
		t.Fatal("test fixture missing slot=1 control id=0")
	}
	min, max := readIntBounds(e)

	for i := 0; i < 200; i++ {
		bytes, err := s.synthesiseValue(testRand(int64(i)), e, false, i)
		if err != nil {
			t.Fatalf("synthesiseValue: %v", err)
		}
		var v int64
		switch len(bytes) {
		case 1:
			v = int64(int8(bytes[0]))
		case 2:
			v = int64(int16(bytes[0])<<8 | int16(bytes[1]))
		case 4:
			v = int64(int32(bytes[0])<<24 | int32(bytes[1])<<16 | int32(bytes[2])<<8 | int32(bytes[3]))
		}
		if v < min || v > max {
			t.Fatalf("synth value %d out of [%d, %d]", v, min, max)
		}
	}
}

// testRand provides a deterministic RNG seeded with the iteration index.
func testRand(seed int64) *rand.Rand {
	return rand.New(rand.NewSource(seed))
}
