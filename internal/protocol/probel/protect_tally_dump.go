package probel

import (
	"context"
	"fmt"

	iprobel "acp/internal/probel"
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
) (iprobel.ProtectTallyDumpParams, error) {
	cli, err := p.getClient()
	if err != nil {
		return iprobel.ProtectTallyDumpParams{}, err
	}
	req := iprobel.EncodeProtectTallyDumpRequest(iprobel.ProtectTallyDumpRequestParams{
		MatrixID: matrix, LevelID: level, DestinationID: firstDst,
	})
	reply, err := cli.Send(ctx, req, func(f iprobel.Frame) bool {
		return f.ID == iprobel.TxProtectTallyDump || f.ID == iprobel.TxProtectTallyDumpExt
	})
	if err != nil {
		return iprobel.ProtectTallyDumpParams{}, fmt.Errorf("probel protect-dump: %w", err)
	}
	d, derr := iprobel.DecodeProtectTallyDump(reply)
	if derr != nil {
		return iprobel.ProtectTallyDumpParams{}, &protocol.TransportError{Op: "decode", Err: derr}
	}
	return d, nil
}
