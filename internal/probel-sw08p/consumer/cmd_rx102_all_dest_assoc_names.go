package probelsw08p

import (
	"context"
	"fmt"
	"sort"

	"dhs/internal/consumer"
	"dhs/internal/probel-sw08p/codec"
)

// AllDestAssocNames asks the matrix for every destination-association name on
// one matrix and auto-collects the full table (multiple tx 107 frames with
// ascending FirstDestAssociationID stream for large matrices). See
// AllSourceNames; merge logic is identical, keyed on FirstDestAssociationID.
//
// Reference: SW-P-08 §3.2.20 / §3.3.20.
func (p *Plugin) AllDestAssocNames(
	ctx context.Context,
	matrix uint8,
	nameLen codec.NameLength,
) (codec.DestAssocNamesResponseParams, error) {
	cli, err := p.getClient()
	if err != nil {
		return codec.DestAssocNamesResponseParams{}, err
	}
	req := codec.EncodeAllDestAssocNamesRequest(codec.AllDestAssocNamesRequestParams{
		MatrixID: matrix, NameLength: nameLen,
	})
	match := func(f codec.Frame) bool { return f.ID == codec.TxDestAssocNamesResponse }
	frames, err := cli.SendCollect(ctx, req, match, match, 0, destNamesDone(p.MatrixConfig().Dsts))
	if err != nil && len(frames) == 0 {
		return codec.DestAssocNamesResponseParams{}, fmt.Errorf("probel all-dest-assoc-names: %w", err)
	}

	parts := make([]codec.DestAssocNamesResponseParams, 0, len(frames))
	for _, f := range frames {
		r, derr := codec.DecodeDestAssocNamesResponse(f)
		if derr != nil {
			return codec.DestAssocNamesResponseParams{}, &consumer.TransportError{Op: "decode", Err: derr}
		}
		parts = append(parts, r)
	}
	sort.SliceStable(parts, func(i, j int) bool {
		return parts[i].FirstDestAssociationID < parts[j].FirstDestAssociationID
	})

	var merged codec.DestAssocNamesResponseParams
	for i, r := range parts {
		if i == 0 {
			merged = r
			continue
		}
		merged.Names = append(merged.Names, r.Names...)
	}
	return merged, nil
}
