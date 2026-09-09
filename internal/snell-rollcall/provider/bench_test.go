package rollcall

import (
	"context"
	"testing"

	"dhs/internal/export/canonical"
	"dhs/internal/snell-rollcall/codec"
	"dhs/internal/snell-rollcall/codec/router"
)

// What a router costs to serve.
//
// The sizes here are a real plant rather than a demonstration: a Centra
// publishes a level with 1152 sources and 1450 destinations, whose menu is
// five and a half thousand lines. Everything a panel does on connecting
// touches that menu, so anything linear in it happens on every one of them.

// benchTree is a plant of the size the vendor's own emulator serves.
func benchTree(sources, targets int64) *canonical.Export {
	return &canonical.Export{Root: &canonical.Node{
		Header: canonical.Header{
			Number: 1, Identifier: "frame", Path: "frame",
			Children: []canonical.Element{testMatrix(targets, sources)},
		},
	}}
}

func BenchmarkBuildAPlant(b *testing.B) {
	// Building the model is what a provider does at startup and never again,
	// but it is also what bounds how large a plant may be served at all.
	for range b.N {
		buildModel(benchTree(1152, 1450), "bench")
	}
}

func BenchmarkReadOneValue(b *testing.B) {
	// The hot path. A panel watching a plant reads and is pushed values
	// continuously, and this is the cost of one of them with the whole menu
	// behind it.
	m := buildModel(benchTree(1152, 1450), "bench")
	prt := m.port(firstCardPort + 1)
	cmd := uint32(router.LvlRoute(700))

	b.ReportAllocs()
	for range b.N {
		if _, ok := prt.value(cmd); !ok {
			b.Fatal("no value")
		}
	}
}

func BenchmarkWriteOneCrosspoint(b *testing.B) {
	m := buildModel(benchTree(1152, 1450), "bench")
	prt := m.port(firstCardPort + 1)
	cmd := uint32(router.LvlRoute(700))

	b.ReportAllocs()
	for range b.N {
		if _, err := prt.setValue(cmd, codec.ModeValue, 42, "", nil); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkWalkTheMenu(b *testing.B) {
	// Every menu request pays for this, and a panel makes one per node.
	m := buildModel(benchTree(1152, 1450), "bench")
	prt := m.port(firstCardPort + 1)

	b.ReportAllocs()
	for range b.N {
		if len(prt.menu(true)) == 0 {
			b.Fatal("empty menu")
		}
	}
}

func BenchmarkReadOneMenuLine(b *testing.B) {
	// What a 32-bit walk actually does: one line at a time, by index, five and
	// a half thousand times for a level this size.
	m := buildModel(benchTree(1152, 1450), "bench")
	prt := m.port(firstCardPort + 1)

	b.ReportAllocs()
	i := uint32(0)
	for range b.N {
		if _, ok := prt.lineAt(i%5000, true); !ok {
			b.Fatal("no line")
		}
		i++
	}
}

func BenchmarkCountTheMenu(b *testing.B) {
	m := buildModel(benchTree(1152, 1450), "bench")
	prt := m.port(firstCardPort + 1)

	b.ReportAllocs()
	for range b.N {
		if prt.menuLen(true) == 0 {
			b.Fatal("empty menu")
		}
	}
}

func BenchmarkProjectTheMenuTo16Bit(b *testing.B) {
	// The older generation is served by rewriting each line, so it pays more
	// than the copy the newer one does.
	m := buildModel(benchTree(1152, 1450), "bench")
	prt := m.port(firstCardPort + 1)

	b.ReportAllocs()
	for range b.N {
		if len(prt.menu(false)) == 0 {
			b.Fatal("empty menu")
		}
	}
}

func BenchmarkBuildTheTemplate(b *testing.B) {
	// A panel fetches this on opening a node, and it is built per request.
	m := buildModel(benchTree(1152, 1450), "bench")
	prt := m.port(firstCardPort + 1)

	b.ReportAllocs()
	for range b.N {
		if len(buildTemplate(prt)) == 0 {
			b.Fatal("empty template")
		}
	}
}

func BenchmarkMirrorACrosspoint(b *testing.B) {
	// The cost of keeping the two views of one crosspoint together, which now
	// runs on every write.
	s := newServedBench(b, benchTree(1152, 1450))
	prt := s.model.port(firstCardPort + 1)
	v := codec.Value{Command: uint32(router.LvlRoute(700)), Mode: codec.ModeValue, Val: 4}
	ctx := context.Background()

	b.ReportAllocs()
	for range b.N {
		s.syncRouterWrite(ctx, nil, prt, v)
	}
}

// newServedBench builds a provider around a tree without a connection, which
// is all the paths under test need.
func newServedBench(b *testing.B, tree *canonical.Export) *Provider {
	b.Helper()
	p := &Provider{model: buildModel(tree, "bench"), unit: 1}
	p.done = make(chan struct{})
	return p
}
