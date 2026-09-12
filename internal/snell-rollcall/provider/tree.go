// Package rollcall serves a canonical tree as a Snell RollCall device.
//
// A RollCall gateway is a unit whose ports are the cards in its frame, so a
// tree becomes one: the root is the frame, each child of the root is a port,
// and everything below a port is that card's menu.
//
// # What a client sees
//
// A menu is a flat array with nested spans, so the tree is flattened
// depth-first and every container line records the size of its whole subtree.
// That is the shape a client expects, and it is the shape the vendor's own
// Control Panel walks.
//
// Values are the same objects seen through the control service: a menu line
// says what a command is and what it may hold, and a value read says what it
// holds now.
//
// # Both generations from one tree
//
// Which one a client gets is decided by the service mask in its Call, not by
// what we are. A 16-bit client sees the same menu with fixed-width labels and
// 16-bit command numbers; a long-string client sees the full ones. A command
// number that will not fit the older generation is not offered to it at all,
// because a truncated number addresses a different command.
package rollcall

import (
	"fmt"
	"strings"
	"sync"

	"dhs/internal/export/canonical"
	"dhs/internal/snell-rollcall/codec"
	"dhs/internal/snell-rollcall/codec/router"
)

// line is one menu entry the provider serves.
type line struct {
	Index    uint32
	Style    codec.Style
	Command  uint32
	MinRange int32
	MaxRange int32
	Step     uint32
	DivScale uint16
	Text     string
	Param    string

	// path is the dotted identifier path this line came from, which is what a
	// caller uses to change the value behind it.
	path string
}

// menuItem renders the line in the long-string form.
func (l line) menuItem() codec.MenuItem {
	return codec.MenuItem{
		MenuIndex: l.Index,
		Style:     l.Style,
		Command:   l.Command,
		MinRange:  l.MinRange,
		MaxRange:  l.MaxRange,
		Step:      l.Step,
		DivScale:  l.DivScale,
		Text:      l.Text,
		Param:     l.Param,
	}
}

// port is one slot: a card, its menu, and its current values.
type port struct {
	number uint8
	id     codec.ID

	mu     sync.RWMutex
	lines  []line
	byCmd  map[uint32]int
	byPath map[string]int
	values map[uint32]codec.Value

	// display holds the unit's status lines, which are not menu objects.
	display map[int16]string

	// router is set on a node that serves the Full Control command space
	// rather than a menu. Nil on a card.
	router *routerModel

	// level is set on a node that serves one level of that router. It is the
	// node an XY panel is drawn on, and the one a crosspoint is taken on;
	// the matrix node above it only says where to find it.
	level *routerLevel

	// matrix is set on a node that names a matrix and nothing else.
	matrix *routerMatrix
}

// model is the whole served device: a frame and its ports.
type model struct {
	mu    sync.RWMutex
	frame codec.ID
	ports map[uint8]*port
	order []uint8
}

// buildModel turns a canonical tree into a served device.
//
// The root becomes the frame's identity and each of its children becomes a
// port. A tree with no children is still a device: it becomes a single port,
// because a client that finds a gateway with no cards has nothing to walk and
// no way to tell that from a fault.
func buildModel(tree *canonical.Export, name string) *model {
	return buildModelAt(tree, name, nil)
}

// buildModelAt is buildModel with the cards on the ports a manifest names:
// ports[i] is where the i-th card in the tree answers. A card with no port
// named takes the next one after the highest before it, counting from one,
// and the router nodes follow the highest card port either way.
func buildModelAt(tree *canonical.Export, name string, ports []uint8) *model {
	m := &model{
		frame: codec.ID{
			Services: codec.SvcMenus | codec.SvcControl | codec.SvcDisplay |
				codec.SvcFile | codec.SvcMap | codec.SvcPorts | codec.SvcLongStr,
			TypeID:  codec.TypeIDPCSoftware,
			Version: codec.Version{Major: 1, Minor: 0, Alpha: ' ', CmdSet: 1},
			Name:    codec.TruncateFixed(name, codec.MaxTextSize),
		},
		ports: make(map[uint8]*port),
	}

	if tree == nil || tree.Root == nil {
		m.addPort(newPort(firstCardPort, name, nil))
		return m
	}

	children := tree.Root.Common().Children
	if len(children) == 0 {
		// One port carrying the root itself, so a client has something to
		// walk rather than an empty frame it cannot distinguish from a fault.
		m.addPort(newPort(firstCardPort, identifierOf(tree.Root), []canonical.Element{tree.Root}))
		return m
	}

	// A matrix is a router, not a card, and a router is not one node. It is
	// built after the cards so that every matrix in the tree lands in one
	// model: the Full Control tables describe a plant rather than a matrix,
	// and two of them built separately would each claim to be the whole thing.
	next := int(firstCardPort)
	var matrices []*canonical.Matrix
	card := 0

	for _, child := range children {
		if next >= int(firstClientPort) {
			// Past here the port numbers are the ones a gateway hands out to
			// its own clients, so a card there would be addressed as one.
			break
		}
		if mx, ok := child.(*canonical.Matrix); ok {
			matrices = append(matrices, mx)
			continue
		}
		n := next
		if card < len(ports) {
			// Where the manifest put it. A real frame does not number its
			// cards consecutively — the IQ frame answers on 01, 03, 05 … —
			// and a client addresses a card by the port it is really on.
			n = int(ports[card])
		}
		card++
		m.addPort(newPort(uint8(n), identifierOf(child), []canonical.Element{child}))
		if n >= next {
			next = n + 1
		}
	}

	if len(matrices) > 0 {
		r := buildRouter(name, matrices)
		for i := range r.matrices {
			mx := &r.matrices[i]
			if next >= int(firstClientPort) {
				break
			}
			m.addPort(newRouterMatrixPort(uint8(next), mx.name, mx))
			next++
			for j := range mx.levels {
				if next >= int(firstClientPort) {
					break
				}
				m.addPort(newRouterLevelPort(uint8(next), mx.levels[j].name, &mx.levels[j]))
				next++
			}
		}
		// One node for the whole plant's tables, last, so the matrices it
		// describes are already in the port list a client just enumerated.
		if next < int(firstClientPort) {
			m.addPort(newXYPanelPort(uint8(next), name, r))
			next++
		}

		// And one for the cables between the matrices, when there are any. A
		// plant of one matrix has nowhere for a cable to go, and a node
		// offering an empty list is a page an operator opens once.
		if len(r.tielines) > 0 && next < int(firstClientPort) {
			m.addPort(newTielinePort(uint8(next), r))
		}
	}

	// The gateway is a unit too and a panel expects to open it. It is not in
	// the order, because the order is the card slots a port list enumerates.
	m.ports[0] = newGatewayPort(m.frame)

	return m
}

// firstCardPort is the port the first card in the frame answers on.
//
// Slots are numbered from one because port zero is the gateway itself: a
// client asks port zero what the unit is and each card port what that card is,
// and a card at zero would answer both questions with one identity.
const firstCardPort uint8 = 1

func identifierOf(e canonical.Element) string {
	if e == nil {
		return ""
	}
	return e.Common().Identifier
}

func (m *model) addPort(p *port) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.ports[p.number] = p
	m.order = append(m.order, p.number)
}

// port returns the card in a slot, or nil.
func (m *model) port(n uint8) *port {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.ports[n]
}

// portNumbers returns the slots in order.
func (m *model) portNumbers() []uint8 {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return append([]uint8(nil), m.order...)
}

// newPort flattens a subtree into a menu.
//
// Command numbers are assigned from one upwards in the order lines appear.
// Zero is skipped because it is what a container and a level-gated placeholder
// both carry, so a client cannot tell a real command numbered zero from
// neither of those.
func newPort(number uint8, name string, roots []canonical.Element) *port {
	p := &port{
		number: number,
		id: codec.ID{
			// What the card serves, which includes the long-string
			// generation: the menu and the values come from one model and are
			// projected per session, so every port serves both. A port that
			// advertised less would have clients negotiating the older
			// generation against a card that can speak the newer one.
			// File as well, because a card serves its own template: the
			// vendor's cards each carry a TEMPLATE.ZIP, and the real IQ frame
			// advertises Menus, Control and File on every one of its cards.
			Services: codec.SvcMenus | codec.SvcControl | codec.SvcDisplay |
				codec.SvcFile | codec.SvcLongStr,
			TypeID:  codec.TypeIDPCSoftware,
			Version: codec.Version{Major: 1, Minor: 0, Alpha: ' ', CmdSet: 1},
			Name:    codec.TruncateFixed(name, codec.MaxTextSize),
		},
		byCmd:   make(map[uint32]int),
		byPath:  make(map[string]int),
		values:  make(map[uint32]codec.Value),
		display: make(map[int16]string),
	}

	var next uint32 = 1
	var flatten func(e canonical.Element) int
	flatten = func(e canonical.Element) int {
		h := e.Common()
		idx := len(p.lines)
		l := line{
			Index: uint32(idx),
			// Cut to the long-string ceiling here rather than when the line is
			// served: a label that will not fit is truncated for both
			// generations, so the two menus stay the same tree.
			Text: codec.TruncateFixed(h.Identifier, codec.MaxLongString),
			path: h.Path,
		}

		switch v := e.(type) {
		case *canonical.Parameter:
			l.Style = parameterStyle(v)
			l.Command = next
			next++
			l.MinRange, l.MaxRange, l.Step, l.DivScale = parameterRange(v)
			l.Param = parameterFormat(v)
			if !writable(h.Access) {
				l.Style |= codec.StyleDisabled
			}
		default:
			// A node, a matrix or a function all become containers: they hold
			// other things rather than a value of their own.
			l.Style = codec.StyleList
		}

		p.lines = append(p.lines, l)

		span := 0
		for _, child := range h.Children {
			span += flatten(child)
		}
		if span > 0 {
			// The step of a container is the size of its whole subtree, not
			// the count of its immediate children. A client walks it as a
			// span, so anything else nests the tree wrongly.
			p.lines[idx].Style = codec.StyleList
			p.lines[idx].Step = uint32(span)
		}
		return span + 1
	}

	for _, root := range roots {
		flatten(root)
	}

	for i, l := range p.lines {
		if l.Command != 0 {
			p.byCmd[l.Command] = i
		}
		if l.path != "" {
			p.byPath[strings.ToLower(l.path)] = i
		}
	}

	// Every command starts at the bottom of its own range, so a read before
	// anything has been written returns something inside the range rather than
	// a zero that may be outside it.
	for _, l := range p.lines {
		if l.Command == 0 {
			continue
		}
		p.values[l.Command] = codec.Value{
			Command: l.Command,
			Mode:    codec.ModeValue,
			Val:     l.MinRange,
		}
	}

	// Then whatever the tree actually declared wins, so a client that reads
	// before anything is written sees the tree's own values.
	p.seedValues(roots)
	return p
}

// seedValues fills in the values a tree declared.
func (p *port) seedValues(roots []canonical.Element) {
	var walk func(canonical.Element)
	walk = func(e canonical.Element) {
		if param, ok := e.(*canonical.Parameter); ok {
			if i, found := p.byPath[strings.ToLower(param.Path)]; found {
				l := p.lines[i]
				if l.Command != 0 {
					p.values[l.Command] = valueOf(l, param)
				}
			}
		}
		for _, c := range e.Common().Children {
			walk(c)
		}
	}
	for _, r := range roots {
		walk(r)
	}
}

// parameterStyle picks the menu style that shows a parameter properly.
func parameterStyle(p *canonical.Parameter) codec.Style {
	switch p.Type {
	case "boolean":
		return codec.StyleCheckbox
	case "string":
		return codec.StyleEditString
	case "enum":
		// A list is what a client renders as a chooser, which is what an
		// enumeration is.
		return codec.StyleList
	case "integer", "real":
		return codec.StyleNumber
	default:
		return codec.StyleDisplay
	}
}

// parameterRange reads the numeric bounds a parameter declared.
//
// A real is carried as an integer scaled by its factor, because that is the
// only numeric form the wire has. The factor becomes the divisor a client
// divides by to display, which is exactly what the field is for.
func parameterRange(p *canonical.Parameter) (min, max int32, step uint32, div uint16) {
	div = 1
	if p.Factor != nil && *p.Factor > 0 && *p.Factor <= 0xFFFF {
		div = uint16(*p.Factor)
	}

	scale := float64(div)
	min = int32(numberOf(p.Minimum) * scale)
	max = int32(numberOf(p.Maximum) * scale)
	if s := numberOf(p.Step); s > 0 {
		step = uint32(s * scale)
	}

	switch p.Type {
	case "boolean":
		// The "on" value lives in the minimum field on a checkbox.
		min, max = 1, 1
	case "string":
		// A string's range is a length rather than a value.
		min, max = 0, int32(codec.MaxLongString-1)
	}
	return min, max, step, div
}

// parameterFormat is the printf string a client renders the value with, and
// the only place a unit can be carried.
func parameterFormat(p *canonical.Parameter) string {
	if p.Format != nil && *p.Format != "" {
		return codec.TruncateFixed(*p.Format, codec.MaxTextSize)
	}
	unit := ""
	if p.Unit != nil {
		unit = *p.Unit
	}
	switch p.Type {
	case "real":
		if unit != "" {
			return codec.TruncateFixed("%0.2f "+unit, codec.MaxTextSize)
		}
		return "%0.2f"
	case "string":
		return "%s"
	default:
		if unit != "" {
			return codec.TruncateFixed("%d "+unit, codec.MaxTextSize)
		}
		return "%d"
	}
}

// valueOf turns a parameter's declared value into a wire value.
func valueOf(l line, p *canonical.Parameter) codec.Value {
	v := codec.Value{Command: l.Command}

	switch p.Type {
	case "string":
		s, _ := p.Value.(string)
		v.Mode = codec.ModeString
		v.Text = s
	case "boolean":
		b, _ := p.Value.(bool)
		v.Mode = codec.ModeValue
		if b {
			v.Val = 1
		}
	default:
		v.Mode = codec.ModeValue
		v.Val = int32(numberOf(p.Value) * float64(l.Scale()))
	}
	return v
}

// Scale returns the divisor with the zero-means-one rule applied.
func (l line) Scale() uint16 {
	if l.DivScale == 0 {
		return 1
	}
	return l.DivScale
}

// numberOf reads a JSON number out of the canonical tree's any-typed fields.
func numberOf(v any) float64 {
	switch n := v.(type) {
	case float64:
		return n
	case float32:
		return float64(n)
	case int:
		return float64(n)
	case int64:
		return float64(n)
	case uint64:
		return float64(n)
	default:
		return 0
	}
}

// writable reports whether the canonical access string permits a write.
func writable(access string) bool {
	switch access {
	case "write", "readWrite":
		return true
	default:
		return false
	}
}

// menu returns the lines a session of the given generation may see.
//
// A command that will not fit the older generation is withheld from it rather
// than truncated, because a truncated number addresses a different command;
// its line is served as a disabled display line so the tree still has the
// right shape and a client can see that something is there.
func (p *port) menu(longStrings bool) []line {
	p.mu.RLock()
	defer p.mu.RUnlock()

	if longStrings {
		return append([]line(nil), p.lines...)
	}

	out := make([]line, 0, len(p.lines))
	for _, l := range p.lines {
		if l.Index > 0xFFFF {
			// Nothing past here can be addressed by a 16-bit client at all:
			// the index is how it asks for a line. The menu it sees stops
			// here rather than continuing with lines it cannot fetch.
			break
		}
		out = append(out, project16(l))
	}
	return out
}

// lineAt returns one line as a generation of client sees it.
//
// A 32-bit walk asks for lines one at a time, by index, so this is the hot
// path of every walk there is: a Centra-sized level is five and a half
// thousand lines and a panel asks for each of them. Copying the whole menu to
// answer for one line made that walk allocate two and a half gigabytes and
// spend over a second copying, which is most of what a panel waits for.
//
// The index is the position, which is what lets this be a lookup at all. Every
// builder assigns it that way, and menuLen depends on the same thing.
func (p *port) lineAt(index uint32, longStrings bool) (line, bool) {
	p.mu.RLock()
	defer p.mu.RUnlock()

	if index >= uint32(len(p.lines)) {
		return line{}, false
	}
	l := p.lines[index]
	if longStrings {
		return l, true
	}
	if l.Index > 0xFFFF {
		return line{}, false
	}
	return project16(l), true
}

// menuLen returns how many lines a generation of client can see.
func (p *port) menuLen(longStrings bool) int {
	p.mu.RLock()
	defer p.mu.RUnlock()

	if longStrings {
		return len(p.lines)
	}
	for i := range p.lines {
		if p.lines[i].Index > 0xFFFF {
			return i
		}
	}
	return len(p.lines)
}

// value returns what a command currently holds.
func (p *port) value(command uint32) (codec.Value, bool) {
	p.mu.RLock()
	defer p.mu.RUnlock()
	v, ok := p.values[command]
	return v, ok
}

// seed writes a value the device produced about itself, without the access
// check a client's write goes through.
//
// A read-only line still holds a value; read-only says a client may not change
// it, not that nothing may. It is how the gateway fills its own status page.
func (p *port) seed(command uint32, v codec.Value) {
	p.mu.Lock()
	defer p.mu.Unlock()
	v.Command = command
	p.values[command] = v
}

// setValue writes a command and returns what was stored.
//
// A numeric write is clamped to the line's own range, which is what a device
// does: the reply carries the stored value, so a client that asked for
// something out of range learns what it got without a second read.
func (p *port) setValue(command uint32, mode codec.Mode, num int32, text string, data []byte) (codec.Value, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	i, ok := p.byCmd[command]
	if !ok {
		// A node that publishes tables rather than a menu has no line behind
		// a command, and the menu is what the check above is made of. The
		// tables are still writable where the specification says they are:
		// setting a destination's routed source is how a structural client
		// routes, and refusing it would leave the whole Full Control interface
		// readable and inert.
		if p.router != nil {
			return p.setTableValue(command, mode, num, data)
		}
		return codec.Value{}, fmt.Errorf("no command %d on port %d", command, p.number)
	}
	l := p.lines[i]

	if l.Style.Disabled() {
		return codec.Value{}, fmt.Errorf("command %d is read-only", command)
	}

	v := codec.Value{Command: command}

	switch {
	case mode.Has(codec.ModePreset):
		// The default is the device's to decide, and the numeric field of a
		// preset write is ignored.
		v.Mode = codec.ModeValue
		v.Val = l.MinRange

	case mode.Has(codec.ModeString):
		v.Mode = codec.ModeString
		v.Text = text

	case mode.Has(codec.ModeData):
		// A command whose value is a structure keeps it. The numeric field of
		// a data write is the length of what follows, not a value, so storing
		// it as a number would replace the structure with its own size.
		v.Mode = codec.ModeData
		v.Data = data

	default:
		v.Mode = codec.ModeValue
		v.Val = num
		if l.Style.Kind() == codec.StyleNumber && l.MinRange != l.MaxRange {
			if v.Val < l.MinRange {
				v.Val = l.MinRange
			}
			if v.Val > l.MaxRange {
				v.Val = l.MaxRange
			}
		}
	}

	p.values[command] = v
	return v, nil
}

// commandForPath finds the command a dotted path names.
func (p *port) commandForPath(path string) (uint32, bool) {
	p.mu.RLock()
	defer p.mu.RUnlock()

	i, ok := p.byPath[strings.ToLower(path)]
	if !ok {
		return 0, false
	}
	return p.lines[i].Command, p.lines[i].Command != 0
}

// setDisplay sets one of the unit's status lines.
func (p *port) setDisplay(n int16, text string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.display[n] = text
}

// displayLine returns a status line.
func (p *port) displayLine(n int16) (string, bool) {
	p.mu.RLock()
	defer p.mu.RUnlock()
	s, ok := p.display[n]
	return s, ok
}

// setTableValue writes one field of the Full Control tables.
//
// A node that publishes tables rather than a menu has no line behind a
// command, and a menu line is what the writability check is made of. The
// tables are still writable where the specification says they are: setting a
// destination's routed source is how a structural client routes, and refusing
// it would leave the whole Full Control interface readable and inert.
//
// Only the two fields that carry state may be written. A name or a base says
// what the plant is rather than what it is doing, and a client able to
// overwrite a table's own base could make the interface undescribable.
//
// The lock is already held by setValue, which is the only caller.
func (p *port) setTableValue(command uint32, mode codec.Mode, num int32, data []byte) (codec.Value, error) {
	// Routing by association is a data command rather than a field: it names
	// two associations and the levels to carry across, so there is no single
	// destination for it to be a field of.
	if command == uint32(router.CmdAssocMakeRoute) {
		v := codec.Value{Command: command, Mode: codec.ModeData, Data: data}
		p.values[command] = v
		return v, nil
	}

	if command == uint32(router.CmdFireSalvo) {
		v := codec.Value{Command: command, Mode: codec.ModeData, Data: data}
		p.values[command] = v
		return v, nil
	}

	_, _, field, ok := p.router.destinationFor(router.Command(command))
	if !ok || (field != router.OffDestRoutedSrc && field != router.OffDestProtect) {
		return codec.Value{}, fmt.Errorf("command %d is read-only", command)
	}
	// Neither field is a name. A string write to one is a client confusing
	// what a destination is called with what is routed to it.
	if mode.Has(codec.ModeString) {
		return codec.Value{}, fmt.Errorf("command %d takes a number", command)
	}

	// A routed source is carried as Data Transfer Params, because it has room
	// for the result of the last set beside the pin. What is stored is the
	// pin; the reply is built by the caller, which knows whether the route was
	// made and what was there before.
	if field == router.OffDestRoutedSrc {
		pin, ok := decodeRoutedSource(codec.Value{Data: data})
		if !ok {
			return codec.Value{}, fmt.Errorf("command %d takes a routed source", command)
		}
		v := routedSourceValue(router.Command(command), pin)
		p.values[command] = v
		return v, nil
	}

	v := codec.Value{Command: command, Mode: codec.ModeValue, Val: num}
	p.values[command] = v
	return v, nil
}

// project16 rewrites one line as the older generation sees it.
//
// A command that will not fit is withheld rather than truncated, because a
// truncated number addresses a different command; the line is served as a
// disabled display line so the tree still has the right shape and a client can
// see that something is there.
func project16(l line) line {
	if l.Command > 0xFFFF || l.Step > 0xFFFF {
		l.Style = codec.StyleDisplay | codec.StyleDisabled
		l.Command = 0
		l.Step = 0
	}
	l.Text = codec.TruncateFixed(l.Text, codec.MaxTextSize)
	l.Param = codec.TruncateFixed(l.Param, codec.MaxTextSize)

	// A string's range is its length, and the older generation carries a
	// string in a fixed field. Reporting the long-string ceiling to a client
	// that will be handed nineteen bytes promises what this generation cannot
	// store: the write is truncated and answered honestly with the stored
	// value, but the menu said otherwise.
	if l.Style.Kind() == codec.StyleEditString && l.MaxRange > codec.MaxTextSize-1 {
		l.MaxRange = codec.MaxTextSize - 1
	}
	return l
}
