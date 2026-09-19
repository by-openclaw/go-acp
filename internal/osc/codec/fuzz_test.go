package codec

import "testing"

func FuzzDecodePacket(f *testing.F) {
	f.Add([]byte{})
	f.Add([]byte{0x00})
	f.Add([]byte{0x02, 0x01, 0x00})
	f.Add([]byte{0x30, 0x84, 0xff, 0xff, 0xff, 0xff})
	f.Add([]byte{0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff})
	f.Add([]byte("/x\x00\x00,\x00\x00\x00"))
	f.Fuzz(func(t *testing.T, data []byte) { _, _ = DecodePacket(data) })
}
