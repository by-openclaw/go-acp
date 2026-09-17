package wiretrace

import (
	"errors"
	"strings"
	"testing"
)

// ReadTrames skips blank lines, refuses a schema newer than it knows, and
// surfaces a scanner failure (a line over the 1 MiB cap) as an error
// rather than silently truncating the capture.
func TestReadTramesEdges(t *testing.T) {
	in := "\n" + `{"schema_version":1,"meta":{"cli":"dhs"}}` + "\n" +
		`{"schema_version":1,"dir":"tx","hex":"01"}` + "\n\n"
	trames, err := ReadTrames(strings.NewReader(in))
	if err != nil || len(trames) != 1 {
		t.Fatalf("blank lines and the ADR-0028 meta line must be skipped: %v (%d trames)", err, len(trames))
	}
	if _, err := ReadTrames(strings.NewReader(`{"dir":` + "\n")); err == nil || !strings.Contains(err.Error(), "line 1") {
		t.Errorf("a malformed line must be reported with its number, got %v", err)
	}
	_, err = ReadTrames(strings.NewReader(`{"schema_version":99,"dir":"tx","hex":"01"}` + "\n"))
	if err == nil || !strings.Contains(err.Error(), "schema_version 99") {
		t.Errorf("a newer schema must be refused, got %v", err)
	}
	huge := `{"dir":"tx","hex":"` + strings.Repeat("00", 600*1024) + `"}` + "\n"
	if _, err := ReadTrames(strings.NewReader(huge)); err == nil || !strings.Contains(err.Error(), "scan") {
		t.Errorf("an over-long line must surface the scanner error, got %v", err)
	}
}

type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) { return 0, errors.New("disk full") }

// WriteTrames reports the trame that could not be written.
func TestWriteTramesReportsWriteFailure(t *testing.T) {
	err := WriteTrames(failingWriter{}, []Trame{{Direction: DirectionTx, Hex: "01"}})
	if err == nil || !strings.Contains(err.Error(), "write trame 0") {
		t.Errorf("write failure = %v, want it to name trame 0", err)
	}
}
