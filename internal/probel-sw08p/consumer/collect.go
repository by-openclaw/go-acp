package probelsw08p

import "dhs/internal/probel-sw08p/codec"

// collectDone builds a codec.SendCollect done-predicate that stops once the
// running total reported by count across the collected frames reaches want.
// want <= 0 returns nil, meaning "no deterministic stop" — SendCollect then
// relies solely on its idle gap. The returned closure is stateful (tracks how
// many frames it has already counted) and is only ever called from SendCollect's
// own goroutine, so it needs no locking.
//
// This is the generic answer to "how do you know the table ended?" — SW-P-08
// carries no matrix-size field on the wire, so an operator who knows the size
// (any brand: --srcs / --dsts) gets an exact stop, and everyone else still gets
// the whole table via the idle gap.
func collectDone(want int, count func(codec.Frame) int) func([]codec.Frame) bool {
	if want <= 0 {
		return nil
	}
	counted, total := 0, 0
	return func(fs []codec.Frame) bool {
		for ; counted < len(fs); counted++ {
			total += count(fs[counted])
		}
		return total >= want
	}
}

// sourceNamesDone stops once `srcs` source labels have arrived (tx 106 frames).
func sourceNamesDone(srcs uint16) func([]codec.Frame) bool {
	return collectDone(int(srcs), func(f codec.Frame) int {
		r, err := codec.DecodeSourceNamesResponse(f)
		if err != nil {
			return 0
		}
		return len(r.Names)
	})
}

// destNamesDone stops once `dsts` destination labels have arrived (tx 107).
func destNamesDone(dsts uint16) func([]codec.Frame) bool {
	return collectDone(int(dsts), func(f codec.Frame) int {
		r, err := codec.DecodeDestAssocNamesResponse(f)
		if err != nil {
			return 0
		}
		return len(r.Names)
	})
}

// tallyDone stops once `dsts` crosspoint entries have arrived (tx 022 byte or
// tx 023/023-ext word). One entry == one destination's current source.
func tallyDone(dsts uint16) func([]codec.Frame) bool {
	return collectDone(int(dsts), func(f codec.Frame) int {
		if f.ID == codec.TxCrosspointTallyDumpByte {
			r, err := codec.DecodeCrosspointTallyDumpByte(f)
			if err != nil {
				return 0
			}
			return len(r.SourceIDs)
		}
		r, err := codec.DecodeCrosspointTallyDumpWord(f)
		if err != nil {
			return 0
		}
		return len(r.SourceIDs)
	})
}
