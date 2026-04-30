package probelsw02p

import (
	"acp/internal/probel-sw02p/codec"
)

// EmitExtendedProtectTally broadcasts tx 96 Extended PROTECT TALLY
// (§3.2.60) on all connected sessions. The provider emits this in
// reply to an EXTENDED PROTECT INTERROGATE (rx 101, §3.2.65) when
// that command lands from the non-VSM queue — until then, external
// callers (REST API, CLI helpers, tests) can trigger it directly via
// this method to exercise the wire path.
func (s *server) EmitExtendedProtectTally(p codec.ExtendedProtectTallyParams) {
	f := codec.EncodeExtendedProtectTally(p)
	s.fanOut(codec.Pack(f), f.ID)
}
