package probelsw08p

import (
	"testing"
	"time"

	"dhs/internal/metrics"
	"dhs/internal/probel-sw08p/codec"
)

// TestIsOnlineWithin_MetricsButNoRx covers the branch where a metrics
// connector exists but no rx frame has been observed yet: a freshly-dialed
// session whose LastRxAt is still zero must report offline.
func TestIsOnlineWithin_MetricsButNoRx(t *testing.T) {
	p := freshPlugin()
	p.metricsConn = metrics.NewConnector() // connected, but never received a frame
	if p.IsOnlineWithin(1 * time.Second) {
		t.Error("IsOnlineWithin true with zero LastRxAt; want false")
	}
}

// The done-predicates in collect.go only build a live closure when the
// operator supplied a non-zero count (--srcs / --dsts). The harness tests
// elsewhere run with an empty MatrixConfig, so they exercise only the
// want<=0 → nil branch. These drive the closure body directly: cumulative
// counting across calls, decode-error frames counting zero, and the
// total>=want stop. The slice grows each call, mirroring how SendCollect
// hands the predicate its running buffer.

func TestCollectDone_CumulativeStop(t *testing.T) {
	// want=3, each frame counts 2.
	done := collectDone(3, func(codec.Frame) int { return 2 })
	if done == nil {
		t.Fatal("collectDone(3,..) = nil; want a live closure")
	}
	if done([]codec.Frame{{}}) {
		t.Fatal("total=2 < 3 → returned true; want false")
	}
	if !done([]codec.Frame{{}, {}}) {
		t.Fatal("total=4 >= 3 → returned false; want true")
	}
}

func TestCollectDone_NilWhenNoCount(t *testing.T) {
	if collectDone(0, func(codec.Frame) int { return 1 }) != nil {
		t.Error("collectDone(0) != nil")
	}
	if collectDone(-1, func(codec.Frame) int { return 1 }) != nil {
		t.Error("collectDone(-1) != nil")
	}
}

func TestSourceNamesDone(t *testing.T) {
	done := sourceNamesDone(3)
	good := codec.EncodeSourceNamesResponse(codec.SourceNamesResponseParams{
		NameLength: codec.NameLen8, Names: []string{"A", "B"},
	})
	bad := codec.Frame{ID: codec.TxSourceNamesResponse, Payload: []byte{0x00}}

	if done([]codec.Frame{good}) {
		t.Fatal("2 names < 3 → true; want false")
	}
	if done([]codec.Frame{good, bad}) {
		t.Fatal("undecodable frame counted >0; want false")
	}
	if !done([]codec.Frame{good, bad, good}) {
		t.Fatal("4 names >= 3 → false; want true")
	}
}

func TestDestNamesDone(t *testing.T) {
	done := destNamesDone(3)
	good := codec.EncodeDestAssocNamesResponse(codec.DestAssocNamesResponseParams{
		NameLength: codec.NameLen8, Names: []string{"A", "B"},
	})
	bad := codec.Frame{ID: codec.TxDestAssocNamesResponse, Payload: []byte{0x00}}

	if done([]codec.Frame{good}) {
		t.Fatal("2 names < 3 → true; want false")
	}
	if done([]codec.Frame{good, bad}) {
		t.Fatal("undecodable frame counted >0; want false")
	}
	if !done([]codec.Frame{good, bad, good}) {
		t.Fatal("4 names >= 3 → false; want true")
	}
}

// TestTallyDone walks every branch of the count func: byte-form decode,
// word-form decode, and the decode-error fall-through (return 0) on each.
func TestTallyDone(t *testing.T) {
	done := tallyDone(3)
	byteGood := codec.EncodeCrosspointTallyDumpByte(codec.CrosspointTallyDumpByteParams{
		SourceIDs: []uint8{1, 2}, // counts 2
	})
	byteBad := codec.Frame{ID: codec.TxCrosspointTallyDumpByte, Payload: []byte{0x00}}
	wordGood := codec.EncodeCrosspointTallyDumpWord(codec.CrosspointTallyDumpWordParams{
		SourceIDs: []uint16{300}, // counts 1
	})
	wordBad := codec.Frame{ID: codec.TxCrosspointTallyDumpWord, Payload: []byte{0x00}}

	if done([]codec.Frame{byteGood}) {
		t.Fatal("total=2 < 3 → true; want false")
	}
	if done([]codec.Frame{byteGood, wordBad}) {
		t.Fatal("undecodable word frame counted >0; want false")
	}
	if done([]codec.Frame{byteGood, wordBad, byteBad}) {
		t.Fatal("undecodable byte frame counted >0; want false")
	}
	if !done([]codec.Frame{byteGood, wordBad, byteBad, wordGood}) {
		t.Fatal("total=3 >= 3 → false; want true")
	}
}

// ---- multi-frame merge + sort (rx021 / rx100 / rx102) ----------------------
//
// The single-frame harness tests never invoke the sort comparator (one
// segment) nor the i>0 merge-append. These reply with two out-of-order
// frames so the collected set must be sorted by First*ID and concatenated.

func TestAllSourceNames_MultiFrameMerge(t *testing.T) {
	p, _, cleanup := newPeerHarness(t, replyWith(
		codec.EncodeSourceNamesResponse(codec.SourceNamesResponseParams{
			NameLength: codec.NameLen8, FirstSourceID: 2, Names: []string{"C", "D"},
		}),
		codec.EncodeSourceNamesResponse(codec.SourceNamesResponseParams{
			NameLength: codec.NameLen8, FirstSourceID: 0, Names: []string{"A", "B"},
		}),
	))
	defer cleanup()
	ctx, cancel := ctxT(t)
	defer cancel()
	got, err := p.AllSourceNames(ctx, 0, 0, codec.NameLen8)
	if err != nil {
		t.Fatalf("AllSourceNames: %v", err)
	}
	if len(got.Names) != 4 || got.Names[0] != "A" || got.Names[3] != "D" {
		t.Errorf("merged names = %v; want [A B C D]", got.Names)
	}
}

func TestAllDestAssocNames_MultiFrameMerge(t *testing.T) {
	p, _, cleanup := newPeerHarness(t, replyWith(
		codec.EncodeDestAssocNamesResponse(codec.DestAssocNamesResponseParams{
			NameLength: codec.NameLen8, FirstDestAssociationID: 2, Names: []string{"C", "D"},
		}),
		codec.EncodeDestAssocNamesResponse(codec.DestAssocNamesResponseParams{
			NameLength: codec.NameLen8, FirstDestAssociationID: 0, Names: []string{"A", "B"},
		}),
	))
	defer cleanup()
	ctx, cancel := ctxT(t)
	defer cancel()
	got, err := p.AllDestAssocNames(ctx, 0, codec.NameLen8)
	if err != nil {
		t.Fatalf("AllDestAssocNames: %v", err)
	}
	if len(got.Names) != 4 || got.Names[0] != "A" || got.Names[3] != "D" {
		t.Errorf("merged names = %v; want [A B C D]", got.Names)
	}
}

func TestCrosspointTallyDump_MultiFrameSorted(t *testing.T) {
	p, _, cleanup := newPeerHarness(t, replyWith(
		codec.EncodeCrosspointTallyDumpByte(codec.CrosspointTallyDumpByteParams{
			FirstDestinationID: 2, SourceIDs: []uint8{3, 4},
		}),
		codec.EncodeCrosspointTallyDumpByte(codec.CrosspointTallyDumpByteParams{
			FirstDestinationID: 0, SourceIDs: []uint8{1, 2},
		}),
	))
	defer cleanup()
	ctx, cancel := ctxT(t)
	defer cancel()
	got, err := p.CrosspointTallyDump(ctx, 0, 0)
	if err != nil {
		t.Fatalf("CrosspointTallyDump: %v", err)
	}
	if got.IsWord {
		t.Fatal("expected byte form")
	}
	if len(got.Byte.SourceIDs) != 4 || got.Byte.SourceIDs[0] != 1 || got.Byte.SourceIDs[3] != 4 {
		t.Errorf("merged srcs = %v; want [1 2 3 4]", got.Byte.SourceIDs)
	}
}
