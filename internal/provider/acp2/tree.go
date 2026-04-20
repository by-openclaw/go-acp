package acp2

import (
	"sync"

	"acp/internal/export/canonical"
)

// tree is the obj-id indexed snapshot the provider serves. Built at
// startup from a canonical.Export and mutated only by set_property.
//
// The entry type (ACP2 object type, access bits, children list, etc.)
// lands in Step 2c where newTree starts actually flattening the
// canonical tree. This Step 2a skeleton only carries the minimum needed
// by server.go's startup log.
type tree struct {
	mu      sync.RWMutex
	entries map[uint32]any // uint32 obj-id -> *entry (typed in Step 2c)
	slotN   uint8
}

func emptyTree() *tree {
	return &tree{entries: map[uint32]any{}, slotN: 1}
}

// newTree flattens a canonical.Export into the obj-id map. Full
// mapping logic (canonical Node/Parameter -> ACP2 object type,
// number_type derivation, access bit mapping, children list) ships
// in Step 2c. Until then, the provider accepts consumers and logs
// incoming frames so the handshake wiring can be validated in isolation.
func newTree(exp *canonical.Export) (*tree, error) {
	_ = exp
	return emptyTree(), nil
}

// count returns the number of indexed objects (used only for logs).
func (t *tree) count() int {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return len(t.entries)
}
