package emberplus

import (
	"fmt"
	"strconv"
	"strings"
	"sync"

	"acp/internal/export/canonical"
)

// tree is the provider's in-memory snapshot of the served element tree.
// Indexed primarily by numeric OID ("1.4.1.3"); a secondary map allows
// lookup by dotted identifier path ("router.matrix.gain").
//
// The tree is read-mostly — GetDirectory walks it concurrently; only
// SetValue mutates a parameter's value, taking a single write lock.
type tree struct {
	mu    sync.RWMutex
	root  canonical.Element   // the top-level root element (Node)
	byOID map[string]*entry   // "1.4.1.3" -> entry
	byIDP map[string][]*entry // "router.matrix.gain" -> entries (dotted identifier path; may be many)
}

// entry wraps a canonical element with the bookkeeping the server needs:
// a pointer to its parent for path walks, a snapshot of the OID split
// into [u32] for RELATIVE-OID encoding, and a last-known value for
// parameters (canonical.Parameter.Value is an `any`; we keep it as-is
// and let the encoder dispatch on the canonical Type field).
type entry struct {
	el       canonical.Element
	parent   *entry
	oidParts []uint32 // parsed from Header.OID (e.g. "1.4.1.3" → [1,4,1,3])
}

// newTree converts a canonical Export into a flattened tree indexed by OID.
// Accepts a Node root; any other top-level Element is an error (a served
// tree always starts at a Node per Ember+ spec §The Node element).
func newTree(exp *canonical.Export) (*tree, error) {
	if exp == nil || exp.Root == nil {
		return nil, fmt.Errorf("tree: nil export or root")
	}
	t := &tree{
		root:  exp.Root,
		byOID: make(map[string]*entry),
		byIDP: make(map[string][]*entry),
	}
	if err := t.indexRecursive(exp.Root, nil); err != nil {
		return nil, err
	}
	return t, nil
}

func (t *tree) indexRecursive(el canonical.Element, parent *entry) error {
	hdr := el.Common()
	parts, err := parseOID(hdr.OID)
	if err != nil {
		return fmt.Errorf("oid %q: %w", hdr.OID, err)
	}
	e := &entry{el: el, parent: parent, oidParts: parts}
	if hdr.OID != "" {
		if _, dup := t.byOID[hdr.OID]; dup {
			return fmt.Errorf("tree: duplicate oid %q", hdr.OID)
		}
		t.byOID[hdr.OID] = e
	}
	if hdr.Path != "" {
		t.byIDP[hdr.Path] = append(t.byIDP[hdr.Path], e)
	}
	for _, child := range hdr.Children {
		if err := t.indexRecursive(child, e); err != nil {
			return err
		}
	}
	return nil
}

// lookupOID resolves a numeric OID ("1.4.1.3") to an entry.
func (t *tree) lookupOID(oid string) (*entry, bool) {
	t.mu.RLock()
	defer t.mu.RUnlock()
	e, ok := t.byOID[oid]
	return e, ok
}

// lookupPath resolves a dotted identifier path ("router.matrix.gain") to
// the first matching entry. Identifier paths can collide across subtrees
// so callers that care about ambiguity should use lookupPathAll.
func (t *tree) lookupPath(path string) (*entry, bool) {
	t.mu.RLock()
	defer t.mu.RUnlock()
	es := t.byIDP[path]
	if len(es) == 0 {
		return nil, false
	}
	return es[0], true
}

// rootEntry returns the root node entry (always present after newTree).
func (t *tree) rootEntry() *entry {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.byOID[t.root.Common().OID]
}

// setParamValue mutates a parameter's value. Returns the actually-stored
// value (for now identical — future work: clamp to min/max/step) and the
// canonical.Parameter pointer so the caller can announce the change.
func (t *tree) setParamValue(oid string, v any) (*canonical.Parameter, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	e, ok := t.byOID[oid]
	if !ok {
		return nil, fmt.Errorf("tree: oid %q not found", oid)
	}
	p, ok := e.el.(*canonical.Parameter)
	if !ok {
		return nil, fmt.Errorf("tree: oid %q is %s, not parameter", oid, e.el.Kind())
	}
	p.Value = v
	return p, nil
}

// parseOID splits "1.4.1.3" into []uint32{1,4,1,3}. Empty string returns
// an empty slice — that's the root node's OID in canonical exports.
func parseOID(oid string) ([]uint32, error) {
	if oid == "" {
		return nil, nil
	}
	parts := strings.Split(oid, ".")
	out := make([]uint32, 0, len(parts))
	for _, p := range parts {
		n, err := strconv.ParseUint(p, 10, 32)
		if err != nil {
			return nil, fmt.Errorf("bad oid segment %q: %w", p, err)
		}
		out = append(out, uint32(n))
	}
	return out, nil
}
