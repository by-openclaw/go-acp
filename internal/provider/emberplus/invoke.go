package emberplus

import (
	"fmt"
	"sync"

	"acp/internal/export/canonical"
	"acp/internal/protocol/emberplus/ber"
	"acp/internal/protocol/emberplus/glow"
)

// FunctionImpl is a provider-side callback bound to a canonical Function.
// Input args arrive as the concrete Go values the decoder emits
// (int64 / float64 / string / bool / []byte / nil). Returning (nil, err)
// produces an InvocationResult with success=false.
type FunctionImpl func(args []any) ([]any, error)

// functionRegistry maps function OID -> callback. Accessed under s.mu.
type functionRegistry struct {
	mu  sync.RWMutex
	byOID map[string]FunctionImpl
}

func newFunctionRegistry() *functionRegistry {
	return &functionRegistry{byOID: map[string]FunctionImpl{}}
}

func (r *functionRegistry) register(oid string, fn FunctionImpl) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.byOID[oid] = fn
}

func (r *functionRegistry) lookup(oid string) (FunctionImpl, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	fn, ok := r.byOID[oid]
	return fn, ok
}

// invokeFunction runs the callback for the given function OID. Returns the
// result tuple and a success flag. Missing function or callback error
// yields (nil, false).
func (s *server) invokeFunction(oid string, args []any) ([]any, bool) {
	if s.funcs == nil {
		return nil, false
	}
	fn, ok := s.funcs.lookup(oid)
	if !ok {
		s.logger.Debug("invoke: no function registered", "oid", oid)
		return nil, false
	}
	result, err := fn(args)
	if err != nil {
		s.logger.Debug("invoke: function returned error", "oid", oid, "err", err.Error())
		return nil, false
	}
	return result, true
}

// applyMatrixConnections mutates a Matrix element's connections per the
// inbound Connection list and returns the resulting post-state to broadcast.
//
// Semantics (spec p.89 ConnectionOperation):
//
//	absolute (default) — for oneToN: set target's sources list to the
//	                     incoming sources. For nToN: replace.
//	connect           — nToN only: add sources to existing connection.
//	disconnect        — nToN only: remove sources from existing connection.
//
// The returned Connection list uses disposition=tally so consumers treat it
// as confirmed current state.
func (s *server) applyMatrixConnections(matrixOID string, incoming []canonical.MatrixConnection) ([]canonical.MatrixConnection, error) {
	e, ok := s.tree.lookupOID(matrixOID)
	if !ok {
		return nil, fmt.Errorf("matrix %q not found", matrixOID)
	}
	m, ok := e.el.(*canonical.Matrix)
	if !ok {
		return nil, fmt.Errorf("oid %q is %s, not matrix", matrixOID, e.el.Kind())
	}

	s.tree.mu.Lock()
	defer s.tree.mu.Unlock()

	out := make([]canonical.MatrixConnection, 0, len(incoming))
	for _, in := range incoming {
		applied := applyOneConnection(m, in)
		out = append(out, applied)
	}
	return out, nil
}

// applyOneConnection mutates m.Connections for a single inbound change and
// returns the post-state (disposition=tally) that should be echoed back.
func applyOneConnection(m *canonical.Matrix, in canonical.MatrixConnection) canonical.MatrixConnection {
	idx := findConnectionIndex(m.Connections, in.Target)
	var sources []int64
	switch in.Operation {
	case canonical.ConnOpConnect:
		if idx >= 0 {
			sources = mergeSources(m.Connections[idx].Sources, in.Sources)
		} else {
			sources = append([]int64{}, in.Sources...)
		}
	case canonical.ConnOpDisconnect:
		if idx >= 0 {
			sources = subtractSources(m.Connections[idx].Sources, in.Sources)
		}
	default: // absolute
		sources = append([]int64{}, in.Sources...)
	}

	post := canonical.MatrixConnection{
		Target:      in.Target,
		Sources:     sources,
		Operation:   canonical.ConnOpAbsolute,
		Disposition: canonical.ConnDispTally,
	}
	if idx >= 0 {
		m.Connections[idx] = post
	} else {
		m.Connections = append(m.Connections, post)
	}
	return post
}

func findConnectionIndex(conns []canonical.MatrixConnection, target int64) int {
	for i, c := range conns {
		if c.Target == target {
			return i
		}
	}
	return -1
}

func mergeSources(existing, add []int64) []int64 {
	seen := make(map[int64]struct{}, len(existing)+len(add))
	out := make([]int64, 0, len(existing)+len(add))
	for _, s := range existing {
		if _, dup := seen[s]; dup {
			continue
		}
		seen[s] = struct{}{}
		out = append(out, s)
	}
	for _, s := range add {
		if _, dup := seen[s]; dup {
			continue
		}
		seen[s] = struct{}{}
		out = append(out, s)
	}
	return out
}

func subtractSources(existing, remove []int64) []int64 {
	drop := make(map[int64]struct{}, len(remove))
	for _, s := range remove {
		drop[s] = struct{}{}
	}
	out := make([]int64, 0, len(existing))
	for _, s := range existing {
		if _, ok := drop[s]; ok {
			continue
		}
		out = append(out, s)
	}
	return out
}

// encodeMatrixConnectionsAnnouncement builds a Root frame carrying a
// QualifiedMatrix with just the path + [CTX 5] connections — the minimal
// shape consumers accept as a live crosspoint update.
func (s *server) encodeMatrixConnectionsAnnouncement(e *entry, conns []canonical.MatrixConnection) []byte {
	qm := ber.AppConstructed(glow.TagQualifiedMatrix,
		ber.ContextConstructed(0, ber.RelOID(encodeRelativeOID(e.oidParts))),
		ber.ContextConstructed(5, encodeConnections(conns)),
	)
	root := ber.AppConstructed(glow.TagRoot,
		ber.AppConstructed(glow.TagRootElementCollection,
			ber.ContextConstructed(0, qm),
		),
	)
	return ber.EncodeTLV(root)
}

// broadcastMatrixConnections sends a connection-change announcement to
// every session subscribed to the matrix OID, plus the originating session
// so strict viewers get a direct echo.
func (s *server) broadcastMatrixConnections(matrixOID string, conns []canonical.MatrixConnection, origin *session) {
	e, ok := s.tree.lookupOID(matrixOID)
	if !ok {
		return
	}
	payload := s.encodeMatrixConnectionsAnnouncement(e, conns)

	s.mu.Lock()
	set := s.subs[matrixOID]
	targets := make([]*session, 0, len(set)+1)
	for sess := range set {
		targets = append(targets, sess)
	}
	s.mu.Unlock()

	sent := map[*session]struct{}{}
	for _, sess := range targets {
		sess.send(payload)
		sent[sess] = struct{}{}
	}
	if origin != nil {
		if _, dup := sent[origin]; !dup {
			origin.send(payload)
		}
	}
}
