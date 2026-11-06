package export

import "bytes"

// crMark stands in for a carriage return inside a quoted field while
// encoding/csv reads a file: the reader drops a \r before a newline even
// inside quotes, and an SDP's lines end in \r\n (RFC 8866 §5). Read back
// without it, an import would write every SDP with other bytes than the
// device holds (measured 2026-11-06 on the SHUFFLE receivers). U+E000 is
// a private-use character no export writes; a file holding one is read
// as before.
const crMark = ""

// hideQuotedCR swaps each \r inside a quoted field for crMark. A \r\n that
// ends a record (a file saved by a spreadsheet) is outside quotes and
// stays as it is.
func hideQuotedCR(data []byte) ([]byte, bool) {
	if bytes.IndexByte(data, '\r') < 0 || bytes.Contains(data, []byte(crMark)) {
		return data, false
	}
	out := make([]byte, 0, len(data)+len(data)/64)
	quoted, kept := false, false
	for _, b := range data {
		switch {
		case b == '"':
			quoted = !quoted // a "" inside a field toggles twice
		case b == '\r' && quoted:
			out = append(out, crMark...)
			kept = true
			continue
		}
		out = append(out, b)
	}
	return out, kept
}
