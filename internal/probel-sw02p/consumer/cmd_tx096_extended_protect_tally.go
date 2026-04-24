package probelsw02p

import (
	"acp/internal/probel-sw02p/codec"
)

// SubscribeExtendedProtectTally registers fn as a listener for tx 96
// Extended PROTECT TALLY (§3.2.60) frames delivered on the current
// session. Matrices emit tx 96 in response to EXTENDED PROTECT
// INTERROGATE (§3.2.65) — until that Rx cmd lands from the non-VSM
// queue this plugin cannot originate one, but the listener still
// fires when a peer volunteers tx 96 as a status broadcast.
//
// The listener MUST NOT block. Returns ErrNotConnected when called
// before Connect().
func (p *Plugin) SubscribeExtendedProtectTally(fn func(codec.ExtendedProtectTallyParams)) error {
	cli, err := p.getClient()
	if err != nil {
		return err
	}
	cli.Subscribe(func(f codec.Frame) {
		if f.ID != codec.TxExtendedProtectTally {
			return
		}
		params, derr := codec.DecodeExtendedProtectTally(f)
		if derr != nil {
			return
		}
		fn(params)
	})
	return nil
}
