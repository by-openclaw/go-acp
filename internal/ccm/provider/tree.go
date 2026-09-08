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
	"fmt"
	"sort"
	"strings"
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
type Tree struct {
	resources map[string]json.RawMessage
	children  map[string]map[string]struct{}
}

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
// nodes" figure the runbook and startup log quote.
func (t *Tree) Len() int { return len(t.resources) }

// Nodes reports how many interior nodes the tree has (the empty-string root
// included), the companion to Len for the same startup summary.
func (t *Tree) Nodes() int { return len(t.children) }
