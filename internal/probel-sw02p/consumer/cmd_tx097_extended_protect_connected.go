package probelsw02p

import (
	"acp/internal/probel-sw02p/codec"
)

// SubscribeExtendedProtectConnected registers fn as a listener for
// tx 97 Extended PROTECT CONNECTED (§3.2.61) frames. The matrix
// broadcasts tx 97 on all ports when protect data is altered.
//
// The listener MUST NOT block. Returns ErrNotConnected when called
// before Connect().
func (p *Plugin) SubscribeExtendedProtectConnected(fn func(codec.ExtendedProtectConnectedParams)) error {
	cli, err := p.getClient()
	if err != nil {
		return err
	}
	cli.Subscribe(func(f codec.Frame) {
		if f.ID != codec.TxExtendedProtectConnected {
			return
		}
		params, derr := codec.DecodeExtendedProtectConnected(f)
		if derr != nil {
			return
		}
		fn(params)
	})
	return nil
}
