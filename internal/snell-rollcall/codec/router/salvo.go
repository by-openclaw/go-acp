package router

import (
	"fmt"

	"dhs/internal/snell-rollcall/codec/dtp"
)

// A salvo is a set of routes made together.
//
// The controller holds them; a client fires one by number and is told how many
// routes it made. The names live in a file rather than in commands, which is
// how everything countable on this interface is named: a plant with a thousand
// salvos would otherwise need a thousand commands to say what they are called.

// FireSalvo asks for a salvo to be made.
type FireSalvo struct {
	// Salvo counts from one, as everything on this interface does.
	Salvo uint32
}

// AppendTo encodes the request as Data Transfer Params.
func (f FireSalvo) AppendTo(dst []byte) ([]byte, error) {
	return dtp.Append(dst, dtp.Params{dtp.Uint(f.Salvo)}, false)
}

// DecodeFireSalvo reads a request to fire a salvo.
func DecodeFireSalvo(b []byte) (FireSalvo, error) {
	p, err := dtp.Decode(b)
	if err != nil {
		return FireSalvo{}, fmt.Errorf("router: fire salvo: %w", err)
	}
	if len(p) < 1 || p[0].Type != dtp.TypeUint {
		return FireSalvo{}, fmt.Errorf("router: fire salvo names no salvo")
	}
	return FireSalvo{Salvo: p[0].Uint}, nil
}

// SalvoFired is what a controller answers a fire with.
//
// There is no result code here as there is on a route: the count is the
// result. Zero means the salvo did nothing, whether because it was empty,
// because it does not exist, or because every route in it was refused — the
// specification says "number of routes made or 0 on error" and does not
// distinguish them.
type SalvoFired struct {
	Salvo  uint32
	Routes uint32
}

// AppendTo encodes the reply.
func (s SalvoFired) AppendTo(dst []byte) ([]byte, error) {
	return dtp.Append(dst, dtp.Params{dtp.Uint(s.Salvo), dtp.Uint(s.Routes)}, false)
}

// DecodeSalvoFired reads the answer to a fire.
func DecodeSalvoFired(b []byte) (SalvoFired, error) {
	p, err := dtp.Decode(b)
	if err != nil {
		return SalvoFired{}, fmt.Errorf("router: salvo fired: %w", err)
	}
	if len(p) < 2 || p[0].Type != dtp.TypeUint || p[1].Type != dtp.TypeUint {
		return SalvoFired{}, fmt.Errorf(
			"router: salvo reply carries %d parameters, want the salvo and the count", len(p))
	}
	return SalvoFired{Salvo: p[0].Uint, Routes: p[1].Uint}, nil
}

// OK reports whether the salvo made any routes at all.
func (s SalvoFired) OK() bool { return s.Routes > 0 }
