package probel

import (
	"context"
	"fmt"

	"acp/internal/probel-sw08p/codec"
	"acp/internal/protocol"
)

// ProtectTallyDump requests the protect table for one (matrix, level),
// starting at firstDst. Reply is tx 020 Protect Tally Dump — one frame
// for our demo matrix. Large protect tables may be split across frames
// on the wire; this consumer returns the first frame only (caller
// iterates with a fresh firstDst for multi-frame dumps).
//
// Reference: SW-P-08 §3.2 (rx 019) / §3.3 (tx 020). TS rx/019/ + tx/020/.
func (p *Plugin) ProtectTallyDump(
	ctx context.Context,
	matrix, level uint8,
	firstDst uint16,
) (codec.ProtectTallyDumpParams, error) {
	cli, err := p.getClient()
	if err != nil {
		return codec.ProtectTallyDumpParams{}, err
	}
	req := codec.EncodeProtectTallyDumpRequest(codec.ProtectTallyDumpRequestParams{
		MatrixID: matrix, LevelID: level, DestinationID: firstDst,
	})
	reply, err := cli.Send(ctx, req, func(f codec.Frame) bool {
		return f.ID == codec.TxProtectTallyDump || f.ID == codec.TxProtectTallyDumpExt
	})
	if err != nil {
		return codec.ProtectTallyDumpParams{}, fmt.Errorf("probel protect-dump: %w", err)
	}
	d, derr := codec.DecodeProtectTallyDump(reply)
	if derr != nil {
		return codec.ProtectTallyDumpParams{}, &protocol.TransportError{Op: "decode", Err: derr}
	}
	return d, nil
}
