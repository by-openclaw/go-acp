package probelsw02p

import (
	"acp/internal/probel-sw02p/codec"
)

// EmitExtendedProtectConnected broadcasts tx 97 Extended PROTECT
// CONNECTED (§3.2.61) on all connected sessions — the "all ports"
// notification a matrix issues when protect data is altered. External
// callers trigger this directly until rx 102 EXTENDED PROTECT CONNECT
// (§3.2.66) lands from the non-VSM queue.
func (s *server) EmitExtendedProtectConnected(p codec.ExtendedProtectConnectedParams) {
	f := codec.EncodeExtendedProtectConnected(p)
	s.fanOut(codec.Pack(f), f.ID)
}
