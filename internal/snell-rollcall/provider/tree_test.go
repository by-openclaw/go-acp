package rollcall

import (
	"testing"

	"dhs/internal/export/canonical"
	"dhs/internal/snell-rollcall/codec"
)

func TestBuildModelWithoutATree(t *testing.T) {
	// A gateway with nothing behind it still has to be walkable: a client that
	// finds no cards cannot tell an empty frame from a fault.
	for _, tc := range []struct {
		name string
		tree *canonical.Export
	}{
		{"no export", nil},
		{"no root", &canonical.Export{}},
		{"a root with no children", &canonical.Export{
			Root: &canonical.Node{Header: canonical.Header{Identifier: "solo", Path: "solo"}},
		}},
	} {
		m := buildModel(tc.tree, "gateway")
		ports := m.portNumbers()
		if len(ports) != 1 || ports[0] != firstCardPort {
			t.Errorf("%s: ports = %v, want one at %d", tc.name, ports, firstCardPort)
		}
		if m.port(firstCardPort) == nil {
			t.Errorf("%s: the one port is missing", tc.name)
		}
	}
}

func TestBuildModelStopsAtTheClientPorts(t *testing.T) {
	var children []canonical.Element
	for i := 0; i < 300; i++ {
		children = append(children, &canonical.Node{
			Header: canonical.Header{Identifier: "card", Path: "frame.card"},
		})
	}
	m := buildModel(&canonical.Export{
		Root: &canonical.Node{Header: canonical.Header{
			Identifier: "frame", Path: "frame", Children: children,
		}},
	}, "gateway")

	// Ports from 0xE0 upwards are the ones a gateway hands out to its own
	// clients, so a card there would be addressed as one.
	ports := m.portNumbers()
	if len(ports) != int(firstClientPort-firstCardPort) {
		t.Errorf("%d ports, want %d", len(ports), firstClientPort-firstCardPort)
	}
	for _, n := range ports {
		if n >= firstClientPort {
			t.Errorf("a card was put at port %02X", n)
		}
	}
}

func TestIdentifierOfNothing(t *testing.T) {
	if got := identifierOf(nil); got != "" {
		t.Errorf("identifierOf(nil) = %q", got)
	}
}

func TestParameterStyles(t *testing.T) {
	for _, tc := range []struct {
		typ  string
		want codec.Style
	}{
		{canonical.ParamBoolean, codec.StyleCheckbox},
		{canonical.ParamString, codec.StyleEditString},
		{"enum", codec.StyleList},
		{canonical.ParamInteger, codec.StyleNumber},
		{canonical.ParamReal, codec.StyleNumber},
		{canonical.ParamOctets, codec.StyleDisplay},
	} {
		got := parameterStyle(&canonical.Parameter{Type: tc.typ})
		if got != tc.want {
			t.Errorf("%s rendered as %s, want %s", tc.typ, got, tc.want)
		}
	}
}

func TestParameterRange(t *testing.T) {
	factor := int64(100)
	huge := int64(0x1_0000)

	// A real is carried as an integer scaled by its factor.
	min, max, step, div := parameterRange(&canonical.Parameter{
		Type: canonical.ParamReal, Minimum: -1.0, Maximum: 1.0, Step: 0.25, Factor: &factor,
	})
	if div != 100 || min != -100 || max != 100 || step != 25 {
		t.Errorf("scaled range = %d..%d step %d div %d", min, max, step, div)
	}

	// A factor that will not fit the field is ignored rather than truncated:
	// a wrong divisor displays every value wrongly.
	_, _, _, div = parameterRange(&canonical.Parameter{Type: canonical.ParamReal, Factor: &huge})
	if div != 1 {
		t.Errorf("an oversized factor gave divisor %d, want 1", div)
	}

	// On a checkbox the minimum carries the "on" value rather than a bound.
	min, max, _, _ = parameterRange(&canonical.Parameter{Type: canonical.ParamBoolean})
	if min != 1 || max != 1 {
		t.Errorf("checkbox range = %d..%d, want 1..1", min, max)
	}

	// A string's range is a length.
	min, max, _, _ = parameterRange(&canonical.Parameter{Type: canonical.ParamString})
	if min != 0 || max != int32(codec.MaxLongString-1) {
		t.Errorf("string range = %d..%d", min, max)
	}
}

func TestParameterFormat(t *testing.T) {
	format := "%03d"
	unit := "Hz"

	for _, tc := range []struct {
		name  string
		param *canonical.Parameter
		want  string
	}{
		{"an explicit format wins", &canonical.Parameter{
			Type: canonical.ParamInteger, Format: &format}, "%03d"},
		{"a real with a unit", &canonical.Parameter{
			Type: canonical.ParamReal, Unit: &unit}, "%0.2f Hz"},
		{"a real without one", &canonical.Parameter{Type: canonical.ParamReal}, "%0.2f"},
		{"a string", &canonical.Parameter{Type: canonical.ParamString}, "%s"},
		{"an integer with a unit", &canonical.Parameter{
			Type: canonical.ParamInteger, Unit: &unit}, "%d Hz"},
		{"an integer without one", &canonical.Parameter{Type: canonical.ParamInteger}, "%d"},
	} {
		if got := parameterFormat(tc.param); got != tc.want {
			t.Errorf("%s: %q, want %q", tc.name, got, tc.want)
		}
	}
}

func TestNumberOfEveryShapeAJSONTreeCanHold(t *testing.T) {
	for _, tc := range []struct {
		in   any
		want float64
	}{
		{float64(1.5), 1.5},
		{float32(2.5), 2.5},
		{int(3), 3},
		{int64(4), 4},
		{uint64(5), 5},
		{"not a number", 0},
		{nil, 0},
	} {
		if got := numberOf(tc.in); got != tc.want {
			t.Errorf("numberOf(%v) = %v, want %v", tc.in, got, tc.want)
		}
	}
}

func TestScaleTreatsZeroAsOne(t *testing.T) {
	if got := (line{}).Scale(); got != 1 {
		t.Errorf("an unset divisor scaled by %d, want 1", got)
	}
	if got := (line{DivScale: 10}).Scale(); got != 10 {
		t.Errorf("scale = %d, want 10", got)
	}
}

func TestWritable(t *testing.T) {
	for _, tc := range []struct {
		access string
		want   bool
	}{
		{canonical.AccessWrite, true},
		{canonical.AccessReadWrite, true},
		{canonical.AccessRead, false},
		{canonical.AccessNone, false},
		{"", false},
	} {
		if got := writable(tc.access); got != tc.want {
			t.Errorf("writable(%q) = %v", tc.access, got)
		}
	}
}

func TestValueOfADeclaredParameter(t *testing.T) {
	m := buildModel(testTree(), "gateway")
	prt := m.port(1)

	// The tree declared these, and a client reading before anything is written
	// must see them rather than a zero.
	_, gain := commandOf(t, &Provider{model: m}, "frame.card1.video.gain")
	if v, _ := prt.value(gain); v.Val != 0 {
		t.Errorf("gain seeded as %d, want 0 (0.0 dB scaled)", v.Val)
	}

	_, enable := commandOf(t, &Provider{model: m}, "frame.card1.video.enable")
	if v, _ := prt.value(enable); v.Val != 1 {
		t.Errorf("enable seeded as %d, want 1", v.Val)
	}

	_, name := commandOf(t, &Provider{model: m}, "frame.card1.video.name")
	v, _ := prt.value(name)
	if v.Text != "SDI 1" || !v.Mode.Has(codec.ModeString) {
		t.Errorf("name seeded as %q mode %s", v.Text, v.Mode)
	}
}

func TestAWideMenuIsProjectedOntoTheOlderGeneration(t *testing.T) {
	// Built by hand: a command past sixteen bits needs sixty-five thousand
	// lines to arise from a tree, and what matters is the projection, not how
	// the numbers got that large.
	p := &port{
		lines: []line{
			{Index: 0, Style: codec.StyleList, Step: 2, Text: "root"},
			{Index: 1, Style: codec.StyleNumber, Command: 0x1_0000, Text: "wide command"},
			{Index: 2, Style: codec.StyleList, Step: 0x1_0000, Text: "wide span"},
			{Index: 0x1_0000, Style: codec.StyleNumber, Command: 3, Text: "unreachable"},
		},
		byCmd:   map[uint32]int{},
		byPath:  map[string]int{},
		values:  map[uint32]codec.Value{},
		display: map[int16]string{},
	}

	long := p.menu(true)
	if len(long) != 4 {
		t.Fatalf("the long-string menu has %d lines, want 4", len(long))
	}

	short := p.menu(false)
	// The line whose index cannot be addressed is not offered at all: an index
	// is how a 16-bit client asks for a line.
	if len(short) != 3 {
		t.Fatalf("the 16-bit menu has %d lines, want 3", len(short))
	}
	// The others are substituted rather than removed, so the tree keeps its
	// shape and a client can see that something is there.
	if short[1].Command != 0 || !short[1].Style.Disabled() {
		t.Errorf("a wide command came through as %d %s", short[1].Command, short[1].Style)
	}
	if short[2].Step != 0 || !short[2].Style.Disabled() {
		t.Errorf("a wide span came through as step %d %s", short[2].Step, short[2].Style)
	}
}

func TestSetValueOnAPortRefusesWhatItCannot(t *testing.T) {
	m := buildModel(testTree(), "gateway")
	prt := m.port(1)

	if _, err := prt.setValue(9999, codec.ModeValue, 0, "", nil); err == nil {
		t.Error("writing a command that does not exist should fail")
	}

	_, status := commandOf(t, &Provider{model: m}, "frame.card1.status")
	if _, err := prt.setValue(status, codec.ModeValue, 1, "", nil); err == nil {
		t.Error("writing a read-only line should fail")
	}
}

func TestCommandForPathIsCaseInsensitiveAndSkipsContainers(t *testing.T) {
	m := buildModel(testTree(), "gateway")
	prt := m.port(1)

	if _, ok := prt.commandForPath("FRAME.CARD1.VIDEO.GAIN"); !ok {
		t.Error("a path in another case did not resolve")
	}
	if _, ok := prt.commandForPath("frame.card1.video"); ok {
		t.Error("a container resolved to a command")
	}
	if _, ok := prt.commandForPath("frame.nothing"); ok {
		t.Error("a path that does not exist resolved")
	}
}

func TestDisplayLinesAreSeparateFromTheMenu(t *testing.T) {
	m := buildModel(testTree(), "gateway")
	prt := m.port(1)

	if _, ok := prt.displayLine(0); ok {
		t.Error("a line nobody set should not exist")
	}
	prt.setDisplay(0, "READY")
	if got, ok := prt.displayLine(0); !ok || got != "READY" {
		t.Errorf("display line 0 = %q %v", got, ok)
	}
}

func TestLineForFindsTheLineBehindACommand(t *testing.T) {
	m := buildModel(testTree(), "gateway")
	prt := m.port(1)

	if _, ok := prt.lineFor(9999); ok {
		t.Error("a command that does not exist has no line")
	}
	_, gain := commandOf(t, &Provider{model: m}, "frame.card1.video.gain")
	if l, ok := prt.lineFor(gain); !ok || l.Text != "gain" {
		t.Errorf("lineFor(%d) = %q %v", gain, l.Text, ok)
	}
}

func TestEncodeAnyPicksTheWireForm(t *testing.T) {
	m := buildModel(testTree(), "gateway")
	prt := m.port(1)
	_, gain := commandOf(t, &Provider{model: m}, "frame.card1.video.gain")

	mode, _, text := encodeAny(prt, gain, "hello")
	if !mode.Has(codec.ModeString) || text != "hello" {
		t.Errorf("a string encoded as mode %s %q", mode, text)
	}

	var num int32
	if mode, num, _ = encodeAny(prt, gain, true); num != 1 || !mode.Has(codec.ModeValue) {
		t.Errorf("true encoded as %d mode %s", num, mode)
	}
	if _, num, _ = encodeAny(prt, gain, false); num != 0 {
		t.Errorf("false encoded as %d", num)
	}

	// A number is scaled by the line's own divisor, so a caller works in the
	// units it sees rather than the ones the wire carries.
	if _, num, _ = encodeAny(prt, gain, -6.0); num != -60 {
		t.Errorf("-6.0 encoded as %d, want -60", num)
	}
	// A command with no line behind it has no divisor to apply.
	if _, num, _ = encodeAny(prt, 9999, 5.0); num != 5 {
		t.Errorf("an unknown command scaled to %d, want 5", num)
	}
}

func TestDecodeStored(t *testing.T) {
	if got := decodeStored(codec.Value{Mode: codec.ModeString, Text: "x"}); got != "x" {
		t.Errorf("a string came back as %v", got)
	}
	if got := decodeStored(codec.Value{Mode: codec.ModeValue, Val: 7}); got != int64(7) {
		t.Errorf("a number came back as %v", got)
	}
}
