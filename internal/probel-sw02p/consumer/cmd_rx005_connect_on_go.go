package probelsw02p

import (
	"context"
	"fmt"

	"acp/internal/probel-sw02p/codec"
)

// SendConnectOnGo emits rx 05 CONNECT ON GO (§3.2.7) and waits for the
// matching tx 12 CONNECT ON GO ACKNOWLEDGE (§3.2.14). One crosspoint is
// staged into the matrix's pending salvo buffer; call SendGo (rx 06)
// to commit or clear. Bilateral by construction — the consumer
// supports sending even though VSM normally acts as the controller.
//
// Returns the decoded ack so the caller can verify the matrix echoed
// the same dst / src. Spec §3.2.14 "bad source bit always 0" is
// validated internally: the decoded ack never carries a BadSource
// flag (the field is absent from ConnectOnGoAckParams).
func (p *Plugin) SendConnectOnGo(ctx context.Context, dst, src uint16, badSource bool) (codec.ConnectOnGoAckParams, error) {
	cli, err := p.getClient()
	if err != nil {
		return codec.ConnectOnGoAckParams{}, err
	}
	req := codec.EncodeConnectOnGo(codec.ConnectOnGoParams{
		Destination: dst,
		Source:      src,
		BadSource:   badSource,
	})
	reply, err := cli.Send(ctx, req, func(f codec.Frame) bool {
		return f.ID == codec.TxConnectOnGoAck
	})
	if err != nil {
		return codec.ConnectOnGoAckParams{}, fmt.Errorf("probel-sw02p: SendConnectOnGo: %w", err)
	}
	ack, err := codec.DecodeConnectOnGoAck(reply)
	if err != nil {
		return codec.ConnectOnGoAckParams{}, fmt.Errorf("probel-sw02p: decode tx 12: %w", err)
	}
	return ack, nil
}
