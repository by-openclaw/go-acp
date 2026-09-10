package rollcall

import (
	"context"
	"fmt"
	"sort"
	"strconv"

	"dhs/internal/consumer"
	"dhs/internal/export/canonical"
	"dhs/internal/snell-rollcall/codec"
)

// Canonicalize turns what has been walked into a canonical export.
//
// It is the reverse of what the provider does, and the two are meant to meet
// in the middle: a tree served as a RollCall device, walked back, should
// describe the same objects. That round trip is what makes a device model
// worth caching — walk a card once, keep the tree, and every later session
// resolves a label without asking the device again.
//
// Shape:
//
//	device (Node, oid="1", identifier=host)
//	├── slot-0 (Node)
//	│   ├── <container> (Node)      a menu line with a subtree
//	│   │   └── <object> (Parameter)
//	│   └── <object> (Parameter)    a menu line with a command
//	└── slot-N ...
//
// Only slots already walked appear. A container in RollCall is a menu line
// whose step spans a subtree, so the nesting here is the nesting the device
// published rather than one imposed on it.
func (p *Plugin) Canonicalize(ctx context.Context) (*canonical.Export, error) {
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("rollcall: canonicalize: %w", err)
	}

	p.mu.RLock()
	host := p.addr
	trees := make(map[int]*slotTree, len(p.trees))
	for slot, t := range p.trees {
		trees[slot] = t
	}
	p.mu.RUnlock()

	slots := make([]int, 0, len(trees))
	for slot := range trees {
		slots = append(slots, slot)
	}
	sort.Ints(slots)

	children := make([]canonical.Element, 0, len(slots))
	for _, slot := range slots {
		children = append(children, slotNode(slot, trees[slot]))
	}

	identifier := "device"
	if host != "" {
		identifier = host
	}

	root := &canonical.Node{
		Header: canonical.Header{
			Number:     1,
			Identifier: identifier,
			Path:       identifier,
			OID:        "1",
			IsOnline:   true,
			Access:     canonical.AccessRead,
			Children:   children,
		},
	}
	if len(children) == 0 {
		root.Children = canonical.EmptyChildren()
	}
	return &canonical.Export{Root: root}, nil
}

// slotNode builds one slot from its walked menu.
func slotNode(slot int, t *slotTree) *canonical.Node {
	ident := "slot-" + strconv.Itoa(slot)
	oid := "1." + strconv.Itoa(slot+1)

	node := &canonical.Node{
		Header: canonical.Header{
			Number:     slot,
			Identifier: ident,
			Path:       ident,
			OID:        oid,
			IsOnline:   true,
			Access:     canonical.AccessRead,
			Children:   canonical.EmptyChildren(),
		},
	}

	// The roots of the menu are the lines nothing else contains.
	contained := make(map[int]bool, len(t.lines))
	for _, line := range t.lines {
		for _, child := range line.children {
			contained[child] = true
		}
	}

	var elems []canonical.Element
	for i := range t.lines {
		if contained[i] {
			continue
		}
		elems = append(elems, menuElement(t, i, ident, oid, len(elems)+1))
	}
	if len(elems) > 0 {
		node.Children = elems
	}
	return node
}

// menuElement turns one menu line into a canonical element: a node when it
// contains others, a parameter when it carries a value.
func menuElement(t *slotTree, index int, parentPath, parentOID string, number int) canonical.Element {
	line := t.lines[index]

	ident := line.Text
	if ident == "" {
		ident = "line-" + strconv.FormatUint(uint64(line.Index), 10)
	}
	path := parentPath + "." + ident
	oid := parentOID + "." + strconv.Itoa(number)

	header := canonical.Header{
		Number:     number,
		Identifier: ident,
		Path:       path,
		OID:        oid,
		IsOnline:   true,
		Access:     accessOf(line.Style),
	}

	if len(line.children) > 0 {
		children := make([]canonical.Element, 0, len(line.children))
		for _, child := range line.children {
			children = append(children, menuElement(t, child, path, oid, len(children)+1))
		}
		// A container holds things rather than a value of its own.
		header.Access = canonical.AccessRead
		header.Children = children
		return &canonical.Node{Header: header}
	}

	// A line with no command and no children is a separator or a placeholder
	// the server substituted for something above this user level. It names
	// nothing that can be read, so it is a node rather than a parameter.
	if line.Command == 0 {
		header.Children = canonical.EmptyChildren()
		return &canonical.Node{Header: header}
	}

	return parameterFor(line, header)
}

// parameterFor maps a menu line onto a canonical parameter.
func parameterFor(line menuLine, header canonical.Header) *canonical.Parameter {
	param := &canonical.Parameter{Header: header}

	switch styleKind(line.Style) {
	case consumer.KindBool:
		param.Type = canonical.ParamBoolean
		param.Value = false

	case consumer.KindString:
		param.Type = canonical.ParamString
		param.Value = ""
		if line.MaxRange > 0 {
			param.Maximum = float64(line.MaxRange)
		}

	default:
		// A divisor means the device is carrying a real as a scaled integer,
		// and the canonical form says so with a factor rather than by
		// pre-dividing: the wire value stays recoverable.
		if line.DivScale > 1 {
			param.Type = canonical.ParamReal
			factor := int64(line.DivScale)
			param.Factor = &factor
			param.Value = 0.0
			param.Minimum = float64(line.MinRange) / float64(line.DivScale)
			param.Maximum = float64(line.MaxRange) / float64(line.DivScale)
			if line.Step > 0 {
				param.Step = float64(line.Step) / float64(line.DivScale)
			}
		} else {
			param.Type = canonical.ParamInteger
			param.Value = int64(0)
			param.Minimum = float64(line.MinRange)
			param.Maximum = float64(line.MaxRange)
			if line.Step > 0 {
				param.Step = float64(line.Step)
			}
		}
	}

	// The format string is the only place a unit can travel on this protocol,
	// so it is kept whole rather than parsed for one.
	if line.Param != "" {
		format := line.Param
		param.Format = &format
	}
	return param
}

// accessOf maps a menu line's style onto the canonical access string.
//
// A disabled line is one the device says may be read and not written, which is
// what read-only means; hidden is a level gate rather than an access, and a
// line we can see is one we may read.
func accessOf(style codec.Style) string {
	if style.Disabled() {
		return canonical.AccessRead
	}
	switch style.Kind() {
	case codec.StyleDisplay, codec.StyleList, codec.StyleTiled, codec.StylePartial:
		return canonical.AccessRead
	default:
		return canonical.AccessReadWrite
	}
}

// ExportCanonical is Canonicalize under the name the device-model cache asks
// for. The cache and the capture want the same thing and there is no reason to
// build it twice.
func (p *Plugin) ExportCanonical(ctx context.Context) (*canonical.Export, error) {
	return p.Canonicalize(ctx)
}

// IdentityProbe names the card in a slot, so one device model is kept per card
// type rather than per slot: a frame with twelve identical cards walks one of
// them and reads the model back eleven times.
//
// The name a unit reports is not the key. It is user-editable — an operator
// calls one card "CAM 1 PROC" and the identical card beside it "SPARE" — and
// two cards with different labels have the same command set. What identifies a
// model is the type the vendor assigned and the command set the firmware
// implements, which is exactly what the specification says: a unit's ID plus
// its command set determine what commands exist.
//
// It costs one message. A walk here would be minutes on a large card, and this
// runs before anything else wants the link.
func (p *Plugin) IdentityProbe(ctx context.Context, slot int) (string, error) {
	s, err := p.session(ctx, slot)
	if err != nil {
		return "", err
	}

	reply, err := s.Do(ctx, codec.MsgGetID, nil)
	if err != nil {
		return "", fmt.Errorf("rollcall: identity of slot %d: %w", slot, err)
	}
	id, err := codec.DecodeID(reply.Payload)
	if err != nil {
		return "", fmt.Errorf("rollcall: identity of slot %d: %w", slot, err)
	}

	// The key is written by the codec, beside the function that reads it
	// back: a provider serving this DM turns the key into the identity the
	// card gave, and the two halves cannot drift when they sit together. A
	// type the vendor's table does not list is filed by its number.
	return codec.DMKey(id.TypeID, id.Version), nil
}
