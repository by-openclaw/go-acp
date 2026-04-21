package probel

import (
	"context"
	"fmt"

	iprobel "acp/internal/probel"
	"acp/internal/protocol"
)

// SalvoGo fires a salvo group: op=SalvoOpSet applies the stored routes,
// op=SalvoOpClear discards them without applying. Matrix replies tx 123
// Go-Done Acknowledge carrying the final status.
//
// Reference: SW-P-08 §3.2.30 / §3.3.25.
func (p *Plugin) SalvoGo(
	ctx context.Context,
	params iprobel.SalvoGoParams,
) (iprobel.SalvoGoDoneAckParams, error) {
	cli, err := p.getClient()
	if err != nil {
		return iprobel.SalvoGoDoneAckParams{}, err
	}
	req := iprobel.EncodeSalvoGo(params)
	reply, err := cli.Send(ctx, req, func(f iprobel.Frame) bool {
		return f.ID == iprobel.TxSalvoGoDoneAck
	})
	if err != nil {
		return iprobel.SalvoGoDoneAckParams{}, fmt.Errorf("probel salvo-go: %w", err)
	}
	ack, derr := iprobel.DecodeSalvoGoDoneAck(reply)
	if derr != nil {
		return iprobel.SalvoGoDoneAckParams{}, &protocol.TransportError{Op: "decode", Err: derr}
	}
	return ack, nil
}
