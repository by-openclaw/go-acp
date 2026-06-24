package glow

import (
	"bytes"
	"testing"
)

// TestEncodeMatrixGetDirectory_IncludesDirFieldMask pins the protocol fix:
// a GetDirectory on a matrix MUST carry dirFieldMask=All (-1), else a
// spec-compliant provider returns only the element number — no contents
// (identifier / targetCount / labels). The mask encodes as CTX[1] INTEGER
// -1 = BER bytes a1 03 02 01 ff (same as the root GetDirectory, consumer.md).
func TestEncodeMatrixGetDirectory_IncludesDirFieldMask(t *testing.T) {
	data := EncodeMatrixGetDirectory([]int32{0, 5, 2})

	mask := []byte{0xa1, 0x03, 0x02, 0x01, 0xff}
	if !bytes.Contains(data, mask) {
		t.Fatalf("matrix GetDirectory missing dirFieldMask=All (a1 03 02 01 ff):\n%x", data)
	}
	// Parity with the node GetDirectory, which always carried the mask.
	if !bytes.Contains(EncodeGetDirectoryFor([]int32{0, 5, 2}), mask) {
		t.Fatalf("node GetDirectoryFor unexpectedly missing dirFieldMask")
	}
	if _, err := DecodeRoot(data); err != nil {
		t.Fatalf("matrix GetDirectory must still decode: %v", err)
	}
}
