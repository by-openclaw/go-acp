package emberplus

import (
	"fmt"
	"sync"

	"acp/internal/export/canonical"
)

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

// setupBuiltinFunctions walks the loaded tree and registers default
// callbacks for any canonical.Function whose identifier matches a known
// builtin name. Order mirrors the spec-p.91 examples + the
// broadcast-router convention (sum / recallSalvo / storeSalvo).
func (s *server) setupBuiltinFunctions() {
	if s.tree == nil {
		return
	}
	s.salvos = newSalvoStore()

	s.walkFunctions(s.tree.root, func(e *entry, f *canonical.Function) {
		oid := e.el.Common().OID
		switch f.Identifier {
		case "sum", "addFunction", "add":
			s.funcs.register(oid, builtinSum)
		case "recallSalvo", "recall":
			s.funcs.register(oid, s.makeBuiltinRecallSalvo())
		case "storeSalvo", "store":
			s.funcs.register(oid, s.makeBuiltinStoreSalvo())
		}
	})
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

// makeBuiltinRecallSalvo binds a recall callback to this server. Args:
//   - args[0] string matrixPath — canonical OID of the target matrix
//   - args[1] int64  salvoID
//
// Applies the saved crosspoint set to the matrix, broadcasts the resulting
// connections, and returns the number of connections applied.
func (s *server) makeBuiltinRecallSalvo() FunctionImpl {
	return func(args []any) ([]any, error) {
		if len(args) < 2 {
			return nil, fmt.Errorf("recallSalvo: need (matrixPath, salvoID)")
		}
		matrixOID, okP := args[0].(string)
		salvoID, okS := asInt64(args[1])
		if !okP || !okS {
			return nil, fmt.Errorf("recallSalvo: bad arg types (%T, %T)", args[0], args[1])
		}
		conns, ok := s.salvos.recall(matrixOID, salvoID)
		if !ok {
			return []any{int64(0)}, nil
		}
		post, err := s.applyMatrixConnections(matrixOID, conns)
		if err != nil {
			return nil, err
		}
		s.broadcastMatrixConnections(matrixOID, post, nil)
		return []any{int64(len(post))}, nil
	}
}

// makeBuiltinStoreSalvo binds a store callback. Args:
//   - args[0] string matrixPath
//   - args[1] int64  salvoID
//
// Snapshots the matrix's current connections under salvoID. Returns true
// on success; false if the matrix OID is unknown.
func (s *server) makeBuiltinStoreSalvo() FunctionImpl {
	return func(args []any) ([]any, error) {
		if len(args) < 2 {
			return nil, fmt.Errorf("storeSalvo: need (matrixPath, salvoID)")
		}
		matrixOID, okP := args[0].(string)
		salvoID, okS := asInt64(args[1])
		if !okP || !okS {
			return nil, fmt.Errorf("storeSalvo: bad arg types (%T, %T)", args[0], args[1])
		}
		e, ok := s.tree.lookupOID(matrixOID)
		if !ok {
			return []any{false}, nil
		}
		m, ok := e.el.(*canonical.Matrix)
		if !ok {
			return []any{false}, nil
		}
		s.salvos.store(matrixOID, salvoID, m.Connections)
		return []any{true}, nil
	}
}
