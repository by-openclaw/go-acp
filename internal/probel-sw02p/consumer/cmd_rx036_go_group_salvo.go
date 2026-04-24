package probelsw02p

import (
	"context"
	"fmt"

	"acp/internal/probel-sw02p/codec"
)

// SendGoGroupSalvo emits rx 36 (§3.2.37) and waits for tx 38 GO DONE
// GROUP SALVO ACKNOWLEDGE (§3.2.39) carrying the matching SalvoID.
// The matrix drains its SalvoID-keyed pending buffer on op=Set
// (applying every slot + broadcasting per-slot tx 04 CONNECTED) or
// discards the group on op=Clear. Result byte distinguishes
// set / cleared / empty-group.
//
// Callers can filter the ack by SalvoID (the matcher below accepts any
// tx 38 frame; callers that pipeline multiple groups simultaneously
// should match on SalvoID explicitly in their own Subscribe path).
func (p *Plugin) SendGoGroupSalvo(ctx context.Context, op codec.GoOperation, salvoID uint8) (codec.GoDoneGroupSalvoAckParams, error) {
	cli, err := p.getClient()
	if err != nil {
		return codec.GoDoneGroupSalvoAckParams{}, err
	}
	req := codec.EncodeGoGroupSalvo(codec.GoGroupSalvoParams{
		Operation: op, SalvoID: salvoID,
	})
	reply, err := cli.Send(ctx, req, func(f codec.Frame) bool {
		if f.ID != codec.TxGoDoneGroupSalvoAck {
			return false
		}
		// Filter by SalvoID so concurrent group fires don't cross-match.
		got, derr := codec.DecodeGoDoneGroupSalvoAck(f)
		return derr == nil && got.SalvoID == salvoID
	})
	if err != nil {
		return codec.GoDoneGroupSalvoAckParams{}, fmt.Errorf("probel-sw02p: SendGoGroupSalvo: %w", err)
	}
	ack, err := codec.DecodeGoDoneGroupSalvoAck(reply)
	if err != nil {
		return codec.GoDoneGroupSalvoAckParams{}, fmt.Errorf("probel-sw02p: decode tx 38: %w", err)
	}
	return ack, nil
}
