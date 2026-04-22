package probelsw08p

import (
	"testing"
)

// BenchmarkTreeApplyConnectSparse covers the hot path of a controller
// issuing many Connect frames in a row: applyConnect → sparse map
// insert. The sparse representation means memory grows with actually
// routed destinations, not with targetCount — critical at the
// broadcast-industry scale target of 65535 destinations per matrix.
func BenchmarkTreeApplyConnectSparse(b *testing.B) {
	t := &tree{matrices: map[matrixKey]*matrixState{
		{matrix: 0, level: 0}: {
			targetCount: 65535,
			sourceCount: 65535,
			sources:     map[uint16]uint16{},
			protects:    map[uint16]protectRecord{},
		},
	}}
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = t.applyConnect(0, 0, uint16(i%65535), uint16((i*7)%65535))
	}
}

// BenchmarkTreeCurrentSourceSparse exercises the read path: 1 M random
// lookups on a sparsely-populated 65535-dest matrix (only 256 of 65535
// destinations routed). Sparse map keeps the read cost proportional to
// the actually-routed subset, not targetCount.
func BenchmarkTreeCurrentSourceSparse(b *testing.B) {
	t := &tree{matrices: map[matrixKey]*matrixState{
		{matrix: 0, level: 0}: {
			targetCount: 65535,
			sourceCount: 65535,
			sources:     map[uint16]uint16{},
			protects:    map[uint16]protectRecord{},
		},
	}}
	for i := 0; i < 256; i++ {
		_ = t.applyConnect(0, 0, uint16(i*241), uint16(i*53))
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = t.currentSource(0, 0, uint16(i&0xFFFF))
	}
}
