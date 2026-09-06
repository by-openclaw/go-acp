package cerebrumnb

// Credential redaction for the wire trace.
//
// The session logs every TX/RX document at debug level and records it to the
// --capture JSONL. The very first frame of every session is
//
//	<LOGIN USERNAME="dhs-staging" PASSWORD="hunter2" MTID="1"/>
//
// so an unredacted trace writes the NB password in cleartext into:
//
//   - the local daily log file, which now persists for as long as
//     --log-retention says (by default: forever);
//   - any remote collector configured with --syslog-addr;
//   - the --capture wire trace, which is precisely the artefact an operator
//     attaches to a bug report for the manufacturer.
//
// Redacting at the point of logging — rather than trusting every downstream
// consumer to be careful — is the only version of this that stays true as the
// number of sinks grows.
//
// The redaction preserves the attribute and its length so the trace still
// reads as a well-formed LOGIN and a length-sensitive eye can still follow the
// framing; only the secret itself is replaced.

import "bytes"

// secretAttrs are the XML attributes whose values must never reach a log or a
// capture. Matched case-insensitively, since the decoder accepts any case.
var secretAttrs = [][]byte{
	[]byte("PASSWORD"),
	[]byte("PASSWD"),
	[]byte("TOKEN"),
	[]byte("SECRET"),
}

// redactSecrets returns payload with every secret attribute value replaced by
// asterisks of the same length. The input is never modified.
//
// Returns the original slice when there is nothing to redact, so the common
// case (every frame that is not a LOGIN) costs one scan and no allocation.
func redactSecrets(payload []byte) []byte {
	if !hasSecretAttr(payload) {
		return payload
	}
	out := make([]byte, len(payload))
	copy(out, payload)
	for _, attr := range secretAttrs {
		redactAttrInPlace(out, attr)
	}
	return out
}

// hasSecretAttr reports whether payload mentions any secret attribute at all.
func hasSecretAttr(payload []byte) bool {
	for _, attr := range secretAttrs {
		if indexFoldBytes(payload, attr) >= 0 {
			return true
		}
	}
	return false
}

// redactAttrInPlace overwrites every `attr="..."` value in buf with asterisks.
// buf is modified directly; the value length is preserved so the document
// keeps its shape.
func redactAttrInPlace(buf, attr []byte) {
	from := 0
	for {
		i := indexFoldBytes(buf[from:], attr)
		if i < 0 {
			return
		}
		i += from
		j := i + len(attr)
		// Skip optional whitespace, require '=', skip whitespace, require '"'.
		for j < len(buf) && isSpaceByte(buf[j]) {
			j++
		}
		if j >= len(buf) || buf[j] != '=' {
			from = i + len(attr)
			continue
		}
		j++
		for j < len(buf) && isSpaceByte(buf[j]) {
			j++
		}
		if j >= len(buf) || buf[j] != '"' {
			from = i + len(attr)
			continue
		}
		j++ // first byte of the value
		end := bytes.IndexByte(buf[j:], '"')
		if end < 0 {
			return // malformed; leave the rest alone
		}
		for k := j; k < j+end; k++ {
			buf[k] = '*'
		}
		from = j + end
	}
}

func isSpaceByte(b byte) bool {
	return b == ' ' || b == '\t' || b == '\r' || b == '\n'
}

// indexFoldBytes is a case-insensitive bytes.Index for ASCII needles.
func indexFoldBytes(haystack, needle []byte) int {
	if len(needle) == 0 {
		return 0
	}
	limit := len(haystack) - len(needle)
	for i := 0; i <= limit; i++ {
		match := true
		for j := 0; j < len(needle); j++ {
			if lowerByte(haystack[i+j]) != lowerByte(needle[j]) {
				match = false
				break
			}
		}
		if match {
			return i
		}
	}
	return -1
}

func lowerByte(b byte) byte {
	if b >= 'A' && b <= 'Z' {
		return b + ('a' - 'A')
	}
	return b
}
