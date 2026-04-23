package codec

import "testing"

// Baseline benchmarks for the five hottest Probel codec paths.
// Run with: go test -run=^$ -bench=. -benchmem ./internal/probel-sw08p/codec
//
// Numbers land in bin/bench.txt as the "before" snapshot against which
// the sparse-tree + streaming-encoder refactors must not regress.

func BenchmarkEncodeCrosspointInterrogateGeneral(b *testing.B) {
	p := CrosspointInterrogateParams{MatrixID: 0, LevelID: 0, DestinationID: 5}
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = EncodeCrosspointInterrogate(p)
	}
}

func BenchmarkEncodeCrosspointInterrogateExtended(b *testing.B) {
	p := CrosspointInterrogateParams{MatrixID: 17, LevelID: 2, DestinationID: 40000}
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = EncodeCrosspointInterrogate(p)
	}
}

func BenchmarkDecodeCrosspointInterrogateGeneral(b *testing.B) {
	f := EncodeCrosspointInterrogate(CrosspointInterrogateParams{MatrixID: 0, LevelID: 0, DestinationID: 5})
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_, _ = DecodeCrosspointInterrogate(f)
	}
}

func BenchmarkEncodeCrosspointConnectGeneral(b *testing.B) {
	p := CrosspointConnectParams{MatrixID: 0, LevelID: 0, DestinationID: 5, SourceID: 12}
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = EncodeCrosspointConnect(p)
	}
}

func BenchmarkEncodeCrosspointConnectExtended(b *testing.B) {
	p := CrosspointConnectParams{MatrixID: 17, LevelID: 2, DestinationID: 40000, SourceID: 50000}
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = EncodeCrosspointConnect(p)
	}
}

func BenchmarkDecodeCrosspointConnectGeneral(b *testing.B) {
	f := EncodeCrosspointConnect(CrosspointConnectParams{MatrixID: 0, LevelID: 0, DestinationID: 5, SourceID: 12})
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_, _ = DecodeCrosspointConnect(f)
	}
}

func BenchmarkEncodeCrosspointTallyGeneral(b *testing.B) {
	p := CrosspointTallyParams{MatrixID: 0, LevelID: 0, DestinationID: 5, SourceID: 12}
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = EncodeCrosspointTally(p)
	}
}

func BenchmarkEncodeCrosspointTallyExtended(b *testing.B) {
	p := CrosspointTallyParams{MatrixID: 17, LevelID: 2, DestinationID: 40000, SourceID: 50000}
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = EncodeCrosspointTally(p)
	}
}

func BenchmarkDecodeCrosspointTallyGeneral(b *testing.B) {
	f := EncodeCrosspointTally(CrosspointTallyParams{MatrixID: 0, LevelID: 0, DestinationID: 5, SourceID: 12})
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_, _ = DecodeCrosspointTally(f)
	}
}

func BenchmarkEncodeSalvoConnectOnGo(b *testing.B) {
	p := SalvoConnectOnGoParams{MatrixID: 0, LevelID: 0, DestinationID: 5, SourceID: 12, SalvoID: 1}
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = EncodeSalvoConnectOnGo(p)
	}
}

func BenchmarkDecodeSalvoConnectOnGo(b *testing.B) {
	f := EncodeSalvoConnectOnGo(SalvoConnectOnGoParams{MatrixID: 0, LevelID: 0, DestinationID: 5, SourceID: 12, SalvoID: 1})
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_, _ = DecodeSalvoConnectOnGo(f)
	}
}

func BenchmarkEncodeSalvoGo(b *testing.B) {
	p := SalvoGoParams{Op: SalvoOpSet, SalvoID: 1}
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = EncodeSalvoGo(p)
	}
}

func BenchmarkDecodeSalvoGo(b *testing.B) {
	f := EncodeSalvoGo(SalvoGoParams{Op: SalvoOpSet, SalvoID: 1})
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_, _ = DecodeSalvoGo(f)
	}
}

// BenchmarkTallyDumpWorstCase exercises the spec-max 65535-dest tally
// dump word form to bound the cost of the dense path we're about to
// replace with streaming. The "before" number from this benchmark is
// what PR C must beat.
func BenchmarkEncodeCrosspointTallyDumpWordWorstCase(b *testing.B) {
	const n = 65535
	srcs := make([]uint16, n)
	for i := range srcs {
		srcs[i] = uint16(i)
	}
	p := CrosspointTallyDumpWordParams{
		MatrixID:           0,
		LevelID:            0,
		FirstDestinationID: 0,
		SourceIDs:          srcs,
	}
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = EncodeCrosspointTallyDumpWord(p)
	}
}
