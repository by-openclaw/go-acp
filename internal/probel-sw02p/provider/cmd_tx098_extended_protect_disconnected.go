package probelsw02p

import (
	"acp/internal/probel-sw02p/codec"
)

// EmitExtendedProtectDisconnected broadcasts tx 98 Extended PROTECT
// DIS-CONNECTED (§3.2.62) on all connected sessions — the "all ports"
// notification a matrix issues when a destination is unprotected.
// External callers trigger this directly until rx 104 EXTENDED PROTECT
// DIS-CONNECT (§3.2.68) lands from the non-VSM queue.
func (s *server) EmitExtendedProtectDisconnected(p codec.ExtendedProtectDisconnectedParams) {
	f := codec.EncodeExtendedProtectDisconnected(p)
	s.fanOut(codec.Pack(f), f.ID)
}
