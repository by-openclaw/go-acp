package probelsw02p

import (
	"context"
	"fmt"

	"acp/internal/probel-sw02p/codec"
)

// SendExtendedProtectDisconnect emits rx 104 Extended PROTECT
// DIS-CONNECT (§3.2.68) and waits for the matching tx 098 Extended
// PROTECT DIS-CONNECTED broadcast (§3.2.62). Per §3.2.62 the matrix
// fires tx 098 on both success AND failure — caller compares the
// returned Protect state against ProtectNone to detect whether the
// unlock actually happened (e.g. owner mismatch or ProbelOverride
// reject will echo the unchanged prior state).
func (p *Plugin) SendExtendedProtectDisconnect(ctx context.Context, dst, device uint16) (codec.ExtendedProtectDisconnectedParams, error) {
	cli, err := p.getClient()
	if err != nil {
		return codec.ExtendedProtectDisconnectedParams{}, err
	}
	req := codec.EncodeExtendedProtectDisconnect(codec.ExtendedProtectDisconnectParams{
		Destination: dst,
		Device:      device,
	})
	reply, err := cli.Send(ctx, req, func(f codec.Frame) bool {
		if f.ID != codec.TxExtendedProtectDisconnected {
			return false
		}
		d, derr := codec.DecodeExtendedProtectDisconnected(f)
		if derr != nil {
			return false
		}
		return d.Destination == dst
	})
	if err != nil {
		return codec.ExtendedProtectDisconnectedParams{}, fmt.Errorf("probel-sw02p: SendExtendedProtectDisconnect: %w", err)
	}
	resp, err := codec.DecodeExtendedProtectDisconnected(reply)
	if err != nil {
		return codec.ExtendedProtectDisconnectedParams{}, fmt.Errorf("probel-sw02p: decode tx 098: %w", err)
	}
	return resp, nil
}
