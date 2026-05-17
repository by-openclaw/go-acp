package emberplus

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"sync"

	"dhs/internal/export/canonical"
)

// lockStore holds per-target locks keyed by matrixOID -> target.
// Spec p.89: a locked target rejects connection changes and the
// provider replies with disposition=locked + the unchanged sources.
type lockStore struct {
	mu     sync.Mutex
	locked map[string]map[int64]bool
}

func newLockStore() *lockStore {
	return &lockStore{locked: map[string]map[int64]bool{}}
}

func (l *lockStore) set(matrixOID string, target int64, on bool) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	inner, ok := l.locked[matrixOID]
	if !ok {
		inner = map[int64]bool{}
		l.locked[matrixOID] = inner
	}
	prev := inner[target]
	if on {
		inner[target] = true
	} else {
		delete(inner, target)
	}
	return prev
}

func (l *lockStore) isLocked(matrixOID string, target int64) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	if inner, ok := l.locked[matrixOID]; ok {
		return inner[target]
	}
	return false
}

func (l *lockStore) list(matrixOID string) []int64 {
	l.mu.Lock()
	defer l.mu.Unlock()
	inner, ok := l.locked[matrixOID]
	if !ok {
		return nil
	}
	out := make([]int64, 0, len(inner))
	for t := range inner {
		out = append(out, t)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

// salvoStore holds matrix-connection snapshots keyed by
// matrixOID -> salvoID -> []MatrixConnection. In-memory only — restart
// drops every salvo. Realistic broadcast routers persist, but that is a
// later concern.
type salvoStore struct {
	mu    sync.Mutex
	saved map[string]map[int64][]canonical.MatrixConnection
}

func newSalvoStore() *salvoStore {
	return &salvoStore{saved: map[string]map[int64][]canonical.MatrixConnection{}}
}

func (s *salvoStore) store(matrixOID string, salvoID int64, conns []canonical.MatrixConnection) {
	s.mu.Lock()
	defer s.mu.Unlock()
	inner, ok := s.saved[matrixOID]
	if !ok {
		inner = map[int64][]canonical.MatrixConnection{}
		s.saved[matrixOID] = inner
	}
	copy := make([]canonical.MatrixConnection, len(conns))
	for i, c := range conns {
		src := make([]int64, len(c.Sources))
		for j, v := range c.Sources {
			src[j] = v
		}
		copy[i] = canonical.MatrixConnection{
			Target:      c.Target,
			Sources:     src,
			Operation:   c.Operation,
			Disposition: c.Disposition,
		}
	}
	inner[salvoID] = copy
}

func (s *salvoStore) recall(matrixOID string, salvoID int64) ([]canonical.MatrixConnection, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	inner, ok := s.saved[matrixOID]
	if !ok {
		return nil, false
	}
	conns, ok := inner[salvoID]
	return conns, ok
}

// list returns the salvoIDs stored for matrixOID, ascending. Empty slice
// if the matrix has no salvos yet.
func (s *salvoStore) list(matrixOID string) []int64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	inner, ok := s.saved[matrixOID]
	if !ok {
		return nil
	}
	ids := make([]int64, 0, len(inner))
	for id := range inner {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	return ids
}

// setupBuiltinFunctions walks the loaded tree and registers default
// callbacks for any canonical.Function whose identifier matches a known
// builtin name. Order mirrors the spec-p.91 examples + the
// broadcast-router convention (sum / recallSalvo / storeSalvo).
func (s *server) setupBuiltinFunctions() {
	if s.tree == nil {
		return
	}
	s.salvos = newSalvoStore()
	s.locks = newLockStore()

	// Pre-seed the lock store from canonical state. A connection with
	// disposition="locked" or locked=true in the seed represents an
	// already-locked target; without this, the runtime lockStore is
	// empty at boot and applyMatrixConnections (spec p.89 enforcement
	// path) would let any incoming Connect rewrite the supposedly
	// locked target. Walk every matrix in the tree once at startup.
	for _, ent := range s.tree.byOID {
		mtx, ok := ent.el.(*canonical.Matrix)
		if !ok {
			continue
		}
		moid := mtx.OID
		for _, c := range mtx.Connections {
			if c.Locked || c.Disposition == canonical.ConnDispLocked {
				s.locks.set(moid, c.Target, true)
			}
		}
	}

	s.walkFunctions(s.tree.root, func(e *entry, f *canonical.Function) {
		oid := e.el.Common().OID
		switch f.Identifier {
		case "sum", "addFunction", "add":
			s.funcs.register(oid, builtinSum)
		case "recallSalvo", "recall":
			s.funcs.register(oid, s.makeBuiltinRecallSalvo())
		case "storeSalvo", "store":
			s.funcs.register(oid, s.makeBuiltinStoreSalvo())
		case "listSalvos", "list":
			s.funcs.register(oid, s.makeBuiltinListSalvos())
		case "getSalvo":
			s.funcs.register(oid, s.makeBuiltinGetSalvo())
		case "setLock", "lock":
			s.funcs.register(oid, s.makeBuiltinSetLock())
		case "listLocks", "locks":
			s.funcs.register(oid, s.makeBuiltinListLocks())
		}
	})
}

// makeBuiltinSetLock binds a lock-toggle callback. Args:
//   - args[0] string matrixPath — OID or dotted identifier
//   - args[1] int64  target
//   - args[2] bool   locked   (true = lock, false = unlock)
//
// Returns the previous lock state (true if target was already locked).
// Locked targets reject Connection changes — see applyMatrixConnections.
func (s *server) makeBuiltinSetLock() FunctionImpl {
	return func(args []any) ([]any, error) {
		if len(args) < 3 {
			return nil, fmt.Errorf("setLock: need (matrixPath, target, locked)")
		}
		matrixRef, okP := args[0].(string)
		target, okT := asInt64(args[1])
		on, okL := args[2].(bool)
		if !okP || !okT || !okL {
			return nil, fmt.Errorf("setLock: bad arg types (%T, %T, %T)", args[0], args[1], args[2])
		}
		oid, _, ok := s.resolveMatrix(matrixRef)
		if !ok {
			return nil, fmt.Errorf("setLock: matrix %q not found", matrixRef)
		}
		prev := s.locks.set(oid, target, on)
		return []any{prev}, nil
	}
}

// makeBuiltinListLocks returns comma-separated locked target IDs for a
// matrix. Empty string if no targets are locked.
func (s *server) makeBuiltinListLocks() FunctionImpl {
	return func(args []any) ([]any, error) {
		if len(args) < 1 {
			return nil, fmt.Errorf("listLocks: need (matrixPath)")
		}
		matrixRef, ok := args[0].(string)
		if !ok {
			return nil, fmt.Errorf("listLocks: bad arg type (%T)", args[0])
		}
		oid, _, ok := s.resolveMatrix(matrixRef)
		if !ok {
			return nil, fmt.Errorf("listLocks: matrix %q not found", matrixRef)
		}
		targets := s.locks.list(oid)
		parts := make([]string, len(targets))
		for i, t := range targets {
			parts[i] = strconv.FormatInt(t, 10)
		}
		return []any{strings.Join(parts, ",")}, nil
	}
}

// walkFunctions invokes visit for every Function element reachable from el.
func (s *server) walkFunctions(el canonical.Element, visit func(*entry, *canonical.Function)) {
	if el == nil {
		return
	}
	if f, ok := el.(*canonical.Function); ok {
		if e, found := s.tree.lookupOID(f.OID); found {
			visit(e, f)
		}
	}
	for _, child := range el.Common().Children {
		s.walkFunctions(child, visit)
	}
}

// builtinSum adds the first two int arguments. Returns a single int result.
// Missing / non-int args produce success=false via the (nil, err) path.
func builtinSum(args []any) ([]any, error) {
	if len(args) < 2 {
		return nil, fmt.Errorf("sum: expected 2 args, got %d", len(args))
	}
	a, okA := asInt64(args[0])
	b, okB := asInt64(args[1])
	if !okA || !okB {
		return nil, fmt.Errorf("sum: non-integer args (%T, %T)", args[0], args[1])
	}
	return []any{a + b}, nil
}

// resolveMatrix accepts either a numeric OID ("1.4.3") or a dotted
// identifier path ("router.nToN.matrix") and returns the canonical OID
// plus the underlying Matrix element. Returns ("", nil, false) if the
// ref does not resolve to a Matrix — every caller (setLock /
// listLocks / storeSalvo / recallSalvo / listSalvos / getSalvo)
// surfaces the miss as a *function-level error* so the consumer's
// InvocationResult carries Success=false rather than a falsy default
// indistinguishable from a real result. Refs #455.
func (s *server) resolveMatrix(ref string) (string, *canonical.Matrix, bool) {
	e, ok := s.tree.lookupOID(ref)
	if !ok {
		e, ok = s.tree.lookupPath(ref)
	}
	if !ok {
		return "", nil, false
	}
	m, ok := e.el.(*canonical.Matrix)
	if !ok {
		return "", nil, false
	}
	return e.el.Common().OID, m, true
}

// makeBuiltinRecallSalvo binds a recall callback to this server. Args:
//   - args[0] string matrixPath — OID (e.g. "1.4.3") OR dotted identifier
//     path (e.g. "router.nToN.matrix"); both resolve to the same matrix.
//   - args[1] int64  salvoID
//
// Applies the saved crosspoint set to the matrix, broadcasts the resulting
// connections, and returns the number of connections applied. Returns 0
// if the ref does not point at a Matrix or the salvo was never stored.
func (s *server) makeBuiltinRecallSalvo() FunctionImpl {
	return func(args []any) ([]any, error) {
		if len(args) < 2 {
			return nil, fmt.Errorf("recallSalvo: need (matrixPath, salvoID)")
		}
		matrixRef, okP := args[0].(string)
		salvoID, okS := asInt64(args[1])
		if !okP || !okS {
			return nil, fmt.Errorf("recallSalvo: bad arg types (%T, %T)", args[0], args[1])
		}
		oid, _, ok := s.resolveMatrix(matrixRef)
		if !ok {
			return nil, fmt.Errorf("recallSalvo: matrix %q not found", matrixRef)
		}
		conns, ok := s.salvos.recall(oid, salvoID)
		if !ok {
			return []any{int64(0)}, nil
		}
		post, err := s.applyMatrixConnections(oid, conns)
		if err != nil {
			return nil, err
		}
		s.broadcastMatrixConnections(oid, post, nil)
		return []any{int64(len(post))}, nil
	}
}

// filterConnectionsByTargets returns the subset of `conns` whose
// Target matches one of the integers in `list`. Empty `list` returns
// the input unchanged (snapshot-all back-compat). Invalid integers in
// the list are skipped silently. Per spec p.91 Tuple — each function
// arg carries one Value, so a target list rides in as a delimited
// string. Any non-digit (and non-leading-minus) byte is a separator —
// `0,2` / `0;2` / `0|2` / `0 2` all parse the same. This dodges the
// PowerShell quoting trap where `--args "X,Y,`"0,2`""` collapses to
// empty third arg because PS strips the inner double-quotes before
// the process sees them.
func filterConnectionsByTargets(conns []canonical.MatrixConnection, list string) []canonical.MatrixConnection {
	list = strings.TrimSpace(list)
	if list == "" {
		return conns
	}
	want := map[int64]struct{}{}
	for _, tok := range strings.FieldsFunc(list, func(r rune) bool {
		return r != '-' && (r < '0' || r > '9')
	}) {
		if n, err := strconv.ParseInt(tok, 10, 64); err == nil {
			want[n] = struct{}{}
		}
	}
	if len(want) == 0 {
		return nil
	}
	out := make([]canonical.MatrixConnection, 0, len(want))
	for _, c := range conns {
		if _, ok := want[c.Target]; ok {
			out = append(out, c)
		}
	}
	return out
}

// makeBuiltinStoreSalvo binds a store callback. Args:
//   - args[0] string matrixPath — OID or dotted identifier path
//   - args[1] int64  salvoID
//   - args[2] string targets    — CSV of target numbers (e.g. "0,2,5")
//                                  to snapshot. Empty/missing = snapshot
//                                  every current connection. Strict per
//                                  spec p.91 Tuple semantics — each arg
//                                  is one Value, lists encode as
//                                  delimited strings.
//
// Returns true if at least one connection was stored. False when the
// matrix resolves but no listed target has any current sources.
// Returns (nil, err) — i.e. Success=false on the wire — when the ref
// does not resolve to a Matrix; the consumer must distinguish "no
// connections to snapshot" (success=true, result=false) from "matrix
// not found" (success=false). Refs #455.
func (s *server) makeBuiltinStoreSalvo() FunctionImpl {
	return func(args []any) ([]any, error) {
		if len(args) < 2 {
			return nil, fmt.Errorf("storeSalvo: need (matrixPath, salvoID[, targets])")
		}
		matrixRef, okP := args[0].(string)
		salvoID, okS := asInt64(args[1])
		if !okP || !okS {
			return nil, fmt.Errorf("storeSalvo: bad arg types (%T, %T)", args[0], args[1])
		}
		targetFilter := ""
		if len(args) >= 3 {
			if s, ok := args[2].(string); ok {
				targetFilter = s
			}
		}
		oid, m, ok := s.resolveMatrix(matrixRef)
		if !ok {
			return nil, fmt.Errorf("storeSalvo: matrix %q not found", matrixRef)
		}
		filtered := filterConnectionsByTargets(m.Connections, targetFilter)
		if len(filtered) == 0 {
			return []any{false}, nil
		}
		s.salvos.store(oid, salvoID, filtered)
		return []any{true}, nil
	}
}

// makeBuiltinListSalvos returns a comma-separated list of stored salvo
// IDs for the given matrix, ascending. Args:
//   - args[0] string matrixPath — OID or dotted identifier path
//
// Empty string if the matrix has no salvos yet. Returns (nil, err) —
// Success=false on the wire — when the ref does not resolve to a
// Matrix; the consumer must distinguish "no salvos stored yet"
// (success=true, result="") from "matrix not found"
// (success=false). Refs #455.
func (s *server) makeBuiltinListSalvos() FunctionImpl {
	return func(args []any) ([]any, error) {
		if len(args) < 1 {
			return nil, fmt.Errorf("listSalvos: need (matrixPath)")
		}
		matrixRef, ok := args[0].(string)
		if !ok {
			return nil, fmt.Errorf("listSalvos: bad arg type (%T)", args[0])
		}
		oid, _, ok := s.resolveMatrix(matrixRef)
		if !ok {
			return nil, fmt.Errorf("listSalvos: matrix %q not found", matrixRef)
		}
		ids := s.salvos.list(oid)
		parts := make([]string, len(ids))
		for i, id := range ids {
			parts[i] = strconv.FormatInt(id, 10)
		}
		return []any{strings.Join(parts, ",")}, nil
	}
}

// makeBuiltinGetSalvo returns the crosspoint dump of one stored salvo as
// a string. Args:
//   - args[0] string matrixPath — OID or dotted identifier path
//   - args[1] int64  salvoID
//
// Output format: semicolon-separated "<tgt>=<csvSrcs>" clusters, ascending
// by target. An empty source list means "target unrouted" (spec p.89).
//
// Example: salvo with target 0 → [1,2], target 5 → [3], target 7 → []
// returns "0=1,2;5=3;7=".
//
// Empty string for an unknown salvo ID inside a valid matrix. Returns
// (nil, err) — Success=false on the wire — when the ref does not
// resolve to a Matrix at all. Refs #455.
func (s *server) makeBuiltinGetSalvo() FunctionImpl {
	return func(args []any) ([]any, error) {
		if len(args) < 2 {
			return nil, fmt.Errorf("getSalvo: need (matrixPath, salvoID)")
		}
		matrixRef, ok := args[0].(string)
		if !ok {
			return nil, fmt.Errorf("getSalvo: bad matrixPath type (%T)", args[0])
		}
		salvoID, ok := args[1].(int64)
		if !ok {
			return nil, fmt.Errorf("getSalvo: bad salvoID type (%T)", args[1])
		}
		oid, _, ok := s.resolveMatrix(matrixRef)
		if !ok {
			return nil, fmt.Errorf("getSalvo: matrix %q not found", matrixRef)
		}
		conns, ok := s.salvos.recall(oid, salvoID)
		if !ok {
			return []any{""}, nil
		}
		sorted := append([]canonical.MatrixConnection(nil), conns...)
		sort.Slice(sorted, func(i, j int) bool { return sorted[i].Target < sorted[j].Target })
		parts := make([]string, 0, len(sorted))
		for _, c := range sorted {
			srcs := make([]string, len(c.Sources))
			for i, s := range c.Sources {
				srcs[i] = strconv.FormatInt(s, 10)
			}
			parts = append(parts, strconv.FormatInt(c.Target, 10)+"="+strings.Join(srcs, ","))
		}
		return []any{strings.Join(parts, ";")}, nil
	}
}
