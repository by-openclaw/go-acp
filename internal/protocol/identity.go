package protocol

import (
	"context"
	"errors"
)

// CardIdentity is the protocol-agnostic fingerprint that drives DM-library
// lookup. Plugins fill this from a deterministic identity probe (ACP1
// fixed-pid getObject at group=1) or a runtime alias-scan (ACP2 label
// matching). Vendor strings are returned raw from the wire; persistence
// callers run them through internal/identity sanitisers before disk write.
type CardIdentity struct {
	Model string // hard-required for DM-library lookup
	SwRev string // strict lookup; partial-match fallback when empty
	HwRev string // metadata only — not in lookup key today
}

// IsZero reports whether the identity carries no useful fields.
func (c CardIdentity) IsZero() bool {
	return c.Model == "" && c.SwRev == "" && c.HwRev == ""
}

// Identifier is the optional contract a Protocol implementation may
// satisfy to expose per-slot identity information. Cross-protocol
// callers (DM-library seed flow, watch hot-plug enrichment) type-assert
// to this interface.
type Identifier interface {
	GetIdentity(ctx context.Context, slot int) (CardIdentity, error)
}

// ErrIdentityUnresolved is returned by GetIdentity when the device did
// not provide enough information to fingerprint the card. Plugins fire
// a per-protocol compliance event before returning this error so the
// session profile reflects the failure.
var ErrIdentityUnresolved = errors.New("protocol: card identity unresolved")
