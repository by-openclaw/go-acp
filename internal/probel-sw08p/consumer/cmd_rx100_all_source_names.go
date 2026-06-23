package probelsw08p

import (
	"context"
	"fmt"
	"sort"

	"dhs/internal/consumer"
	"dhs/internal/probel-sw08p/codec"
)

// AllSourceNames asks the matrix for every source name on one
// (matrix, level) and auto-collects the full table.
//
// The spec (§3.3.19) caps one tx 106 frame at 32×4-char / 16×8-char /
// 10×12-char names; larger tables (a 5952-source matrix needs hundreds of
// frames) stream as multiple tx 106 frames with ascending FirstSourceID.
// SendCollect gathers every frame until the stream goes idle, so this returns
// the whole table — and any attached --capture recorder logs all of it. The
// per-frame results are decoded, ordered by FirstSourceID, and concatenated.
//
// Reference: SW-P-08 §3.2.18 (request) / §3.3.19 (response).
func (p *Plugin) AllSourceNames(
	ctx context.Context,
	matrix, level uint8,
	nameLen codec.NameLength,
) (codec.SourceNamesResponseParams, error) {
	cli, err := p.getClient()
	if err != nil {
		return codec.SourceNamesResponseParams{}, err
	}
	req := codec.EncodeAllSourceNamesRequest(codec.AllSourceNamesRequestParams{
		MatrixID: matrix, LevelID: level, NameLength: nameLen,
	})
	match := func(f codec.Frame) bool { return f.ID == codec.TxSourceNamesResponse }
	// When the operator supplied the source count (--srcs), stop deterministically
	// once that many names are in; otherwise SendCollect falls back to its idle
	// gap. SW-P-08 has no on-wire size primitive, so this is the only exact stop.
	frames, err := cli.SendCollect(ctx, req, match, match, 0, sourceNamesDone(p.MatrixConfig().Srcs))
	if err != nil && len(frames) == 0 {
		return codec.SourceNamesResponseParams{}, fmt.Errorf("probel all-source-names: %w", err)
	}

	parts := make([]codec.SourceNamesResponseParams, 0, len(frames))
	for _, f := range frames {
		r, derr := codec.DecodeSourceNamesResponse(f)
		if derr != nil {
			return codec.SourceNamesResponseParams{}, &consumer.TransportError{Op: "decode", Err: derr}
		}
		parts = append(parts, r)
	}
	sort.SliceStable(parts, func(i, j int) bool {
		return parts[i].FirstSourceID < parts[j].FirstSourceID
	})

	var merged codec.SourceNamesResponseParams
	for i, r := range parts {
		if i == 0 {
			merged = r
			continue
		}
		merged.Names = append(merged.Names, r.Names...)
	}
	return merged, nil
}
