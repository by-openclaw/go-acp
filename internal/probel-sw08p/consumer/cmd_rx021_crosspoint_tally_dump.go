package probelsw08p

import (
	"context"
	"fmt"
	"sort"

	"dhs/internal/consumer"
	"dhs/internal/probel-sw08p/codec"
)

// TallyDumpResult carries the decoded reply of a CrosspointTallyDump
// call. Exactly one of Byte / Word is populated; IsWord indicates which.
type TallyDumpResult struct {
	Byte   codec.CrosspointTallyDumpByteParams
	Word   codec.CrosspointTallyDumpWordParams
	IsWord bool
}

// CrosspointTallyDump asks the matrix to dump the full tally table for one
// (matrix, level) and auto-collects every reply frame. The matrix streams the
// table as tx 022 (byte form, ≤191 dests) or tx 023 / tx 023-ext (word form)
// frames with ascending FirstDestinationID; a 5952-destination matrix spans
// many frames. SendCollect gathers them until the stream goes idle, then they
// are decoded, ordered by FirstDestinationID, and concatenated into one result.
// The merged result is word form if any frame was word or the range exceeds
// byte limits, so callers always see the full dst→src table.
//
// Reference: SW-P-08 §3.2 (dump request) / §3.3 (tx 022 byte / tx 023 word).
func (p *Plugin) CrosspointTallyDump(
	ctx context.Context,
	matrix, level uint8,
) (TallyDumpResult, error) {
	cli, err := p.getClient()
	if err != nil {
		return TallyDumpResult{}, err
	}
	req := codec.EncodeCrosspointTallyDumpRequest(codec.CrosspointTallyDumpRequestParams{
		MatrixID: matrix, LevelID: level,
	})
	match := func(f codec.Frame) bool {
		return f.ID == codec.TxCrosspointTallyDumpByte ||
			f.ID == codec.TxCrosspointTallyDumpWord ||
			f.ID == codec.TxCrosspointTallyDumpWordExt
	}
	frames, err := cli.SendCollect(ctx, req, match, match, 0, tallyDone(p.MatrixConfig().Dsts))
	if err != nil && len(frames) == 0 {
		return TallyDumpResult{}, fmt.Errorf("probel tally-dump: %w", err)
	}

	type seg struct {
		first uint16
		src   []uint16
	}
	var (
		segs     []seg
		anyWord  bool
		mtx, lvl uint8
		haveMeta bool
	)
	for _, f := range frames {
		if f.ID == codec.TxCrosspointTallyDumpByte {
			b, derr := codec.DecodeCrosspointTallyDumpByte(f)
			if derr != nil {
				return TallyDumpResult{}, &consumer.TransportError{Op: "decode", Err: derr}
			}
			src := make([]uint16, len(b.SourceIDs))
			for i, s := range b.SourceIDs {
				src[i] = uint16(s)
			}
			segs = append(segs, seg{first: uint16(b.FirstDestinationID), src: src})
			if !haveMeta {
				mtx, lvl, haveMeta = b.MatrixID, b.LevelID, true
			}
		} else {
			w, derr := codec.DecodeCrosspointTallyDumpWord(f)
			if derr != nil {
				return TallyDumpResult{}, &consumer.TransportError{Op: "decode", Err: derr}
			}
			segs = append(segs, seg{first: w.FirstDestinationID, src: w.SourceIDs})
			anyWord = true
			if !haveMeta {
				mtx, lvl, haveMeta = w.MatrixID, w.LevelID, true
			}
		}
	}
	sort.SliceStable(segs, func(i, j int) bool { return segs[i].first < segs[j].first })

	var (
		all      []uint16
		firstDst uint16
	)
	for i, s := range segs {
		if i == 0 {
			firstDst = s.first
		}
		all = append(all, s.src...)
	}

	res := TallyDumpResult{}
	if anyWord || firstDst > 255 || len(all) > 255 {
		res.IsWord = true
		res.Word = codec.CrosspointTallyDumpWordParams{
			MatrixID: mtx, LevelID: lvl, FirstDestinationID: firstDst, SourceIDs: all,
		}
	} else {
		bsrc := make([]uint8, len(all))
		for i, s := range all {
			bsrc[i] = uint8(s)
		}
		res.Byte = codec.CrosspointTallyDumpByteParams{
			MatrixID: mtx, LevelID: lvl, FirstDestinationID: uint8(firstDst), SourceIDs: bsrc,
		}
	}
	return res, nil
}
