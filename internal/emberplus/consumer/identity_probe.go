package emberplus

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"dhs/internal/protocol"
)

// IdentityProbe returns "<Product>@<Version>" for the connected provider.
//
// Ember+ does NOT mandate a fixed identity location in the spec. This
// probe uses a layered detection strategy, in order of strictness:
//
//  1. DTD 2.30+ schemaIdentifiers (spec p.87 NodeContents [4]):
//     find any Node whose schemaIdentifiers list contains an entry
//     ending in ".identity". Modern providers (Lawo, Riedel) follow
//     this mechanism.
//
//  2. Identifier "identity" (case-insensitive): find any Node whose
//     own identifier matches "identity". Covers TinyEmberPlus
//     ("identity"), DHD audio ("Identity"), Lawo legacy
//     ("identity").
//
//  3. Identity-like Node by content: find any Node whose direct
//     children include BOTH a product-candidate Parameter AND a
//     version-candidate Parameter (case- and space-insensitive
//     matching). Covers vendors that omit the "identity" wrapper
//     and put identity fields directly on a generic Node — e.g.
//     EMTWO devices use a "Device" Node with "Hardware Name" /
//     "Software Version" / "Serial Number" children.
//
// Inside the located Node, child Parameter identifiers are matched
// against these candidate lists. Identifier normalisation strips
// spaces, hyphens, and underscores, and lower-cases the result; so
// "Hardware Name" → "hardwarename" matches the "hardwarename"
// candidate.
//
//   - product: {product, name, model, hardwarename, productname,
//     modelname}
//   - version: {version, softwareversion, release, firmwareversion,
//     firmware, hardwareversion}
//
// First non-empty candidate in each list wins.
//
// Identity format matches ACP1 (CardName@HwVer) and ACP2 (Model@SwRev):
// "<Product>@<Version>", e.g. "EMTWO@4.8.0.1745420015".
//
// Slot is always 0 for Ember+ — the plugin flattens the Glow tree
// into one logical slot. Any other slot is a usage error.
//
// Pre-condition: a successful Walk must have populated the in-RAM
// indexes. IdentityProbe does no wire I/O.
func (p *Plugin) IdentityProbe(ctx context.Context, slot int) (string, error) {
	if slot != 0 {
		return "", fmt.Errorf("emberplus: only slot 0")
	}
	_ = ctx

	p.treeMu.RLock()
	defer p.treeMu.RUnlock()

	node := p.findIdentityNode()
	if node == nil {
		return "", fmt.Errorf("emberplus: identity probe — no identity node found (layers tried: schemaIdentifiers, 'identity' identifier, identity-like-children; walk before probe)")
	}

	children := p.directChildrenByIdentifier(node)
	product := pickIdentityValue(children, productCandidates()...)
	version := pickIdentityValue(children, versionCandidates()...)

	parentPath := strings.Join(node.obj.Path, ".")
	if product == "" {
		return "", fmt.Errorf("emberplus: identity probe — product Parameter not found under %s (tried: %s)", parentPath, strings.Join(productCandidates(), ", "))
	}
	if version == "" {
		return "", fmt.Errorf("emberplus: identity probe — version Parameter not found under %s (tried: %s)", parentPath, strings.Join(versionCandidates(), ", "))
	}

	return fmt.Sprintf("%s@%s", product, version), nil
}

// productCandidates returns the ordered list of identifier names the
// probe accepts for the product field. Comparison is normalised
// (lower-case, no spaces / hyphens / underscores).
func productCandidates() []string {
	return []string{"product", "name", "model", "hardwarename", "productname", "modelname"}
}

// versionCandidates returns the ordered list of identifier names the
// probe accepts for the version field. Comparison is normalised
// (lower-case, no spaces / hyphens / underscores).
func versionCandidates() []string {
	return []string{"version", "softwareversion", "release", "firmwareversion", "firmware", "hardwareversion"}
}

// findIdentityNode locates the identity Node per the layered
// strategy documented on IdentityProbe. Returns nil when no
// candidate matches any layer.
func (p *Plugin) findIdentityNode() *treeEntry {
	// Layer 1: DTD 2.30+ schemaIdentifiers (spec p.87 NodeContents [4]).
	for _, e := range p.sortedNodeEntries() {
		if hasIdentitySchema(e.glowNode.SchemaIdentifiers) {
			return e
		}
	}
	// Layer 2: identifier "identity" (case-insensitive).
	for _, e := range p.sortedNodeEntries() {
		if strings.EqualFold(e.glowNode.Identifier, "identity") {
			return e
		}
	}
	// Layer 3: Node whose direct children include both a product and
	// a version candidate. Sorted iteration → deterministic pick when
	// multiple Nodes match (shallower depth + lower OID wins).
	pc := productCandidates()
	vc := versionCandidates()
	for _, e := range p.sortedNodeEntries() {
		children := p.directChildrenByIdentifier(e)
		if pickIdentityValue(children, pc...) == "" {
			continue
		}
		if pickIdentityValue(children, vc...) == "" {
			continue
		}
		return e
	}
	return nil
}

// sortedNodeEntries returns every Node entry in numIndex ordered by
// (path-length asc, numericKey asc) so layered detection is
// deterministic. Shallower nodes win when multiple satisfy a layer.
func (p *Plugin) sortedNodeEntries() []*treeEntry {
	nodes := make([]*treeEntry, 0, len(p.numIndex))
	for _, e := range p.numIndex {
		if e.glowNode == nil {
			continue
		}
		nodes = append(nodes, e)
	}
	sort.Slice(nodes, func(i, j int) bool {
		if len(nodes[i].numericPath) != len(nodes[j].numericPath) {
			return len(nodes[i].numericPath) < len(nodes[j].numericPath)
		}
		return numericKey(nodes[i].numericPath) < numericKey(nodes[j].numericPath)
	})
	return nodes
}

// hasIdentitySchema returns true when the newline-separated
// schemaIdentifiers list (spec p.87 NodeContents [4]) contains any
// entry ending in ".identity".
func hasIdentitySchema(schemas string) bool {
	if schemas == "" {
		return false
	}
	for _, s := range strings.Split(schemas, "\n") {
		if strings.HasSuffix(strings.TrimSpace(s), ".identity") {
			return true
		}
	}
	return false
}

// directChildrenByIdentifier returns the direct child entries of
// parent, keyed by normalised identifier (lower-case, no spaces /
// hyphens / underscores) so subsequent lookups are case- and
// punctuation-insensitive. Direct child = entries whose numericPath
// has the parent's numericPath as a strict prefix and is exactly one
// segment longer.
func (p *Plugin) directChildrenByIdentifier(parent *treeEntry) map[string]*treeEntry {
	parentLen := len(parent.numericPath)
	children := make(map[string]*treeEntry)
	for _, e := range p.numIndex {
		if len(e.numericPath) != parentLen+1 {
			continue
		}
		match := true
		for i := 0; i < parentLen; i++ {
			if e.numericPath[i] != parent.numericPath[i] {
				match = false
				break
			}
		}
		if !match {
			continue
		}
		if id := entryIdentifier(e); id != "" {
			children[normalizeIdentifier(id)] = e
		}
	}
	return children
}

// normalizeIdentifier lower-cases and strips spaces / hyphens /
// underscores so vendor variants like "Hardware Name",
// "hardware_name", "Hardware-Name" all collapse to "hardwarename".
func normalizeIdentifier(id string) string {
	var b strings.Builder
	b.Grow(len(id))
	for _, r := range id {
		switch r {
		case ' ', '\t', '-', '_':
			continue
		default:
			if r >= 'A' && r <= 'Z' {
				r += 'a' - 'A'
			}
			b.WriteRune(r)
		}
	}
	return b.String()
}

// entryIdentifier returns the bare identifier of a tree entry,
// preferring the Glow struct's Identifier field over the protocol
// Object's Label fallback.
func entryIdentifier(e *treeEntry) string {
	if e.glowParam != nil {
		return e.glowParam.Identifier
	}
	if e.glowNode != nil {
		return e.glowNode.Identifier
	}
	if e.glowMatrix != nil {
		return e.glowMatrix.Identifier
	}
	if e.glowFunc != nil {
		return e.glowFunc.Identifier
	}
	if len(e.obj.Path) > 0 {
		return e.obj.Path[len(e.obj.Path)-1]
	}
	return e.obj.Label
}

// pickIdentityValue resolves the first non-empty string value among
// candidate child identifiers (normalised match). Returns "" when
// none resolves.
func pickIdentityValue(children map[string]*treeEntry, candidates ...string) string {
	for _, name := range candidates {
		if entry, ok := children[normalizeIdentifier(name)]; ok {
			if v := identityStringValue(entry); v != "" {
				return v
			}
		}
	}
	return ""
}

// identityStringValue extracts the textual value of an
// identity-subtree entry. Product/version are KindString on every
// real provider observed; a numeric version (rare) is rendered via
// fmt.Sprintf as a fallback so the cache key stays stable.
func identityStringValue(entry *treeEntry) string {
	if entry == nil {
		return ""
	}
	if entry.obj.Value.Kind == protocol.KindString {
		return entry.obj.Value.Str
	}
	if entry.glowParam != nil && entry.glowParam.Value != nil {
		return fmt.Sprintf("%v", entry.glowParam.Value)
	}
	return ""
}
