package rollcall

import (
	"context"
	"testing"

	"dhs/internal/snell-rollcall/codec"
	"dhs/internal/snell-rollcall/session"
)

// The two generations serve the same tree, and this is where that claim is
// checked: the same card, walked both ways, has to come back with the same
// shape, the same labels and the same command numbers.

func TestMenuIsTheSameTreeInBothGenerations(t *testing.T) {
	s := newServed(t, testTree())

	old := walk16(t, s)
	modern := walk32(t, s)

	if len(old) != len(modern) {
		t.Fatalf("16-bit menu has %d lines, 32-bit has %d", len(old), len(modern))
	}
	for i := range old {
		if old[i].MenuIndex != modern[i].MenuIndex {
			t.Errorf("line %d: index %d vs %d", i, old[i].MenuIndex, modern[i].MenuIndex)
		}
		if old[i].Command != modern[i].Command {
			t.Errorf("line %d: command %d vs %d", i, old[i].Command, modern[i].Command)
		}
		if old[i].Text != modern[i].Text {
			t.Errorf("line %d: label %q vs %q", i, old[i].Text, modern[i].Text)
		}
		if old[i].Step != modern[i].Step {
			t.Errorf("line %d: step %d vs %d", i, old[i].Step, modern[i].Step)
		}
	}
}

func TestMenuNestsBySpanNotByChildCount(t *testing.T) {
	s := newServed(t, testTree())
	lines := walk32(t, s)

	// card1 holds a container "video" with three parameters under it and a
	// "status" line beside it. The container's step is the size of its whole
	// subtree, which is what a client walks; the count of its children would
	// nest the tree wrongly.
	if len(lines) != 6 {
		t.Fatalf("card1 has %d lines, want 6", len(lines))
	}
	if lines[0].Text != "card1" || lines[0].Step != 5 {
		t.Errorf("root line = %q step %d, want card1 step 5", lines[0].Text, lines[0].Step)
	}
	if lines[1].Text != "video" || lines[1].Step != 3 {
		t.Errorf("container = %q step %d, want video step 3", lines[1].Text, lines[1].Step)
	}
	if lines[5].Text != "status" || lines[5].Step != 0 {
		t.Errorf("leaf = %q step %d, want status step 0", lines[5].Text, lines[5].Step)
	}
}

func TestMenuLinesCarryWhatAClientNeedsToRenderThem(t *testing.T) {
	s := newServed(t, testTree())
	lines := walk32(t, s)

	gain := lines[2]
	if gain.Text != "gain" {
		t.Fatalf("line 2 = %q, want gain", gain.Text)
	}
	// A real is carried as an integer scaled by its factor, and the factor is
	// the divisor a client divides by to display it.
	if gain.DivScale != 10 {
		t.Errorf("divisor = %d, want 10", gain.DivScale)
	}
	if gain.MinRange != -600 || gain.MaxRange != 60 {
		t.Errorf("range = %d..%d, want -600..60", gain.MinRange, gain.MaxRange)
	}
	if gain.Param != "%0.2f dB" {
		t.Errorf("format = %q", gain.Param)
	}
	if gain.Style.Kind() != codec.StyleNumber {
		t.Errorf("style = %s, want a number", gain.Style)
	}

	if lines[3].Style.Kind() != codec.StyleCheckbox {
		t.Errorf("enable rendered as %s, want a checkbox", lines[3].Style)
	}
	if lines[4].Style.Kind() != codec.StyleEditString {
		t.Errorf("name rendered as %s, want an editable string", lines[4].Style)
	}
	// A parameter nobody may write is disabled rather than absent: the line is
	// still there, and a client shows it greyed.
	if !lines[5].Style.Disabled() {
		t.Errorf("a read-only parameter came back as %s", lines[5].Style)
	}
}

func TestMenuCountIsRelativeToTheIndexAsked(t *testing.T) {
	s := newServed(t, testTree())
	sess := s.open(1, codec.SvcMenus|codec.SvcLongStr)

	for _, base := range []uint32{0, 2, 6, 99} {
		reply := do(t, sess, codec.MsgGetMenuCount,
			codec.MenuReq{MenuIndex: base}.AppendTo(nil))
		size, err := codec.DecodeMenuSize(reply.Payload)
		if err != nil {
			t.Fatalf("decode: %v", err)
		}
		if size.MenuIndex != base {
			t.Errorf("base %d echoed as %d", base, size.MenuIndex)
		}
		want := uint32(0)
		if base < 6 {
			want = 6 - base
		}
		if size.MenuCount != want {
			t.Errorf("count from %d = %d, want %d", base, size.MenuCount, want)
		}
	}
}

func TestMenuRefusals(t *testing.T) {
	s := newServed(t, testTree())

	empty := s.open(0x40, codec.SvcMenus|codec.SvcLongStr)
	if got := refused(t, empty, codec.MsgGetMenuCount, codec.MenuReq{}.AppendTo(nil)); got.Type != codec.MsgNack {
		t.Errorf("an empty slot answered %s, want Nack", got.Type)
	}

	// The menu service is not implied by having a session: a client that did
	// not ask for it does not get it.
	noMenu := s.open(1, codec.SvcControl)
	if got := refused(t, noMenu, codec.MsgGetMenuCount, codec.MenuReq{}.AppendTo(nil)); got.Type != codec.MsgNack {
		t.Errorf("a session without the menu service answered %s, want Nack", got.Type)
	}

	sess := s.open(1, codec.SvcMenus|codec.SvcLongStr)
	if got := refused(t, sess, codec.MsgGetMenuCount, []byte{0x01}); got.Type != codec.MsgNack {
		t.Errorf("a malformed request answered %s, want Nack", got.Type)
	}
	if got := refused(t, sess, codec.MsgGetMenuItem, []byte{0x01}); got.Type != codec.MsgNack {
		t.Errorf("a malformed item request answered %s, want Nack", got.Type)
	}
	if got := refused(t, sess, codec.MsgGetMenuItem,
		codec.MenuReq{MenuIndex: 99}.AppendTo(nil)); got.Type != codec.MsgNack {
		t.Errorf("an index past the end answered %s, want Nack", got.Type)
	}
}

func TestTransferRefusals(t *testing.T) {
	s := newServed(t, testTree())
	sess := s.open(1, codec.SvcMenus)

	// Nothing has been opened, so there is nothing to fetch from.
	req := codec.GetNext{Index: 0, PktType: codec.MsgGetFunc}.AppendTo(nil)
	if got := refused(t, sess, codec.MsgGetNextPkt, req); got.Type != codec.MsgNack {
		t.Errorf("a fetch with no transfer open answered %s, want Nack", got.Type)
	}

	if got := refused(t, sess, codec.MsgGetNextPkt, []byte{0x01}); got.Type != codec.MsgNack {
		t.Errorf("a malformed fetch answered %s, want Nack", got.Type)
	}

	// Open one, then ask past its end.
	do(t, sess, codec.MsgGetFunc, nil)
	past := codec.GetNext{Index: 99, PktType: codec.MsgGetFunc}.AppendTo(nil)
	if got := refused(t, sess, codec.MsgGetNextPkt, past); got.Type != codec.MsgNack {
		t.Errorf("a fetch past the end answered %s, want Nack", got.Type)
	}
}

func TestATransferBelongsToOneSession(t *testing.T) {
	s := newServed(t, testTree())

	first := s.open(1, codec.SvcMenus)
	second := s.open(2, codec.SvcMenus)

	// The first session opens a menu transfer on card1. The second has none
	// of its own, and must not inherit it.
	do(t, first, codec.MsgGetFunc, nil)

	req := codec.GetNext{Index: 0, PktType: codec.MsgGetFunc}.AppendTo(nil)
	if got := refused(t, second, codec.MsgGetNextPkt, req); got.Type != codec.MsgNack {
		t.Errorf("a second session inherited a transfer: %s", got.Type)
	}
	if got := do(t, first, codec.MsgGetNextPkt, req); got.Type != codec.MsgRetFunc {
		t.Errorf("the session that opened the transfer got %s", got.Type)
	}
}

// walk16 fetches a card's menu the way a client without long strings does: a
// block header, then each line by offset.
func walk16(t *testing.T, s *served) []codec.MenuItem {
	t.Helper()

	sess := s.open(1, codec.SvcMenus)

	var out []codec.MenuItem
	err := session.Walk(context.Background(), sess, codec.MsgGetFunc, nil,
		func(_ int, f codec.Frame) error {
			fn, err := codec.DecodeFunc(f.Payload)
			if err != nil {
				return err
			}
			out = append(out, fn.ToMenuItem())
			return nil
		})
	if err != nil {
		t.Fatalf("16-bit walk: %v", err)
	}
	return out
}

// walk32 fetches the same menu the way a long-string client does: a count,
// then each line by absolute index.
func walk32(t *testing.T, s *served) []codec.MenuItem {
	t.Helper()

	sess := s.open(1, codec.SvcMenus|codec.SvcLongStr)

	reply := do(t, sess, codec.MsgGetMenuCount, codec.MenuReq{}.AppendTo(nil))
	size, err := codec.DecodeMenuSize(reply.Payload)
	if err != nil {
		t.Fatalf("decode count: %v", err)
	}

	out := make([]codec.MenuItem, 0, size.MenuCount)
	for i := uint32(0); i < size.MenuCount; i++ {
		item := do(t, sess, codec.MsgGetMenuItem, codec.MenuReq{MenuIndex: i}.AppendTo(nil))
		m, err := codec.DecodeMenuItem(item.Payload)
		if err != nil {
			t.Fatalf("decode item %d: %v", i, err)
		}
		out = append(out, m)
	}
	return out
}

func TestA16BitMenuPromisesOnlyWhatItCanStore(t *testing.T) {
	// A string's range is its length. The older generation carries a string in
	// a fixed twenty-byte field, so a menu that reports the long-string
	// ceiling to a 16-bit client promises sixty-three characters and stores
	// nineteen. The write is answered honestly with the stored value, but the
	// menu had already said otherwise.
	m := buildModel(testTree(), "dhs rollcall")
	prt := m.port(1)

	var long, short int32
	for _, l := range prt.menu(true) {
		if l.Style.Kind() == codec.StyleEditString {
			long = l.MaxRange
			break
		}
	}
	for _, l := range prt.menu(false) {
		if l.Style.Kind() == codec.StyleEditString {
			short = l.MaxRange
			break
		}
	}

	if long != codec.MaxLongString-1 {
		t.Errorf("32-bit string length = %d, want %d", long, codec.MaxLongString-1)
	}
	if short != codec.MaxTextSize-1 {
		t.Errorf("16-bit string length = %d, want %d", short, codec.MaxTextSize-1)
	}
}
