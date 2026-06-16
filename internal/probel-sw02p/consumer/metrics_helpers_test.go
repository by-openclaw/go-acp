package probelsw02p

import (
	"testing"

	"dhs/internal/probel-sw02p/codec"
)

// TestProbelCmdFromBytes pins the raw-frame → command-byte extractor
// used by the metrics observer wrappers in Connect.
func TestProbelCmdFromBytes(t *testing.T) {
	// A well-formed rx 02 CONNECT frame: SOM + cmd + 3 payload + cksum.
	good := codec.Pack(codec.EncodeConnect(codec.ConnectParams{Destination: 5, Source: 6}))
	if got, ok := probelCmdFromBytes(good); !ok || got != byte(codec.RxConnect) {
		t.Errorf("probelCmdFromBytes(good) = (%#x, %v); want (%#x, true)", got, ok, byte(codec.RxConnect))
	}

	tests := []struct {
		name string
		in   []byte
	}{
		{"too short", []byte{codec.SOM, 0x02}},
		{"empty", nil},
		{"bad SOM", []byte{0x00, 0x02, 0x00, 0x00}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got, ok := probelCmdFromBytes(tc.in); ok {
				t.Errorf("probelCmdFromBytes(%v) = (%#x, true); want (_, false)", tc.in, got)
			}
		})
	}
}
