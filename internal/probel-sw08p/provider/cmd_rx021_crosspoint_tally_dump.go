package probelsw08p

import (
	"acp/internal/probel-sw08p/codec"
)

// Per-frame tally caps. SW-P-08 soft-caps DATA at 128 bytes (§2), so:
//   - Byte form (tx 022) carries 1 byte per source → ~125 tallies per frame
//     after the 3-byte header. 128 is the conventional cap.
//   - Word form (tx 023) carries 2 bytes per source + 2 bytes per dst
//     header → 64 tallies per frame. 64 is the conventional cap.
// Keeping them tight means controllers that honour only the soft limit
// still parse our output cleanly.
const (
	tallyDumpByteChunk = 128
	tallyDumpWordChunk = 64
)

// handleCrosspointTallyDumpRequest dumps every destination of one
// (matrix, level) pair **streaming, no buffer**. Each output frame
// carries at most tallyDumpByteChunk or tallyDumpWordChunk tallies;
// larger matrices produce multiple frames. Sparse tree reads mean
// memory allocation is O(chunk) per frame, not O(targetCount).
//
// Wire form selection:
//   - tx 022 byte form when both addr spaces fit in u8 (<=255)
//   - tx 023 word form otherwise
//
// For 65535×65535 this emits 65535/64 = 1024 word-form frames, each
// ~133 bytes wire length, roughly 136 KB total — but only one frame
// lives in memory at a time.
//
// Reference: SW-P-08 §3.2 (rx 021 / rx 0x95) → §3.3 (tx 022 / tx 023 / tx 0x97).
func (s *server) handleCrosspointTallyDumpRequest(f codec.Frame) (handlerResult, error) {
	p, err := codec.DecodeCrosspointTallyDumpRequest(f)
	if err != nil {
		return handlerResult{}, err
	}
	st, ok := s.tree.lookup(p.MatrixID, p.LevelID)
	if !ok {
		// Unknown matrix/level — single-frame empty reply so the
		// controller isn't left waiting.
		empty := codec.EncodeCrosspointTallyDumpByte(codec.CrosspointTallyDumpByteParams{
			MatrixID: p.MatrixID, LevelID: p.LevelID, FirstDestinationID: 0, SourceIDs: nil,
		})
		return handlerResult{reply: &empty}, nil
	}

	// Snapshot the per-level state under a short read lock so the stream
	// emitter below doesn't hold tree.mu for the whole dump (which can
	// be seconds at 65535 targets).
	s.tree.mu.RLock()
	targetCount := st.targetCount
	needWord := targetCount > 255 || st.sourceCount > 255
	snapshot := make(map[uint16]uint16, len(st.sources))
	for k, v := range st.sources {
		snapshot[k] = v
	}
	s.tree.mu.RUnlock()

	matrixID, levelID := p.MatrixID, p.LevelID

	if needWord {
		stream := func(emit func(codec.Frame) error) error {
			chunk := make([]uint16, tallyDumpWordChunk)
			for start := 0; start < targetCount; start += tallyDumpWordChunk {
				n := tallyDumpWordChunk
				if start+n > targetCount {
					n = targetCount - start
				}
				// Reuse the chunk buffer — fill + zero where no route.
				for i := 0; i < n; i++ {
					chunk[i] = snapshot[uint16(start+i)]
				}
				frame := codec.EncodeCrosspointTallyDumpWord(codec.CrosspointTallyDumpWordParams{
					MatrixID: matrixID, LevelID: levelID,
					FirstDestinationID: uint16(start),
					SourceIDs:          chunk[:n],
				})
				if err := emit(frame); err != nil {
					return err
				}
			}
			return nil
		}
		return handlerResult{streamToSender: stream}, nil
	}

	// Byte form path.
	stream := func(emit func(codec.Frame) error) error {
		chunk := make([]uint8, tallyDumpByteChunk)
		for start := 0; start < targetCount; start += tallyDumpByteChunk {
			n := tallyDumpByteChunk
			if start+n > targetCount {
				n = targetCount - start
			}
			for i := 0; i < n; i++ {
				chunk[i] = uint8(snapshot[uint16(start+i)])
			}
			frame := codec.EncodeCrosspointTallyDumpByte(codec.CrosspointTallyDumpByteParams{
				MatrixID: matrixID, LevelID: levelID,
				FirstDestinationID: uint8(start),
				SourceIDs:          chunk[:n],
			})
			if err := emit(frame); err != nil {
				return err
			}
		}
		return nil
	}
	return handlerResult{streamToSender: stream}, nil
}
