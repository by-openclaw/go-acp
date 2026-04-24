package probelsw02p

import (
	"context"
	"fmt"

	"acp/internal/probel-sw02p/codec"
)

// SendExtendedConnectOnGoGroupSalvo emits rx 71 (§3.2.53) and waits
// for the matching tx 72 ACK (§3.2.54). Use this instead of
// SendConnectOnGoGroupSalvo when dst / src indices exceed 1023 — the
// extended wire form supports up to 16383 on each axis.
//
// No BadSource flag in extended form — the concept exists only in
// the narrow-form §3.2.3 Multiplier.
func (p *Plugin) SendExtendedConnectOnGoGroupSalvo(ctx context.Context, dst, src uint16, salvoID uint8) (codec.ExtendedConnectOnGoGroupSalvoAckParams, error) {
	cli, err := p.getClient()
	if err != nil {
		return codec.ExtendedConnectOnGoGroupSalvoAckParams{}, err
	}
	req := codec.EncodeExtendedConnectOnGoGroupSalvo(codec.ExtendedConnectOnGoGroupSalvoParams{
		Destination: dst,
		Source:      src,
		SalvoID:     salvoID,
	})
	reply, err := cli.Send(ctx, req, func(f codec.Frame) bool {
		return f.ID == codec.TxExtendedConnectOnGoGroupSalvoAck
	})
	if err != nil {
		return codec.ExtendedConnectOnGoGroupSalvoAckParams{}, fmt.Errorf("probel-sw02p: SendExtendedConnectOnGoGroupSalvo: %w", err)
	}
	ack, err := codec.DecodeExtendedConnectOnGoGroupSalvoAck(reply)
	if err != nil {
		return codec.ExtendedConnectOnGoGroupSalvoAckParams{}, fmt.Errorf("probel-sw02p: decode tx 72: %w", err)
	}
	return ack, nil
}
