package consumer

import (
	"fmt"
	"net"
	"strconv"
	"strings"

	"dhs/internal/rrcs/codec"
)

// A configuration change (§8.10): one edit, create or delete of a port
// or a client card, in the forms proven on a production RRCS 9.0 —
// what the set, import and ensure verbs send, and what the plugin's
// SetValue sends for a property the lists report. The rules of
// ADR-0035 live here: a stream in NMOS mode takes its mode alone, a
// main port's channel count goes through manual, a link is Mode then
// Selection on an input and both in one request on an output.

// ChangeMethod is the request every change goes by (§8.10.1).
const ChangeMethod = "ConfigurationChange"

// NMOSRefusal is said when an edit would hand stream fields to a stream
// in NMOS mode.
const NMOSRefusal = "refused: this edit gives stream fields to a sender or receiver that is, or becomes, NMOS (Protocol 5). Two such edits stopped a production RRCS 9.0 at 'Applying change: Edit PortEx' (2026-10-06 with ConfigurationChangeEx, 2026-10-08 with ConfigurationChange); the same edit towards manual mode passed. It is not sent"

// CardAes67 are the client-card blocks the specification puts inside
// the Aes67 struct of a change (§8.10.4.34), while the lists print them
// at the top of the card.
var CardAes67 = map[string]bool{"Media_1": true, "Media_2": true, "Ptp": true, "Dns": true}

// Change is one change, ready to send.
type Change struct {
	ObjectType string
	// ChangeType is "edit" when empty; "create" makes the object
	// (proven 2026-10-14: an unused port becomes a typed port by one
	// create with PortType), "delete" removes it by its address.
	ChangeType string
	// Address identifies the object inside SpecificParams.
	Address []codec.Member
	// Blocks holds the wanted members, by block ("" = top level).
	Blocks map[string][]codec.Member
	// Order keeps the blocks in the order they were given.
	Order []string
}

// Add puts one wanted value into the change: NAME or BLOCK.NAME.
func (c *Change) Add(name string, value codec.Value) error {
	block, field, nested := strings.Cut(name, ".")
	if !nested {
		block, field = "", name
	}
	if field == "" || strings.Contains(field, ".") {
		return fmt.Errorf("--prop %s: want NAME=VALUE or BLOCK.NAME=VALUE", name)
	}
	for _, m := range c.Blocks[block] {
		if m.Name == field {
			return fmt.Errorf("--prop %s given twice", name)
		}
	}
	if _, seen := c.Blocks[block]; !seen {
		c.Order = append(c.Order, block)
	}
	c.Blocks[block] = append(c.Blocks[block], codec.Member{Name: field, Value: value})
	return nil
}

// Params builds SpecificParams.
func (c *Change) Params() codec.Value {
	members := append([]codec.Member{}, c.Address...)
	var aes67 []codec.Member
	for _, block := range c.Order {
		fields := c.Blocks[block]
		switch {
		case block == "":
			members = append(members, fields...)
		case c.ObjectType == "client-card" && CardAes67[block]:
			aes67 = append(aes67, codec.Member{Name: block, Value: codec.Struct(fields...)})
		default:
			members = append(members, codec.Member{Name: block, Value: codec.Struct(fields...)})
		}
	}
	if len(aes67) > 0 {
		members = append(members, codec.Member{Name: "Aes67", Value: codec.Struct(aes67...)})
	}
	return codec.Struct(members...)
}

// Request is the parameter of ConfigurationChange (§8.10.1): an array
// of changes, here one.
func (c *Change) Request() codec.Value {
	changeType := c.ChangeType
	if changeType == "" {
		changeType = "edit"
	}
	return codec.Array(codec.Struct(
		codec.Member{Name: "ChangeType", Value: codec.String(changeType)},
		codec.Member{Name: "ObjectType", Value: codec.String(c.ObjectType)},
		codec.Member{Name: "SpecificParams", Value: c.Params()},
	))
}

// ChangeFor reads a path into the object type and the address of a
// change.
func ChangeFor(path string) (*Change, error) {
	c := &Change{Blocks: map[string][]codec.Member{}}
	parts := strings.Split(path, ".")
	if len(parts) == 6 && parts[0] == "net" && parts[2] == "node" && parts[4] == "card" {
		node, err1 := strconv.Atoi(parts[3])
		bay, err2 := strconv.Atoi(parts[5])
		if err1 != nil || err2 != nil {
			return nil, fmt.Errorf("--path %s: not a client card path", path)
		}
		c.ObjectType = "client-card"
		c.Address = []codec.Member{
			{Name: "Node", Value: codec.Int(int32(node))},
			{Name: "Slot", Value: codec.Int(int32(bay))},
		}
		return c, nil
	}
	_, node, port, isInput, ok := PortOfPath(path)
	if !ok || len(parts) > 7 || (len(parts) == 7 && parts[6] != "in" && parts[6] != "out") {
		return nil, fmt.Errorf("--path %s: set takes a port path or a client card path", path)
	}
	// §8.10.4.6: PortAddress is a TPortAddress; IsInput is given only
	// where an input and an output share the number.
	addr := []codec.Member{}
	if len(parts) == 7 {
		addr = append(addr, codec.Member{Name: "IsInput", Value: codec.Bool(isInput)})
	}
	addr = append(addr,
		codec.Member{Name: "Node", Value: codec.Int(int32(node))},
		codec.Member{Name: "Port", Value: codec.Int(int32(port))},
	)
	c.ObjectType = "portex"
	c.Address = []codec.Member{{Name: "PortAddress", Value: codec.Struct(addr...)}}
	return c, nil
}

// NMOSStream names the stream block of the change that would be edited
// while its protocol is, or becomes, NMOS; empty when there is none.
// current is what the object holds now.
func (c *Change) NMOSStream(current map[string]any) string {
	if c.ObjectType != "portex" {
		return ""
	}
	for _, block := range []string{"PortAes67Output", "PortAes67Input"} {
		fields, edited := c.Blocks[block]
		if !edited {
			continue
		}
		// A link is not a stream edit: it names the port whose stream
		// this one takes a channel of (Mode, §8.10.4.6) and the channel
		// (Selection), and RRCS allows nothing else beside them ("When
		// linking to a port, no further property than 'Selection' is
		// allowed to be set").
		link := true
		for _, f := range fields {
			if f.Name != "Mode" && f.Name != "Selection" {
				link = false
			}
		}
		if link {
			continue
		}
		// The mode alone, no other field, is proven safe on a production
		// RRCS 9.0 (2026-10-13, In. and Out. -7.49): what stopped RRCS
		// was address fields beside the mode, never the mode itself.
		if len(fields) == 1 && fields[0].Name == "Protocol" {
			continue
		}
		protocol := jInt(jMap(current[block]), "Protocol")
		for _, f := range fields {
			if f.Name == "Protocol" && f.Value.Kind == codec.KindInt {
				protocol = int(f.Value.Int)
			}
		}
		if protocol == 5 {
			return block
		}
	}
	return ""
}

// StreamSteps shapes one object's wanted values into the requests the
// stream hardware takes (ADR-0035): a link is Mode then Selection on an
// input and both in one request on an output, a link write carries
// nothing else; the channel count of a main port in NMOS mode goes
// through manual mode and back. drop says that the link is already in
// place (RRCS never reports it; a Selection row alone counts as that).
func StreamSteps(object string, c *Change, props map[string]any) (steps []*Change, drop bool, refused string) {
	blk := ""
	for _, b := range []string{"PortAes67Input", "PortAes67Output"} {
		if _, ok := c.Blocks[b]; ok {
			blk = b
		}
	}
	if blk == "" {
		return []*Change{c}, false, ""
	}
	var mode, sel *codec.Member
	named := map[string]codec.Value{}
	for i := range c.Blocks[blk] {
		m := &c.Blocks[blk][i]
		named[m.Name] = m.Value
		switch m.Name {
		case "Mode":
			mode = m
		case "Selection":
			sel = m
		}
	}
	one := func(members ...codec.Member) *Change {
		step, err := ChangeFor(object)
		if err != nil {
			return nil
		}
		for _, m := range members {
			if step.Add(blk+"."+m.Name, m.Value) != nil {
				return nil
			}
		}
		return step
	}
	if mode != nil {
		if len(c.Order) > 1 || len(c.Blocks[blk]) > 2 {
			return nil, false, "a link write carries Mode and Selection and nothing else (RRCS: when linking, no further property than Selection)"
		}
		if sel == nil {
			return nil, true, ""
		}
		if strings.HasSuffix(object, ".in") {
			return []*Change{one(*mode), one(*sel)}, false, ""
		}
		return []*Change{one(*mode, *sel)}, false, ""
	}
	ch, hasCh := named["Channels"]
	proto, hasProto := named["Protocol"]
	if hasCh && len(c.Order) == 1 && jInt(jMap(props[blk]), "Protocol") == 5 && (!hasProto || proto.Int == 5) {
		others := 0
		for name := range named {
			if name != "Channels" && name != "Protocol" {
				others++
			}
		}
		if others == 0 {
			return []*Change{
				one(codec.Member{Name: "Protocol", Value: codec.Int(2)}, codec.Member{Name: "Channels", Value: ch}),
				one(codec.Member{Name: "Protocol", Value: codec.Int(5)}),
			}, false, ""
		}
	}
	return []*Change{c}, false, ""
}

// EnumSet reads a printed value list ("1=Auto|2=Momentary|3=Latching")
// into the allowed numbers. complete is false where the specification
// prints only some values of a wider scale (the dots of its tables),
// told apart the way export tells an enum from a hinted range: every key
// numeric and the keys forming one unbroken run.
func EnumSet(enum string) (allowed map[int64]bool, complete bool) {
	if enum == "" {
		return nil, false
	}
	allowed = map[int64]bool{}
	lo, hi := int64(0), int64(0)
	for _, item := range strings.Split(enum, "|") {
		numberText, _, _ := strings.Cut(item, "=")
		n, err := strconv.ParseInt(numberText, 10, 32)
		if err != nil {
			return nil, false
		}
		if len(allowed) == 0 || n < lo {
			lo = n
		}
		if len(allowed) == 0 || n > hi {
			hi = n
		}
		allowed[n] = true
	}
	return allowed, len(allowed) > 0 && hi-lo+1 == int64(len(allowed))
}

// TypedValue types one wanted value the way the specification's tables
// print the property — its type, its bounds, its named values, its
// longest text — and refuses what they would not take; the reported
// value's wire type is the fallback where the tables say nothing.
// creating with types is a PortType create checked against the
// gateway's own type list. reason is empty when the value is taken.
func TypedValue(object, block, field, wanted string, live any, creating bool, types map[int]string) (value codec.Value, reason string) {
	spec := SpecFor(object, block, field)
	kind := spec.Type
	if kind == "" {
		switch live.(type) {
		case bool:
			kind = "bool"
		case float64:
			kind = "int"
		default:
			kind = "string"
		}
	}
	switch kind {
	case "bool":
		b, err := strconv.ParseBool(strings.ToLower(wanted))
		if err != nil {
			return codec.Value{}, wanted + " is not true or false"
		}
		return codec.Bool(b), ""
	case "int":
		n, err := strconv.ParseInt(wanted, 10, 32)
		if err != nil {
			return codec.Value{}, wanted + " is not a whole number"
		}
		if lo, errLo := strconv.ParseInt(spec.Min, 10, 32); errLo == nil && n < lo {
			return codec.Value{}, fmt.Sprintf("%d is below the specification's minimum %s", n, spec.Min)
		}
		if hi, errHi := strconv.ParseInt(spec.Max, 10, 32); errHi == nil && n > hi {
			return codec.Value{}, fmt.Sprintf("%d is above the specification's maximum %s", n, spec.Max)
		}
		// Where the complete set of values is known, membership is
		// checked too (Protocol is 2, 3 or 5 — not 4). The type of a port
		// to create is what this gateway lists, not the printed table;
		// the curated enums are complete by definition; a printed list
		// counts only when its keys form one unbroken run, since the
		// specification abbreviates longer scales with dots.
		switch words, known := Enums[field]; {
		case creating && len(types) > 0:
			if _, ok := types[int(n)]; !ok {
				return codec.Value{}, fmt.Sprintf("type %d is not in the gateway's type list (%d names)", n, len(types))
			}
		case known:
			if _, ok := words[int(n)]; !ok {
				return codec.Value{}, wanted + " is not one of its values"
			}
		default:
			if allowed, complete := EnumSet(spec.Enum); complete && !allowed[n] {
				return codec.Value{}, wanted + " is not one of the specification's values (" + spec.Enum + ")"
			}
		}
		return codec.Int(int32(n)), ""
	case "ip":
		if net.ParseIP(wanted) == nil {
			return codec.Value{}, wanted + " is not an IP address"
		}
		return codec.String(wanted), ""
	default:
		if max, err := strconv.Atoi(spec.MaxLen); err == nil && len(wanted) > max {
			return codec.Value{}, fmt.Sprintf("%q is longer than the specification's %s characters", wanted, spec.MaxLen)
		}
		return codec.String(wanted), ""
	}
}

// PortNameEdits builds the two per-direction name edits of a port just
// created, by the pattern Director's own client uses: "In. -7.N - Bay B
// - <node long name>". Where the model cannot say the bay or the node's
// name, the shorter form is used; a 2022-7 port (1024 to 1151) is
// numbered -7.N, any other by its plain number.
func PortNameEdits(m *Model, object string) []*Change {
	_, node, port, _, ok := PortOfPath(object)
	if !ok {
		return nil
	}
	number := strconv.Itoa(port)
	if port >= 1024 && port <= 1151 {
		number = "-7." + strconv.Itoa(port-1023)
	}
	suffix := ""
	if card := m.CardOfNode()[node]; card != nil {
		suffix = " - Bay " + strconv.Itoa(card.Bay)
		for _, n := range m.Nodes {
			if n.Node == card.Node && n.LongName != "" {
				suffix += " - " + n.LongName
			}
		}
	}
	clip := func(s string, max int) string {
		if len(s) > max {
			return s[:max]
		}
		return s
	}
	var edits []*Change
	for _, side := range []struct {
		dir, labelPrefix, longPrefix string
	}{{".in", "I.", "In. "}, {".out", "O.", "Out. "}} {
		change, err := ChangeFor(object + side.dir)
		if err != nil {
			continue
		}
		_ = change.Add("Label", codec.String(clip(side.labelPrefix+number, 8)))
		_ = change.Add("LongName", codec.String(clip(side.longPrefix+number+suffix, 32)))
		edits = append(edits, change)
	}
	return edits
}
