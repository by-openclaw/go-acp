package transport

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestCaptureFormat_ConformsToADR0021 pins the on-disk JSONL shape per
// ADR-0021. Every capture line is a JSON object with `ts` (RFC3339Nano),
// `proto` (lowercase plugin name), `dir` ("tx" or "rx"), `hex` (lowercase
// hex string of the wire bytes), and `len` (int byte count, matching the
// hex string's decoded length).
func TestCaptureFormat_ConformsToADR0021(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "frames.jsonl")
	rec, err := NewRecorder(path)
	if err != nil {
		t.Fatalf("NewRecorder: %v", err)
	}

	rec.Record("acp1", "tx", []byte{0x00, 0x00, 0x00, 0x01, 0x01, 0x01, 0x00, 0x05})
	rec.Record("acp1", "rx", []byte{0x00, 0x00, 0x00, 0x01, 0x01, 0x02, 0x00, 0x05, 0x01, 0x02})
	if err := rec.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer func() { _ = f.Close() }()

	scanner := bufio.NewScanner(f)
	var n int
	for scanner.Scan() {
		var rec CaptureRecord
		if err := json.Unmarshal(scanner.Bytes(), &rec); err != nil {
			t.Fatalf("decode line %d: %v\nraw: %s", n, err, scanner.Text())
		}
		if rec.Timestamp == "" {
			t.Errorf("line %d: missing ts", n)
		}
		if rec.Proto != "acp1" {
			t.Errorf("line %d: proto = %q, want acp1", n, rec.Proto)
		}
		if rec.Direction != "tx" && rec.Direction != "rx" {
			t.Errorf("line %d: dir = %q, want tx or rx", n, rec.Direction)
		}
		if rec.Len <= 0 {
			t.Errorf("line %d: len = %d", n, rec.Len)
		}
		// hex string length must be 2*len
		if len(rec.Hex) != 2*rec.Len {
			t.Errorf("line %d: hex length %d != 2*len %d", n, len(rec.Hex), rec.Len)
		}
		// hex must be lowercase (per ADR-0021).
		if rec.Hex != strings.ToLower(rec.Hex) {
			t.Errorf("line %d: hex %q must be lowercase", n, rec.Hex)
		}
		n++
	}
	if scanner.Err() != nil {
		t.Fatalf("scan: %v", scanner.Err())
	}
	if n != 2 {
		t.Fatalf("got %d records, want 2", n)
	}
}

// TestCaptureFormat_NilRecorderSafe documents that Record on a nil
// Recorder is a no-op. Used by verbs that may not have set up a
// recorder (no --capture flag) but still call Record() unconditionally.
func TestCaptureFormat_NilRecorderSafe(t *testing.T) {
	var r *Recorder
	r.Record("acp1", "tx", []byte{0x01})
	if err := r.Close(); err != nil {
		t.Fatalf("Close on nil Recorder: %v", err)
	}
}

// TestCapture_WrapTransport_RecordsBothDirections proves the
// RecordingTransport wrapper records both Send and Receive.
func TestCapture_WrapTransport_RecordsBothDirections(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "frames.jsonl")
	rec, err := NewRecorder(path)
	if err != nil {
		t.Fatalf("NewRecorder: %v", err)
	}

	stub := &stubInnerTransport{rxData: []byte{0xAA, 0xBB}}
	wrapped := rec.WrapTransport(stub, "acp1")
	if err := wrapped.Send(context.Background(), []byte{0x11, 0x22}); err != nil {
		t.Fatalf("Send: %v", err)
	}
	got, err := wrapped.Receive(context.Background(), 16)
	if err != nil {
		t.Fatalf("Receive: %v", err)
	}
	if len(got) != 2 || got[0] != 0xAA || got[1] != 0xBB {
		t.Fatalf("recv data = %v", got)
	}
	if err := rec.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	// Read records.
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer func() { _ = f.Close() }()
	scanner := bufio.NewScanner(f)
	var dirs []string
	for scanner.Scan() {
		var r CaptureRecord
		if err := json.Unmarshal(scanner.Bytes(), &r); err != nil {
			t.Fatalf("decode: %v", err)
		}
		dirs = append(dirs, r.Direction)
	}
	if len(dirs) != 2 || dirs[0] != "tx" || dirs[1] != "rx" {
		t.Fatalf("dirs = %v, want [tx rx]", dirs)
	}
}

// stubInnerTransport is the test double for RecordingTransport's inner
// field. Send accepts every payload; Receive returns rxData once, then
// io.EOF (forwarded as nil err in this stub for simplicity).
type stubInnerTransport struct {
	rxData []byte
	served bool
}

func (s *stubInnerTransport) Send(_ context.Context, _ []byte) error { return nil }

func (s *stubInnerTransport) Receive(_ context.Context, _ int) ([]byte, error) {
	if s.served {
		return nil, errors.New("stub: drained")
	}
	s.served = true
	return s.rxData, nil
}

func (s *stubInnerTransport) Close() error { return nil }
