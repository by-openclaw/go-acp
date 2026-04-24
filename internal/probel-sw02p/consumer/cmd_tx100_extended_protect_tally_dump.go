package probelsw02p

import (
	"acp/internal/probel-sw02p/codec"
)

// SubscribeExtendedProtectTallyDump registers fn as a listener for
// tx 100 Extended PROTECT TALLY DUMP (§3.2.64). Matrices emit tx 100
// in reply to EXTENDED PROTECT TALLY DUMP REQUEST (rx 105, §3.2.69);
// on a master-controller reset the Count byte is 127, which decodes
// to ExtendedProtectTallyDumpParams{Reset: true}.
//
// The listener MUST NOT block. Returns ErrNotConnected when called
// before Connect().
func (p *Plugin) SubscribeExtendedProtectTallyDump(fn func(codec.ExtendedProtectTallyDumpParams)) error {
	cli, err := p.getClient()
	if err != nil {
		return err
	}
	cli.Subscribe(func(f codec.Frame) {
		if f.ID != codec.TxExtendedProtectTallyDump {
			return
		}
		params, derr := codec.DecodeExtendedProtectTallyDump(f)
		if derr != nil {
			return
		}
		fn(params)
	})
	return nil
}
