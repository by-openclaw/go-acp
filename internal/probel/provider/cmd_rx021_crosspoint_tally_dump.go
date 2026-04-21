package probel

import (
	"acp/internal/probel/codec"
)

// handleCrosspointTallyDumpRequest dumps every destination of one
// (matrix, level) pair. The encoder picks between:
//
//   - tx 022 byte form: one frame, one byte per source; used when both
//     dst and src fit in u8 (<=255) and tally count <=191.
//   - tx 023 word form: one frame, two bytes per source; used when
//     either dst or src needs u16.
//
// Split-across-frames for very large matrices (>191 byte-form tallies,
// >64 word-form tallies) is a future scope item — see tracking comment
// on the consumer (internal/protocol/probel/crosspoint_tally_dump.go).
//
// Reference: SW-P-08 §3.2 (rx 021 / rx 0x95) → §3.3 (tx 022 / tx 023 / tx 0x97).
func (s *server) handleCrosspointTallyDumpRequest(f codec.Frame) (handlerResult, error) {
	p, err := codec.DecodeCrosspointTallyDumpRequest(f)
	if err != nil {
		return handlerResult{}, err
	}
	st, ok := s.tree.lookup(p.MatrixID, p.LevelID)
	if !ok {
		// Unknown matrix/level — reply with an empty byte-form dump so
		// the controller isn't left waiting. SW-P-08 has no error reply
		// for tally dump; empty dump = "nothing here".
		empty := codec.EncodeCrosspointTallyDumpByte(codec.CrosspointTallyDumpByteParams{
			MatrixID: p.MatrixID, LevelID: p.LevelID, FirstDestinationID: 0, SourceIDs: nil,
		})
		return handlerResult{reply: &empty}, nil
	}

	// Choose wire form based on source/dest address width.
	needWord := st.targetCount > 255 || st.sourceCount > 255
	if needWord {
		srcs := make([]uint16, st.targetCount)
		for i, v := range st.sources {
			if v < 0 {
				srcs[i] = 0
			} else {
				srcs[i] = uint16(v)
			}
		}
		reply := codec.EncodeCrosspointTallyDumpWord(codec.CrosspointTallyDumpWordParams{
			MatrixID: p.MatrixID, LevelID: p.LevelID,
			FirstDestinationID: 0, SourceIDs: srcs,
		})
		return handlerResult{reply: &reply}, nil
	}
	srcs := make([]uint8, st.targetCount)
	for i, v := range st.sources {
		if v < 0 {
			srcs[i] = 0
		} else {
			srcs[i] = uint8(v)
		}
	}
	reply := codec.EncodeCrosspointTallyDumpByte(codec.CrosspointTallyDumpByteParams{
		MatrixID: p.MatrixID, LevelID: p.LevelID,
		FirstDestinationID: 0, SourceIDs: srcs,
	})
	return handlerResult{reply: &reply}, nil
}
