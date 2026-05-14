package emberplus

import (
	"context"

	"dhs/internal/export/canonical"
)

// ExportCanonical returns the in-RAM Glow tree serialised as a
// canonical.Export ready for either provider startup (--tree X.json,
// --manifest X.json) or DM-cache persistence (refs #438, ADR-0022).
//
// Default opts:
//   - Labels: "inline" — embeds targetLabels / sourceLabels in each
//     Matrix so the file is self-contained for crosspoint rendering
//     without a second round trip through parameters.connections.t-N.s-M.
//   - Gain: "inline" — embeds connectionParams gain values per
//     crosspoint when parametersLocation + gainParameterNumber are set
//     (chunk 8 enrichment).
//   - Templates: "pointer" — keeps the in-tree TemplateReference
//     unresolved so templates stay deduplicated in the cache.
//
// Wraps Canonicalize. Safe to call concurrently with reads on the tree.
func (p *Plugin) ExportCanonical(ctx context.Context) (*canonical.Export, error) {
	return p.Canonicalize(ctx, CanonicalOptions{
		Labels:    "inline",
		Gain:      "inline",
		Templates: "pointer",
	})
}
