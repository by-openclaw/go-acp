package rollcall

import (
	"context"
	"testing"

	"dhs/internal/snell-rollcall/codec/router"
)

// Names come in bulk or not at all: a level of sixty-five thousand sources is
// sixty-five thousand round trips the other way, which is what the file
// mechanism exists to avoid.

func TestLevelNamesInBulk(t *testing.T) {
	h, r := routerHarness(t, nil)
	ctx := context.Background()

	for _, tc := range []struct {
		width    int
		firstSrc string
		firstDst string
	}{
		{router.NameWidth8, "M1L1S1", "M1L1D1"},
		{router.NameWidth32, "matrix1 level1 source1", "matrix1 level1 dest1"},
	} {
		names, err := h.plugin.LevelNames(ctx, r, 1, 1, tc.width)
		if err != nil {
			t.Fatalf("width %d: %v", tc.width, err)
		}
		if !names.Verified {
			t.Errorf("width %d: the file did not reproduce the published checksum", tc.width)
		}
		if len(names.Srcs) != 10 || len(names.Dsts) != 10 {
			t.Fatalf("width %d: %d sources and %d destinations",
				tc.width, len(names.Srcs), len(names.Dsts))
		}
		// Counting from one, because that is how the command space counts.
		if got := names.Src(1); got != tc.firstSrc {
			t.Errorf("width %d: source 1 = %q, want %q", tc.width, got, tc.firstSrc)
		}
		if got := names.Dst(1); got != tc.firstDst {
			t.Errorf("width %d: destination 1 = %q, want %q", tc.width, got, tc.firstDst)
		}
	}
}

func TestNamesOutsideTheirRange(t *testing.T) {
	h, r := routerHarness(t, nil)

	names, err := h.plugin.LevelNames(context.Background(), r, 1, 1, router.NameWidth32)
	if err != nil {
		t.Fatalf("LevelNames: %v", err)
	}
	for _, i := range []uint32{0, 11, 9999} {
		if got := names.Src(i); got != "" {
			t.Errorf("source %d = %q, want nothing", i, got)
		}
		if got := names.Dst(i); got != "" {
			t.Errorf("destination %d = %q, want nothing", i, got)
		}
	}
}

func TestNamesAreCachedByTheirChecksum(t *testing.T) {
	h, r := routerHarness(t, nil)
	ctx := context.Background()

	if _, err := h.plugin.LevelNames(ctx, r, 1, 1, router.NameWidth32); err != nil {
		t.Fatalf("LevelNames: %v", err)
	}

	// Take the file away. A second call must not need it: the checksum the
	// controller published is what identifies what we already hold, and
	// re-fetching a level's names on every lookup is the cost this avoids.
	lv, err := r.Level(1, 1)
	if err != nil {
		t.Fatalf("Level: %v", err)
	}
	h.device.mu.Lock()
	delete(h.device.files, lv.Names.Name)
	h.device.mu.Unlock()

	names, err := h.plugin.LevelNames(ctx, r, 1, 1, router.NameWidth32)
	if err != nil {
		t.Fatalf("the second call went back to the device: %v", err)
	}
	if names.Src(2) != "matrix1 level2 source2" && names.Src(2) != "matrix1 level1 source2" {
		t.Errorf("cached names look wrong: %q", names.Src(2))
	}

	// The two widths are cached apart, so one does not answer for the other.
	eight, err := h.plugin.LevelNames(ctx, r, 1, 1, router.NameWidth8)
	if err != nil {
		t.Fatalf("the 8-character names: %v", err)
	}
	if eight.Width != router.NameWidth8 || eight.Src(1) != "M1L1S1" {
		t.Errorf("width %d gave %q", eight.Width, eight.Src(1))
	}
}

func TestNamesFallBackToOneCommandPerName(t *testing.T) {
	h, r := routerHarness(t, nil)
	ctx := context.Background()

	// A controller can name a file its own file service will not serve: the
	// name is a path in the controller's filesystem, and a node's file service
	// is rooted at that node's directory. The vendor Centra does exactly this.
	lv, err := r.Level(2, 1)
	if err != nil {
		t.Fatalf("Level: %v", err)
	}
	h.device.mu.Lock()
	delete(h.device.files, lv.Names.Name)
	h.device.mu.Unlock()

	names, err := h.plugin.LevelNames(ctx, r, 2, 1, router.NameWidth32)
	if err != nil {
		t.Fatalf("LevelNames: %v", err)
	}
	if got := names.Src(3); got != "matrix2 level1 source3" {
		t.Errorf("source 3 = %q", got)
	}
	if got := names.Dst(4); got != "matrix2 level1 dest4" {
		t.Errorf("destination 4 = %q", got)
	}
	// Nothing was verified: there was no file to hash.
	if names.Verified {
		t.Error("names read command by command cannot be verified against a checksum")
	}
	if !hasEvent(h.plugin, EventNamesFileUnreadable) {
		t.Error("falling back should be recorded; it is much slower and silent otherwise")
	}
}

func TestNamesWithNoFileAtAll(t *testing.T) {
	// A level that publishes no names file at all goes straight to the
	// per-command path rather than reporting nothing.
	h, r := routerHarness(t, func(f *fakeRouter) {
		f.values[121+router.MatrixTableSize+0] = f.values[121] // harmless
	})
	ctx := context.Background()

	lv, err := r.Level(1, 2)
	if err != nil {
		t.Fatalf("Level: %v", err)
	}
	lv.Names = RouterFile{}
	lv.Names8 = RouterFile{}

	names, err := h.plugin.LevelNames(ctx, r, 1, 2, router.NameWidth8)
	if err != nil {
		t.Fatalf("LevelNames: %v", err)
	}
	if got := names.Src(1); got != "M1L2S1" {
		t.Errorf("source 1 = %q", got)
	}
}

func TestNamesThatDoNotMatchTheirChecksum(t *testing.T) {
	h, r := routerHarness(t, nil)
	ctx := context.Background()

	lv, err := r.Level(1, 1)
	if err != nil {
		t.Fatalf("Level: %v", err)
	}

	// Replace the file with different names of the same shape. The checksum
	// the controller published no longer describes it.
	other := router.NamesFile{Width: router.NameWidth32}
	for i := 0; i < 10; i++ {
		other.Srcs = append(other.Srcs, "something else")
		other.Dsts = append(other.Dsts, "something else")
	}
	body, err := other.AppendTo(nil)
	if err != nil {
		t.Fatalf("AppendTo: %v", err)
	}
	h.device.mu.Lock()
	h.device.files[lv.Names.Name] = body
	h.device.mu.Unlock()

	names, err := h.plugin.LevelNames(ctx, r, 1, 1, router.NameWidth32)
	if err != nil {
		t.Fatalf("LevelNames: %v", err)
	}
	// The names are kept: they are probably right, and a client with none is
	// worse off than one with names it cannot prove.
	if names.Verified {
		t.Error("a file that does not hash to its published checksum was reported as verified")
	}
	if names.Src(1) != "something else" {
		t.Errorf("the names were discarded: %q", names.Src(1))
	}
	if !hasEvent(h.plugin, EventNamesChecksum) {
		t.Error("a checksum mismatch should be recorded")
	}
}

func TestAssociationNamesAndMappings(t *testing.T) {
	h, r := routerHarness(t, nil)
	ctx := context.Background()

	names, err := h.plugin.AssociationNames(ctx, r, 1, router.NameWidth32)
	if err != nil {
		t.Fatalf("AssociationNames: %v", err)
	}
	if !names.Verified {
		t.Error("the association names did not reproduce their checksum")
	}
	if got := names.Src(2); got != "matrix1 source assoc 2" {
		t.Errorf("source association 2 = %q", got)
	}

	short, err := h.plugin.AssociationNames(ctx, r, 1, router.NameWidth8)
	if err != nil {
		t.Fatalf("AssociationNames at 8: %v", err)
	}
	if got := short.Dst(1); got != "M1DA1" {
		t.Errorf("destination association 1 = %q", got)
	}

	// An association groups one entity per level, which is what makes routing
	// by association route every level at once.
	mf, err := h.plugin.Mappings(ctx, r, 1)
	if err != nil {
		t.Fatalf("Mappings: %v", err)
	}
	if mf.Levels != 2 {
		t.Errorf("%d levels in the mappings, want 2", mf.Levels)
	}
	if len(mf.Srcs) != 10 || len(mf.Srcs[0]) != 2 {
		t.Fatalf("mappings shape = %d associations of %d", len(mf.Srcs), len(mf.Srcs[0]))
	}
	if mf.Srcs[2][0] != 3 || mf.Srcs[2][1] != 3 {
		t.Errorf("association 3 maps to %v, want entity 3 on both levels", mf.Srcs[2])
	}
}

func TestNamesForThingsTheRouterDoesNotHave(t *testing.T) {
	h, r := routerHarness(t, nil)
	ctx := context.Background()

	if _, err := h.plugin.LevelNames(ctx, r, 9, 1, router.NameWidth32); err == nil {
		t.Error("names for a matrix past the end should fail")
	}
	if _, err := h.plugin.AssociationNames(ctx, r, 9, router.NameWidth32); err == nil {
		t.Error("association names for a matrix past the end should fail")
	}
	if _, err := h.plugin.Mappings(ctx, r, 9); err == nil {
		t.Error("mappings for a matrix past the end should fail")
	}

	// A matrix that publishes no association file cannot supply the names, and
	// there is no per-command fallback for associations: the commands do not
	// exist.
	m, err := r.Matrix(2)
	if err != nil {
		t.Fatalf("Matrix: %v", err)
	}
	m.AssocNames = RouterFile{}
	m.AssocNames8 = RouterFile{}
	m.AssocMappings = RouterFile{}

	if _, err := h.plugin.AssociationNames(ctx, r, 2, router.NameWidth32); err == nil {
		t.Error("a matrix with no association names file should say so")
	}
	if _, err := h.plugin.Mappings(ctx, r, 2); err == nil {
		t.Error("a matrix with no mappings file should say so")
	}
}

func TestMappingsThatWillNotDecode(t *testing.T) {
	h, r := routerHarness(t, nil)
	ctx := context.Background()

	m, err := r.Matrix(1)
	if err != nil {
		t.Fatalf("Matrix: %v", err)
	}
	h.device.mu.Lock()
	h.device.files[m.AssocMappings.Name] = []byte{0x01}
	h.device.mu.Unlock()

	if _, err := h.plugin.Mappings(ctx, r, 1); err == nil {
		t.Error("a mappings file too short for its declared contents should fail")
	}

	// And one that decodes but hashes to something else is kept with the
	// mismatch recorded, the same as a names file.
	other := router.MappingsFile{Levels: 2}
	for i := 0; i < 10; i++ {
		other.Srcs = append(other.Srcs, []uint32{9, 9})
		other.Dsts = append(other.Dsts, []uint32{9, 9})
	}
	h.device.mu.Lock()
	h.device.files[m.AssocMappings.Name] = other.AppendTo(nil)
	h.device.mu.Unlock()

	got, err := h.plugin.Mappings(ctx, r, 1)
	if err != nil {
		t.Fatalf("Mappings: %v", err)
	}
	if got.Srcs[0][0] != 9 {
		t.Errorf("the mappings were discarded: %v", got.Srcs[0])
	}
	if !hasEvent(h.plugin, EventNamesChecksum) {
		t.Error("a mappings checksum mismatch should be recorded")
	}
}

func TestNamesWhenTheDeviceStopsAnswering(t *testing.T) {
	h, r := routerHarness(t, nil)
	ctx := context.Background()

	lv, err := r.Level(2, 2)
	if err != nil {
		t.Fatalf("Level: %v", err)
	}
	h.device.mu.Lock()
	delete(h.device.files, lv.Names.Name)
	h.device.mu.Unlock()

	// The file is gone and the per-name commands are refused too, so there is
	// nothing left to fall back to.
	cmd, err := r.srcCommand(2, 2, 1, router.OffSrcName32)
	if err != nil {
		t.Fatalf("srcCommand: %v", err)
	}
	h.device.mu.Lock()
	h.device.routers[2].refuse[cmd] = true
	h.device.mu.Unlock()

	if _, err := h.plugin.LevelNames(ctx, r, 2, 2, router.NameWidth32); err == nil {
		t.Error("names that cannot be read either way should fail")
	}

	// The same on the destination side.
	dst, err := r.destCommand(2, 2, 1, router.OffDestName32)
	if err != nil {
		t.Fatalf("destCommand: %v", err)
	}
	h.device.mu.Lock()
	delete(h.device.routers[2].refuse, cmd)
	h.device.routers[2].refuse[dst] = true
	h.device.mu.Unlock()

	if _, err := h.plugin.LevelNames(ctx, r, 2, 2, router.NameWidth32); err == nil {
		t.Error("destination names that cannot be read should fail")
	}
}
