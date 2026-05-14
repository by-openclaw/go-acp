package emberplus

import (
	"context"
	"fmt"

	"dhs/internal/protocol"
)

// IdentityProbe returns "<Product>@<Version>" for the connected provider,
// derived from the Ember+ identity subtree.
//
// Ember+ §p.55 (sample providers) and the libember-cpp reference both place
// provider identity under a fixed convention path: router.identity carries
// product (parameter 1), company (parameter 2), version (parameter 3) as
// string parameters. Real Lawo providers (Power Core, Compact Engine, R3LAY)
// follow this layout. TinyEmberPlus' DHD_Example1 + TinyEmberPlusRouter
// also follow it. We never fabricate identity — providers that omit it
// fall back to the IP-keyed cache path (legacy behaviour, preserved).
//
// IdentityProbe satisfies the cross-protocol identityProber interface used
// by cmd_walk.go / cmd_watch.go to key the DM cache at
// .cache/dm/emberplus/<identity>.json per ADR-0022 (card data model).
// Identity format matches ACP1 (CardName@HwVer) and ACP2 (Model@SwRev):
// "<Product>@<Version>", e.g. "Tiny Ember+ Router@1.6.2".
//
// Slot is always 0 for Ember+ — the plugin flattens the Glow tree into one
// logical slot. Any other slot is a usage error.
//
// Pre-condition: a successful Walk must have populated the in-RAM pathIndex.
// IdentityProbe does no wire I/O — it queries the already-decoded identity
// subtree. A future atomic chunk may add a targeted partial GetDirectory
// for first-contact optimisation, but that is out of scope here.
func (p *Plugin) IdentityProbe(ctx context.Context, slot int) (string, error) {
	if slot != 0 {
		return "", fmt.Errorf("emberplus: only slot 0")
	}
	_ = ctx

	p.treeMu.RLock()
	defer p.treeMu.RUnlock()

	productEntry := p.pathIndex["router.identity.product"]
	versionEntry := p.pathIndex["router.identity.version"]

	if productEntry == nil {
		return "", fmt.Errorf("emberplus: identity probe — router.identity.product not found (walk before probe)")
	}
	if versionEntry == nil {
		return "", fmt.Errorf("emberplus: identity probe — router.identity.version not found")
	}

	product := identityStringValue(productEntry)
	version := identityStringValue(versionEntry)

	if product == "" {
		return "", fmt.Errorf("emberplus: identity probe — router.identity.product is empty")
	}
	if version == "" {
		return "", fmt.Errorf("emberplus: identity probe — router.identity.version is empty")
	}

	return fmt.Sprintf("%s@%s", product, version), nil
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
