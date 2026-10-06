package codec

import (
	"errors"
	"fmt"
	"io"
	"strings"
	"testing"
)

// §6.5: "Port 5 on Slot 2 has the port address ((2 - 1) * 8 + 5 - 1) = 12".
func TestPortNumber(t *testing.T) {
	tests := []struct {
		slot, pos, want int
	}{
		{2, 5, 12},
		{1, 1, 0},
		{1, 8, 7},
		{16, 8, 127},
		{32, 8, 255},
	}
	for _, tc := range tests {
		got, err := PortNumber(tc.slot, tc.pos)
		if err != nil || got != tc.want {
			t.Errorf("PortNumber(%d, %d) = %d, %v; want %d", tc.slot, tc.pos, got, err, tc.want)
		}
		slot, pos, err := SlotPosition(tc.want)
		if err != nil || slot != tc.slot || pos != tc.pos {
			t.Errorf("SlotPosition(%d) = %d, %d, %v; want %d, %d", tc.want, slot, pos, err, tc.slot, tc.pos)
		}
	}
	for _, bad := range [][2]int{{0, 1}, {1, 0}, {1, 9}, {33, 1}} {
		if _, err := PortNumber(bad[0], bad[1]); !errors.Is(err, ErrRange) {
			t.Errorf("PortNumber(%d, %d): got %v, want ErrRange", bad[0], bad[1], err)
		}
	}
	for _, bad := range []int{-1, 256} {
		if _, _, err := SlotPosition(bad); !errors.Is(err, ErrRange) {
			t.Errorf("SlotPosition(%d): got %v, want ErrRange", bad, err)
		}
	}
}

// §8.5: range -36..36, "gain [dB] = Gain / 2.0", -128 is mute.
func TestGainDB(t *testing.T) {
	tests := []struct {
		gain int
		db   float64
		mute bool
	}{
		{-36, -18, false}, {0, 0, false}, {1, 0.5, false}, {36, 18, false}, {-128, 0, true},
	}
	for _, tc := range tests {
		db, mute, err := GainDB(tc.gain)
		if err != nil || db != tc.db || mute != tc.mute {
			t.Errorf("GainDB(%d) = %v, %v, %v; want %v, %v", tc.gain, db, mute, err, tc.db, tc.mute)
		}
	}
	for _, bad := range []int{-37, 37, -127} {
		if _, _, err := GainDB(bad); !errors.Is(err, ErrRange) {
			t.Errorf("GainDB(%d): got %v, want ErrRange", bad, err)
		}
	}
}

// §6.5: "<= 0 mute; 1..255 (volume-230)/2 dB; > 255 +12.5 dB".
func TestVolumeDB(t *testing.T) {
	tests := []struct {
		vol  int
		db   float64
		mute bool
	}{
		{-1, 0, true}, {0, 0, true}, {1, -114.5, false}, {230, 0, false}, {255, 12.5, false}, {256, 12.5, false},
	}
	for _, tc := range tests {
		db, mute := VolumeDB(tc.vol)
		if db != tc.db || mute != tc.mute {
			t.Errorf("VolumeDB(%d) = %v, %v; want %v, %v", tc.vol, db, mute, tc.db, tc.mute)
		}
	}
}

// §6.5: a starting character followed by 10 digits; RRCS uses "R".
func TestTransKey(t *testing.T) {
	if got := NewTransKey('C', 2817191); got != "C0002817191" {
		t.Errorf("NewTransKey = %q, want the §11.1 key C0002817191", got)
	}
	if got := NewTransKey(RRCSPrefix, 1947584733); got != "R1947584733" {
		t.Errorf("NewTransKey = %q, want the §11.2 key R1947584733", got)
	}
	if got := NewTransKey('X', 123456789012); got != "X3456789012" {
		t.Errorf("NewTransKey kept more than ten digits: %q", got)
	}
	for _, good := range []string{"C0002817191", "R1947584733", "#0000000000"} {
		if !ValidTransKey(good) {
			t.Errorf("ValidTransKey(%q) = false", good)
		}
	}
	for _, bad := range []string{"", "C000281719", "C00028171911", "C00028171x1"} {
		if ValidTransKey(bad) {
			t.Errorf("ValidTransKey(%q) = true", bad)
		}
	}
}

// §7, every row.
func TestErrorCodes(t *testing.T) {
	want := map[int]string{
		0: "Success", 1: "Transaction key invalid", 2: "Net address invalid", 3: "Node address invalid",
		4: "Port address invalid", 5: "Slot no. invalid", 6: "Input Gain invalid", 7: "IP-address invalid",
		8: "TCP-Port invalid", 9: "Label invalid", 10: "Conference position invalid",
		11: "Operation failed, because Artist-network not connected",
		12: "Operation failed, because route does not exist",
		13: "Operation not possible, because gateway is standby",
		14: "XML-RPC parameters wrong for this request",
		15: "Invalid conference or conference not found",
		16: "Invalid conference member or conference member not found",
		17: "Invalid priority", 18: "Invalid GPIO number", 19: "Invalid gain value", 20: "Timeout",
		21: "No permission", 22: "Object does not exist", 23: "No USB-dongle available",
		24: "Port is not online", 25: "Object property not supported", 26: "Limit exceeded", 99: "Generic error",
	}
	if len(codeText) != len(want) {
		t.Errorf("table has %d codes, §7 has %d", len(codeText), len(want))
	}
	for n, text := range want {
		c := ErrorCode(n)
		if !c.Known() || c.String() != text {
			t.Errorf("code %d: Known=%v String=%q, want %q", n, c.Known(), c.String(), text)
		}
	}
	unknown := ErrorCode(27)
	if unknown.Known() || unknown.String() != "error code 27" {
		t.Errorf("code 27: Known=%v String=%q", unknown.Known(), unknown.String())
	}
	if CodeSuccess.Err() != nil {
		t.Error("success is an error")
	}
	err := CodeGatewayStandby.Err()
	if err.Error() != "rrcs: Operation not possible, because gateway is standby (code 13)" {
		t.Errorf("Error() = %q", err.Error())
	}
	if !errors.Is(fmt.Errorf("wrapped: %w", err), &CodeError{Code: CodeGatewayStandby}) {
		t.Error("errors.Is did not match the same code")
	}
	if errors.Is(err, &CodeError{Code: CodeTimeout}) || errors.Is(err, ErrMalformed) {
		t.Error("errors.Is matched a different error")
	}
	var ce *CodeError
	if !errors.As(err, &ce) || ce.Code != CodeGatewayStandby {
		t.Errorf("errors.As: %+v", ce)
	}
}

func TestParseResult(t *testing.T) {
	t.Run("array, §11.1", func(t *testing.T) {
		resp, err := DecodeResponse([]byte(specSetXpResponse))
		if err != nil {
			t.Fatalf("DecodeResponse: %v", err)
		}
		r, err := ParseResult(resp.Value)
		if err != nil || r.TransKey != "C0002817191" || r.Code != CodeSuccess || len(r.Rest) != 0 {
			t.Errorf("got %+v, %v", r, err)
		}
	})
	t.Run("array with payload", func(t *testing.T) {
		r, err := ParseResult(Array(String("C0000000001"), Int(24), Bool(true)))
		if err != nil || r.Code != CodePortNotOnline || len(r.Rest) != 1 || !r.Rest[0].Bool {
			t.Errorf("got %+v, %v", r, err)
		}
	})
	t.Run("struct, §8.7", func(t *testing.T) {
		v := Struct(Member{"ErrorCode", Int(0)}, Member{"TransKey", String("C0000000002")}, Member{"LogicSourceCount", Int(3)})
		r, err := ParseResult(v)
		if err != nil || r.TransKey != "C0000000002" || r.Code != CodeSuccess {
			t.Fatalf("got %+v, %v", r, err)
		}
		if n, ok := r.Fields.Field("LogicSourceCount"); !ok || n.Int != 3 {
			t.Errorf("payload member lost: %+v", r.Fields)
		}
	})
	bad := map[string]Value{
		"scalar":              Int(0),
		"short array":         Array(String("C0000000001")),
		"array key not text":  Array(Int(1), Int(0)),
		"array code not int":  Array(String("C0000000001"), String("0")),
		"struct no key":       Struct(Member{"ErrorCode", Int(0)}),
		"struct key not text": Struct(Member{"TransKey", Int(1)}, Member{"ErrorCode", Int(0)}),
		"struct no code":      Struct(Member{"TransKey", String("C0000000001")}),
	}
	for name, v := range bad {
		t.Run(name, func(t *testing.T) {
			_, err := ParseResult(v)
			if !errors.Is(err, ErrMalformed) && !errors.Is(err, ErrType) {
				t.Errorf("got %v, want ErrMalformed or ErrType", err)
			}
		})
	}
}

func TestParseXMLLimitsAndNoise(t *testing.T) {
	deep := strings.Repeat("<a>", maxDepth+1) + strings.Repeat("</a>", maxDepth+1)
	if _, err := parseXML([]byte(deep)); !errors.Is(err, ErrMalformed) {
		t.Errorf("nesting past the bound: %v", err)
	}
	atBound := strings.Repeat("<a>", maxDepth) + strings.Repeat("</a>", maxDepth)
	if _, err := parseXML([]byte(atBound)); err != nil {
		t.Errorf("nesting at the bound: %v", err)
	}
	// Comments, processing instructions and attributes carry nothing.
	doc := `<?xml version="1.0"?><!-- c --><value kind="x"><!-- inner --><int>5</int></value>`
	if v, err := specValue(doc); err != nil || v.Int != 5 {
		t.Errorf("noise was not ignored: %+v, %v", v, err)
	}
	if _, err := parseXML([]byte(`<?xml version="1.0"?>`)); !errors.Is(err, ErrMalformed) {
		t.Errorf("no root: %v", err)
	}
}

func TestCharsets(t *testing.T) {
	for _, enc := range []string{"UTF-8", "utf8", "US-ASCII", "ascii"} {
		doc := `<?xml version="1.0" encoding="` + enc + `"?><value>ok</value>`
		if v, err := specValue(doc); err != nil || v.Str != "ok" {
			t.Errorf("%s: %+v, %v", enc, v, err)
		}
	}
	// "Zürich" in ISO-8859-1: ü is the single byte 0xFC.
	latin := append([]byte(`<?xml version="1.0" encoding="ISO-8859-1"?><value>Z`), 0xFC)
	latin = append(latin, []byte(`rich</value>`)...)
	if v, err := specValue(string(latin)); err != nil || v.Str != "Zürich" {
		t.Errorf("ISO-8859-1: %+v, %v", v, err)
	}
	if _, err := specValue(`<?xml version="1.0" encoding="EBCDIC"?><value>x</value>`); !errors.Is(err, ErrMalformed) {
		t.Errorf("unknown encoding: %v", err)
	}
}

// The converter must give the same bytes whatever the size of the
// buffer it is read into, including one too small for a two-byte
// character.
func TestLatin1ReaderSmallBuffers(t *testing.T) {
	in := "a\xE9\xFCz\xFF"
	want := "aéüzÿ"
	for size := 1; size <= 8; size++ {
		r := &latin1Reader{r: strings.NewReader(in)}
		var out []byte
		buf := make([]byte, size)
		for {
			n, err := r.Read(buf)
			out = append(out, buf[:n]...)
			if err == io.EOF {
				break
			}
			if err != nil {
				t.Fatalf("size %d: %v", size, err)
			}
		}
		if string(out) != want {
			t.Errorf("size %d: got %q, want %q", size, out, want)
		}
	}
}
