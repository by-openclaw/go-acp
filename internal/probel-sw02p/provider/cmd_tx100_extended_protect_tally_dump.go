package probelsw02p

import (
	"fmt"

	"acp/internal/probel-sw02p/codec"
)

// EmitExtendedProtectTallyDump broadcasts tx 100 Extended PROTECT
// TALLY DUMP (§3.2.64) on all connected sessions. Callers split dumps
// larger than codec.ExtendedProtectTallyDumpMaxCount entries into
// multiple calls — per §3.2.64 "the message length is limited to 132
// bytes, therefore a message that exceeds this will require more than
// one message". Returns an error and does not broadcast if Entries
// exceeds the per-message maximum so that the spec-enforced cap is
// visible to the caller rather than silently truncated.
func (s *server) EmitExtendedProtectTallyDump(p codec.ExtendedProtectTallyDumpParams) error {
	if !p.Reset && len(p.Entries) > codec.ExtendedProtectTallyDumpMaxCount {
		return fmt.Errorf(
			"probel-sw02p: tally dump entries %d > per-message max %d (§3.2.64); caller must split",
			len(p.Entries), codec.ExtendedProtectTallyDumpMaxCount,
		)
	}
	f := codec.EncodeExtendedProtectTallyDump(p)
	s.fanOut(codec.Pack(f), f.ID)
	return nil
}
