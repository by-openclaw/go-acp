package emberplus

import (
	"strconv"

	"acp/internal/export/canonical"
	"acp/internal/protocol/emberplus/compliance"
)

// modePointer / modeInline / modeBoth are the three values of each
// CanonicalOptions mode flag. Kept as constants so the resolver
// reads like prose.
const (
	modePointer = "pointer"
	modeInline  = "inline"
	modeBoth    = "both"
)

// resolve runs after Canonicalize's bottom-up tree build. It mutates
// the element map in place when opts request inline or both:
//
//   - labels (Ember+ §5.1.1) — per-level walk of basePath subtree,
//     populates Matrix.TargetLabels / SourceLabels, absorbs (removes
//     from tree) in inline mode. Preserves in both mode. No-op in
//     pointer mode.
//
//   - parametersLocation (Ember+ §5.1.2) — same contract applied to
//     Matrix.ParametersLocation, populates Matrix.TargetParams /
//     SourceParams / ConnectionParams.
//
//   - templateReference (Ember+ §8) — walks Parameter / Node / Matrix
//     elements and inflates templateReference in inline / both modes,
//     resolving against the top-level Templates slice.
//
// Compliance events fire on every branch where the wire is malformed
// (missing basePath, absent description, level-count mismatch). Events
// are informational for the "absent" cases and warnings for the
// "unresolved" cases; see profile.go for the catalog.
func (p *Plugin) resolve(elements map[string]canonical.Element, templates []*canonical.TemplateEntry, opts CanonicalOptions) {
	if elements == nil {
		return
	}
	for _, el := range elements {
		m, ok := el.(*canonical.Matrix)
		if !ok {
			continue
		}
		p.resolveMatrixLabels(m, elements, opts.Labels)
		p.resolveMatrixGain(m, elements, opts.Gain)
	}
	p.resolveTemplates(elements, templates, opts.Templates)
}

// resolveMatrixLabels walks each labels[] entry, locates the pointed-at
// Node, pulls target / source label Parameters out of it, and keys the
// result by the level's description (or basePath if description is
// empty). Handles:
//
//   - no labels[]         → MatrixLabelNone (info)
//   - basePath unresolved → MatrixLabelBasepathUnresolved (warn)
//   - blank description   → MatrixLabelDescriptionEmpty (info)
//   - level-count skew    → MatrixLabelLevelMismatch (info)
//   - inline mode         → absorb (remove Labels Node from tree)
//   - both mode           → populate maps, keep subtree
//   - pointer mode        → no-op (wire-faithful)
func (p *Plugin) resolveMatrixLabels(m *canonical.Matrix, elements map[string]canonical.Element, mode string) {
	if mode == modePointer {
		return
	}

	if len(m.Labels) == 0 {
		if p.profile != nil {
			p.profile.Note(compliance.MatrixLabelNone)
		}
		return
	}

	targetLabels := make(map[string]map[string]string)
	sourceLabels := make(map[string]map[string]string)
	absorb := make([]string, 0, len(m.Labels))

	for _, lbl := range m.Labels {
		key := ""
		if lbl.Description != nil && *lbl.Description != "" {
			key = *lbl.Description
		} else {
			key = lbl.BasePath
			if p.profile != nil {
				p.profile.Note(compliance.MatrixLabelDescriptionEmpty)
			}
		}

		base, ok := elements[lbl.BasePath]
		if !ok {
			if p.profile != nil {
				p.profile.Note(compliance.MatrixLabelBasepathUnresolved)
			}
			continue
		}
		baseNode, ok := base.(*canonical.Node)
		if !ok {
			if p.profile != nil {
				p.profile.Note(compliance.MatrixLabelBasepathUnresolved)
			}
			continue
		}

		tgts, srcs := extractTargetSourceNodes(baseNode)
		if tgts != nil {
			if out := extractLabelMap(tgts); len(out) > 0 {
				targetLabels[key] = out
			}
		}
		if srcs != nil {
			if out := extractLabelMap(srcs); len(out) > 0 {
				sourceLabels[key] = out
			}
		}

		if mode == modeInline {
			absorb = append(absorb, lbl.BasePath)
		}
	}

	if mismatched(targetLabels) || mismatched(sourceLabels) {
		if p.profile != nil {
			p.profile.Note(compliance.MatrixLabelLevelMismatch)
		}
	}

	if len(targetLabels) > 0 {
		m.TargetLabels = targetLabels
	}
	if len(sourceLabels) > 0 {
		m.SourceLabels = sourceLabels
	}

	if mode == modeInline && len(absorb) > 0 {
		removeFromTree(elements, absorb)
		if p.profile != nil {
			p.profile.Note(compliance.LabelsAbsorbed)
		}
	}
}

// resolveMatrixGain handles parametersLocation — a single-valued
// analog of labels. The pointed-at Node contains targets (number=1),
// sources (number=2), and connections (number=3) subtrees; each holds
// Parameters (typically gain). Populates Matrix.TargetParams /
// SourceParams / ConnectionParams.
//
// Unlike labels, parametersLocation is not multi-level — matrix has
// exactly one or zero pointers. The map structure flattens straight
// to number → pid → value.
func (p *Plugin) resolveMatrixGain(m *canonical.Matrix, elements map[string]canonical.Element, mode string) {
	if mode == modePointer {
		return
	}
	if m.ParametersLocation == nil || *m.ParametersLocation == "" {
		return
	}

	base, ok := elements[*m.ParametersLocation]
	if !ok {
		if p.profile != nil {
			p.profile.Note(compliance.MatrixParametersLocationUnresolved)
		}
		return
	}
	baseNode, ok := base.(*canonical.Node)
	if !ok {
		if p.profile != nil {
			p.profile.Note(compliance.MatrixParametersLocationUnresolved)
		}
		return
	}

	tgts, srcs, conns := extractParamSubtrees(baseNode)
	if tgts != nil {
		if out := extractParamMap(tgts); len(out) > 0 {
			m.TargetParams = out
		}
	}
	if srcs != nil {
		if out := extractParamMap(srcs); len(out) > 0 {
			m.SourceParams = out
		}
	}
	if conns != nil {
		if out := extractConnectionParamMap(conns); len(out) > 0 {
			m.ConnectionParams = out
		}
	}

	if mode == modeInline {
		removeFromTree(elements, []string{*m.ParametersLocation})
		if p.profile != nil {
			p.profile.Note(compliance.GainAbsorbed)
		}
	}
}

// resolveTemplates walks every element carrying a templateReference
// and inflates it when mode is inline or both. Templates live at
// Export.Templates — the referring element carries only the OID ref.
//
// Inflation copies the template's content into the referring element:
//
//   - Node ref → Node template : copy Children; inherit Description
//     if the referrer has none. Preserve referrer's own Number /
//     Identifier / Path / OID.
//   - Parameter ref → Parameter template : copy Type / Default /
//     Minimum / Maximum / Step / Unit / Format / Factor / Formula /
//     Enumeration / EnumMap / StreamDescriptor when the referrer
//     has no value for that field. Value stays the referrer's.
//   - Matrix ref → Matrix template : copy Type / Mode / counts /
//     Labels / Targets / Sources when unset.
//   - Function ref → Function template : copy Arguments / Result.
//
// Cross-type references (e.g. Node ref to a Parameter template) are
// spec-illegal; resolveTemplates fires TemplateReferenceUnresolved
// and leaves the referrer untouched.
func (p *Plugin) resolveTemplates(elements map[string]canonical.Element, templates []*canonical.TemplateEntry, mode string) {
	if mode == modePointer {
		return
	}

	index := make(map[string]*canonical.TemplateEntry, len(templates))
	for _, t := range templates {
		if t != nil && t.OID != "" {
			index[t.OID] = t
		}
	}

	for _, el := range elements {
		ref := elementTemplateRef(el)
		if ref == nil || *ref == "" {
			continue
		}
		t, ok := index[*ref]
		if !ok || t == nil || t.Template == nil {
			if p.profile != nil {
				p.profile.Note(compliance.TemplateReferenceUnresolved)
			}
			continue
		}
		if !inflateTemplate(el, t.Template) {
			if p.profile != nil {
				p.profile.Note(compliance.TemplateReferenceUnresolved)
			}
			continue
		}
		if p.profile != nil {
			p.profile.Note(compliance.TemplateAbsorbed)
		}
	}
}

// inflateTemplate copies fields from tpl into dst when dst has the
// zero value for each field. Returns false if dst and tpl are
// incompatible concrete types (spec-illegal cross-type reference).
func inflateTemplate(dst, tpl canonical.Element) bool {
	switch d := dst.(type) {
	case *canonical.Node:
		t, ok := tpl.(*canonical.Node)
		if !ok {
			return false
		}
		if d.Description == nil && t.Description != nil {
			v := *t.Description
			d.Description = &v
		}
		if len(d.Children) == 0 && len(t.Children) > 0 {
			d.Children = append([]canonical.Element(nil), t.Children...)
		}
		if d.SchemaIdentifiers == nil && t.SchemaIdentifiers != nil {
			v := *t.SchemaIdentifiers
			d.SchemaIdentifiers = &v
		}
		return true

	case *canonical.Parameter:
		t, ok := tpl.(*canonical.Parameter)
		if !ok {
			return false
		}
		if d.Description == nil && t.Description != nil {
			v := *t.Description
			d.Description = &v
		}
		if d.Type == "" {
			d.Type = t.Type
		}
		if d.Default == nil {
			d.Default = t.Default
		}
		if d.Minimum == nil {
			d.Minimum = t.Minimum
		}
		if d.Maximum == nil {
			d.Maximum = t.Maximum
		}
		if d.Step == nil {
			d.Step = t.Step
		}
		if d.Unit == nil && t.Unit != nil {
			v := *t.Unit
			d.Unit = &v
		}
		if d.Format == nil && t.Format != nil {
			v := *t.Format
			d.Format = &v
		}
		if d.Factor == nil && t.Factor != nil {
			v := *t.Factor
			d.Factor = &v
		}
		if d.Formula == nil && t.Formula != nil {
			v := *t.Formula
			d.Formula = &v
		}
		if d.Enumeration == nil && t.Enumeration != nil {
			v := *t.Enumeration
			d.Enumeration = &v
		}
		if len(d.EnumMap) == 0 && len(t.EnumMap) > 0 {
			d.EnumMap = append([]canonical.EnumEntry(nil), t.EnumMap...)
		}
		if d.StreamDescriptor == nil && t.StreamDescriptor != nil {
			v := *t.StreamDescriptor
			d.StreamDescriptor = &v
		}
		return true
	}
	return false
}

// elementTemplateRef returns the templateReference pointer for any
// element that has one. Matrix / Function currently carry no ref in
// the canonical shape, so they return nil. Node / Parameter return
// their respective fields.
func elementTemplateRef(el canonical.Element) *string {
	switch v := el.(type) {
	case *canonical.Node:
		return v.TemplateReference
	case *canonical.Parameter:
		return v.TemplateReference
	}
	return nil
}

// extractTargetSourceNodes finds the labels Node's children named
// "targets" (number=1) and "sources" (number=2) per Ember+ §5.1.1.
// Returns nil when a child is missing — no synthesis.
func extractTargetSourceNodes(base *canonical.Node) (targets, sources *canonical.Node) {
	for _, c := range base.Children {
		n, ok := c.(*canonical.Node)
		if !ok {
			continue
		}
		switch n.Number {
		case 1:
			targets = n
		case 2:
			sources = n
		}
	}
	return targets, sources
}

// extractParamSubtrees finds the parametersLocation Node's children
// named "targets" (1), "sources" (2), "connections" (3) per §5.1.2.
func extractParamSubtrees(base *canonical.Node) (targets, sources, conns *canonical.Node) {
	for _, c := range base.Children {
		n, ok := c.(*canonical.Node)
		if !ok {
			continue
		}
		switch n.Number {
		case 1:
			targets = n
		case 2:
			sources = n
		case 3:
			conns = n
		}
	}
	return targets, sources, conns
}

// extractLabelMap pulls `{number: value}` string pairs out of a
// targets/sources Node. Non-string Parameter values are skipped with
// no event — the Parameter still exists on the pointer path.
func extractLabelMap(container *canonical.Node) map[string]string {
	out := make(map[string]string, len(container.Children))
	for _, c := range container.Children {
		pr, ok := c.(*canonical.Parameter)
		if !ok {
			continue
		}
		s, ok := pr.Value.(string)
		if !ok {
			continue
		}
		out[strconv.Itoa(pr.Number)] = s
	}
	return out
}

// extractParamMap pulls `{number: {paramIdentifier: value}}` out of a
// targets/sources Node under parametersLocation. Each child is itself
// a Node whose children are Parameters; we flatten to
// {childNumber: {paramIdentifier: paramValue}}.
//
// Spec §5.1.2: targets/N and sources/N are single-indexed (target or
// source index); connections/N/M is double-indexed — see
// extractConnectionParamMap for that two-deep case.
func extractParamMap(container *canonical.Node) map[string]map[string]any {
	out := make(map[string]map[string]any, len(container.Children))
	for _, c := range container.Children {
		n, ok := c.(*canonical.Node)
		if !ok {
			continue
		}
		inner := make(map[string]any, len(n.Children))
		for _, cc := range n.Children {
			pr, ok := cc.(*canonical.Parameter)
			if !ok {
				continue
			}
			inner[pr.Identifier] = pr.Value
		}
		if len(inner) > 0 {
			out[strconv.Itoa(n.Number)] = inner
		}
	}
	return out
}

// extractConnectionParamMap pulls `{"target.source": {param: value}}`
// out of the connections Node under parametersLocation. Per spec
// §5.1.2 the connections subtree is two-deep: each `target` child is
// a Node whose children are per-`source` Nodes, each carrying the
// crosspoint's Parameters (typically a `gain` float).
//
// Composite key "<target>.<source>" keeps the map flat and
// addressable — matches the Matrix.Connections shape where each row
// references a target+source pair. Consumers can split on "." to
// recover the indices.
func extractConnectionParamMap(container *canonical.Node) map[string]map[string]any {
	out := make(map[string]map[string]any, len(container.Children))
	for _, c := range container.Children {
		targetNode, ok := c.(*canonical.Node)
		if !ok {
			continue
		}
		for _, cc := range targetNode.Children {
			sourceNode, ok := cc.(*canonical.Node)
			if !ok {
				continue
			}
			inner := make(map[string]any, len(sourceNode.Children))
			for _, ccc := range sourceNode.Children {
				pr, ok := ccc.(*canonical.Parameter)
				if !ok {
					continue
				}
				inner[pr.Identifier] = pr.Value
			}
			if len(inner) > 0 {
				key := strconv.Itoa(targetNode.Number) + "." + strconv.Itoa(sourceNode.Number)
				out[key] = inner
			}
		}
	}
	return out
}

// mismatched reports whether a labels map has any two levels with
// different entry counts. Single-level or empty maps always return
// false.
func mismatched(m map[string]map[string]string) bool {
	if len(m) < 2 {
		return false
	}
	count := -1
	for _, inner := range m {
		if count < 0 {
			count = len(inner)
			continue
		}
		if len(inner) != count {
			return true
		}
	}
	return false
}

// removeFromTree deletes each OID from the element map AND from its
// parent's children slice. Called by inline-mode absorption. Silent
// on missing parents (the element might be a tree root).
func removeFromTree(elements map[string]canonical.Element, oids []string) {
	// Delete children recursively first — when absorbing a labels
	// subtree, the `targets`/`sources` children also vanish.
	for _, oid := range oids {
		deleteSubtree(elements, oid)
	}

	// Patch each parent's Children slice.
	for _, oid := range oids {
		parentOID := parentOfNumericKey(oid)
		if parentOID == "" {
			continue
		}
		parent, ok := elements[parentOID]
		if !ok {
			continue
		}
		header := parent.Common()
		filtered := make([]canonical.Element, 0, len(header.Children))
		for _, c := range header.Children {
			if c.Common().OID == oid {
				continue
			}
			filtered = append(filtered, c)
		}
		header.Children = filtered
	}
}

// deleteSubtree walks the descendants of oid and removes each from
// the element map. Does not patch the parent — that's removeFromTree's
// job, to avoid mutating parent.Children while we're still reading it.
func deleteSubtree(elements map[string]canonical.Element, oid string) {
	el, ok := elements[oid]
	if !ok {
		return
	}
	for _, c := range el.Common().Children {
		deleteSubtree(elements, c.Common().OID)
	}
	delete(elements, oid)
}
