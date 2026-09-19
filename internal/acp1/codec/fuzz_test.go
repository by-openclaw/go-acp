package codec

import "testing"

// The decoder runs on every frame from the wire before anything trusts
// it, so it must never panic or read out of bounds on hostile input.
// Seeds run as regression tests; `go test -fuzz` drives the search.
func FuzzDecode(f *testing.F) {
	f.Add([]byte{})
	f.Add([]byte{0x00})
	f.Add([]byte{0x02, 0x01, 0x00})
	f.Add([]byte{0x30, 0x84, 0xff, 0xff, 0xff, 0xff})
	f.Add([]byte{0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff})
	f.Fuzz(func(t *testing.T, data []byte) { _, _ = Decode(data) })
}
