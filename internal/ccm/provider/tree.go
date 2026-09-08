// Package ccm is the provider side of the CCM connector: an HTTP server that
// replays a captured CCM device model so a controller such as Cerebrum can
// drive dhs as if it were a real CCM (EVS BRIDGE / Neuron) device.
//
// The device model is a "dm-tree": a flat map of resource path -> resource
// JSON, exactly the shape the consumer walk captures (issue #984). The
// provider does not synthesise or guess the path layout — it serves whatever
// tree it is given, so what a real device exposes and what we replay cannot
// drift. The self-describing contract a CCM controller relies on is:
//
//	GET a node path     -> a JSON array of the node's immediate child names
//	GET a resource path -> the stored resource JSON object
//	GET an unknown path -> 404
//
// which is the mirror of the consumer's branch-vs-resource classification.
package ccm

import (
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
)

// Kind classifies what a path is in the tree.
type Kind int

const (
	// KindAbsent means the path is neither a resource nor a node.
	KindAbsent Kind = iota
	// KindResource means the path holds a captured resource object.
	KindResource
	// KindNode means the path is an interior node; its body is the sorted
	// array of its immediate child names.
	KindNode
)

// Tree is a replayable CCM device model: resource paths mapped to their
// captured JSON, plus the interior-node structure derived from those paths so
// a node GET can answer with its child names.
//
// Reads and writes may race — a controller PATCHes while another GETs — so
// the maps are guarded by mu. Writes only change a resource's body; the
// node structure is fixed at load (a CCM device with a fixed resource set
// exposes no POST/DELETE, §11.1), so children is read-only after LoadTree.
type Tree struct {
	mu        sync.RWMutex
	resources map[string]json.RawMessage
	children  map[string]map[string]struct{}
}

// ErrNotFound reports a write to a path that is not a stored resource — an
// unknown path, or an interior node, which has no body to mutate.
var ErrNotFound = errors.New("ccm provider: no such resource")

// ErrNotMutable reports a write to a stored body that is not a JSON object —
// a collection array, for instance. §11.1 puts PUT/PATCH on individual
// resources; a collection is read through GET only.
var ErrNotMutable = errors.New("ccm provider: not a mutable resource")

// immutable are the identity fields §14.1 declares device-assigned and
// immutable. A write never overwrites them, whatever the body carries, so a
// client cannot re-key a resource out from under a controller.
var immutable = map[string]struct{}{"uuid": {}, "id": {}}

// normalizePath trims the leading and trailing slashes a caller's GET may
// carry ("/io/ip/" and "io/ip" are one path) so lookups are stable.
func normalizePath(p string) string {
	return strings.Trim(p, "/")
}

// LoadTree builds a Tree from a dm-tree document: a JSON object whose keys are
// resource paths and whose values are the resource bodies. Every ancestor of
// a resource path becomes an interior node automatically, so the caller only
// records leaves.
func LoadTree(raw []byte) (*Tree, error) {
	var flat map[string]json.RawMessage
	if err := json.Unmarshal(raw, &flat); err != nil {
		return nil, fmt.Errorf("ccm provider: parse dm-tree: %w", err)
	}
	t := &Tree{
		resources: make(map[string]json.RawMessage, len(flat)),
		children:  map[string]map[string]struct{}{},
	}
	for p, body := range flat {
		np := normalizePath(p)
		if np == "" {
			return nil, fmt.Errorf("ccm provider: dm-tree has an empty resource path")
		}
		t.resources[np] = body
		// Register every ancestor -> immediate-child link, including the
		// empty-string root, so a GET at any interior node lists its children.
		segs := strings.Split(np, "/")
		for i := range segs {
			parent := strings.Join(segs[:i], "/")
			if t.children[parent] == nil {
				t.children[parent] = map[string]struct{}{}
			}
			t.children[parent][segs[i]] = struct{}{}
		}
	}
	if len(t.resources) == 0 {
		return nil, fmt.Errorf("ccm provider: dm-tree is empty")
	}
	return t, nil
}

// Get resolves a GET against the tree. A resource path returns its stored body
// (KindResource); an interior node returns a sorted JSON array of its
// immediate child names (KindNode); anything else returns KindAbsent. A path
// that is recorded as a resource wins over the same path as a node, so a
// captured leaf is always served verbatim.
func (t *Tree) Get(path string) ([]byte, Kind) {
	np := normalizePath(path)
	t.mu.RLock()
	defer t.mu.RUnlock()
	if body, ok := t.resources[np]; ok {
		return body, KindResource
	}
	kids, ok := t.children[np]
	if !ok {
		return nil, KindAbsent
	}
	names := make([]string, 0, len(kids))
	for k := range kids {
		names = append(names, k)
	}
	sort.Strings(names)
	body, _ := json.Marshal(names)
	return body, KindNode
}

// Len reports how many resources the tree holds — the "N resources across M
// nodes" figure the runbook and startup log quote. Locked because Update
// writes the same map concurrently.
func (t *Tree) Len() int {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return len(t.resources)
}

// Nodes reports how many interior nodes the tree has (the empty-string root
// included), the companion to Len for the same startup summary.
func (t *Tree) Nodes() int {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return len(t.children)
}

// Update applies a §11 mutation to a stored resource and reports whether it
// could be applied. PATCH (replace=false) merges the given fields into the
// resource — §11.2, one or more fields, only what the client named changes.
// PUT (replace=true) replaces the mutable fields wholesale — §11.1, all
// mutable fields. In both cases the immutable identity keys are carried over
// from the stored copy and any value the body offers for them is ignored.
//
// Only a resource path is writable: an unknown path or an interior node is
// ErrNotFound, and a stored body that is not an object (a collection array)
// is ErrNotMutable. The caller validates the body as a non-empty object
// before calling; this applies it.
func (t *Tree) Update(path string, fields map[string]json.RawMessage, replace bool) error {
	np := normalizePath(path)
	t.mu.Lock()
	defer t.mu.Unlock()
	stored, ok := t.resources[np]
	if !ok {
		return ErrNotFound
	}
	var cur map[string]json.RawMessage
	if err := json.Unmarshal(stored, &cur); err != nil || cur == nil {
		return ErrNotMutable
	}
	next := cur
	if replace {
		next = make(map[string]json.RawMessage, len(fields)+len(immutable))
		for k := range immutable {
			if v, ok := cur[k]; ok {
				next[k] = v
			}
		}
	}
	for k, v := range fields {
		if _, ro := immutable[k]; ro {
			continue
		}
		next[k] = v
	}
	// Both inputs were already valid JSON, so re-encoding cannot fail.
	body, _ := json.Marshal(next)
	t.resources[np] = body
	return nil
}
