package consumer

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"dhs/internal/ccm/codec"
)

// maxWalkNodes bounds a WalkTree run. The lab bridge has a few hundred
// resources; this is a runaway guard (a device that keeps listing new
// children, or a cycle the visited-set somehow misses), not a real cap.
const maxWalkNodes = 20000

// WalkTree recursively captures the WHOLE CCM device model — not just the
// io/ip streams that Walk returns. It follows the tree's own shape: a node
// lists its child names, a resource is a terminal payload (codec.ClassifyBody
// draws the line). There is no wildcard that returns a subtree, so the walk
// GETs each node and recurses only into the names that node lists.
//
// Seeding matches the field reality that "the root may not exist": pass one
// or more start paths (each relative to the API base, e.g. "/self", "/io",
// "/processing") to walk exactly those subtrees, the way a caller who knows
// the node paths in advance would. With no start paths, WalkTree discovers
// the top-level node names from the API root (GET of the base) and walks
// them all.
//
// A path the device 404s (or otherwise errors on) is recorded as a
// deviation and the walk continues — a partial device still yields every
// subtree it does serve. Deviations are returned, never swallowed.
func (c *Client) WalkTree(ctx context.Context, startPaths ...string) (*codec.DMTree, []string, error) {
	tree := codec.NewDMTree(c.base)
	var deviations []string

	// Build the initial frontier.
	var frontier []string
	if len(startPaths) == 0 {
		rootBody, err := c.get(ctx, "")
		if err != nil {
			return nil, nil, fmt.Errorf("ccm walk-tree: read API root: %w", err)
		}
		kind, children, cerr := codec.ClassifyBody(rootBody)
		if cerr != nil {
			return nil, nil, fmt.Errorf("ccm walk-tree: classify API root: %w", cerr)
		}
		if kind == codec.NodeResource {
			// A device whose root is itself a resource: capture it and stop.
			tree.AddResource("/", rootBody)
			return tree, deviations, nil
		}
		tree.AddBranch("")
		for _, ch := range children {
			frontier = append(frontier, "/"+ch)
		}
	} else {
		for _, p := range startPaths {
			frontier = append(frontier, normalizePath(p))
		}
	}

	visited := map[string]bool{}
	nodes := 0
	for len(frontier) > 0 {
		path := frontier[0]
		frontier = frontier[1:]
		if visited[path] {
			continue
		}
		visited[path] = true
		if nodes++; nodes > maxWalkNodes {
			deviations = append(deviations, fmt.Sprintf("walk-tree: aborted after %d nodes (runaway guard) at %s", maxWalkNodes, path))
			break
		}

		body, err := c.get(ctx, path)
		if err != nil {
			deviations = append(deviations, fmt.Sprintf("%s: %v", path, err))
			continue
		}
		kind, children, cerr := codec.ClassifyBody(body)
		if cerr != nil {
			deviations = append(deviations, fmt.Sprintf("%s: %v", path, cerr))
			continue
		}
		if kind == codec.NodeResource {
			tree.AddResource(path, json.RawMessage(body))
			continue
		}
		tree.AddBranch(path)
		for _, ch := range children {
			frontier = append(frontier, path+"/"+ch)
		}
	}
	return tree, deviations, nil
}

// normalizePath makes a caller-supplied start path API-relative: leading
// slash, no trailing slash (except the bare root).
func normalizePath(p string) string {
	p = strings.TrimSpace(p)
	if p == "" || p == "/" {
		return ""
	}
	if !strings.HasPrefix(p, "/") {
		p = "/" + p
	}
	return strings.TrimRight(p, "/")
}
