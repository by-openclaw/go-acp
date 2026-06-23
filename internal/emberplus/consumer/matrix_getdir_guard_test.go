package emberplus

import (
	"net"
	"sync/atomic"
	"testing"
	"time"

	"dhs/internal/consumer"
	"dhs/internal/consumer/compliance"
	"dhs/internal/emberplus/codec/glow"
	"dhs/internal/emberplus/codec/s101"
)

// TestProcessMatrix_EmptyConn_RequestsOnlyOnFirstSighting is the
// regression for the DHD "Device.routing.2" infinite loop.
//
// A matrix with ZERO current connections is a legal steady state. The
// provider answers our lazy GetDirectory with the matrix again, still
// connections-empty. processMatrix must request the directory only on
// FIRST sighting (isInitial) — re-requesting on every empty re-delivery
// keeps the walk settle-timer alive forever and spins the CPU.
func TestProcessMatrix_EmptyConn_RequestsOnlyOnFirstSighting(t *testing.T) {
	srvConn, cliConn := net.Pipe()
	defer func() { _ = srvConn.Close() }()
	defer func() { _ = cliConn.Close() }()

	s := NewSession(discardLogger())
	s.SetProfile(&compliance.Profile{})
	s.mu.Lock()
	s.conn = srvConn
	s.reader = s101.NewReader(srvConn)
	s.writer = s101.NewWriter(srvConn)
	s.closed = false
	s.mu.Unlock()

	// Count outbound frames on the client end. processMatrix's only wire
	// side effect is the lazy GetDirectory, so frame count == request count.
	var frames int64
	r := s101.NewReader(cliConn)
	go func() {
		for {
			if _, err := r.ReadFrame(); err != nil {
				return
			}
			atomic.AddInt64(&frames, 1)
		}
	}()

	p := freshPlugin()
	p.session = s

	emptyMatrix := func() []glow.Element {
		return []glow.Element{
			{Matrix: &glow.Matrix{Number: 1, Identifier: "routing", MatrixType: glow.MatrixTypeNToN,
				TargetCount: 2, SourceCount: 2}}, // no Connections
		}
	}

	// First sighting → exactly one lazy GetDirectory.
	p.handleElements(emptyMatrix())
	time.Sleep(100 * time.Millisecond)
	// Re-delivery, still empty → must NOT fire again (isInitial == false).
	p.handleElements(emptyMatrix())
	time.Sleep(100 * time.Millisecond)

	if got := atomic.LoadInt64(&frames); got != 1 {
		t.Errorf("matrix GetDirectory must fire once on first sighting; got %d sends "+
			"(>1 == the empty-connections re-request loop is back)", got)
	}
}

// TestProcessMatrix_TallyFloodSuppressed is the regression for the DHD
// Device.Routing.2 watch flood: a big matrix streams its current tally in
// multiple waves (and providers re-broadcast it), and the consumer must
// emit watch events ONLY for genuine reroutes of already-known targets —
// not for the initial tally or its re-sends. (acp2-style: changes only.)
func TestProcessMatrix_TallyFloodSuppressed(t *testing.T) {
	p := freshPlugin()
	var events int64
	p.subs["*"] = func(consumer.Event) { atomic.AddInt64(&events, 1) }

	mat := func(conns []glow.Connection) []glow.Element {
		return []glow.Element{{Matrix: &glow.Matrix{
			Number: 1, Identifier: "Routing", MatrixType: glow.MatrixTypeNToN,
			TargetCount: 8, SourceCount: 8, Connections: conns,
		}}}
	}

	// Initial tally wave 1 (first sight) — silent.
	p.handleElements(mat([]glow.Connection{
		{Target: 1, Sources: []int32{10}},
		{Target: 2, Sources: []int32{}},
		{Target: 3, Sources: []int32{20}},
	}))
	// Initial tally wave 2 — multi-frame continuation, brand-new targets 4,5.
	// New targets == initial population, NOT changes → silent.
	p.handleElements(mat([]glow.Connection{
		{Target: 4, Sources: []int32{}},
		{Target: 5, Sources: []int32{30}},
	}))
	// Provider re-broadcasts the whole tally, identical sources → silent.
	p.handleElements(mat([]glow.Connection{
		{Target: 1, Sources: []int32{10}},
		{Target: 3, Sources: []int32{20}},
		{Target: 5, Sources: []int32{30}},
	}))
	if got := atomic.LoadInt64(&events); got != 0 {
		t.Fatalf("initial tally + re-broadcast must be silent, got %d events", got)
	}

	// A genuine reroute of a KNOWN target (1: 10 -> 11) fires exactly once.
	p.handleElements(mat([]glow.Connection{{Target: 1, Sources: []int32{11}}}))
	if got := atomic.LoadInt64(&events); got != 1 {
		t.Fatalf("one real reroute should fire exactly one event, got %d", got)
	}
}
