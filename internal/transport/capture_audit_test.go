package transport

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
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
	rxData   []byte
	served   bool
	closed   bool
	closeErr error
}

func (s *stubInnerTransport) Send(_ context.Context, _ []byte) error { return nil }

func (s *stubInnerTransport) Receive(_ context.Context, _ int) ([]byte, error) {
	if s.served {
		return nil, errors.New("stub: drained")
	}
	s.served = true
	return s.rxData, nil
}

func (s *stubInnerTransport) Close() error {
	s.closed = true
	return s.closeErr
}

// A path that cannot be created — here a directory — is a typed error, not
// a nil Recorder that silently drops every frame of the session.
func TestCapture_NewRecorderCreateFailure(t *testing.T) {
	_, err := NewRecorder(t.TempDir())
	if !errors.Is(err, ErrCaptureCreateFailed) {
		t.Fatalf("NewRecorder(dir) = %v, want ErrCaptureCreateFailed", err)
	}
}

// WriteMeta is LINE ONE of a capture per ADR-0028: a schema_version, a
// timestamp and the caller's meta object, and nothing when there is no
// recorder or no meta to write.
func TestCapture_WriteMeta(t *testing.T) {
	var none *Recorder
	none.WriteMeta(map[string]string{"k": "v"}) // nil-safe like Record

	path := filepath.Join(t.TempDir(), "frames.jsonl")
	rec, err := NewRecorder(path)
	if err != nil {
		t.Fatalf("NewRecorder: %v", err)
	}
	rec.WriteMeta(nil) // nothing to describe: no line
	rec.WriteMeta(map[string]string{"device": "neuron"})
	rec.Record("acp2", "tx", []byte{0x01})
	if err := rec.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	lines := readLines(t, path)
	if len(lines) != 2 {
		t.Fatalf("got %d lines, want meta + one record:\n%s", len(lines), strings.Join(lines, "\n"))
	}
	var meta struct {
		SchemaVersion int               `json:"schema_version"`
		Timestamp     string            `json:"ts"`
		Meta          map[string]string `json:"meta"`
	}
	if err := json.Unmarshal([]byte(lines[0]), &meta); err != nil {
		t.Fatalf("decode meta line: %v\nraw: %s", err, lines[0])
	}
	if meta.SchemaVersion != 1 || meta.Timestamp == "" || meta.Meta["device"] != "neuron" {
		t.Errorf("meta line = %+v, want schema_version 1, a ts and the caller's meta", meta)
	}
}

// A meta object JSON cannot encode must not poison the capture: the line is
// not written (the encoder fails before touching the file), the failure is
// said out loud on stderr, and the recorder keeps recording traffic.
func TestCapture_WriteMetaEncodeFailureIsReported(t *testing.T) {
	path := filepath.Join(t.TempDir(), "frames.jsonl")
	rec, err := NewRecorder(path)
	if err != nil {
		t.Fatalf("NewRecorder: %v", err)
	}
	stderr := captureStderr(t, func() { rec.WriteMeta(make(chan int)) })
	if !strings.Contains(stderr, "capture meta write error") {
		t.Errorf("stderr = %q, want the meta write error reported", stderr)
	}
	rec.Record("acp2", "rx", []byte{0x02})
	if err := rec.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	lines := readLines(t, path)
	if len(lines) != 1 {
		t.Fatalf("got %d lines, want only the record:\n%s", len(lines), strings.Join(lines, "\n"))
	}
	var r CaptureRecord
	if err := json.Unmarshal([]byte(lines[0]), &r); err != nil || r.Direction != "rx" {
		t.Errorf("record after a failed meta = %+v, %v; want the rx record intact", r, err)
	}
}

// Close is forwarded to the wrapped transport, error included: the recorder
// must not swallow the close of the socket it was only observing.
func TestCapture_WrapTransport_CloseForwards(t *testing.T) {
	rec, err := NewRecorder(filepath.Join(t.TempDir(), "frames.jsonl"))
	if err != nil {
		t.Fatalf("NewRecorder: %v", err)
	}
	defer func() { _ = rec.Close() }()

	stub := &stubInnerTransport{closeErr: errors.New("stub: close refused")}
	wrapped := rec.WrapTransport(stub, "acp1")
	if err := wrapped.Close(); !errors.Is(err, stub.closeErr) {
		t.Errorf("Close = %v, want the inner transport's error forwarded", err)
	}
	if !stub.closed {
		t.Error("Close did not reach the inner transport")
	}
}

// readLines returns the non-empty lines of a capture file.
func readLines(t *testing.T, path string) []string {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	var lines []string
	for _, l := range strings.Split(string(raw), "\n") {
		if strings.TrimSpace(l) != "" {
			lines = append(lines, l)
		}
	}
	return lines
}

// captureStderr runs fn with os.Stderr pointed at a temp file and returns
// what fn wrote there. Tests in this package do not run in parallel, so
// swapping the process-wide handle for the duration is safe.
func captureStderr(t *testing.T, fn func()) string {
	t.Helper()
	f, err := os.CreateTemp(t.TempDir(), "stderr")
	if err != nil {
		t.Fatalf("temp stderr: %v", err)
	}
	orig := os.Stderr
	os.Stderr = f
	defer func() { os.Stderr = orig }()
	fn()
	os.Stderr = orig
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		t.Fatalf("seek: %v", err)
	}
	out, err := io.ReadAll(f)
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		t.Fatalf("read captured stderr: %v", err)
	}
	return string(out)
}
