package rollcall

import (
	"fmt"

	"dhs/internal/snell-rollcall/codec"
	"dhs/internal/snell-rollcall/codec/router"
)

// A router level is served as a menu, not as a table.
//
// The shape below is the one a vendor Centra publishes, walked line for line
// off Level 1 of its Matrix 1 and reproduced here rather than invented:
//
//	Menu
//	  RETURN
//	  Routing
//	    Router Control
//	      Source List   "#SEL:"   Source N   (Button, cmd 110, value in min)
//	      Dest List     "#SEL:"   Dest N     (Button, cmd 100, value in min)
//	      Source Count            (Display, cmd 130)
//	      Dest Count              (Display, cmd 131)
//	      Take Mode     "#SEL:"   Immediate Take / Use Take Button (cmd 120)
//	      Take                    (Button, cmd 121)
//	      Cancel                  (Button, cmd 122)
//	      Dest Protect            (Checkbox, cmd 113)
//	    Direct Routing            Routing Dest N (Number, cmd 10000+N)
//	    Direct Protect            Protect Dest N (Checkbox, cmd 20000+N)
//	  Config
//	    Edit Names              Source Name / Dest Name / their indices
//	    Monitor                 Monitor 1..4, five readouts each
//
// Two things in it are worth naming because nothing in the specification says
// them and a panel depends on both.
//
// A list a panel draws as a listbox is a container whose parameter field is
// "#SEL:" and whose children are buttons that all carry the same command, each
// holding the value it sends in its minimum-range field. That is how a menu
// carries a set of choices at all: the entries are lines, not a payload.
//
// A checkbox on this interface counts from one — off is 1 and on is 2, with a
// range of 1..2 — where a checkbox on a card counts from zero. Writing zero to
// a protect does nothing at all.

// levelMenuBuilder accumulates the lines of one level's menu.
type levelMenuBuilder struct {
	p *port
}

func (b *levelMenuBuilder) add(text, path string, style codec.Style, cmd router.Command, minRange, maxRange int32, param string) int {
	i := len(b.p.lines)
	b.p.lines = append(b.p.lines, line{
		Index:    uint32(i),
		Style:    style,
		Command:  uint32(cmd),
		MinRange: minRange,
		MaxRange: maxRange,
		Text:     text,
		Param:    param,
		path:     path,
	})
	if cmd != 0 {
		// A selector's entries share one command, so the first of them owns
		// the lookup: a read of command 110 answers with the selection, and
		// the entries are how a client says what to select.
		if _, taken := b.p.byCmd[uint32(cmd)]; !taken {
			b.p.byCmd[uint32(cmd)] = i
		}
		b.p.byPath[path] = i
	}
	return i
}

// step sets a leaf's increment, which is what the step field means on a line
// that is not a container.
func (b *levelMenuBuilder) step(i int, n uint32) {
	b.p.lines[i].Step = n
}

// group opens a container and closes it with the span of everything inside,
// which is what a container's step means: the size of its whole subtree, not
// the count of its immediate children.
func (b *levelMenuBuilder) group(text, path, param string, body func()) {
	i := b.add(text, path, codec.StyleList|codec.StyleCacheable, 0, 0, 0, param)
	body()
	b.p.lines[i].Step = uint32(len(b.p.lines) - i - 1)
}

// buildLevelMenu fills a level node's menu and its values.
func buildLevelMenu(p *port, lv *routerLevel) {
	b := &levelMenuBuilder{p: p}

	nsrc := int32(len(lv.sources))
	ndst := int32(len(lv.dests))

	cache := codec.StyleCacheable
	btn := codec.StyleButton | cache
	disp := codec.StyleDisplay | cache
	num := codec.StyleNumber | cache
	chk := codec.StyleCheckbox | cache
	str := codec.StyleEditString | cache

	// The root, and the hidden line every vendor menu carries as its way back
	// out. It is not ours to leave off: a panel looks for it.
	root := b.add("Menu", "menu", codec.StyleList, 0, 0, 0, "")
	b.add("RETURN", "menu.return", codec.StylePartial|codec.StyleHidden, 0, 0, 0, "")

	b.group("Routing", "routing", "", func() {
		b.group("Router Control", "routing.control", "", func() {
			b.group("Source List", "routing.control.sources", "#SEL:", func() {
				for i, name := range lv.sources {
					b.add(name, fmt.Sprintf("routing.control.sources.%d", i+1),
						btn, router.LvlSrcSelect, int32(i+1), 0, "")
				}
			})
			b.group("Dest List", "routing.control.dests", "#SEL:", func() {
				for i := range lv.dests {
					b.add(lv.dests[i].name, fmt.Sprintf("routing.control.dests.%d", i+1),
						btn, router.LvlDestSelect, int32(i+1), 0, "")
				}
			})
			b.step(b.add("Source Count", "routing.control.source-count", disp,
				router.LvlSourceCount, -32767, 23767, ""), 1)
			b.step(b.add("Dest Count", "routing.control.dest-count", disp,
				router.LvlDestCount, -32767, 23767, ""), 1)

			b.group("Take Mode", "routing.control.take-mode", "#SEL:", func() {
				b.add("Immediate Take", "routing.control.take-mode.immediate",
					btn, router.LvlTakeMode, router.TakeImmediate, 0, "")
				b.add("Use Take Button", "routing.control.take-mode.button",
					btn, router.LvlTakeMode, router.TakeOnButton, 0, "")
			})
			b.add("Take", "routing.control.take", btn, router.LvlTake, 1, 0, "")
			b.add("Cancel", "routing.control.cancel", btn, router.LvlCancel, 1, 0, "")
			b.add("Dest Protect", "routing.control.protect", chk,
				router.LvlDestProtect, router.ProtectOff, router.ProtectOn, "")
		})

		// One command per destination. This is the block a panel subscribes to
		// for tally: it is the whole crosspoint state of the level, and every
		// change is pushed on it.
		b.group("Direct Routing", "routing.direct", "", func() {
			for i := range lv.dests {
				b.step(b.add(fmt.Sprintf("Routing Dest %d", i+1),
					fmt.Sprintf("routing.direct.%d", i+1),
					num, router.LvlRoute(i+1), 1, nsrc, "%0.0f"), 1)
			}
		})
		b.group("Direct Protect", "routing.protect", "", func() {
			for i := range lv.dests {
				b.add(fmt.Sprintf("Protect Dest %d", i+1),
					fmt.Sprintf("routing.protect.%d", i+1),
					chk, router.LvlProtect(i+1), router.ProtectOff, router.ProtectOn, "")
			}
		})
	})

	b.group("Config", "config", "", func() {
		// Renaming is a two-step: point the index at an entry, then write the
		// string. The index lines carry the plant's size as their range, which
		// is how a client learns it without reading the counts.
		b.group("Edit Names", "config.names", "", func() {
			// The step on a name is three. Nothing explains it and the field
			// is an increment everywhere else, but a Centra carries three on
			// both name lines and this side of the wire is the one that has to
			// agree with it.
			b.step(b.add("Source Name", "config.names.source", str, router.LvlSrcName, 0, 0, "source name"), 3)
			b.step(b.add("Dest Name", "config.names.dest", str, router.LvlDstName, 0, 0, "dest name"), 3)
			b.step(b.add("Source Name Index", "config.names.source-index", num,
				router.LvlSrcNameIndex, 1, nsrc, "%0.0f"), 1)
			b.step(b.add("Dest Name Index", "config.names.dest-index", num,
				router.LvlDstNameIndex, 1, ndst, "%0.0f"), 1)
		})
		b.group("Monitor", "config.monitor", "", func() {
			readouts := []struct {
				text string
				base router.Command
			}{
				{"Src or Dest", router.LvlMonKind},
				{"Index", router.LvlMonIndex},
				{"Name", router.LvlMonName},
				{"RC Src Addr", router.LvlMonSrcAddr},
				{"RC Dest Addr", router.LvlMonDstAddr},
			}
			for m := 1; m <= router.LvlMonitors; m++ {
				b.group(fmt.Sprintf("Monitor %d", m), fmt.Sprintf("config.monitor.%d", m), "", func() {
					for _, r := range readouts {
						b.add(r.text, fmt.Sprintf("config.monitor.%d.%s", m, identifierToken(r.text)),
							disp, router.LvlMonitor(r.base, m), 0, 0, "")
					}
				})
			}
		})
	})

	// The root spans everything under it, which is everything but itself.
	p.lines[root].Step = uint32(len(p.lines) - root - 1)

	seedLevelValues(p, lv)
}

// seedLevelValues gives every command its starting value.
//
// A level with no value on a command is a level a panel cannot draw: it asks
// for all of them before it shows anything, and one refusal is enough for it
// to treat the node as broken rather than as empty.
func seedLevelValues(p *port, lv *routerLevel) {
	num := func(c router.Command, v int32) {
		p.values[uint32(c)] = codec.Value{Command: uint32(c), Mode: codec.ModeValue, Val: v}
	}
	str := func(c router.Command, v string) {
		p.values[uint32(c)] = codec.Value{Command: uint32(c), Mode: codec.ModeString, Text: v}
	}

	num(router.LvlSrcSelect, 1)
	num(router.LvlDestSelect, 1)
	num(router.LvlSrcNameIndex, 1)
	num(router.LvlDstNameIndex, 1)
	num(router.LvlTakeMode, router.TakeImmediate)
	num(router.LvlTake, 0)
	num(router.LvlCancel, 0)
	num(router.LvlSourceCount, int32(len(lv.sources)))
	num(router.LvlDestCount, int32(len(lv.dests)))
	num(router.LvlDestProtect, router.ProtectOff)

	if len(lv.sources) > 0 {
		str(router.LvlSrcName, lv.sources[0])
	} else {
		str(router.LvlSrcName, "")
	}
	if len(lv.dests) > 0 {
		str(router.LvlDstName, lv.dests[0].name)
	} else {
		str(router.LvlDstName, "")
	}

	for i := range lv.dests {
		d := &lv.dests[i]
		// A destination nothing has been routed to yet carries source one
		// rather than nothing, because the command is a number in 1..n and
		// there is no value in that range that means "no source".
		src := int32(d.routed.Source)
		if src < 1 {
			src = 1
		}
		num(router.LvlRoute(i+1), src)
		num(router.LvlProtect(i+1), router.ProtectOff)
	}

	// A monitor that is not assigned says so rather than staying blank, which
	// is what the Centra answers on every one of its twenty readouts.
	for m := 1; m <= router.LvlMonitors; m++ {
		for _, base := range []router.Command{
			router.LvlMonKind, router.LvlMonIndex, router.LvlMonName,
			router.LvlMonSrcAddr, router.LvlMonDstAddr,
		} {
			str(router.LvlMonitor(base, m), "Unknown")
		}
	}
}

// identifierToken makes a readout's label usable as a path segment.
func identifierToken(s string) string {
	out := make([]rune, 0, len(s))
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			out = append(out, r)
		case r >= 'A' && r <= 'Z':
			out = append(out, r+32)
		default:
			out = append(out, '-')
		}
	}
	return string(out)
}
