package probelsw08p

import (
	"context"
	"fmt"

	"dhs/internal/export/canonical"
)

// Canonicalize emits a canonical.Export shaped from the externally-
// supplied MatrixConfig (per SetMatrixConfig). SW-P-08 has no on-the-
// wire matrix-discovery primitive, so the consumer relies on the
// caller declaring matrix shape up front; this function projects that
// declaration into the cross-protocol Matrix element shape per
// docs/protocols/elements/matrix.md.
//
// Tree shape (initial):
//
//	device (Node, oid="1", identifier=host)
//	└── matrix-<id>.level-<lvl> (Matrix, type=oneToN, mode=linear,
//	                              targetCount=Dsts, sourceCount=Srcs,
//	                              targets=[], sources=[], connections=[])
//
// Targets / Sources / Connections are empty in this initial pass —
// they get populated when the consumer aggregates per-crosspoint
// state from cmd 003 / cmd 022 / cmd 023 tally traffic into a cached
// matrix-state map (separate follow-up tracker per
// internal/probel-sw08p/CLAUDE.md "Matrix-centric RX/TX convention").
//
// Pre-Connect Plugin instances (no host yet, MatrixConfig still
// zero-value) yield a device Node with an empty children slice.
func (p *Plugin) Canonicalize(ctx context.Context) (*canonical.Export, error) {
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("canonicalize canceled: %w", err)
	}

	p.mu.Lock()
	host := p.host
	cfg := p.matrixCfg
	p.mu.Unlock()

	identifier := "device"
	if host != "" {
		identifier = host
	}

	root := &canonical.Node{
		Header: canonical.Header{
			Number:     1,
			Identifier: identifier,
			Path:       identifier,
			OID:        "1",
			IsOnline:   p.IsOnline(),
			Access:     canonical.AccessRead,
		},
	}

	// Empty MatrixConfig (no SetMatrixConfig call yet) → empty children.
	if cfg.Dsts == 0 && cfg.Srcs == 0 {
		root.Children = canonical.EmptyChildren()
		return &canonical.Export{Root: root}, nil
	}

	matrixIdent := fmt.Sprintf("matrix-%d.level-%d", cfg.MatrixID, cfg.Level)
	matrixOID := fmt.Sprintf("1.%d.%d", cfg.MatrixID+1, cfg.Level+1)

	matrix := &canonical.Matrix{
		Header: canonical.Header{
			Number:     int(cfg.MatrixID)*100 + int(cfg.Level) + 1,
			Identifier: matrixIdent,
			Path:       identifier + "." + matrixIdent,
			OID:        matrixOID,
			IsOnline:   p.IsOnline(),
			Access:     canonical.AccessReadWrite,
		},
		Type:        canonical.MatrixOneToN,
		Mode:        canonical.ModeLinear,
		TargetCount: int64(cfg.Dsts),
		SourceCount: int64(cfg.Srcs),
		Targets:     []canonical.MatrixTarget{},
		Sources:     []canonical.MatrixSource{},
		Connections: []canonical.MatrixConnection{},
	}

	root.Children = []canonical.Element{matrix}
	return &canonical.Export{Root: root}, nil
}
