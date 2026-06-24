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

// TestProcessMatrix_ConnectedButNoContents_FetchesContents is the DHD-9000
// regression: the console serves the matrix node + its connections inline but
// defers MatrixContents (targetCount/labels/targets) to an explicit matrix
// GetDirectory — exactly what EmberPlusView issues to render the labelled
// grid. A matrix that arrives WITH a connection but WITHOUT contents must
// still trigger that fetch on first sight (else crosspoint labels never
// resolve). One request, isInitial-gated (no spin).
func TestProcessMatrix_ConnectedButNoContents_FetchesContents(t *testing.T) {
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

	// Matrix arrives WITH a connection but NO MatrixContents (targetCount 0,
	// no labels) — the DHD console shape.
	bare := func() []glow.Element {
		return []glow.Element{{Matrix: &glow.Matrix{
			Number: 2, Identifier: "matrix",
			Connections: []glow.Connection{{Target: 361, Sources: []int32{213}}},
		}}}
	}
	p.handleElements(bare())
	time.Sleep(100 * time.Millisecond)
	// Re-delivery (still no contents) must NOT re-fire — isInitial gate.
	p.handleElements(bare())
	time.Sleep(100 * time.Millisecond)

	if got := atomic.LoadInt64(&frames); got != 1 {
		t.Errorf("matrix with connection-but-no-contents must fetch contents once on first sight; got %d GetDirectory sends", got)
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

// TestProcessMatrix_ConnectionDeltaPreservesContents is the regression for
// the DHD matrix-label loss: a provider sends full MatrixContents once
// (targetCount/sourceCount/labels) then connection-only deltas. The delta
// must NOT wipe the static contents — otherwise targetCount resets to 0 and
// the labels descriptor is dropped, so crosspoint labels vanish. Ground
// truth from the DHD (Ember+ Viewer XML): targetCount 147, sourceCount 130,
// labels basePath 0.5.1 "Primary".
func TestProcessMatrix_ConnectionDeltaPreservesContents(t *testing.T) {
	p := freshPlugin()

	// Full MatrixContents (first sight).
	p.handleElements([]glow.Element{{Matrix: &glow.Matrix{
		Number: 2, Identifier: "matrix", MatrixType: glow.MatrixTypeNToN,
		AddressingMode: 1, TargetCount: 147, SourceCount: 130,
		Labels:      []glow.Label{{BasePath: []int32{0, 5, 1}, Description: "Primary"}},
		Connections: []glow.Connection{{Target: 20, Sources: []int32{126}}},
	}}})

	// Connection-only delta: a later reroute, no MatrixContents fields.
	p.handleElements([]glow.Element{{Matrix: &glow.Matrix{
		Number: 2, Identifier: "matrix",
		Connections: []glow.Connection{{Target: 21, Sources: []int32{69}}},
	}}})

	e := p.numIndex["2"]
	if e == nil {
		t.Fatal("matrix not indexed at OID 2")
	}
	if tc, _ := e.obj.Meta["targetCount"].(int32); tc != 147 {
		t.Errorf("targetCount after connection-only delta = %v, want 147 (delta wiped contents)", e.obj.Meta["targetCount"])
	}
	if sc, _ := e.obj.Meta["sourceCount"].(int32); sc != 130 {
		t.Errorf("sourceCount after delta = %v, want 130", e.obj.Meta["sourceCount"])
	}
	labels, ok := e.obj.Meta["labels"].([]map[string]any)
	if !ok || len(labels) != 1 {
		t.Fatalf("labels descriptor lost after delta: %#v", e.obj.Meta["labels"])
	}
	if labels[0]["basePath"] != "0.5.1" || labels[0]["description"] != "Primary" {
		t.Errorf("labels descriptor = %v, want basePath 0.5.1 / Primary", labels[0])
	}
}
