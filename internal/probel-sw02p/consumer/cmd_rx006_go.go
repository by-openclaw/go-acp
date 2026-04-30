package probelsw02p

import (
	"context"
	"fmt"

	"acp/internal/probel-sw02p/codec"
)

// SendGo emits rx 06 GO (§3.2.8) and waits for the matching tx 13 GO
// DONE ACKNOWLEDGE (§3.2.15). The matrix drains its pending salvo
// buffer on op=Set (applying every staged crosspoint + broadcasting
// a per-slot tx 04 CONNECTED) or discards it on op=Clear.
//
// The ack is broadcast on all ports per §3.2.15, so every controller
// connected to the matrix sees the same "go done" result. Tally
// updates from the set path arrive as unsolicited tx 04 frames —
// callers that want a live crosspoint mirror should subscribe via
// codec.Client.Subscribe before invoking SendGo.
func (p *Plugin) SendGo(ctx context.Context, op codec.GoOperation) (codec.GoDoneAckParams, error) {
	cli, err := p.getClient()
	if err != nil {
		return codec.GoDoneAckParams{}, err
	}
	req := codec.EncodeGo(codec.GoParams{Operation: op})
	reply, err := cli.Send(ctx, req, func(f codec.Frame) bool {
		return f.ID == codec.TxGoDoneAck
	})
	if err != nil {
		return codec.GoDoneAckParams{}, fmt.Errorf("probel-sw02p: SendGo: %w", err)
	}
	ack, err := codec.DecodeGoDoneAck(reply)
	if err != nil {
		return codec.GoDoneAckParams{}, fmt.Errorf("probel-sw02p: decode tx 13: %w", err)
	}
	return ack, nil
}
