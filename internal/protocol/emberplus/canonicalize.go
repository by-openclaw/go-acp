package emberplus

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"acp/internal/export/canonical"
	"acp/internal/protocol"
	"acp/internal/protocol/emberplus/compliance"
	"acp/internal/protocol/emberplus/glow"
)

// CanonicalOptions controls which form the exporter emits for
// templateReference, matrix labels, and matrix gain.
//
// Values per `docs/protocols/schema.md` §4. Defaults are "pointer":
// the translator always preserves the wire-provided reference and
// never inflates. A later resolver pass (step 4) converts to
// inline/both when asked. Using the default keeps the output
// deterministic and side-effect-free.
type CanonicalOptions struct {
	Templates string // "inline" | "pointer" | "both"  (default "pointer")
	Labels    string // "inline" | "pointer" | "both"  (default "pointer")
	Gain      string // "inline" | "pointer" | "both"  (default "pointer")
}

// normalise replaces empty strings with the canonical default
// ("pointer") and lower-cases the rest. Unknown values map to
// "pointer" with no error — misconfiguration must never fail a
// capture.
func (o *CanonicalOptions) normalise() {
	o.Templates = normMode(o.Templates)
	o.Labels = normMode(o.Labels)
	o.Gain = normMode(o.Gain)
}

func normMode(s string) string {
	switch strings.ToLower(s) {
	case "inline":
		return "inline"
	case "both":
		return "both"
	default:
		return "pointer"
	}
}

// Canonicalize walks the plugin's in-RAM Glow tree and returns a
// canonical Export. The translator is pure: it takes the treeMu read
// lock, builds the tree bottom-up from the flat numIndex, and
// releases the lock before returning.
//
// The resolver pass (step 4) inflates templateReference / basePath /
// parametersLocation when opts request inline or both; until that
// lands, Canonicalize emits pointer-form regardless of opts, logging
// compliance events so the inflated output is reproducible later
// without rewalking.
//
// Returns (*Export, nil) on success. Errors are context-cancellation
// or tree-state problems (nil plugin state, unknown concrete type on
// an entry).
func (p *Plugin) Canonicalize(ctx context.Context, opts CanonicalOptions) (*canonical.Export, error) {
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("canonicalize canceled: %w", err)
	}
	opts.normalise()

	p.treeMu.RLock()
	defer p.treeMu.RUnlock()

	// Pass 1: build the canonical element per entry (children empty).
	elements := make(map[string]canonical.Element, len(p.numIndex))
	for oid, entry := range p.numIndex {
		el, err := p.buildElement(entry)
		if err != nil {
			return nil, fmt.Errorf("build %q: %w", oid, err)
		}
		if el != nil {
			elements[oid] = el
		}
	}

	// Pass 2: attach each element under its parent, collect roots.
	type indexed struct {
		oid  string
		elem canonical.Element
	}
	childrenByParent := make(map[string][]indexed)
	var roots []indexed

	for oid, el := range elements {
		parentOID := parentOfNumericKey(oid)
		if parentOID == "" || elements[parentOID] == nil {
			roots = append(roots, indexed{oid, el})
			continue
		}
		childrenByParent[parentOID] = append(childrenByParent[parentOID], indexed{oid, el})
	}

	// Sort children deterministically by Number then OID.
	for parentOID, kids := range childrenByParent {
		sort.Slice(kids, func(i, j int) bool {
			a := kids[i].elem.Common()
			b := kids[j].elem.Common()
			if a.Number != b.Number {
				return a.Number < b.Number
			}
			return kids[i].oid < kids[j].oid
		})
		childrenByParent[parentOID] = kids
	}

	// Pass 3: assign children slices into parents.
	for oid, kids := range childrenByParent {
		parent := elements[oid]
		header := parent.Common()
		slice := make([]canonical.Element, 0, len(kids))
		for _, k := range kids {
			slice = append(slice, k.elem)
		}
		header.Children = slice
	}

	// Root selection: one root is the tree; many roots get wrapped
	// in a synthetic Node so Export.Root is always a single element.
	sort.Slice(roots, func(i, j int) bool {
		return roots[i].oid < roots[j].oid
	})

	var root canonical.Element
	switch {
	case len(roots) == 0:
		root = emptyRoot()
	case len(roots) == 1:
		root = roots[0].elem
	default:
		kids := make([]canonical.Element, 0, len(roots))
		for _, r := range roots {
			kids = append(kids, r.elem)
		}
		rn := emptyRoot().(*canonical.Node)
		rn.Children = kids
		root = rn
	}

	// Templates: always emit as pointer-form under the current
	// translator. Resolver (step 4) will drop under --templates=inline.
	var templates []*canonical.TemplateEntry
	p.templatesMu.RLock()
	for _, t := range p.templates {
		if te := p.buildTemplateEntry(t); te != nil {
			templates = append(templates, te)
		}
	}
	p.templatesMu.RUnlock()
	sort.Slice(templates, func(i, j int) bool {
		return templates[i].OID < templates[j].OID
	})

	// Resolver pass (step 4): inflate and/or absorb label subtrees,
	// parametersLocation subtrees, and templateReference inclusions
	// when opts request inline or both. Pointer mode is a no-op.
	// Compliance events fire for every unresolved reference so audit
	// tooling can surface provider wire defects.
	p.resolve(elements, templates, opts)

	// If roots changed (e.g. inline absorbed a root-level labels Node,
	// which would be unusual but legal), re-derive the single root.
	// We keep the original pass-3 root; Canonicalize guarantees the
	// matrix and its ancestors never get absorbed, so this is a
	// belt-and-braces guard.
	if root.Common().Children != nil && len(root.Common().Children) == 0 && len(elements) > 0 {
		p.logger.Debug("emberplus: canonicalize resolver emptied root children")
	}

	return &canonical.Export{
		Root:      root,
		Templates: templates,
	}, nil
}

// parentOfNumericKey returns the parent OID of a dot-joined numeric
// key. "1.2.3" → "1.2"; "1" → "". Empty input → "".
func parentOfNumericKey(k string) string {
	i := strings.LastIndex(k, ".")
	if i < 0 {
		return ""
	}
	return k[:i]
}

// emptyRoot builds the synthetic root Node used when a tree has zero
// or multiple top-level Glow elements. Identifier/path/oid are empty
// strings by design — exporters differentiate synthetic roots by the
// empty identifier, and importers treat the node as transparent.
func emptyRoot() canonical.Element {
	return &canonical.Node{
		Header: canonical.Header{
			Number:      0,
			Identifier:  "",
			Path:        "",
			OID:         "",
			Description: nil,
			IsOnline:    true,
			Access:      canonical.AccessRead,
			Children:    canonical.EmptyChildren(),
		},
	}
}

// buildElement dispatches on the concrete Glow pointer stored on the
// entry and produces the matching canonical element without its
// children (children are wired up in pass 3).
func (p *Plugin) buildElement(e *treeEntry) (canonical.Element, error) {
	switch {
	case e.glowNode != nil:
		return p.buildNode(e), nil
	case e.glowParam != nil:
		return p.buildParameter(e), nil
	case e.glowMatrix != nil:
		return p.buildMatrix(e), nil
	case e.glowFunc != nil:
		return p.buildFunction(e), nil
	}
	return nil, fmt.Errorf("tree entry at %q has no concrete glow type", e.obj.OID)
}

// buildHeader returns the common 8-key header every canonical element
// begins with. Takes identifier/description/isOnline/access because
// those aren't uniformly available on the treeEntry — the caller
// threads them from the relevant Glow struct.
func buildHeader(e *treeEntry, identifier, description string, isOnline bool, access int64) canonical.Header {
	return canonical.Header{
		Number:      int(e.obj.ID),
		Identifier:  identifier,
		Path:        strings.Join(e.obj.Path, "."),
		OID:         e.obj.OID,
		Description: optString(description),
		IsOnline:    isOnline,
		Access:      canonicalAccess(access),
		Children:    canonical.EmptyChildren(),
	}
}

// canonicalAccess maps the Ember+ ParameterAccess integer to the
// canonical access string. Zero maps to "read" — the spec default
// when the field is absent on the wire (Ember+ p.85).
func canonicalAccess(a int64) string {
	switch a {
	case 0, 1:
		return canonical.AccessRead
	case 2:
		return canonical.AccessWrite
	case 3:
		return canonical.AccessReadWrite
	}
	return canonical.AccessRead
}

// optString returns nil for empty strings, otherwise a pointer to a
// copy. Used for nullable Description / Unit / Format / Formula keys
// so empty Glow fields round-trip as null, not "".
func optString(s string) *string {
	if s == "" {
		return nil
	}
	v := s
	return &v
}

func optInt64(i int64) *int64 {
	if i == 0 {
		return nil
	}
	v := i
	return &v
}

func (p *Plugin) buildNode(e *treeEntry) *canonical.Node {
	n := e.glowNode
	// Ember+ spec p.87: isOnline defaults true when absent. The Glow
	// decoder stores false when absent as well as when explicitly
	// false, so we cannot disambiguate — apply the spec default here
	// for canonical output.
	return &canonical.Node{
		Header: buildHeader(e, n.Identifier, n.Description, true, 1),
		TemplateReference: func() *string {
			if len(n.TemplateReference) > 0 {
				v := numericKey(n.TemplateReference)
				return &v
			}
			return nil
		}(),
		SchemaIdentifiers: optString(n.SchemaIdentifiers),
	}
}

// splitFormatUnit splits an Ember+ Parameter.Format string at the
// unit marker '°' (spec p.85). The left part is the printf-style
// format; the right part is the unit text. Empty results become nil.
func splitFormatUnit(f string) (format, unit *string) {
	if f == "" {
		return nil, nil
	}
	if i := strings.IndexRune(f, '°'); i >= 0 {
		return optString(f[:i]), optString(f[i+len("°"):])
	}
	return optString(f), nil
}

// enumMapToCanonical converts Glow's map[int64]string to the
// canonical []EnumEntry form, applying the smh masked-item convention
// (leading '~' in label → strip + set masked=true). Deterministic
// order: by numeric value ascending.
func (p *Plugin) enumMapToCanonical(glowMap map[int64]string, enumeration string) []canonical.EnumEntry {
	if len(glowMap) == 0 && enumeration == "" {
		return nil
	}

	// If native map is absent, derive from the LF-joined legacy form
	// (spec §4.5 enumMap unification).
	if len(glowMap) == 0 {
		entries := make([]canonical.EnumEntry, 0)
		for i, lbl := range strings.Split(enumeration, "\n") {
			entry := canonical.EnumEntry{
				Key:   stripMaskPrefix(lbl),
				Value: int64(i),
			}
			if isMasked(lbl) {
				entry.Masked = true
				if p.profile != nil {
					p.profile.Note(compliance.EnumMaskedItem)
				}
			}
			entries = append(entries, entry)
		}
		if p.profile != nil {
			p.profile.Note(compliance.EnumMapDerived)
		}
		return entries
	}

	// Native map — materialise and sort.
	entries := make([]canonical.EnumEntry, 0, len(glowMap))
	for v, k := range glowMap {
		entry := canonical.EnumEntry{
			Key:   stripMaskPrefix(k),
			Value: v,
		}
		if isMasked(k) {
			entry.Masked = true
			if p.profile != nil {
				p.profile.Note(compliance.EnumMaskedItem)
			}
		}
		entries = append(entries, entry)
	}
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].Value < entries[j].Value
	})
	// Both forms present — fires only when they disagree on count,
	// which we check via a quick length compare against the legacy
	// enumeration split.
	if enumeration != "" && p.profile != nil {
		legacy := strings.Split(enumeration, "\n")
		if len(legacy) != len(entries) {
			p.profile.Note(compliance.EnumDoubleSource)
		}
	}
	return entries
}

func stripMaskPrefix(lbl string) string {
	if strings.HasPrefix(lbl, "~") {
		return lbl[1:]
	}
	return lbl
}

func isMasked(lbl string) bool {
	return strings.HasPrefix(lbl, "~")
}

func (p *Plugin) buildParameter(e *treeEntry) *canonical.Parameter {
	pr := e.glowParam
	typeName := paramTypeName(pr.Type)
	if typeName == "" || typeName == "null" {
		if inferred := inferParamType(pr.Value); inferred != "" {
			typeName = inferred
			if p.profile != nil {
				p.profile.Note(compliance.FieldInferred)
			}
		} else {
			typeName = canonical.ParamString
			if p.profile != nil {
				p.profile.Note(compliance.FieldInferred)
			}
		}
	}

	format, unit := splitFormatUnit(pr.Format)

	var streamID *int64
	if pr.StreamIdentifier != 0 {
		v := pr.StreamIdentifier
		streamID = &v
	}
	var streamDesc *canonical.StreamDescriptor
	if pr.StreamDescriptor != nil {
		streamDesc = &canonical.StreamDescriptor{
			Format: int(pr.StreamDescriptor.Format),
			Offset: int(pr.StreamDescriptor.Offset),
		}
	}

	tmplRef := func() *string {
		if len(pr.TemplateReference) > 0 {
			v := numericKey(pr.TemplateReference)
			return &v
		}
		return nil
	}()

	return &canonical.Parameter{
		Header:            buildHeader(e, pr.Identifier, pr.Description, pr.IsOnline || true, pr.Access),
		Type:              typeName,
		Value:             pr.Value,
		Default:           pr.Default,
		Minimum:           pr.Minimum,
		Maximum:           pr.Maximum,
		Step:              pr.Step,
		Unit:              unit,
		Format:            format,
		Factor:            optInt64(pr.Factor),
		Formula:           optString(pr.Formula),
		Enumeration:       optString(pr.Enumeration),
		EnumMap:           p.enumMapToCanonical(pr.EnumMap, pr.Enumeration),
		StreamIdentifier:  streamID,
		StreamDescriptor:  streamDesc,
		TemplateReference: tmplRef,
		SchemaIdentifiers: optString(pr.SchemaIdentifiers),
	}
}

func (p *Plugin) buildMatrix(e *treeEntry) *canonical.Matrix {
	m := e.glowMatrix

	// ParametersLocation on the wire is a CHOICE: RelOID path OR
	// inline integer. Path → canonical string OID; integer → we
	// leave ParametersLocation nil and rely on GainParameterNumber
	// to carry the index.
	var parametersLocation *string
	switch v := m.ParametersLocation.(type) {
	case []int32:
		if len(v) > 0 {
			s := numericKey(v)
			parametersLocation = &s
		}
	case int32:
		// Inline integer — not a pointer. GainParameterNumber
		// below carries the effective number.
	}

	var gainNum *int64
	if m.GainParameterNumber != 0 {
		v := int64(m.GainParameterNumber)
		gainNum = &v
	}

	labels := make([]canonical.MatrixLabel, 0, len(m.Labels))
	for _, l := range m.Labels {
		labels = append(labels, canonical.MatrixLabel{
			BasePath:    numericKey(l.BasePath),
			Description: optString(l.Description),
		})
	}

	targets := make([]canonical.MatrixTarget, 0, len(m.Targets))
	for _, t := range m.Targets {
		targets = append(targets, canonical.MatrixTarget{Number: int64(t)})
	}
	sort.Slice(targets, func(i, j int) bool { return targets[i].Number < targets[j].Number })

	sources := make([]canonical.MatrixSource, 0, len(m.Sources))
	for _, s := range m.Sources {
		sources = append(sources, canonical.MatrixSource{Number: int64(s)})
	}
	sort.Slice(sources, func(i, j int) bool { return sources[i].Number < sources[j].Number })

	connections := make([]canonical.MatrixConnection, 0, len(m.Connections))
	for _, c := range m.Connections {
		srcs := make([]int64, 0, len(c.Sources))
		for _, s := range c.Sources {
			srcs = append(srcs, int64(s))
		}
		sort.Slice(srcs, func(i, j int) bool { return srcs[i] < srcs[j] })
		locked := connDispName(c.Disposition) == canonical.ConnDispLocked
		connections = append(connections, canonical.MatrixConnection{
			Target:      int64(c.Target),
			Sources:     srcs,
			Operation:   connOpName(c.Operation),
			Disposition: connDispName(c.Disposition),
			Locked:      locked,
		})
	}
	sort.Slice(connections, func(i, j int) bool { return connections[i].Target < connections[j].Target })

	var maxTotal *int64
	var maxPer *int64
	if m.MatrixType == glow.MatrixTypeNToN {
		if m.MaxTotalConnects != 0 {
			v := int64(m.MaxTotalConnects)
			maxTotal = &v
		}
		if m.MaxConnectsPerTarget != 0 {
			v := int64(m.MaxConnectsPerTarget)
			maxPer = &v
		}
	}

	return &canonical.Matrix{
		Header:                   buildHeader(e, m.Identifier, m.Description, true, 3),
		Type:                     matrixTypeName(m.MatrixType),
		Mode:                     matrixAddrName(m.AddressingMode),
		TargetCount:              int64(m.TargetCount),
		SourceCount:              int64(m.SourceCount),
		MaximumTotalConnects:     maxTotal,
		MaximumConnectsPerTarget: maxPer,
		ParametersLocation:       parametersLocation,
		GainParameterNumber:      gainNum,
		Labels:                   labels,
		Targets:                  targets,
		Sources:                  sources,
		Connections:              connections,
		// Pointer-mode defaults. Resolver (step 4) fills these when
		// --labels=inline / --gain=inline.
		TargetLabels:     nil,
		SourceLabels:     nil,
		TargetParams:     nil,
		SourceParams:     nil,
		ConnectionParams: nil,
	}
}

func (p *Plugin) buildFunction(e *treeEntry) *canonical.Function {
	f := e.glowFunc
	args := make([]canonical.TupleItem, 0, len(f.Arguments))
	for _, a := range f.Arguments {
		args = append(args, canonical.TupleItem{Name: a.Name, Type: paramTypeName(a.Type)})
	}
	res := make([]canonical.TupleItem, 0, len(f.Result))
	for _, r := range f.Result {
		res = append(res, canonical.TupleItem{Name: r.Name, Type: paramTypeName(r.Type)})
	}
	return &canonical.Function{
		Header:    buildHeader(e, f.Identifier, f.Description, true, 1),
		Arguments: args,
		Result:    res,
	}
}

// buildTemplateEntry constructs a TemplateEntry for the top-level
// templates[] array. Each entry embeds the full element shape (Node /
// Parameter / Matrix / Function). The embedded element's children[]
// are the template's declared children.
func (p *Plugin) buildTemplateEntry(t *glow.Template) *canonical.TemplateEntry {
	if t == nil || t.Element == nil {
		return nil
	}
	oid := numericKey(t.Path)
	var number int
	if len(t.Path) > 0 {
		number = int(t.Path[len(t.Path)-1])
	} else {
		number = int(t.Number)
	}

	// Build a synthetic treeEntry so we reuse the element builders.
	synthetic := func(identifier string) *treeEntry {
		return &treeEntry{
			numericPath: t.Path,
			obj: protocol.Object{
				Slot:  0,
				ID:    number,
				OID:   oid,
				Label: identifier,
				Kind:  protocol.KindRaw,
				Path:  []string{identifier},
			},
		}
	}

	var el canonical.Element
	switch {
	case t.Element.Node != nil:
		te := synthetic(t.Element.Node.Identifier)
		te.glowNode = t.Element.Node
		el = p.buildNode(te)
	case t.Element.Parameter != nil:
		te := synthetic(t.Element.Parameter.Identifier)
		te.glowParam = t.Element.Parameter
		el = p.buildParameter(te)
	case t.Element.Matrix != nil:
		te := synthetic(t.Element.Matrix.Identifier)
		te.glowMatrix = t.Element.Matrix
		el = p.buildMatrix(te)
	case t.Element.Function != nil:
		te := synthetic(t.Element.Function.Identifier)
		te.glowFunc = t.Element.Function
		el = p.buildFunction(te)
	default:
		return nil
	}

	return &canonical.TemplateEntry{
		Number:      number,
		OID:         oid,
		Identifier:  el.Common().Identifier,
		Description: optString(t.Description),
		Template:    el,
	}
}
