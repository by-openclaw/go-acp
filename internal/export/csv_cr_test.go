package export

import (
	"bytes"
	"errors"
	"strings"
	"testing"
	"testing/iotest"

	"dhs/internal/consumer"
)

// An SDP keeps its \r\n line ends through an export and back, whether the
// file's own records end in \n (WriteCSV) or in \r\n (saved by a
// spreadsheet).
func TestCSVKeepsTheCarriageReturnsOfAValue(t *testing.T) {
	sdp := "v=0\r\no=- 1 2 IN IP4 10.6.40.60\r\ns=ATX-01\r\na=mid:secondary\r\n"
	snap := &Snapshot{Device: DeviceInfo{IP: "10.6.255.103", Protocol: "ccm"}, Slots: []SlotDump{{Slot: 0, Objects: []consumer.Object{{
		Path: []string{"io", "ip", "receivers", "audio", "u", "sdp"}, Label: "sdp", Kind: consumer.KindString,
		Access: 3, Value: consumer.Value{Kind: consumer.KindString, Str: sdp},
	}}}}}
	var buf bytes.Buffer
	if err := WriteCSV(&buf, snap); err != nil {
		t.Fatal(err)
	}
	for name, file := range map[string]string{
		"as written":            buf.String(),
		"records ended in CRLF": strings.ReplaceAll(strings.ReplaceAll(buf.String(), "\r\n", "\x00"), "\n", "\r\n"),
	} {
		file = strings.ReplaceAll(file, "\x00", "\r\n")
		got, err := ReadCSV(strings.NewReader(file))
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if v := got.Slots[0].Objects[0].Value.Str; v != sdp {
			t.Errorf("%s: %q, want %q", name, v, sdp)
		}
	}
}

// A file that cannot be read is an error, not an empty snapshot.
func TestReadCSVReportsAReadError(t *testing.T) {
	if _, err := ReadCSV(iotest.ErrReader(errors.New("disk gone"))); err == nil {
		t.Error("a failed read gave no error")
	}
}
