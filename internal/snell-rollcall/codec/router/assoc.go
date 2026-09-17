package router

import (
	"fmt"

	"dhs/internal/snell-rollcall/codec/dtp"
)

// An association is one logical thing spread across the levels of a matrix.
//
// A source in a plant is rarely one signal. A camera is a picture, a pair of
// audio levels and some ancillary data, and each of those lives on a level of
// its own with its own numbering: the picture may be source 4 on the video
// level while its audio is sources 7 and 8 on two audio levels. An association
// is the name for all of them at once, and routing by association is how a
// panel takes a camera rather than taking four unrelated crosspoints and
// hoping they match.
//
// The specification draws the line plainly. Multi-level control is routing by
// association; multi-matrix control is tielines. They are different mechanisms
// for different problems and neither substitutes for the other.
//
// Offsets within an association's table, from the association base. The first
// three are fixed; after them comes one entry per level, so the table is as
// long as the matrix has levels.
const (
	OffAssocName8   = 0
	OffAssocName32  = 1
	OffAssocAltName = 2

	// OffAssocSrcDst is the first level's member. Level L is at
	// OffAssocSrcDst + (L-1).
	OffAssocSrcDst = 3
)

// AssocTableSize is how many commands one association occupies on a matrix
// with the given number of levels.
func AssocTableSize(levels int) uint32 {
	if levels < 0 {
		levels = 0
	}
	return uint32(OffAssocSrcDst + levels)
}

// AssocMember is the command carrying an association's entity on one level,
// counting levels from one.
func AssocMember(base Command, level int) (Command, bool) {
	if level < 1 {
		return 0, false
	}
	return base + Command(OffAssocSrcDst+level-1), true
}

// MakeRoute is a request to route one association to another.
//
// It is the only routing command that is not a write to a destination's own
// table entry, because it is not about one destination: it names a source
// association, a destination association, and which of the source's levels to
// carry across. A camera taken to a monitor without its audio is one route on
// this command with two bits set rather than two commands.
type MakeRoute struct {
	// All four count from one, as the specification says of the array.
	DestMatrix   uint32
	DestAssoc    uint32
	SourceMatrix uint32
	SourceAssoc  uint32

	// Levels says which of the source's levels to use. An empty bitmap asks
	// for none of them, which is a request to do nothing rather than a
	// request to do everything: the difference matters when a panel has
	// deselected every level and the operator presses take.
	Levels dtp.Bitmap
}

// AppendTo encodes the request as Data Transfer Params.
func (m MakeRoute) AppendTo(dst []byte) ([]byte, error) {
	return dtp.Append(dst, dtp.Params{
		dtp.Uints(m.DestMatrix, m.DestAssoc, m.SourceMatrix, m.SourceAssoc),
		dtp.Bits(m.Levels),
	}, false)
}

// DecodeMakeRoute reads a route request.
func DecodeMakeRoute(b []byte) (MakeRoute, error) {
	p, err := dtp.Decode(b)
	if err != nil {
		return MakeRoute{}, fmt.Errorf("router: make route: %w", err)
	}
	if len(p) < 2 {
		return MakeRoute{}, fmt.Errorf(
			"router: make route carries %d parameters, want the addresses and the levels", len(p))
	}
	if p[0].Type != dtp.TypeUintArray || len(p[0].Uints) != 4 {
		return MakeRoute{}, fmt.Errorf(
			"router: make route addresses are %s of %d, want an array of four",
			p[0].Type, len(p[0].Uints))
	}
	if p[1].Type != dtp.TypeBitmap {
		return MakeRoute{}, fmt.Errorf(
			"router: make route levels are %s, want a bitmap", p[1].Type)
	}

	a := p[0].Uints
	return MakeRoute{
		DestMatrix:   a[0],
		DestAssoc:    a[1],
		SourceMatrix: a[2],
		SourceAssoc:  a[3],
		Levels:       p[1].Bitmap,
	}, nil
}

// AppendRouteResult encodes what a controller answers a route request with.
func AppendRouteResult(dst []byte, r RouteResult) ([]byte, error) {
	return dtp.Append(dst, dtp.Params{dtp.Uint(uint32(r))}, false)
}

// DecodeRouteResult reads the answer to a route request.
func DecodeRouteResult(b []byte) (RouteResult, error) {
	p, err := dtp.Decode(b)
	if err != nil {
		return 0, fmt.Errorf("router: route result: %w", err)
	}
	if len(p) < 1 || p[0].Type != dtp.TypeUint {
		return 0, fmt.Errorf("router: route result is not a number")
	}
	return RouteResult(p[0].Uint), nil
}
