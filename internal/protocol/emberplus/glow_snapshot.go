package emberplus

import (
	"context"
	"fmt"
	"sort"

	"acp/internal/protocol/emberplus/glow"
)

// GlowEntry is one row of the plugin's flat Glow tree dump.
// Exactly one of Node / Parameter / Matrix / Function is non-nil;
// Kind tells callers which to consult. Meant for lossless capture
// so a replay harness can reconstruct state without re-walking.
type GlowEntry struct {
	NumericPath []int32         `json:"numericPath"`
	StringPath  []string        `json:"stringPath"`
	OID         string          `json:"oid"`
	Kind        string          `json:"kind"` // node | parameter | matrix | function
	Node        *glow.Node      `json:"node,omitempty"`
	Parameter   *glow.Parameter `json:"parameter,omitempty"`
	Matrix      *glow.Matrix    `json:"matrix,omitempty"`
	Function    *glow.Function  `json:"function,omitempty"`
}

// GlowDump is the snapshot envelope. Entries is a slice sorted by
// numeric OID path — NOT a map, because Go marshals map keys as
// strings and "1.1.1.1.10" would sort before "1.1.1.1.2". A slice
// ordered by []int32 comparison gives the natural 1,2,...,10,
// 11,...,100 sequence. Replay code can rebuild a lookup map from
// the slice at load time.
// Templates is a flat slice sorted by OID.
type GlowDump struct {
	Entries   []*GlowEntry     `json:"entries"`
	Templates []*glow.Template `json:"templates"`
}

// lessNumericPath compares two int32 paths lexicographically, giving
// the natural 1,2,10,11,100 ordering humans expect. Empty paths sort
// before non-empty ones.
func lessNumericPath(a, b []int32) bool {
	n := len(a)
	if len(b) < n {
		n = len(b)
	}
	for i := 0; i < n; i++ {
		if a[i] != b[i] {
			return a[i] < b[i]
		}
	}
	return len(a) < len(b)
}

// GlowSnapshot returns the plugin's current Glow tree as a
// lossless dump. Safe to call after Walk(); takes the treeMu
// read lock for the duration of the copy.
//
// Errors: only context cancellation (before the copy starts).
// The snapshot itself cannot fail — it is an in-memory copy of
// already-decoded structs.
func (p *Plugin) GlowSnapshot(ctx context.Context) (*GlowDump, error) {
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("snapshot canceled: %w", err)
	}

	p.treeMu.RLock()
	defer p.treeMu.RUnlock()

	entries := make([]*GlowEntry, 0, len(p.numIndex))
	for oid, e := range p.numIndex {
		ge := &GlowEntry{
			NumericPath: append([]int32(nil), e.numericPath...),
			StringPath:  append([]string(nil), e.obj.Path...),
			OID:         oid,
		}
		switch {
		case e.glowNode != nil:
			ge.Kind = "node"
			ge.Node = e.glowNode
		case e.glowParam != nil:
			ge.Kind = "parameter"
			ge.Parameter = e.glowParam
		case e.glowMatrix != nil:
			ge.Kind = "matrix"
			ge.Matrix = e.glowMatrix
		case e.glowFunc != nil:
			ge.Kind = "function"
			ge.Function = e.glowFunc
		default:
			continue
		}
		entries = append(entries, ge)
	}
	sort.Slice(entries, func(i, j int) bool {
		return lessNumericPath(entries[i].NumericPath, entries[j].NumericPath)
	})

	p.templatesMu.RLock()
	templates := make([]*glow.Template, 0, len(p.templates))
	for _, t := range p.templates {
		templates = append(templates, t)
	}
	p.templatesMu.RUnlock()
	sort.Slice(templates, func(i, j int) bool {
		return numericKey(templates[i].Path) < numericKey(templates[j].Path)
	})

	return &GlowDump{
		Entries:   entries,
		Templates: templates,
	}, nil
}
