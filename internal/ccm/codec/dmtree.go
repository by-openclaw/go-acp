package codec

import (
	"encoding/json"
	"sort"
)

// DMTree is a full recursive capture of a CCM device's REST tree: every
// terminal resource path mapped to its verbatim JSON, plus the branch
// paths that structure them. Paths are relative to the API base and start
// with "/" (e.g. "/self", "/io/sdi", "/processing/video"). It is the
// complete device model — the artifact to store versioned per ADR-0022
// and diff across firmware — as opposed to the UUID-keyed stream view in
// Device, which is only the io/ip slice of this tree.
//
// Pure data: the consumer's recursive walk fills it; this type only holds
// and orders it. ADR-0006 (no dhs/* imports).
type DMTree struct {
	// Base is the API base the paths hang off (e.g. "https://host/api/v1"),
	// for provenance. Not part of the diffable content.
	Base string
	// Resources maps each terminal resource path to its raw JSON body.
	Resources map[string]json.RawMessage
	// Branches is the set of node paths that listed child names (the tree
	// skeleton). Kept so the structure is auditable even where a branch
	// has no direct resource of its own.
	Branches []string
}

// NewDMTree returns an empty tree rooted at base.
func NewDMTree(base string) *DMTree {
	return &DMTree{Base: base, Resources: map[string]json.RawMessage{}}
}

// AddResource records one terminal resource.
func (t *DMTree) AddResource(path string, body json.RawMessage) {
	dup := make(json.RawMessage, len(body))
	copy(dup, body)
	t.Resources[path] = dup
}

// AddBranch records one node path that listed children.
func (t *DMTree) AddBranch(path string) { t.Branches = append(t.Branches, path) }

// SortedPaths returns every resource path, lexically sorted — the stable
// order that makes two firmware captures diff cleanly.
func (t *DMTree) SortedPaths() []string {
	out := make([]string, 0, len(t.Resources))
	for p := range t.Resources {
		out = append(out, p)
	}
	sort.Strings(out)
	return out
}

// Len is the number of terminal resources captured.
func (t *DMTree) Len() int { return len(t.Resources) }
