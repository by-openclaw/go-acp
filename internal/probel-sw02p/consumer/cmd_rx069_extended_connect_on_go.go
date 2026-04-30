package probelsw02p

import (
	"context"
	"fmt"

	"acp/internal/probel-sw02p/codec"
)

// SendExtendedConnectOnGo emits rx 069 Extended CONNECT ON GO
// (§3.2.51) and waits for the matching tx 070 Extended CONNECT ON GO
// ACKNOWLEDGE (§3.2.52). Stages one crosspoint into the matrix's
// unnamed salvo buffer — a subsequent SendGo (rx 006) commits or
// clears it together with any narrow rx 005 slots staged before.
//
// Bilateral by construction — the consumer supports sending even
// though VSM normally acts as the controller.
func (p *Plugin) SendExtendedConnectOnGo(ctx context.Context, dst, src uint16) (codec.ExtendedConnectOnGoAckParams, error) {
	cli, err := p.getClient()
	if err != nil {
		return codec.ExtendedConnectOnGoAckParams{}, err
	}
	req := codec.EncodeExtendedConnectOnGo(codec.ExtendedConnectOnGoParams{
		Destination: dst,
		Source:      src,
	})
	reply, err := cli.Send(ctx, req, func(f codec.Frame) bool {
		return f.ID == codec.TxExtendedConnectOnGoAck
	})
	if err != nil {
		return codec.ExtendedConnectOnGoAckParams{}, fmt.Errorf("probel-sw02p: SendExtendedConnectOnGo: %w", err)
	}
	ack, err := codec.DecodeExtendedConnectOnGoAck(reply)
	if err != nil {
		return codec.ExtendedConnectOnGoAckParams{}, fmt.Errorf("probel-sw02p: decode tx 070: %w", err)
	}
	return ack, nil
}
