package main

import (
	"fmt"
	"io"
	"strings"

	"dhs/internal/consumer"
)

// renderTreePlantUML emits a PlantUML mindmap derived from the same
// pathNode tree the ASCII renderer uses. Output is a complete document
// — `@startmindmap` … `@endmindmap` — ready to feed to `plantuml.jar`
// for SVG/PNG generation.
//
// Mindmap notation (one line per element, depth encoded by `*` count):
//
//	* root
//	** identity [oid=1.0]
//	*** product (string) = "Tiny Ember+ Router"
//	*** dtdVersion (string) = "2.60"
//	** types [oid=1.6]
//	*** vInteger (int) = 0
//
// Containers render as `* <ident> [oid=X]`; leaves render as
// `* <ident> (<kind>) = <value>` so docs viewers see the type + current
// value alongside the structure.
//
// Refs R5b #469.
func renderTreePlantUML(w io.Writer, objs []consumer.Object, opts treeRenderOpts) error {
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
	filterLower := strings.ToLower(opts.Filter)

	if _, err := fmt.Fprintln(w, "@startmindmap"); err != nil {
		return err
	}
	if _, err := fmt.Fprintln(w, "* device"); err != nil {
		return err
	}
	for _, c := range root.sortedChildren() {
		renderPlantUMLNode(w, c, 2, focus, 0, opts.Depth, filterLower)
	}
	if _, err := fmt.Fprintln(w, "@endmindmap"); err != nil {
		return err
	}
	return nil
}

// renderPlantUMLNode walks one node + its descendants, honouring the
// focus chain (no-sibling-expansion above the focus) and the depth cap
// (measured from the focus node).
func renderPlantUMLNode(w io.Writer, n *pathNode, level int, focus []string, depthFromFocus, maxDepth int, filterLower string) {
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

	line := formatPlantUMLLine(n)
	if filterLower == "" || strings.Contains(strings.ToLower(line), filterLower) {
		_, _ = fmt.Fprintf(w, "%s %s\n", strings.Repeat("*", level), line)
	}

	if onChain && !atFocus {
		nextFocus := focus[1:]
		for _, c := range n.sortedChildren() {
			if !strings.EqualFold(c.Name, nextFocus[0]) {
				continue
			}
			renderPlantUMLNode(w, c, level+1, nextFocus, 0, maxDepth, filterLower)
			return
		}
		return
	}

	if maxDepth > 0 && depthFromFocus >= maxDepth {
		return
	}
	for _, c := range n.sortedChildren() {
		renderPlantUMLNode(w, c, level+1, nil, depthFromFocus+1, maxDepth, filterLower)
	}
}

// formatPlantUMLLine renders one element. Mirrors formatNodeLine but
// without the ASCII connectors; PlantUML's `*` repetition carries the
// depth.
func formatPlantUMLLine(n *pathNode) string {
	if n.Obj == nil {
		return n.Name
	}
	o := n.Obj
	if len(n.Children) > 0 {
		return fmt.Sprintf("%s [oid=%s]", n.Name, formatOID(o))
	}
	val := walkValueColumn(*o)
	if val == "" {
		return fmt.Sprintf("%s (%s)", n.Name, kindName(o.Kind))
	}
	return fmt.Sprintf("%s (%s) = %s", n.Name, kindName(o.Kind), val)
}
