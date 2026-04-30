package probelsw02p

import (
	"acp/internal/probel-sw02p/codec"
)

// handleExtendedProtectTallyDumpRequest processes rx 105 Extended
// PROTECT TALLY DUMP REQUEST (§3.2.69). Router asks for Count
// protect entries starting at StartDestination; controller /
// provider replies with one or more tx 100 EXTENDED PROTECT TALLY
// DUMP broadcasts (§3.2.64).
//
// §3.2.64 caps each tx 100 at 32 entries (Extended
// ProtectTallyDumpMaxCount), so counts above 32 split into
// multiple broadcast frames — all emitted on the same handler
// invocation, in ascending-destination order.
//
// When the stored state has fewer matching entries than Count, the
// handler emits whatever is available — the §3.2.69 request is
// "up to" Count, not "exactly". An empty stored state replies with
// a single tx 100 carrying Count=0 so peers observe a well-formed
// "no protections" response.
func (s *server) handleExtendedProtectTallyDumpRequest(f codec.Frame) (handlerResult, error) {
	p, err := codec.DecodeExtendedProtectTallyDumpRequest(f)
	if err != nil {
		return handlerResult{}, err
	}
	all := s.tree.protectDump(p.StartDestination, int(p.Count))

	// Split into chunks of at most codec.ExtendedProtectTallyDumpMaxCount.
	maxPerMsg := codec.ExtendedProtectTallyDumpMaxCount
	if len(all) == 0 {
		// Spec allows Count=0 as a valid "nothing to dump" reply.
		empty := codec.EncodeExtendedProtectTallyDump(codec.ExtendedProtectTallyDumpParams{})
		return handlerResult{broadcast: []codec.Frame{empty}}, nil
	}
	broadcast := make([]codec.Frame, 0, (len(all)+maxPerMsg-1)/maxPerMsg)
	for off := 0; off < len(all); off += maxPerMsg {
		end := off + maxPerMsg
		if end > len(all) {
			end = len(all)
		}
		entries := make([]codec.ExtendedProtectTallyDumpEntry, 0, end-off)
		for _, e := range all[off:end] {
			entries = append(entries, codec.ExtendedProtectTallyDumpEntry{
				Destination: e.Destination,
				Device:      e.Entry.OwnerDevice,
				Protect:     e.Entry.State,
			})
		}
		frame := codec.EncodeExtendedProtectTallyDump(codec.ExtendedProtectTallyDumpParams{
			Entries: entries,
		})
		broadcast = append(broadcast, frame)
	}
	return handlerResult{broadcast: broadcast}, nil
}
