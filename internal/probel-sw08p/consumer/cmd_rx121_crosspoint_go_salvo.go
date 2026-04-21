package probelsw08p

import (
	"context"
	"fmt"

	"acp/internal/probel-sw08p/codec"
	"acp/internal/protocol"
)

// SalvoGo fires a salvo group: op=SalvoOpSet applies the stored routes,
// op=SalvoOpClear discards them without applying. Matrix replies tx 123
// Go-Done Acknowledge carrying the final status.
//
// Reference: SW-P-08 §3.2.30 / §3.3.25.
func (p *Plugin) SalvoGo(
	ctx context.Context,
	params codec.SalvoGoParams,
) (codec.SalvoGoDoneAckParams, error) {
	cli, err := p.getClient()
	if err != nil {
		return codec.SalvoGoDoneAckParams{}, err
	}
	req := codec.EncodeSalvoGo(params)
	reply, err := cli.Send(ctx, req, func(f codec.Frame) bool {
		return f.ID == codec.TxSalvoGoDoneAck
	})
	if err != nil {
		return codec.SalvoGoDoneAckParams{}, fmt.Errorf("probel salvo-go: %w", err)
	}
	ack, derr := codec.DecodeSalvoGoDoneAck(reply)
	if derr != nil {
		return codec.SalvoGoDoneAckParams{}, &protocol.TransportError{Op: "decode", Err: derr}
	}
	return ack, nil
}
