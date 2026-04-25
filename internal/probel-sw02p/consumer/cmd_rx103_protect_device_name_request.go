package probelsw02p

import (
	"context"
	"fmt"

	"acp/internal/probel-sw02p/codec"
)

// SendProtectDeviceNameRequest emits rx 103 PROTECT DEVICE NAME
// REQUEST (§3.2.67) asking the peer for the 8-char ASCII name of
// the given Device Number. Waits for tx 099 PROTECT DEVICE NAME
// RESPONSE (§3.2.63) with a matching Device Number.
//
// §3.2.67 says "Any device issues this message" — a controller asks
// the matrix OR the matrix asks a controller. This helper covers
// the consumer→peer direction; the matrix→consumer direction is
// handled by SubscribeProtectDeviceNameRequest below.
func (p *Plugin) SendProtectDeviceNameRequest(ctx context.Context, device uint16) (codec.ProtectDeviceNameResponseParams, error) {
	cli, err := p.getClient()
	if err != nil {
		return codec.ProtectDeviceNameResponseParams{}, err
	}
	req := codec.EncodeProtectDeviceNameRequest(codec.ProtectDeviceNameRequestParams{Device: device})
	reply, err := cli.Send(ctx, req, func(f codec.Frame) bool {
		if f.ID != codec.TxProtectDeviceNameResponse {
			return false
		}
		r, derr := codec.DecodeProtectDeviceNameResponse(f)
		if derr != nil {
			return false
		}
		return r.Device == device
	})
	if err != nil {
		return codec.ProtectDeviceNameResponseParams{}, fmt.Errorf("probel-sw02p: SendProtectDeviceNameRequest: %w", err)
	}
	resp, err := codec.DecodeProtectDeviceNameResponse(reply)
	if err != nil {
		return codec.ProtectDeviceNameResponseParams{}, fmt.Errorf("probel-sw02p: decode tx 099: %w", err)
	}
	return resp, nil
}

// SubscribeProtectDeviceNameRequest registers fn as a listener for
// inbound rx 103 frames — matrices that issue rx 103 to a
// controller asking "who are you?" per §3.2.67. The callback
// receives the decoded Device Number; the caller is responsible
// for emitting tx 099 in reply (typically via a companion plugin
// method or a server.SetSelfDevice-equivalent on the consumer).
//
// The listener MUST NOT block. Returns ErrNotConnected when called
// before Connect().
func (p *Plugin) SubscribeProtectDeviceNameRequest(fn func(codec.ProtectDeviceNameRequestParams)) error {
	cli, err := p.getClient()
	if err != nil {
		return err
	}
	cli.Subscribe(func(f codec.Frame) {
		if f.ID != codec.RxProtectDeviceNameRequest {
			return
		}
		params, derr := codec.DecodeProtectDeviceNameRequest(f)
		if derr != nil {
			return
		}
		fn(params)
	})
	return nil
}
