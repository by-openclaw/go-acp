package emberplus

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"sync"

	"acp/internal/export/canonical"
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
			return []any{false}, nil
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
			return []any{""}, nil
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
// ref does not resolve to a Matrix — so storeSalvo/recallSalvo reject
// attempts against label string params, nodes, or bogus OIDs rather
// than silently keying into the salvo store.
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
			return []any{int64(0)}, nil
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

// makeBuiltinStoreSalvo binds a store callback. Args mirror recallSalvo.
// Snapshots the matrix's current connections under salvoID. Returns true
// on success; false if the ref does not resolve to a Matrix element.
func (s *server) makeBuiltinStoreSalvo() FunctionImpl {
	return func(args []any) ([]any, error) {
		if len(args) < 2 {
			return nil, fmt.Errorf("storeSalvo: need (matrixPath, salvoID)")
		}
		matrixRef, okP := args[0].(string)
		salvoID, okS := asInt64(args[1])
		if !okP || !okS {
			return nil, fmt.Errorf("storeSalvo: bad arg types (%T, %T)", args[0], args[1])
		}
		oid, m, ok := s.resolveMatrix(matrixRef)
		if !ok {
			return []any{false}, nil
		}
		s.salvos.store(oid, salvoID, m.Connections)
		return []any{true}, nil
	}
}

// makeBuiltinListSalvos returns a comma-separated list of stored salvo
// IDs for the given matrix, ascending. Args:
//   - args[0] string matrixPath — OID or dotted identifier path
//
// Empty string if the matrix has no salvos yet. Returns an empty string
// for non-matrix refs rather than an error so consumers can probe
// cheaply without trapping exceptions.
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
			return []any{""}, nil
		}
		ids := s.salvos.list(oid)
		parts := make([]string, len(ids))
		for i, id := range ids {
			parts[i] = strconv.FormatInt(id, 10)
		}
		return []any{strings.Join(parts, ",")}, nil
	}
}
