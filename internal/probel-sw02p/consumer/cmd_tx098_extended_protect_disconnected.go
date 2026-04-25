package probelsw02p

import (
	"acp/internal/probel-sw02p/codec"
)

// SubscribeExtendedProtectDisconnected registers fn as a listener for
// tx 98 Extended PROTECT DIS-CONNECTED (§3.2.62) frames. The matrix
// broadcasts tx 98 on all ports when a destination is unprotected.
//
// The listener MUST NOT block. Returns ErrNotConnected when called
// before Connect().
func (p *Plugin) SubscribeExtendedProtectDisconnected(fn func(codec.ExtendedProtectDisconnectedParams)) error {
	cli, err := p.getClient()
	if err != nil {
		return err
	}
	cli.Subscribe(func(f codec.Frame) {
		if f.ID != codec.TxExtendedProtectDisconnected {
			return
		}
		params, derr := codec.DecodeExtendedProtectDisconnected(f)
		if derr != nil {
			return
		}
		fn(params)
	})
	return nil
}
