package probel

import (
	"context"
	"fmt"

	iprobel "acp/internal/probel"
	"acp/internal/protocol"
)

// TallyDumpResult carries the decoded reply of a CrosspointTallyDump
// call. Exactly one of Byte / Word is populated; IsWord indicates which.
type TallyDumpResult struct {
	Byte   iprobel.CrosspointTallyDumpByteParams
	Word   iprobel.CrosspointTallyDumpWordParams
	IsWord bool
}

// CrosspointTallyDump asks the matrix to dump the full tally table for
// one (matrix, level). The reply is one frame — tx 022 (byte form) if
// the matrix fits in u8 indices, tx 023 (word form) otherwise. Large
// matrices may require multiple frames on spec; this implementation
// currently waits for a single frame and returns its payload — which
// is sufficient for matrices up to 191 destinations in byte form or
// 64 in word form. For larger dumps the caller should iterate with
// narrower ranges (a future API extension).
//
// Reference: SW-P-08 §3.2 (dump request) / §3.3 (tx 022 byte / tx 023 word).
// TS reference: assets/probel/smh-probelsw08p/src/rx/021/ + tx/022/ + tx/023/.
func (p *Plugin) CrosspointTallyDump(
	ctx context.Context,
	matrix, level uint8,
) (TallyDumpResult, error) {
	cli, err := p.getClient()
	if err != nil {
		return TallyDumpResult{}, err
	}
	req := iprobel.EncodeCrosspointTallyDumpRequest(iprobel.CrosspointTallyDumpRequestParams{
		MatrixID: matrix, LevelID: level,
	})
	reply, err := cli.Send(ctx, req, func(f iprobel.Frame) bool {
		return f.ID == iprobel.TxCrosspointTallyDumpByte ||
			f.ID == iprobel.TxCrosspointTallyDumpWord ||
			f.ID == iprobel.TxCrosspointTallyDumpWordExt
	})
	if err != nil {
		return TallyDumpResult{}, fmt.Errorf("probel tally-dump: %w", err)
	}
	res := TallyDumpResult{}
	if reply.ID == iprobel.TxCrosspointTallyDumpByte {
		b, derr := iprobel.DecodeCrosspointTallyDumpByte(reply)
		if derr != nil {
			return TallyDumpResult{}, &protocol.TransportError{Op: "decode", Err: derr}
		}
		res.Byte = b
	} else {
		w, derr := iprobel.DecodeCrosspointTallyDumpWord(reply)
		if derr != nil {
			return TallyDumpResult{}, &protocol.TransportError{Op: "decode", Err: derr}
		}
		res.Word = w
		res.IsWord = true
	}
	return res, nil
}
