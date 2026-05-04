package tsl

import (
	"context"
	"fmt"

	"dhs/internal/export/canonical"
)

// Canonicalize emits a canonical.Export shaped from the TSL display
// vocabulary. TSL UMD is positional tally — there is no walkable tree
// (the consumer's Walk method returns ErrNotImplemented by design;
// see CLAUDE.md "TSL not use all the verb like walk").
//
// Tree shape (initial):
//
//	device (Node, oid="1", identifier=tsl-vXX)
//	└── displays (Node, identifier="displays", children=[])
//
// The "displays" Node is a stable container under which per-INDEX
// Parameter elements get added when the consumer caches V31/V40/V50
// frame state per display address. Empty children initially —
// populated when the per-INDEX state aggregation lands as a
// follow-up (parallel to the probel-sw08p / probel-sw02p
// connection-state aggregation tracker).
//
// Pre-Connect Plugin instances yield the empty-children shape so
// `dhs producer ... validate --out-tree ...` produces a deterministic
// document even on a freshly-instantiated plugin.
func (p *Plugin) Canonicalize(ctx context.Context) (*canonical.Export, error) {
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("canonicalize canceled: %w", err)
	}

	identifier := p.version.name()

	displays := &canonical.Node{
		Header: canonical.Header{
			Number:     1,
			Identifier: "displays",
			Path:       identifier + ".displays",
			OID:        "1.1",
			IsOnline:   true,
			Access:     canonical.AccessRead,
			Children:   canonical.EmptyChildren(),
		},
	}

	root := &canonical.Node{
		Header: canonical.Header{
			Number:     1,
			Identifier: identifier,
			Path:       identifier,
			OID:        "1",
			IsOnline:   true,
			Access:     canonical.AccessRead,
			Children:   []canonical.Element{displays},
		},
	}

	return &canonical.Export{Root: root}, nil
}
