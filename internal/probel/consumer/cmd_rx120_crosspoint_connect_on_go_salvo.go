package probel

import (
	"context"
	"fmt"

	"acp/internal/probel/codec"
	"acp/internal/protocol"
)

// SalvoConnectOnGo appends one crosspoint to a salvo group. Repeat for
// every route in the salvo. The matrix stores the crosspoint until
// SalvoGo (op=set) applies it, or SalvoGo (op=clear) discards it.
// Reply is tx 122 Connect-On-Go Acknowledge.
//
// Spec caveat: per SW-P-08 §3.2.29 group-salvo commands are only
// implemented on the XD and ECLIPSE router ranges. Other matrices
// may NAK this command.
//
// Reference: SW-P-08 §3.2.29 / §3.3.24.
func (p *Plugin) SalvoConnectOnGo(
	ctx context.Context,
	params codec.SalvoConnectOnGoParams,
) (codec.SalvoConnectOnGoAckParams, error) {
	cli, err := p.getClient()
	if err != nil {
		return codec.SalvoConnectOnGoAckParams{}, err
	}
	req := codec.EncodeSalvoConnectOnGo(params)
	reply, err := cli.Send(ctx, req, func(f codec.Frame) bool {
		return f.ID == codec.TxSalvoConnectOnGoAck
	})
	if err != nil {
		return codec.SalvoConnectOnGoAckParams{}, fmt.Errorf("probel salvo-connect-on-go: %w", err)
	}
	ack, derr := codec.DecodeSalvoConnectOnGoAck(reply)
	if derr != nil {
		return codec.SalvoConnectOnGoAckParams{}, &protocol.TransportError{Op: "decode", Err: derr}
	}
	return ack, nil
}
