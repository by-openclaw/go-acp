package probel

import (
	"acp/internal/probel-sw08p/codec"
)

// handleAllSourceNames: rx 100 → tx 106.  Builds one tx 106 frame from
// the tree's source labels for (matrix, level), capped at
// NameLength.MaxNamesPerMessage (spec §3.3.19). Missing labels get a
// positional default ("SRC 0001", "SRC 0002", …) so controllers always
// see a stable string for every source.
//
// Multi-frame pagination for > cap names is a future scope item; this
// handler returns only the first frame (caller retries with single-name
// requests for the rest).
//
// Reference: SW-P-08 §3.2.18 (rx 100) → §3.3.19 (tx 106).
func (s *server) handleAllSourceNames(f codec.Frame) (handlerResult, error) {
	p, err := codec.DecodeAllSourceNamesRequest(f)
	if err != nil {
		return handlerResult{}, err
	}
	st, ok := s.tree.lookup(p.MatrixID, p.LevelID)
	if !ok {
		// Unknown matrix/level — return empty response (no names).
		empty := codec.EncodeSourceNamesResponse(codec.SourceNamesResponseParams{
			MatrixID: p.MatrixID, LevelID: p.LevelID, NameLength: p.NameLength,
			FirstSourceID: 0, Names: nil,
		})
		return handlerResult{reply: &empty}, nil
	}
	max := p.NameLength.MaxNamesPerMessage()
	count := st.sourceCount
	if count > max {
		count = max
	}
	names := make([]string, count)
	for i := 0; i < count; i++ {
		names[i] = sourceNameOrDefault(st, i)
	}
	reply := codec.EncodeSourceNamesResponse(codec.SourceNamesResponseParams{
		MatrixID: p.MatrixID, LevelID: p.LevelID, NameLength: p.NameLength,
		FirstSourceID: 0, Names: names,
	})
	return handlerResult{reply: &reply}, nil
}

// sourceNameOrDefault returns the declared label at index i, or a
// positional fallback.
func sourceNameOrDefault(st *matrixState, i int) string {
	if i < len(st.sourceLabels) && st.sourceLabels[i] != "" {
		return st.sourceLabels[i]
	}
	return positionalName("SRC ", i+1)
}

// destNameOrDefault mirrors sourceNameOrDefault for destinations.
func destNameOrDefault(st *matrixState, i int) string {
	if i < len(st.targetLabels) && st.targetLabels[i] != "" {
		return st.targetLabels[i]
	}
	return positionalName("DST ", i+1)
}

// positionalName renders "prefix0001", "prefix0042" etc. — 4-digit
// padded so the strings collate lexically.
func positionalName(prefix string, n int) string {
	// 4-digit decimal, zero-padded.
	buf := []byte(prefix + "0000")
	// Replace the tail 4 chars with the decimal digits of n.
	i := len(buf) - 1
	for d := n; d > 0 && i >= len(prefix); d /= 10 {
		buf[i] = byte('0' + d%10)
		i--
	}
	return string(buf)
}
