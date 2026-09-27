package probelsw08p

import (
	"context"
	"errors"
	"fmt"

	"dhs/internal/consumer"
	"dhs/internal/probel-sw08p/codec"
)

// CrosspointInterrogate queries the current source routed to one
// destination on one (matrix, level). Encodes rx 001 (general) or
// rx 0x81 (extended) automatically; matches both tx 003 / tx 0x83 on
// reply.
//
// A matrix known to have more than 1024 sources is asked in the extended
// form. The reply may come back in either form and both are accepted; the
// request form matters because the EVS Neuron answers a general interrogate
// only when the routed source is 1023 or below, and otherwise sends no tally
// at all (see CLAUDE.md "Known deviations from spec").
//
// Reference: SW-P-08 §3.2 (interrogate) / §3.3 (tally reply).
func (p *Plugin) CrosspointInterrogate(
	ctx context.Context,
	matrix, level uint8,
	dst uint16,
) (codec.CrosspointTallyParams, error) {
	cli, err := p.getClient()
	if err != nil {
		return codec.CrosspointTallyParams{}, err
	}
	params := codec.CrosspointInterrogateParams{
		MatrixID: matrix, LevelID: level, DestinationID: dst,
		Extended: p.sourcesExceedGeneralForm(),
	}
	req := codec.EncodeCrosspointInterrogate(params)
	reply, err := cli.Send(ctx, req, func(f codec.Frame) bool {
		return f.ID == codec.TxCrosspointTally || f.ID == codec.TxCrosspointTallyExt
	})
	if err != nil {
		if req.ID == codec.RxCrosspointInterrogate && errors.Is(err, context.DeadlineExceeded) {
			return codec.CrosspointTallyParams{}, fmt.Errorf("probel interrogate: the router acknowledged but sent no tally. "+
				"Some routers (the EVS Neuron) do not answer a general-form interrogate when the routed source is above %d; "+
				"give the source count with --srcs N so dhs asks in the extended form: %w",
				generalFormSources-1, err)
		}
		return codec.CrosspointTallyParams{}, fmt.Errorf("probel interrogate: %w", err)
	}
	t, derr := codec.DecodeCrosspointTally(reply)
	if derr != nil {
		return codec.CrosspointTallyParams{}, &consumer.TransportError{Op: "decode", Err: derr}
	}
	return t, nil
}
