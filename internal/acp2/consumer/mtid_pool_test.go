package acp2

import (
	"context"
	"io"
	"log/slog"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// newPoolTestSession builds a Session with the mtid pool ready (the
// fields the pool exercises don't need a live TCP connection — alloc
// /release just touch mtidPool + mtidCond + mtidMu).
func newPoolTestSession() *Session {
	s := &Session{
		logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
	s.mtidCond = sync.NewCond(&s.mtidMu)
	return s
}

// TestMTIDPool_AllocateUniqueAndRelease spec p.4 §"Mtid": "ACP2 only
// uses the mtid to send a reply and does no other checks." The
// allocator MUST never hand out an mtid that's currently in flight.
func TestMTIDPool_AllocateUniqueAndRelease(t *testing.T) {
	s := newPoolTestSession()
	ctx := context.Background()

	// Allocate every mtid (1..255). Each must be unique.
	seen := make(map[uint8]bool, 255)
	got := make([]uint8, 0, 255)
	for i := 0; i < 255; i++ {
		m, err := s.allocMTID(ctx)
		if err != nil {
			t.Fatalf("allocMTID #%d: %v", i, err)
		}
		if m == 0 {
			t.Fatalf("allocMTID returned 0 (reserved for announces)")
		}
		if seen[m] {
			t.Fatalf("allocMTID returned duplicate in-flight mtid %d", m)
		}
		seen[m] = true
		got = append(got, m)
	}
	if len(seen) != 255 {
		t.Errorf("got %d unique mtids; want 255", len(seen))
	}

	// Release them all and verify the next alloc reuses freed slots.
	for _, m := range got {
		s.releaseMTID(m)
	}
	m2, err := s.allocMTID(ctx)
	if err != nil {
		t.Fatalf("alloc after full release: %v", err)
	}
	if m2 == 0 {
		t.Fatal("allocMTID returned 0 after release")
	}
	s.releaseMTID(m2)
}

// TestMTIDPool_BlocksWhenExhausted verifies that a 256th concurrent
// alloc blocks on mtidCond until a release. Spec p.4 says the mtid
// space is 1..255; never wrap.
func TestMTIDPool_BlocksWhenExhausted(t *testing.T) {
	s := newPoolTestSession()
	ctx := context.Background()

	// Fill the pool.
	held := make([]uint8, 0, 255)
	for i := 0; i < 255; i++ {
		m, err := s.allocMTID(ctx)
		if err != nil {
			t.Fatalf("alloc #%d: %v", i, err)
		}
		held = append(held, m)
	}

	// 256th call must block. Verify by racing it against a 50 ms
	// release-trigger goroutine.
	allocResult := make(chan uint8, 1)
	allocErr := make(chan error, 1)
	go func() {
		m, err := s.allocMTID(ctx)
		if err != nil {
			allocErr <- err
			return
		}
		allocResult <- m
	}()

	select {
	case m := <-allocResult:
		t.Fatalf("256th alloc returned %d immediately; expected to block", m)
	case <-time.After(20 * time.Millisecond):
		// Good — the 256th call is blocked.
	}

	// Release one and verify the blocked goroutine unblocks.
	s.releaseMTID(held[42])

	select {
	case m := <-allocResult:
		if m == 0 {
			t.Errorf("alloc returned 0 after release")
		}
		// Clean up: release everything we still hold + the new one.
		s.releaseMTID(m)
		for i, h := range held {
			if i == 42 {
				continue // already released
			}
			s.releaseMTID(h)
		}
	case err := <-allocErr:
		t.Fatalf("alloc errored after release: %v", err)
	case <-time.After(200 * time.Millisecond):
		t.Fatal("alloc did not unblock after release")
	}
}

// TestMTIDPool_ConcurrentAllocReleaseNeverDuplicates pounds the pool
// with 50 goroutines each running 100 alloc/release cycles. Tracks
// every (alloc, release) timestamp to detect any moment where two
// goroutines hold the same mtid simultaneously.
func TestMTIDPool_ConcurrentAllocReleaseNeverDuplicates(t *testing.T) {
	s := newPoolTestSession()
	ctx := context.Background()

	const goroutines = 50
	const iterations = 100

	// inFlight tracks mtid -> bool atomically. Use a [256]uint32 with
	// CAS to detect simultaneous holding.
	var inFlight [256]uint32
	var dupCount uint64

	var wg sync.WaitGroup
	wg.Add(goroutines)
	for g := 0; g < goroutines; g++ {
		go func() {
			defer wg.Done()
			for i := 0; i < iterations; i++ {
				m, err := s.allocMTID(ctx)
				if err != nil {
					t.Errorf("alloc: %v", err)
					return
				}
				if !atomic.CompareAndSwapUint32(&inFlight[m], 0, 1) {
					atomic.AddUint64(&dupCount, 1)
				}
				// Hold for a short time, then release.
				atomic.StoreUint32(&inFlight[m], 0)
				s.releaseMTID(m)
			}
		}()
	}
	wg.Wait()

	if dupCount > 0 {
		t.Errorf("mtid pool returned %d simultaneously-held mtids across %d goroutines x %d iterations",
			dupCount, goroutines, iterations)
	}
}

// TestMTIDPool_RespectsContextCancel verifies that allocMTID returns
// ctx.Err() when the context cancels while waiting on a full pool.
func TestMTIDPool_RespectsContextCancel(t *testing.T) {
	s := newPoolTestSession()

	// Fill the pool.
	for i := 0; i < 255; i++ {
		if _, err := s.allocMTID(context.Background()); err != nil {
			t.Fatalf("fill alloc #%d: %v", i, err)
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()

	_, err := s.allocMTID(ctx)
	if err == nil {
		t.Fatal("alloc on full pool with cancelled ctx returned no error")
	}
	if err != context.DeadlineExceeded && err != context.Canceled {
		t.Errorf("alloc returned %v; want context.DeadlineExceeded", err)
	}
}
