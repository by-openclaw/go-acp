package codec

import (
	"bytes"
	"errors"
	"testing"
)

func TestBuilders(t *testing.T) {
	t.Parallel()
	text, err := WriteText(3, 1, 0, "CAM 1")
	if err != nil {
		t.Fatal(err)
	}
	multi, err := Multiple(SetColour(1, 2), Clear(4))
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name      string
		got, want []byte
	}{
		{"poll §1.2.1", Poll(), []byte{0x01}},
		{"sound alarm §1.2.3", SoundAlarm(0, 10), []byte{0x03, 0x00, 0x0A}},
		{"clear all §1.2.4", Clear(0), []byte{0x04, 0x00}},
		{"set colour §1.2.5", SetColour(5, 0x1A), []byte{0x05, 0x05, 0x1A}},
		{"show bitmap §1.2.8", ShowBitmap(2, 101), []byte{0x08, 0x02, 0x65}},
		{"write text §1.2.9", text, []byte{0x09, 0x03, 0x01, 0x00, 'C', 'A', 'M', ' ', '1', 0x00}},
		{"set GPO §1.2.10", SetGPO([]byte{0x01, 0x80}), []byte{0x0A, 0x02, 0x01, 0x80}},
		{"read T-bar §1.2.15", ReadTBar(1), []byte{0x0F, 0x01}},
		{"set brightness §1.2.41", SetBrightness(0, 7), []byte{0x29, 0x00, 0x07}},
		{"set RGB LSB first §1.2.53", SetRGBColour(4, 0xF800), []byte{0x35, 0x04, 0x00, 0xF8}},
		{"multiple §1.2.64", multi, []byte{0x40, 0x02, 0x00, 0x05, 0x05, 0x01, 0x02, 0x04, 0x04}},
		{"report firmware §1.2.113", ReportFirmware(), []byte{0x71}},
		{"read MAC §1.2.118", ReadMAC(), []byte{0x76}},
	}
	for _, c := range cases {
		if !bytes.Equal(c.got, c.want) {
			t.Errorf("%s: % x, want % x", c.name, c.got, c.want)
		}
	}
	if _, err := WriteText(1, 0, 0, "é"); !errors.Is(err, ErrText) {
		t.Fatalf("non-ASCII text: %v", err)
	}
	if _, err := Multiple(Poll()); err == nil {
		t.Fatal("multiple with one command must fail")
	}
	if _, err := Multiple(Poll(), []byte{0x40, 0, 0, 0}); err == nil {
		t.Fatal("nested 0x40 must fail")
	}
}

// Bytes captured from the lab's CCP-1601 (10.6.255.105, firmware
// DEL869-12) on 2026-10-25, and spec layouts for the rest.
func TestNextFraming(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		in   []byte
		cmd  byte
		data []byte
		n    int
	}{
		{"poll reply (device)", []byte{0x81, 0x01, 0x0d, 0x01, 0x00, 0x01}, RspPoll, []byte{0x01, 0x0d, 0x01, 0x00, 0x01}, 6},
		{"firmware, 9 chars no NUL (device)", append([]byte{0xF1}, "DEL869-12"...), RspFirmware, []byte("DEL869-12"), 10},
		{"firmware, NUL-terminated", []byte{0xF1, '0', '1', '0', '4', 0, 0x84}, RspFirmware, []byte("0104"), 6},
		{"error invalid command (device)", []byte{0x83, 0x02, 0x76, 0x00}, RspError, []byte{0x02, 0x76, 0x00}, 4},
		{"DHCP (device)", []byte{0x89, 0x01}, RspDHCP, []byte{0x01}, 2},
		{"subnet (device)", []byte{0x87, 0xff, 0xff, 0xff, 0x00}, RspSubnet, []byte{0xff, 0xff, 0xff, 0x00}, 5},
		{"ack", []byte{0x84, 0x84}, RspAck, []byte{}, 1},
		{"button down", []byte{0x82, 0x01, 0x07, 0x00}, RspButton, []byte{0x01, 0x07, 0x00}, 4},
		{"T-bar movement 16-bit", []byte{0x82, 0x05, 0x01, 0x27, 0x10}, RspButton, []byte{0x05, 0x01, 0x27, 0x10}, 5},
		{"GPI 6 bytes", []byte{0x85, 0x06, 1, 2, 3, 4, 5, 6}, RspGPI, []byte{0x06, 1, 2, 3, 4, 5, 6}, 8},
		{"MAC", []byte{0x8B, 0, 3, 0x41, 9, 2, 0x14}, RspMAC, []byte{0, 3, 0x41, 9, 2, 0x14}, 7},
		{"serial data", []byte{0x8E, 0x00, 0x02, 'h', 'i'}, RspSerialData, []byte{0x00, 0x02, 'h', 'i'}, 5},
	}
	for _, c := range cases {
		f, n, err := Next(c.in)
		if err != nil || n != c.n || f.Cmd != c.cmd || !bytes.Equal(f.Data, c.data) {
			t.Errorf("%s: frame %x % x n=%d err=%v; want %x % x n=%d", c.name, f.Cmd, f.Data, n, err, c.cmd, c.data, c.n)
		}
	}
	for _, partial := range [][]byte{{}, {0x81, 0x01}, {0x82}, {0x82, 0x05, 0x01, 0x27}, {0x85, 0x06, 1}, {0xF1, 'D', 'E'}, {0x8E, 0x00}} {
		if _, n, err := Next(partial); n != 0 || err != nil {
			t.Errorf("partial % x: n=%d err=%v, want more bytes", partial, n, err)
		}
	}
	var unk ErrUnknownCommand
	if _, _, err := Next([]byte{0x90, 0x00}); !errors.As(err, &unk) || unk.Byte != 0x90 {
		t.Fatalf("reserved 0x90 must be unknown: %v", err)
	}
}

func TestParsers(t *testing.T) {
	t.Parallel()
	p, err := ParsePoll(Frame{Cmd: RspPoll, Data: []byte{0x01, 0x0d, 0x01, 0x00, 0x01}})
	if err != nil || p.Model != "CCP-1601" || p.ProtocolVersion != 13 || !p.PSUA || p.PSUB || !p.RefreshRequired {
		t.Fatalf("poll %+v %v", p, err)
	}
	b, _ := ParseButton(Frame{Cmd: RspButton, Data: []byte{0x05, 0x01, 0x27, 0x10}})
	if b.ActionName() != "t-bar" || b.ID != 1 || b.Value != 10000 {
		t.Fatalf("t-bar %+v", b)
	}
	b, _ = ParseButton(Frame{Cmd: RspButton, Data: []byte{0x01, 0x07, 0x00}})
	if b.ActionName() != "down" || b.ID != 7 {
		t.Fatalf("button %+v", b)
	}
	e, _ := ParseError(Frame{Cmd: RspError, Data: []byte{0x02, 0x76, 0x00}})
	if e.Error() != "ccp: panel refused command 0x76 (read MAC) as invalid" {
		t.Fatalf("error text %q", e.Error())
	}
	if IPv4([]byte{192, 168, 0, 1}) != "192.168.0.1" || Firmware(Frame{Data: []byte("DEL869-12")}) != "DEL869-12" {
		t.Fatal("IPv4 / Firmware")
	}
	if ExpectedReply(CmdPoll) != RspPoll || ExpectedReply(CmdSetColour) != RspAck || ExpectedReply(CmdReadMAC) != RspMAC {
		t.Fatal("ExpectedReply")
	}
}
