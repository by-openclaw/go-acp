package emberplus

import (
	"context"

	"dhs/internal/export/canonical"
)

// ExportCanonical returns the in-RAM Glow tree serialised as a
// canonical.Export ready for either provider startup (--tree X.json,
// --manifest X.json) or DM-cache persistence (refs #438, ADR-0022).
//
// Default opts ("both" for labels and gain) emit the DM file in the
// shape useful to BOTH a consumer and a provider:
//
//   - Labels: "both"  — matrix carries inline targetLabels /
//     sourceLabels (rendering convenience) AND keeps the underlying
//     Glow Label-Parameter subtree (so a consumer can re-walk or
//     subscribe to individual labels by OID).
//   - Gain: "both"    — matrix carries inline targetParams /
//     sourceParams / connectionParams (per-target / per-source /
//     per-crosspoint resolved values) AND keeps the underlying Glow
//     Parameter nodes (gain / mode / other per-signal extras).
//   - Templates: "pointer" — keeps templateReference unresolved so
//     templates stay deduplicated in the cache.
//
// Wraps Canonicalize. Safe to call concurrently with reads on the tree.
func (p *Plugin) ExportCanonical(ctx context.Context) (*canonical.Export, error) {
	return p.Canonicalize(ctx, CanonicalOptions{
		Labels:    "both",
		Gain:      "both",
		Templates: "pointer",
	})
}
