package emberplus

import (
	"context"
	"fmt"
	"strings"

	"dhs/internal/protocol"
)

// IdentityProbe returns "<Product>@<Version>" for the connected provider.
//
// Ember+ does NOT mandate a fixed identity location in the spec. This probe
// uses a layered detection strategy, in order:
//
//  1. DTD 2.30+ schemaIdentifiers (spec p.87 NodeContents [4]):
//     find any Node whose schemaIdentifiers list contains an entry ending
//     in ".identity". Modern providers (Lawo "de.l-s-b.emberplus.identity",
//     Riedel, DHD) follow this mechanism.
//
//  2. Identifier convention (used by DTD < 2.30 providers where
//     schemaIdentifiers does not exist): find any Node whose own
//     identifier is "identity". This is the libember-cpp / TinyEmberPlus
//     sample convention.
//
// Inside the located Node, two well-known Parameter identifiers are tried
// in order; first non-empty value wins:
//
//   - product: {product, name, model}
//   - version: {version, softwareVersion, release}
//
// Identity format matches ACP1 (CardName@HwVer) and ACP2 (Model@SwRev):
// "<Product>@<Version>", e.g. "Power Core@1.21".
//
// Slot is always 0 for Ember+ — the plugin flattens the Glow tree into one
// logical slot. Any other slot is a usage error.
//
// Pre-condition: a successful Walk must have populated the in-RAM indexes.
// IdentityProbe does no wire I/O.
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

	parentPath := strings.Join(node.obj.Path, ".")
	product := p.lookupChildString(parentPath, "product", "name", "model")
	version := p.lookupChildString(parentPath, "version", "softwareVersion", "release")

	if product == "" {
		return "", fmt.Errorf("emberplus: identity probe — product Parameter not found under %s (tried: product, name, model)", parentPath)
	}
	if version == "" {
		return "", fmt.Errorf("emberplus: identity probe — version Parameter not found under %s (tried: version, softwareVersion, release)", parentPath)
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
	// Layer 2: identifier convention. labelIndex maps bare identifier →
	// entries. DTD < 2.30 providers (TinyEmberPlus, libember-cpp sample)
	// do not emit schemaIdentifiers and rely on this convention.
	for _, e := range p.labelIndex["identity"] {
		if e.glowNode != nil {
			return e
		}
	}
	return nil
}

// hasIdentitySchema returns true when the newline-separated
// schemaIdentifiers list (spec p.87 NodeContents [4]) contains any entry
// ending in ".identity".
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

// lookupChildString resolves the first non-empty string value among
// candidate child identifiers under parentPath. Returns "" when none
// resolves.
func (p *Plugin) lookupChildString(parentPath string, candidates ...string) string {
	for _, name := range candidates {
		key := parentPath + "." + name
		if entry, ok := p.pathIndex[key]; ok {
			if v := identityStringValue(entry); v != "" {
				return v
			}
		}
	}
	return ""
}

// identityStringValue extracts the textual value of an identity-subtree
// entry. Product/version are KindString on every real provider observed;
// a numeric version (rare) is rendered via fmt.Sprintf as a fallback so
// the cache key stays stable.
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
