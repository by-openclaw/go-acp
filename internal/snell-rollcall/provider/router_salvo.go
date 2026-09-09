package rollcall

import (
	"fmt"

	"dhs/internal/snell-rollcall/codec/router"
)

// A salvo is a set of routes made together.
//
// The controller holds them and a client fires one by number, so what a salvo
// contains is never on the wire: only its name, in a file, and the count of
// routes it managed. That makes them cheap to serve and impossible to inspect,
// which is the vendor's design rather than ours.
//
// What is in ours is this provider's own choice, because a canonical tree has
// no field for a salvo. One per source, routing that source to every
// destination on every level — the thing an operator most often wants a button
// for, and the thing that most obviously proves a salvo fired when it did.

// routerSalvo is one salvo: a name and the routes it makes.
type routerSalvo struct {
	name   string
	routes []salvoRoute
}

// salvoRoute is one crosspoint a salvo makes.
type salvoRoute struct {
	matrix int
	level  int
	dest   int
	source uint32
}

// salvoNamesFile is where the names live, at each of the two widths.
//
// The path is the vendor's. A controller publishes a filename and a checksum
// on a command, and the client fetches the file from the same node's file
// service; naming them anything else would work only because a client believes
// what it is told, and matching costs nothing.
const (
	salvoNames8File  = `RC_Files\SalvoNames_8.dat`
	salvoNames32File = `RC_Files\SalvoNames_32.dat`
)

// buildSalvos makes one salvo per source of the first matrix.
func buildSalvos(matrices []routerMatrix) []routerSalvo {
	if len(matrices) == 0 || len(matrices[0].levels) == 0 {
		return nil
	}
	m := &matrices[0]

	out := make([]routerSalvo, 0, len(m.levels[0].sources))
	for s := range m.levels[0].sources {
		sv := routerSalvo{name: fmt.Sprintf("All %s", m.levels[0].sources[s])}
		for l := range m.levels {
			for d := range m.levels[l].dests {
				sv.routes = append(sv.routes, salvoRoute{
					matrix: 1, level: l + 1, dest: d + 1, source: uint32(s + 1),
				})
			}
		}
		out = append(out, sv)
	}
	return out
}

// salvoNames renders the names file at one width.
func (r *routerModel) salvoNames(width int) []byte {
	f := router.NamesFile{Width: width}
	for i := range r.salvos {
		f.Srcs = append(f.Srcs, r.salvos[i].name)
	}
	// The width is one of the two the format allows, so this cannot fail.
	b, _ := f.AppendTo(nil)
	return b
}

// salvoNamesCRC is what the command publishes beside the filename, so a client
// can tell a file it already has from one that has changed.
func (r *routerModel) salvoNamesCRC(width int) uint32 {
	f := router.NamesFile{Width: width}
	for i := range r.salvos {
		f.Srcs = append(f.Srcs, r.salvos[i].name)
	}
	return f.CRC()
}

// fireSalvo makes every route in a salvo and says how many it made.
//
// There is no result code, only the count: the specification says "number of
// routes made or 0 on error" and does not distinguish an empty salvo from a
// refused one. A route onto a protected destination is skipped rather than
// failing the salvo, because a salvo is a set of independent routes and an
// operator who protected one destination did not mean to disable the button.
func (r *routerModel) fireSalvo(n uint32) (uint32, []routedChange) {
	if n < 1 || int(n) > len(r.salvos) {
		return 0, nil
	}
	sv := &r.salvos[n-1]

	var moved []routedChange
	for _, rt := range sv.routes {
		mx, ok := r.matrixAt(uint32(rt.matrix))
		if !ok || rt.level < 1 || rt.level > len(mx.levels) {
			continue
		}
		lv := &mx.levels[rt.level-1]
		if rt.dest < 1 || rt.dest > len(lv.dests) ||
			rt.source < 1 || int(rt.source) > len(lv.sources) {
			continue
		}
		if lv.dests[rt.dest-1].protect.Protected {
			continue
		}
		r.releaseTielines(lv, rt.dest)
		lv.dests[rt.dest-1].routed = router.SourcePin{
			Matrix: lv.matrixNumber,
			Level:  lv.levelNumber,
			Source: uint16(rt.source),
		}
		moved = append(moved, routedChange{level: lv, dest: rt.dest})
	}
	return uint32(len(moved)), moved
}

// salvoAt returns one salvo, counting from one.
func (r *routerModel) salvoAt(n uint32) (*routerSalvo, bool) {
	if n < 1 || int(n) > len(r.salvos) {
		return nil, false
	}
	return &r.salvos[n-1], true
}
