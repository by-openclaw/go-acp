package codec

// The CCM REST tree is self-describing and recursive. A *node* answers a
// GET with a JSON array of child NAMES (`["madi","ip","sdi"]`); a
// *resource* answers with an object (`/self` → `{…}`) or an array of
// objects (`/io/sdi` → `[{…uuid…}]`). There is no wildcard that returns a
// whole subtree — a caller must GET each node and recurse only into the
// names a node lists. This file is the pure classifier that decides which
// shape a body is; the recursive GET loop lives in the consumer (I/O).
//
// ADR-0006: stdlib only, no dhs/* imports.

import (
	"bytes"
	"encoding/json"
	"fmt"
)

// NodeKind classifies one CCM response body.
type NodeKind int

const (
	// NodeBranch is a JSON array of strings — child node names to recurse.
	NodeBranch NodeKind = iota
	// NodeResource is a terminal payload — a JSON object or an array of
	// objects — captured verbatim into the DM, never recursed.
	NodeResource
)

func (k NodeKind) String() string {
	if k == NodeBranch {
		return "branch"
	}
	return "resource"
}

// ClassifyBody inspects a CCM GET response and reports whether it is a
// branch (with the child names to recurse into) or a terminal resource.
//
// Rules, from the live device shape:
//   - a JSON array whose every element is a string  → branch (the strings
//     are child path segments)
//   - a JSON array of objects, or a JSON object     → resource
//   - an empty array `[]`                            → resource (an empty
//     collection; there is nothing to recurse and no child names to read)
//   - any scalar (number/string/bool/null)          → resource
//
// The distinction is structural, not by endpoint name, so it holds for
// nodes the connector has never seen. A body that is not valid JSON is an
// error, surfaced to the caller (never guessed).
func ClassifyBody(body []byte) (kind NodeKind, children []string, err error) {
	trimmed := bytes.TrimSpace(body)
	if len(trimmed) == 0 {
		return NodeResource, nil, fmt.Errorf("ccm: classify: empty body")
	}
	if trimmed[0] != '[' {
		// Object or scalar — a terminal resource.
		if !json.Valid(trimmed) {
			return NodeResource, nil, fmt.Errorf("ccm: classify: invalid JSON body")
		}
		return NodeResource, nil, nil
	}

	// An array: branch iff every element is a JSON string.
	var elems []json.RawMessage
	if uerr := json.Unmarshal(trimmed, &elems); uerr != nil {
		return NodeResource, nil, fmt.Errorf("ccm: classify: %w", uerr)
	}
	if len(elems) == 0 {
		// Empty collection — a resource with nothing to recurse.
		return NodeResource, nil, nil
	}
	names := make([]string, 0, len(elems))
	for _, e := range elems {
		et := bytes.TrimSpace(e)
		if len(et) == 0 || et[0] != '"' {
			// A non-string element ⇒ this is a resource collection, not a
			// branch of child names.
			return NodeResource, nil, nil
		}
		var s string
		if uerr := json.Unmarshal(et, &s); uerr != nil {
			return NodeResource, nil, fmt.Errorf("ccm: classify: array element: %w", uerr)
		}
		names = append(names, s)
	}
	return NodeBranch, names, nil
}
