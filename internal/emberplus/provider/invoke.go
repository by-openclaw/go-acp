package emberplus

import (
	"fmt"
	"sync"

	"dhs/internal/export/canonical"
	"dhs/internal/emberplus/codec/ber"
	"dhs/internal/emberplus/codec/glow"
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
// Matrix type is enforced here (spec p.88):
//
//	oneToN   — target has at most 1 source; same source MAY be on many targets
//	oneToOne — target has at most 1 source; source has at most 1 target
//	           (bijective). When a new binding steals a source, the losing
//	           target is emitted in the returned tally so consumers redraw.
//	nToN     — free many-to-many. connect / disconnect are additive;
//	           absolute replaces the target's sources list.
//
// Operation semantics (spec p.89):
//
//	absolute (default) — set target's sources list to the incoming sources
//	connect            — nToN only: add sources to existing connection
//	disconnect         — nToN only: remove sources from existing connection
//
// The returned list uses disposition=tally so consumers treat it as
// confirmed current state.
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

	// Use a map keyed by target so multiple updates to the same target in
	// one request collapse to the final state (last-write-wins). Preserves
	// insertion order via a parallel slice.
	out := []canonical.MatrixConnection{}
	touched := map[int64]int{} // target -> index into out
	emit := func(post canonical.MatrixConnection) {
		if idx, ok := touched[post.Target]; ok {
			out[idx] = post
			return
		}
		touched[post.Target] = len(out)
		out = append(out, post)
	}

	for _, in := range incoming {
		// Spec p.89: locked target rejects the change. Provider echoes
		// the unchanged sources with disposition=locked so the consumer
		// knows the request was seen but not applied.
		if s.locks != nil && s.locks.isLocked(matrixOID, in.Target) {
			var current []int64
			if idx := findConnectionIndex(m.Connections, in.Target); idx >= 0 {
				current = append([]int64{}, m.Connections[idx].Sources...)
			}
			emit(canonical.MatrixConnection{
				Target:      in.Target,
				Sources:     current,
				Operation:   canonical.ConnOpAbsolute,
				Disposition: canonical.ConnDispLocked,
			})
			continue
		}

		// oneToN + oneToOne enforce target-side cardinality of 1. Connect
		// performs a union via applyOneConnection, which on these matrix
		// types would leave the target holding (oldSrc + newSrc) = 2
		// sources — violating the invariant. Coerce Connect to Absolute
		// so the single source is replaced, matching spec p.89 plus the
		// click-to-route UX every Ember+ client implements.
		//
		// Disconnect is NOT coerced — spec p.89 ConnectionOperation says
		// Disconnect removes the listed sources, and an empty sources[]
		// after the subtract is valid (target unrouted). Earlier coercion
		// of Disconnect → Absolute (PR #98) broke this — it kept the
		// target routed instead of clearing it.
		if m.Type == canonical.MatrixOneToN || m.Type == canonical.MatrixOneToOne {
			if len(in.Sources) > 1 {
				in.Sources = in.Sources[:1]
			}
			if in.Operation == canonical.ConnOpConnect {
				in.Operation = canonical.ConnOpAbsolute
			}
		}

		// Spec p.88: enforce maximumConnectsPerTarget and
		// maximumTotalConnects when declared. Compute the would-be
		// post-state first; if it would exceed either cap, reject the
		// request and echo the unchanged current sources back. Only
		// applies on operations that ADD sources (Connect + Absolute);
		// Disconnect can never grow a target so it bypasses the check.
		if (in.Operation == canonical.ConnOpConnect || in.Operation == canonical.ConnOpAbsolute) &&
			matrixHasCaps(m) {
			if rejected, post := rejectIfExceedsCap(m, in); rejected {
				emit(post)
				continue
			}
		}

		applied := applyOneConnection(m, in)
		emit(applied)

		// oneToOne: source-side exclusivity — any other target that held
		// one of the newly bound sources must release it. Emit the loser
		// as a separate tally so the consumer redraws.
		if m.Type == canonical.MatrixOneToOne {
			for _, src := range applied.Sources {
				for i := range m.Connections {
					if m.Connections[i].Target == applied.Target {
						continue
					}
					stripped := subtractSources(m.Connections[i].Sources, []int64{src})
					if len(stripped) == len(m.Connections[i].Sources) {
						continue
					}
					m.Connections[i].Sources = stripped
					emit(canonical.MatrixConnection{
						Target:      m.Connections[i].Target,
						Sources:     append([]int64{}, stripped...),
						Operation:   canonical.ConnOpAbsolute,
						Disposition: canonical.ConnDispTally,
					})
				}
			}
		}
	}
	return out, nil
}

// matrixHasCaps reports whether either MatrixContents [6]
// maximumTotalConnects or [7] maximumConnectsPerTarget is declared,
// per spec p.88.
func matrixHasCaps(m *canonical.Matrix) bool {
	return m.MaximumTotalConnects != nil || m.MaximumConnectsPerTarget != nil
}

// rejectIfExceedsCap computes the would-be post-state of in against m
// and, if it exceeds either of the declared caps (spec p.88), returns
// (true, echo) where echo carries the unchanged current sources with
// disposition=tally — the consumer-visible "your request was seen but
// rejected" reply. Returns (false, _) when the request is within caps.
func rejectIfExceedsCap(m *canonical.Matrix, in canonical.MatrixConnection) (bool, canonical.MatrixConnection) {
	idx := findConnectionIndex(m.Connections, in.Target)
	var wouldBe []int64
	switch in.Operation {
	case canonical.ConnOpConnect:
		if idx >= 0 {
			wouldBe = mergeSources(m.Connections[idx].Sources, in.Sources)
		} else {
			wouldBe = append([]int64{}, in.Sources...)
		}
	default: // absolute (and Disconnect already filtered out by caller)
		wouldBe = append([]int64{}, in.Sources...)
	}

	if m.MaximumConnectsPerTarget != nil && int64(len(wouldBe)) > *m.MaximumConnectsPerTarget {
		return true, currentTally(m, in.Target)
	}
	if m.MaximumTotalConnects != nil {
		total := int64(0)
		foundTarget := false
		for _, c := range m.Connections {
			if c.Target == in.Target {
				total += int64(len(wouldBe))
				foundTarget = true
			} else {
				total += int64(len(c.Sources))
			}
		}
		if !foundTarget {
			// New target not yet in m.Connections — its post-state
			// sources still count toward the matrix-wide total.
			total += int64(len(wouldBe))
		}
		if total > *m.MaximumTotalConnects {
			return true, currentTally(m, in.Target)
		}
	}
	return false, canonical.MatrixConnection{}
}

// currentTally echoes the existing sources of the target with
// disposition=tally — the spec p.89 shape for "no change, current
// state".
func currentTally(m *canonical.Matrix, target int64) canonical.MatrixConnection {
	var current []int64
	if idx := findConnectionIndex(m.Connections, target); idx >= 0 {
		current = append([]int64{}, m.Connections[idx].Sources...)
	}
	return canonical.MatrixConnection{
		Target:      target,
		Sources:     current,
		Operation:   canonical.ConnOpAbsolute,
		Disposition: canonical.ConnDispTally,
	}
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
// every active consumer session. Spec §p.88 implicitly subscribes a
// consumer to matrix changes on GetDirectory of that matrix, but the
// rule is patchy across consumers — some viewers walk matrix contents
// from a parent node reply instead of direct GetDirectory and never
// register a subscription. Matching the broadcastParam fan-out
// (libember/TinyEmber+ pattern), push to every connected session so
// crosspoint changes always reach the client regardless of walking
// style. The origin parameter stays for symmetry with the per-session
// echo callers expect.
func (s *server) broadcastMatrixConnections(matrixOID string, conns []canonical.MatrixConnection, origin *session) {
	e, ok := s.tree.lookupOID(matrixOID)
	if !ok {
		return
	}
	payload := s.encodeMatrixConnectionsAnnouncement(e, conns)

	s.mu.Lock()
	targets := make([]*session, 0, len(s.sessions))
	for sess := range s.sessions {
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
