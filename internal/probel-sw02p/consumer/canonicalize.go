package probelsw02p

import (
	"context"
	"fmt"

	"dhs/internal/export/canonical"
)

// Canonicalize emits a canonical.Export shaped from the externally-
// supplied MatrixConfig (per SetMatrixConfig). SW-P-02 is single-
// matrix single-level per spec §3.1 so the projection is uniform —
// no per-level multiplexing.
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
// state from tx 003 / tx 004 / tx 067 / tx 068 tally traffic into a
// cached matrix-state map (separate follow-up tracker).
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
			IsOnline:   true,
			Access:     canonical.AccessRead,
		},
	}

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
			IsOnline:   true,
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
