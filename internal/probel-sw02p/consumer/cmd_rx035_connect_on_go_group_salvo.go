package probelsw02p

import (
	"context"
	"fmt"

	"acp/internal/probel-sw02p/codec"
)

// SendConnectOnGoGroupSalvo emits rx 35 (§3.2.36) and waits for the
// matching tx 37 ACK (§3.2.38). Like SendConnectOnGo but the slot is
// filed under a named SalvoID (0-127) so it can be fired independently
// via SendGoGroupSalvo (rx 36).
func (p *Plugin) SendConnectOnGoGroupSalvo(ctx context.Context, dst, src uint16, salvoID uint8, badSource bool) (codec.ConnectOnGoGroupSalvoAckParams, error) {
	cli, err := p.getClient()
	if err != nil {
		return codec.ConnectOnGoGroupSalvoAckParams{}, err
	}
	req := codec.EncodeConnectOnGoGroupSalvo(codec.ConnectOnGoGroupSalvoParams{
		Destination: dst,
		Source:      src,
		BadSource:   badSource,
		SalvoID:     salvoID,
	})
	reply, err := cli.Send(ctx, req, func(f codec.Frame) bool {
		return f.ID == codec.TxConnectOnGoGroupSalvoAck
	})
	if err != nil {
		return codec.ConnectOnGoGroupSalvoAckParams{}, fmt.Errorf("probel-sw02p: SendConnectOnGoGroupSalvo: %w", err)
	}
	ack, err := codec.DecodeConnectOnGoGroupSalvoAck(reply)
	if err != nil {
		return codec.ConnectOnGoGroupSalvoAckParams{}, fmt.Errorf("probel-sw02p: decode tx 37: %w", err)
	}
	return ack, nil
}
