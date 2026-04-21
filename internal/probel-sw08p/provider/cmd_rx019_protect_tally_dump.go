package probelsw08p

import (
	"acp/internal/probel-sw08p/codec"
)

// handleProtectTallyDumpRequest dumps the protect table starting at
// FirstDestinationID. For our demo matrix (targetCount ≤ 64) a single
// frame suffices; for wider matrices the handler caps at 64 items per
// frame (SW-P-08 §3.3 tx 020 cap) and returns the first chunk.
// Multi-frame iteration is a future scope item.
//
// Reference: SW-P-08 §3.2 (rx 019 / rx 0x93) → §3.3 (tx 020).
func (s *server) handleProtectTallyDumpRequest(f codec.Frame) (handlerResult, error) {
	p, err := codec.DecodeProtectTallyDumpRequest(f)
	if err != nil {
		return handlerResult{}, err
	}
	st, ok := s.tree.lookup(p.MatrixID, p.LevelID)
	if !ok {
		empty := codec.EncodeProtectTallyDump(codec.ProtectTallyDumpParams{
			MatrixID: p.MatrixID, LevelID: p.LevelID,
			FirstDestinationID: p.DestinationID,
		})
		return handlerResult{reply: &empty}, nil
	}
	start := int(p.DestinationID)
	if start >= st.targetCount {
		empty := codec.EncodeProtectTallyDump(codec.ProtectTallyDumpParams{
			MatrixID: p.MatrixID, LevelID: p.LevelID,
			FirstDestinationID: p.DestinationID,
		})
		return handlerResult{reply: &empty}, nil
	}
	end := start + 64
	if end > st.targetCount {
		end = st.targetCount
	}
	items := make([]codec.ProtectTallyItem, 0, end-start)
	for d := start; d < end; d++ {
		rec := st.protects[uint16(d)]
		items = append(items, codec.ProtectTallyItem{
			State:    codec.ProtectState(rec.state),
			DeviceID: rec.deviceID,
		})
	}
	reply := codec.EncodeProtectTallyDump(codec.ProtectTallyDumpParams{
		MatrixID:           p.MatrixID,
		LevelID:            p.LevelID,
		FirstDestinationID: p.DestinationID,
		Items:              items,
	})
	return handlerResult{reply: &reply}, nil
}
