package rollcall

import (
	"context"
	"fmt"

	"dhs/internal/snell-rollcall/codec/router"
)

// Names come in bulk or not at all.
//
// A level of a large router has tens of thousands of sources and destinations,
// each with three names. Reading them one command at a time is tens of
// thousands of round trips, which is why the controller publishes them as files
// instead: one fetch per level per width, with a checksum that lets a client
// skip the fetch entirely when what it already has is still current.
//
// The checksum is computed from the names, not from the bytes — each name
// hashed with its own index and the hashes summed — so it survives a controller
// rewriting the file and changes when any single name does.

// Names are the sources and destinations of one level.
type Names struct {
	// Width is the field width these came from: 8 or 32.
	Width int

	// Srcs and Dsts are indexed from zero, so source n is Srcs[n-1].
	Srcs []string
	Dsts []string

	// CRC is what the controller published alongside the filename.
	CRC uint32

	// Verified says whether the file we read reproduces that checksum. A file
	// that does not is still returned: the names are probably right and a
	// client with no names at all is worse off, but the mismatch is recorded.
	Verified bool
}

// Name returns a source's name, counting from one.
func (n Names) Src(i uint32) string { return nameAt(n.Srcs, i) }

// Dst returns a destination's name, counting from one.
func (n Names) Dst(i uint32) string { return nameAt(n.Dsts, i) }

func nameAt(names []string, i uint32) string {
	if i < 1 || int(i) > len(names) {
		return ""
	}
	return names[i-1]
}

// LevelNames fetches the names of one level at the given width.
//
// The cache is keyed by the checksum the controller published, so a second call
// costs nothing and a controller that renames anything invalidates it by
// itself.
func (p *Plugin) LevelNames(ctx context.Context, r *RouterInterface,
	matrix, level uint32, width int) (Names, error) {

	lv, err := r.Level(matrix, level)
	if err != nil {
		return Names{}, err
	}

	file := lv.Names
	nameOff, dstOff := uint32(router.OffSrcName32), uint32(router.OffDestName32)
	if width == router.NameWidth8 {
		file, nameOff, dstOff = lv.Names8, router.OffSrcName8, router.OffDestName8
	}

	if !file.Empty() {
		names, err := p.namesFile(ctx, r.Slot, file, width, int(lv.Srcs.Count), int(lv.Dsts.Count))
		if err == nil {
			return names, nil
		}
		// The controller named a file it will not serve. That happens: the
		// name is a path in the controller's own filesystem, and the file
		// service on a node is rooted at that node's own directory, so the two
		// need not meet. One command per name is what is left.
		p.fire(EventNamesFileUnreadable, fmt.Sprintf(
			"%s could not be read (%v); falling back to one command per name", file.Name, err))
	}

	return p.namesByCommand(ctx, r, lv, width, nameOff, dstOff)
}

// namesByCommand reads a level's names one command at a time.
//
// This is what the file mechanism exists to avoid: a level of sixty-five
// thousand sources costs sixty-five thousand round trips here. It is the
// fallback rather than the method, and a caller reaching it on a large level
// should expect to wait.
func (p *Plugin) namesByCommand(ctx context.Context, r *RouterInterface,
	lv *RouterLevel, width int, srcOff, dstOff uint32) (Names, error) {

	names := Names{Width: width}

	for i := uint32(1); i <= lv.Srcs.Count; i++ {
		cmd, ok := lv.Srcs.Field(i, srcOff)
		if !ok {
			break
		}
		n, err := p.readString(ctx, r.Slot, uint32(cmd))
		if err != nil {
			return names, err
		}
		names.Srcs = append(names.Srcs, n)
	}
	for i := uint32(1); i <= lv.Dsts.Count; i++ {
		cmd, ok := lv.Dsts.Field(i, dstOff)
		if !ok {
			break
		}
		n, err := p.readString(ctx, r.Slot, uint32(cmd))
		if err != nil {
			return names, err
		}
		names.Dsts = append(names.Dsts, n)
	}
	return names, nil
}

// AssociationNames fetches a matrix's association names.
//
// An association groups one entity per level, and it is what a panel actually
// shows: an operator picks a destination association and takes a source
// association, and the controller routes every level underneath.
func (p *Plugin) AssociationNames(ctx context.Context, r *RouterInterface,
	matrix uint32, width int) (Names, error) {

	m, err := r.Matrix(matrix)
	if err != nil {
		return Names{}, err
	}

	file := m.AssocNames
	if width == router.NameWidth8 {
		file = m.AssocNames8
	}
	if file.Empty() {
		return Names{}, fmt.Errorf(
			"rollcall: matrix %d publishes no %d-character association names file", matrix, width)
	}

	return p.namesFile(ctx, r.Slot, file, width,
		int(m.SrcAssocs.Count), int(m.DstAssocs.Count))
}

// Mappings fetches which entity each association reaches on each level.
func (p *Plugin) Mappings(ctx context.Context, r *RouterInterface,
	matrix uint32) (router.MappingsFile, error) {

	m, err := r.Matrix(matrix)
	if err != nil {
		return router.MappingsFile{}, err
	}
	if m.AssocMappings.Empty() {
		return router.MappingsFile{}, fmt.Errorf(
			"rollcall: matrix %d publishes no association mappings file", matrix)
	}

	body, err := p.ReadFile(ctx, r.Slot, m.AssocMappings.Name)
	if err != nil {
		return router.MappingsFile{}, err
	}

	mf, err := router.DecodeMappingsFile(body, len(m.Levels),
		int(m.SrcAssocs.Count), int(m.DstAssocs.Count))
	if err != nil {
		return router.MappingsFile{}, fmt.Errorf("rollcall: %s: %w", m.AssocMappings.Name, err)
	}
	if crc := mf.CRC(); crc != m.AssocMappings.CRC {
		p.fire(EventNamesChecksum, fmt.Sprintf(
			"%s hashes to %08X, and the router published %08X",
			m.AssocMappings.Name, crc, m.AssocMappings.CRC))
	}
	return mf, nil
}

// namesFile fetches and decodes one names file, checking what came back
// against the checksum the controller published.
func (p *Plugin) namesFile(ctx context.Context, slot int, file RouterFile,
	width, srcs, dsts int) (Names, error) {

	if cached, ok := p.cachedNames(file.CRC, width); ok {
		return cached, nil
	}

	body, err := p.ReadFile(ctx, slot, file.Name)
	if err != nil {
		return Names{}, err
	}

	nf, err := router.DecodeNamesFile(body, width, srcs, dsts)
	if err != nil {
		return Names{}, fmt.Errorf("rollcall: %s: %w", file.Name, err)
	}

	names := Names{Width: width, Srcs: nf.Srcs, Dsts: nf.Dsts, CRC: file.CRC}
	if crc := nf.CRC(); crc == file.CRC {
		names.Verified = true
	} else {
		// The names are probably right and a client with none at all is worse
		// off, so they are kept; the mismatch is what gets reported.
		p.fire(EventNamesChecksum, fmt.Sprintf(
			"%s hashes to %08X, and the router published %08X", file.Name, crc, file.CRC))
	}

	p.cacheNames(file.CRC, width, names)
	return names, nil
}

// namesKey identifies a cached names file. The checksum alone would collide
// between widths on a level whose two files happen to hash alike.
type namesKey struct {
	crc   uint32
	width int
}

func (p *Plugin) cachedNames(crc uint32, width int) (Names, bool) {
	p.mu.RLock()
	defer p.mu.RUnlock()
	n, ok := p.names[namesKey{crc: crc, width: width}]
	return n, ok
}

func (p *Plugin) cacheNames(crc uint32, width int, n Names) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.names == nil {
		p.names = make(map[namesKey]Names)
	}
	p.names[namesKey{crc: crc, width: width}] = n
}
