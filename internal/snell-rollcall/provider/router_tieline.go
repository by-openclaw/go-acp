package rollcall

import (
	"fmt"

	"dhs/internal/snell-rollcall/codec/router"
)

// A tieline is a cable between two matrices.
//
// It runs from a destination on one to a source on the other, and it is what
// makes a route across matrices possible at all: a source on matrix 1 cannot
// reach a destination on matrix 2 by itself, because they are different pieces
// of hardware with no crosspoint in common. The controller routes the real
// source onto the tieline's destination on matrix 1, routes the tieline's
// source to the asked-for destination on matrix 2, and holds the pair until
// that destination is routed elsewhere.
//
// So a client never routes a tieline. It asks for a source on one matrix and a
// destination on another, and is told either that the route was made or that
// there was no tieline left to make it with — result code 6, which this
// provider has been answering unconditionally because it had no pool to take
// one from.
//
// The specification draws the line plainly: multi-level control is routing by
// association, multi-matrix control is tielines. This is the second.

// routerTieline is one cable, and what is using it.
type routerTieline struct {
	name string

	// upstream is where the signal is put in: a destination on the matrix the
	// source lives on.
	upMatrix uint8
	upLevel  uint8
	upDest   int

	// downstream is where it comes out: a source on the matrix the
	// destination lives on.
	downMatrix uint8
	downLevel  uint8
	downSource int

	// heldFor is the destination this tieline was taken for, on the
	// downstream matrix and level, or zero when it is free. A tieline is held
	// by exactly one destination: two would mean two signals on one cable.
	heldFor int
}

// free reports whether the tieline can be taken.
func (t *routerTieline) free() bool { return t.heldFor == 0 }

// tielinesBetween returns how many cables this provider wires between each
// ordered pair of matrices.
//
// A real plant is wired by whoever installed it and the number is whatever
// they pulled. Two is enough to prove both cases an operator meets: a route
// across matrices that works, and a third one that is told there is no tieline
// left.
const tielinesBetween = 2

// buildTielines wires the plant.
//
// The cables take the last destinations of the upstream matrix and the last
// sources of the downstream one, because that is where an installer puts them:
// the low numbers are the real plant and the high ones are what is left. A
// matrix too small to spare any is wired to nothing, which is honest — a
// two-by-two matrix with a tieline taken out of it is a one-by-two matrix.
func buildTielines(matrices []routerMatrix) []routerTieline {
	if len(matrices) < 2 {
		return nil
	}

	var out []routerTieline
	for up := range matrices {
		for down := range matrices {
			if up == down {
				continue
			}
			out = append(out, cablesBetween(&matrices[up], &matrices[down], len(out))...)
		}
	}
	return out
}

// cablesBetween wires one ordered pair of matrices on every level they share.
func cablesBetween(up, down *routerMatrix, sofar int) []routerTieline {
	levels := min(len(up.levels), len(down.levels))

	var out []routerTieline
	for l := range levels {
		upLevel, downLevel := &up.levels[l], &down.levels[l]

		// Room for a cable at both ends, and something left over: a matrix
		// whose whole output is tielines is not a matrix any more.
		room := min(len(upLevel.dests), len(downLevel.sources)) - 1
		if room > tielinesBetween {
			room = tielinesBetween
		}
		for i := range room {
			out = append(out, routerTieline{
				name:       fmt.Sprintf("TL %d", sofar+len(out)+1),
				upMatrix:   upLevel.matrixNumber,
				upLevel:    upLevel.levelNumber,
				upDest:     len(upLevel.dests) - i,
				downMatrix: downLevel.matrixNumber,
				downLevel:  downLevel.levelNumber,
				downSource: len(downLevel.sources) - i,
			})
		}
	}
	return out
}

// takeTieline finds a free cable from one matrix and level to another.
//
// A tieline already held for this destination is reused rather than a second
// one taken: routing the same destination twice is a change of source, not a
// second signal, and taking another cable each time is how a plant runs out of
// them while looking idle.
func (r *routerModel) takeTieline(up, down *routerLevel, dest int) (*routerTieline, bool) {
	for i := range r.tielines {
		t := &r.tielines[i]
		if t.upMatrix != up.matrixNumber || t.upLevel != up.levelNumber ||
			t.downMatrix != down.matrixNumber || t.downLevel != down.levelNumber {
			continue
		}
		if t.heldFor == dest {
			return t, true
		}
	}

	for i := range r.tielines {
		t := &r.tielines[i]
		if !t.free() || t.upMatrix != up.matrixNumber || t.upLevel != up.levelNumber ||
			t.downMatrix != down.matrixNumber || t.downLevel != down.levelNumber {
			continue
		}
		t.heldFor = dest
		return t, true
	}
	return nil, false
}

// releaseTielines frees every cable held for a destination.
//
// It runs when that destination is routed to something else, which is the only
// thing that can free one: a tieline is held by the destination it feeds, and
// the destination stops needing it the moment it is fed by something nearer.
func (r *routerModel) releaseTielines(down *routerLevel, dest int) {
	for i := range r.tielines {
		t := &r.tielines[i]
		if t.heldFor == dest && t.downMatrix == down.matrixNumber &&
			t.downLevel == down.levelNumber {
			t.heldFor = 0
		}
	}
}

// routeAcross makes a route between two matrices, taking a tieline for it.
//
// Both halves are applied or neither is: a tieline fed from the right source
// but not connected to the destination is a cable carrying signal to nobody,
// and it would stay held.
func (r *routerModel) routeAcross(up, down *routerLevel, source uint16, dest int) (router.RouteResult, []routedChange) {
	if int(source) < 1 || int(source) > len(up.sources) {
		return router.RouteNotInstalled, nil
	}
	if dest < 1 || dest > len(down.dests) {
		return router.RouteNotInstalled, nil
	}
	if down.dests[dest-1].protect.Protected {
		return router.RouteProtected, nil
	}

	t, ok := r.takeTieline(up, down, dest)
	if !ok {
		return router.RouteNoTieline, nil
	}

	// The real source onto the cable, then the cable to the destination.
	up.dests[t.upDest-1].routed = router.SourcePin{
		Matrix: up.matrixNumber, Level: up.levelNumber, Source: source,
	}
	down.dests[dest-1].routed = router.SourcePin{
		Matrix: down.matrixNumber, Level: down.levelNumber, Source: uint16(t.downSource),
	}

	return router.RouteOK, []routedChange{
		{level: up, dest: t.upDest},
		{level: down, dest: dest},
	}
}

// levelOf returns one level of one matrix, counting both from one.
func (r *routerModel) levelOf(matrix, level uint32) (*routerLevel, bool) {
	m, ok := r.matrixAt(matrix)
	if !ok || level < 1 || int(level) > len(m.levels) {
		return nil, false
	}
	return &m.levels[level-1], true
}
