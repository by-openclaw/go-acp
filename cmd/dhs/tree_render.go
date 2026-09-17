package main

import (
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"

	"dhs/internal/consumer"
)

// pathNode is one position in the tree built from Object.Path slices.
// Obj is nil for synthetic intermediate nodes; the renderer falls back
// to Name for those.
type pathNode struct {
	Name     string
	Obj      *consumer.Object
	Children map[string]*pathNode
}

func newPathNode(name string) *pathNode {
	return &pathNode{Name: name, Children: map[string]*pathNode{}}
}

func (n *pathNode) sortedChildren() []*pathNode {
	out := make([]*pathNode, 0, len(n.Children))
	for _, c := range n.Children {
		out = append(out, c)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// buildPathTree groups objects into a tree by Object.Path. The returned
// root is synthetic; its Children are the real top-level segments.
// ACP1 objects with empty Path but non-empty Group are placed under a
// single-segment path equal to the group name.
func buildPathTree(objs []consumer.Object) *pathNode {
	root := newPathNode("")
	for i := range objs {
		o := &objs[i]
		path := o.Path
		if len(path) == 0 && o.Group != "" {
			path = []string{o.Group}
		}
		if len(path) == 0 {
			continue
		}
		cur := root
		for _, seg := range path {
			child, ok := cur.Children[seg]
			if !ok {
				child = newPathNode(seg)
				cur.Children[seg] = child
			}
			cur = child
		}
		cur.Obj = o
	}
	return root
}

type treeChars struct {
	branch, last, vertical, space string
}

var unicodeTreeChars = treeChars{branch: "├── ", last: "└── ", vertical: "│   ", space: "    "}
var asciiTreeChars = treeChars{branch: "+-- ", last: "+-- ", vertical: "|   ", space: "    "}

type treeRenderOpts struct {
	FromOID  string
	FromPath string
	Depth    int
	ASCII    bool
	Filter   string
}

// renderTree writes a tree view of objs to w. When FromPath or FromOID
// is set, the tree is focused: the ancestor chain from the slot root
// down to the focus node renders without sibling expansion, then
// descendants under the focus expand fully (subject to Depth).
//
// Depth is measured from the focus node; 0 means unlimited. Ancestors
// above the focus always render regardless of Depth.
func renderTree(w io.Writer, objs []consumer.Object, opts treeRenderOpts) error {
	if opts.FromPath != "" && opts.FromOID != "" {
		return fmt.Errorf("--from-path and --from-oid are mutually exclusive")
	}
	var focus []string
	switch {
	case opts.FromPath != "":
		userSegs := splitFocusPath(opts.FromPath)
		p, ok := resolveFromPath(objs, userSegs)
		if !ok {
			return fmt.Errorf("no object at --from-path %q", opts.FromPath)
		}
		focus = p
	case opts.FromOID != "":
		p, ok := resolveFromOID(objs, opts.FromOID)
		if !ok {
			return fmt.Errorf("no object with --from-oid %q", opts.FromOID)
		}
		focus = p
	}
	root := buildPathTree(objs)
	chars := unicodeTreeChars
	if opts.ASCII {
		chars = asciiTreeChars
	}
	filterLower := strings.ToLower(opts.Filter)
	// When focused, the first segment of focus selects which top-level
	// subtree to render; pruning happens inside renderNode.
	children := root.sortedChildren()
	// Collapse a single device-root anchor (ROOT_NODE_V2 / ROOT) so the
	// tree starts at its children — matches the root-stripped path strings
	// and Cerebrum's UI. resolveFromPath always returns the canonical path
	// from the real root, so focus[0] is that root; drop it in lock-step.
	if len(children) == 1 && isDisplayRoot(children[0].Name) {
		if len(focus) > 0 && strings.EqualFold(focus[0], children[0].Name) {
			focus = focus[1:]
		}
		children = children[0].sortedChildren()
	}
	for i, c := range children {
		isLast := i == len(children)-1
		renderNode(w, c, "", isLast, focus, 0, opts.Depth, chars, filterLower)
	}
	return nil
}

func renderNode(w io.Writer, n *pathNode, prefix string, isLast bool, focus []string, depthFromFocus, maxDepth int, chars treeChars, filterLower string) {
	onChain := len(focus) > 0
	atFocus := false
	if onChain {
		if !strings.EqualFold(n.Name, focus[0]) {
			return
		}
		if len(focus) == 1 {
			atFocus = true
		}
	}

	line := formatNodeLine(n)
	if filterLower == "" || strings.Contains(strings.ToLower(line), filterLower) {
		connector := chars.branch
		if isLast {
			connector = chars.last
		}
		_, _ = fmt.Fprintf(w, "%s%s%s\n", prefix, connector, line)
	}

	childPrefix := prefix + chars.vertical
	if isLast {
		childPrefix = prefix + chars.space
	}

	if onChain && !atFocus {
		// Still on the ancestor chain: descend into the one matching child.
		nextFocus := focus[1:]
		for _, c := range n.sortedChildren() {
			if !strings.EqualFold(c.Name, nextFocus[0]) {
				continue
			}
			renderNode(w, c, childPrefix, true, nextFocus, 0, maxDepth, chars, filterLower)
			return
		}
		return
	}

	if maxDepth > 0 && depthFromFocus >= maxDepth {
		return
	}
	kids := n.sortedChildren()
	for i, c := range kids {
		last := i == len(kids)-1
		renderNode(w, c, childPrefix, last, nil, depthFromFocus+1, maxDepth, chars, filterLower)
	}
}

// formatNodeLine renders one tree-node line. Containers (with children)
// show "Name [oid=X]"; leaves show "Name  kind  access [= value]".
func formatNodeLine(n *pathNode) string {
	if n.Obj == nil {
		return n.Name
	}
	o := n.Obj
	if len(n.Children) > 0 {
		return fmt.Sprintf("%s [oid=%s]", n.Name, formatOID(o))
	}
	val := walkValueColumn(*o)
	line := fmt.Sprintf("%s  %s  %s", n.Name, kindName(o.Kind), accessStr(o.Access))
	if val != "" {
		line += " = " + val
	}
	return line
}

func formatOID(o *consumer.Object) string {
	if o.OID != "" {
		return o.OID
	}
	return strconv.Itoa(o.ID)
}

func splitFocusPath(s string) []string {
	parts := strings.Split(s, ".")
	out := parts[:0]
	for _, p := range parts {
		if p == "" {
			continue
		}
		out = append(out, p)
	}
	return out
}

// resolveFromPath matches the user's focus segments as a contiguous
// run inside any Object.Path. Returns the canonical Path slice up to
// (and including) the focus run, so the renderer always starts at the
// actual slot root.
//
// Works for:
//   - leaf focus (CHANNEL 01.Direction)         → segments at the end of a leaf path
//   - intermediate focus (INPUT.SDI.CHANNEL 01) → segments mid-path inside leaf paths
//   - ACP1 group focus (identity)               → first segment of leaf paths
func resolveFromPath(objs []consumer.Object, userSegs []string) ([]string, bool) {
	if len(userSegs) == 0 {
		return nil, false
	}
	for i := range objs {
		path := objs[i].Path
		if len(path) == 0 && objs[i].Group != "" {
			path = []string{objs[i].Group}
		}
		if len(path) < len(userSegs) {
			continue
		}
		for start := 0; start+len(userSegs) <= len(path); start++ {
			match := true
			for j, seg := range userSegs {
				if !strings.EqualFold(path[start+j], seg) {
					match = false
					break
				}
			}
			if match {
				return path[:start+len(userSegs)], true
			}
		}
	}
	return nil, false
}

// resolveFromOID accepts either a dotted Ember+ OID (matched against
// Object.OID) or a decimal integer matching Object.ID (ACP1 byte /
// ACP2 obj-id). Returns the full Path of the first match.
func resolveFromOID(objs []consumer.Object, oid string) ([]string, bool) {
	for i := range objs {
		if objs[i].OID != "" && objs[i].OID == oid {
			path := objs[i].Path
			if len(path) == 0 && objs[i].Group != "" {
				path = []string{objs[i].Group}
			}
			return path, true
		}
	}
	if n, err := strconv.Atoi(oid); err == nil {
		for i := range objs {
			if objs[i].ID == n {
				path := objs[i].Path
				if len(path) == 0 && objs[i].Group != "" {
					path = []string{objs[i].Group}
				}
				return path, true
			}
		}
	}
	return nil, false
}
