package emberplus

import (
	"context"
	"fmt"
	"strings"

	"dhs/internal/protocol"
)

// IdentityProbe returns "<Product>@<Version>" for the connected provider.
//
// Ember+ does NOT mandate a fixed identity location in the spec. This
// probe uses a layered detection strategy, in order:
//
//  1. DTD 2.30+ schemaIdentifiers (spec p.87 NodeContents [4]):
//     find any Node whose schemaIdentifiers list contains an entry
//     ending in ".identity". Modern providers (Lawo, Riedel) follow
//     this mechanism.
//
//  2. Identifier convention (used by DTD < 2.30 providers and any
//     provider not emitting schemaIdentifiers): find any Node whose
//     own identifier matches "identity" case-insensitively. This
//     covers TinyEmberPlus / libember-cpp samples ("identity") as
//     well as vendors that use a capitalised form like "Identity"
//     (e.g. DHD audio).
//
// Inside the located Node, child Parameter identifiers are matched
// case-insensitively against these candidate lists; the first
// candidate with a non-empty string value wins:
//
//   - product: {product, name, model}
//   - version: {version, softwareversion, release, firmwareversion,
//     firmware}
//
// Identity format matches ACP1 (CardName@HwVer) and ACP2 (Model@SwRev):
// "<Product>@<Version>", e.g. "52-7520@10.1.7.1".
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
		return "", fmt.Errorf("emberplus: identity probe — no identity node found (DTD 2.30+ schemaIdentifiers nor 'identity' identifier; walk before probe)")
	}

	children := p.directChildrenByIdentifier(node)
	product := pickIdentityValue(children, "product", "name", "model")
	version := pickIdentityValue(children, "version", "softwareversion", "release", "firmwareversion", "firmware")

	parentPath := strings.Join(node.obj.Path, ".")
	if product == "" {
		return "", fmt.Errorf("emberplus: identity probe — product Parameter not found under %s (tried: product, name, model)", parentPath)
	}
	if version == "" {
		return "", fmt.Errorf("emberplus: identity probe — version Parameter not found under %s (tried: version, softwareversion, release, firmwareversion, firmware)", parentPath)
	}

	return fmt.Sprintf("%s@%s", product, version), nil
}

// findIdentityNode locates the identity Node per the layered strategy
// documented on IdentityProbe. Returns nil when no candidate matches
// either layer.
func (p *Plugin) findIdentityNode() *treeEntry {
	// Layer 1: DTD 2.30+ schemaIdentifiers (spec p.87 NodeContents [4]).
	for _, e := range p.numIndex {
		if e.glowNode == nil {
			continue
		}
		if hasIdentitySchema(e.glowNode.SchemaIdentifiers) {
			return e
		}
	}
	// Layer 2: identifier "identity" (case-insensitive). Different
	// vendors choose different casing — TinyEmberPlus uses "identity",
	// DHD uses "Identity", Lawo's modern format uses schemaIdentifiers
	// (caught by Layer 1).
	for _, e := range p.numIndex {
		if e.glowNode == nil {
			continue
		}
		if strings.EqualFold(e.glowNode.Identifier, "identity") {
			return e
		}
	}
	return nil
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
// parent, keyed by lowercased identifier so subsequent lookups are
// case-insensitive. Direct child = entries whose numericPath has the
// parent's numericPath as a strict prefix and is exactly one segment
// longer.
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
			children[strings.ToLower(id)] = e
		}
	}
	return children
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
// candidate child identifiers (case-insensitive). Returns "" when
// none resolves.
func pickIdentityValue(children map[string]*treeEntry, candidates ...string) string {
	for _, name := range candidates {
		if entry, ok := children[strings.ToLower(name)]; ok {
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
