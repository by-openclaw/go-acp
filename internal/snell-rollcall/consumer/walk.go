package rollcall

import (
	"context"
	"fmt"
	"strings"

	"dhs/internal/consumer"
	"dhs/internal/snell-rollcall/codec"
	"dhs/internal/snell-rollcall/session"
)

// slotTree is a walked menu: the lines as they arrived, plus the indexes that
// make a label or a path resolvable without walking again.
type slotTree struct {
	slot    int
	lines   []menuLine
	byLabel map[string]int // lowercased label to line index
	byPath  map[string]int // lowercased dotted path to line index
	byCmd   map[uint32]int
	objects []consumer.Object
}

// menuLine is one menu entry, normalised across the two generations so that
// nothing above this point has to know which one delivered it.
type menuLine struct {
	Index    uint32
	Style    codec.Style
	Command  uint32
	MinRange int32
	MaxRange int32
	Step     uint32
	DivScale uint16
	Text     string
	Param    string

	// path is the line's position in the tree, containers first.
	path []string

	// children are the indices of the lines directly below a container.
	children []int
}

// Scale returns the divisor with the zero-means-one rule applied.
func (m menuLine) Scale() uint16 {
	if m.DivScale == 0 {
		return 1
	}
	return m.DivScale
}

// AccessGated reports whether the server substituted this line because it sits
// above the session's user level.
func (m menuLine) AccessGated() bool {
	return codec.AccessGated(m.Style, m.Command, m.Text)
}

// Walk enumerates every menu line on a slot and returns them as objects.
//
// A slot is a port on the gateway unit, and a menu is walked on that port's
// own session, which is what makes the lines belong to the card rather than to
// the frame.
func (p *Plugin) Walk(ctx context.Context, slot int) ([]consumer.Object, error) {
	tree, err := p.walkTree(ctx, slot)
	if err != nil {
		return nil, err
	}
	return tree.objects, nil
}

// walkTree walks a slot and caches the result.
func (p *Plugin) walkTree(ctx context.Context, slot int) (*slotTree, error) {
	if slot < 0 || slot > 0xFF {
		return nil, fmt.Errorf("rollcall: slot %d is outside the port range", slot)
	}

	s, err := p.session(ctx, slot)
	if err != nil {
		return nil, err
	}

	var lines []menuLine
	if s.Uses32Bit() {
		lines, err = p.walk32(ctx, s)
	} else {
		lines, err = p.walk16(ctx, s)
	}
	if err != nil {
		return nil, err
	}

	tree := buildTree(slot, lines, p)

	p.mu.Lock()
	p.trees[slot] = tree
	p.mu.Unlock()
	return tree, nil
}

// walk16 reads a menu in the original generation.
//
// The transfer is stateful: a request opens it, the server remembers the base,
// and each fetch names an offset from that base. So the whole walk has to
// happen on one session with nothing interleaved, which the session layer's
// one-in-flight rule already guarantees.
func (p *Plugin) walk16(ctx context.Context, s *session.Session) ([]menuLine, error) {
	var lines []menuLine

	err := session.Walk(ctx, s, codec.MsgGetFunc, []byte{0, 0},
		func(_ int, f codec.Frame) error {
			if f.Type != codec.MsgRetFunc {
				// A server may answer an item with something else; skip it
				// rather than abandon the menu.
				return nil
			}
			fn, err := codec.DecodeFunc(f.Payload)
			if err != nil {
				return err
			}
			lines = append(lines, menuLine{
				Index:    uint32(fn.MenuIndex),
				Style:    fn.Style,
				Command:  uint32(fn.Command),
				MinRange: fn.MinRange,
				MaxRange: fn.MaxRange,
				Step:     uint32(fn.Step),
				DivScale: fn.DivScale,
				Text:     fn.Text,
				Param:    fn.Param,
			})
			return nil
		})
	if err != nil {
		return nil, fmt.Errorf("rollcall: walk menu: %w", err)
	}
	return lines, nil
}

// walk32 reads a menu in the long-string generation.
//
// Every item is fetched by absolute index and the server holds no state, so a
// caller could fetch them in any order. They are read in order anyway, because
// a menu is a tree written depth-first and the spans only mean anything read
// that way.
func (p *Plugin) walk32(ctx context.Context, s *session.Session) ([]menuLine, error) {
	var lines []menuLine

	err := session.WalkMenu32(ctx, s, 0, func(m codec.MenuItem) error {
		lines = append(lines, menuLine{
			Index:    m.MenuIndex,
			Style:    m.Style,
			Command:  m.Command,
			MinRange: m.MinRange,
			MaxRange: m.MaxRange,
			Step:     m.Step,
			DivScale: m.DivScale,
			Text:     m.Text,
			Param:    m.Param,
		})
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("rollcall: walk menu: %w", err)
	}
	return lines, nil
}

// buildTree turns a flat menu into a tree and an object list.
//
// This is where the menu model earns its comment. The array is flat, and a
// container line's step field is the span of its whole subtree, not the count
// of its immediate children. So reconstructing the tree is a pre-order walk
// that consumes that many following entries, recursing into each container it
// meets. Reading the step as a child count nests everything below the second
// level in the wrong place, and the result still looks like a tree.
func buildTree(slot int, lines []menuLine, p *Plugin) *slotTree {
	t := &slotTree{
		slot:    slot,
		lines:   lines,
		byLabel: make(map[string]int, len(lines)),
		byPath:  make(map[string]int, len(lines)),
		byCmd:   make(map[uint32]int, len(lines)),
	}

	var assign func(start, end int, prefix []string) int
	assign = func(start, end int, prefix []string) int {
		i := start
		for i < end {
			line := &t.lines[i]
			line.path = append(append([]string{}, prefix...), line.Text)

			span := 0
			if line.Style.Container() {
				span = int(line.Step)
				if start+1+span > end {
					// The line claims a subtree longer than what follows it.
					// Clamp rather than run off the end, and say so.
					if p != nil {
						p.fire(EventMenuSpanOverruns, fmt.Sprintf(
							"slot %d line %d claims a span of %d with %d lines left",
							slot, line.Index, span, end-i-1))
					}
					span = end - i - 1
				}
			}
			if span > 0 {
				kids := assign(i+1, i+1+span, line.path)
				for k := i + 1; k < i+1+span; k++ {
					if len(t.lines[k].path) == len(line.path)+1 {
						line.children = append(line.children, k)
					}
				}
				_ = kids
			}
			i += 1 + span
		}
		return i
	}
	assign(0, len(lines), nil)

	for i := range t.lines {
		line := &t.lines[i]

		if label := strings.ToLower(line.Text); label != "" {
			if _, taken := t.byLabel[label]; !taken {
				t.byLabel[label] = i
			}
		}
		if path := strings.ToLower(strings.Join(line.path, ".")); path != "" {
			if _, taken := t.byPath[path]; !taken {
				t.byPath[path] = i
			}
		}
		if line.Command != 0 {
			if prev, taken := t.byCmd[line.Command]; taken {
				if p != nil {
					p.fire(EventDuplicateCommand, fmt.Sprintf(
						"slot %d command %d is claimed by both %q and %q",
						slot, line.Command, t.lines[prev].Text, line.Text))
				}
			} else {
				t.byCmd[line.Command] = i
			}
		}
		if line.AccessGated() && p != nil {
			p.fire(EventAccessGated, fmt.Sprintf(
				"slot %d line %d is above the session user level", slot, line.Index))
		}
	}

	t.objects = make([]consumer.Object, 0, len(t.lines))
	for i := range t.lines {
		t.objects = append(t.objects, t.lines[i].object(slot))
	}
	return t
}

// object renders one menu line as a neutral object.
func (m menuLine) object(slot int) consumer.Object {
	o := consumer.Object{
		Slot:   slot,
		ID:     int(m.Command),
		Path:   m.path,
		Label:  m.Text,
		Kind:   styleKind(m.Style),
		Access: styleAccess(m.Style),
	}
	if len(m.path) > 0 {
		o.Group = m.path[0]
	}

	// A container is a node in the tree rather than a value, so it carries no
	// range and is marked as the section header it is.
	if m.Style.Container() {
		o.SubGroupMarker = true
		return o
	}

	switch o.Kind {
	case consumer.KindInt:
		o.Min = int64(m.MinRange)
		o.Max = int64(m.MaxRange)
		if m.Step > 0 {
			o.Step = int64(m.Step)
		}
		// The format string is the closest thing a menu line has to a unit:
		// "%d dB" says the number is decibels.
		o.Unit = unitFromFormat(m.Param)

	case consumer.KindString:
		// A string line's range is a length rather than a value.
		if m.MaxRange > 0 {
			o.MaxLen = int(m.MaxRange)
		}

	case consumer.KindBool:
		// A checkbox's "on" value lives in the minimum-range field, which is
		// conventionally one and occasionally not.
		o.Min = int64(0)
		o.Max = int64(1)
	}
	return o
}

// unitFromFormat extracts a unit from a printf format string.
//
// A menu line's format is what the device wants displayed, so "%0.1f dB"
// carries both the precision and the unit. Everything after the conversion is
// the unit; a format with no conversion has none.
func unitFromFormat(format string) string {
	i := strings.IndexByte(format, '%')
	if i < 0 {
		return ""
	}
	rest := format[i+1:]
	for j := 0; j < len(rest); j++ {
		switch rest[j] {
		case 'd', 'f', 'g', 'e', 's', 'x', 'X', 'u', 'c':
			return strings.TrimSpace(rest[j+1:])
		}
	}
	return ""
}

// tree returns a slot's walked menu, walking it if there is none.
//
// This is what makes a label or a path resolve cold: the first request that
// needs one walks, and every later one uses what it found.
func (p *Plugin) tree(ctx context.Context, slot int) (*slotTree, error) {
	p.mu.RLock()
	t := p.trees[slot]
	p.mu.RUnlock()

	if t != nil {
		return t, nil
	}
	return p.walkTree(ctx, slot)
}

// resolve finds the menu line a request addresses.
//
// The order follows the neutral contract: an explicit path first, because it
// is unambiguous; then a label; then a command number. A command number of
// zero with no label is not an address, because zero is what a container and a
// gated placeholder both carry.
func (t *slotTree) resolve(req consumer.ValueRequest) (*menuLine, error) {
	if req.Path != "" {
		if i, ok := t.byPath[strings.ToLower(req.Path)]; ok {
			return &t.lines[i], nil
		}
		// A path that names one line is also accepted, because a caller that
		// knows a label often writes it as a path.
		if i, ok := t.byLabel[strings.ToLower(req.Path)]; ok {
			return &t.lines[i], nil
		}
		return nil, fmt.Errorf("rollcall: no object at path %q on slot %d", req.Path, t.slot)
	}

	if req.Label != "" {
		if i, ok := t.byLabel[strings.ToLower(req.Label)]; ok {
			return &t.lines[i], nil
		}
		return nil, fmt.Errorf("rollcall: no object labelled %q on slot %d", req.Label, t.slot)
	}

	if req.ID != 0 {
		if i, ok := t.byCmd[uint32(req.ID)]; ok {
			return &t.lines[i], nil
		}
		// A command the menu does not list is still addressable: the device
		// knows its own command set better than a cached walk does.
		return &menuLine{Command: uint32(req.ID), Style: codec.StyleNumber}, nil
	}

	return nil, fmt.Errorf("rollcall: request names no object: give a path, a label or an id")
}
