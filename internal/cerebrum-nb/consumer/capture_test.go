package cerebrumnb

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// captureRec mirrors transport.CaptureRecord for decoding the JSONL
// wire-trace in assertions (kept local so the test reads standalone).
type captureRec struct {
	Proto string `json:"proto"`
	Dir   string `json:"dir"`
	Hex   string `json:"hex"`
	Len   int    `json:"len"`
}

// TestConnect_CaptureRecordsWire proves the #242 contract: with
// Plugin.Capture set, every TX/RX XML document lands in the JSONL
// wire-trace with proto=cerebrum-nb and the exact wire bytes.
func TestConnect_CaptureRecordsWire(t *testing.T) {
	path := filepath.Join(t.TempDir(), "capture.jsonl")
	fs := newFakeCerebrum(t, func(fc *fakeConn) {
		drainClient(fc, func(frame []byte) ([]byte, bool) {
			return []byte(`<POLL_REPLY MTID="` + mtidOf(frame) +
				`" CONNECTED_SERVER_ACTIVE="1" PRIMARY_SERVER_STATE="1" SECONDARY_SERVER_STATE="0"/>`), true
		})
	})
	p := NewPlugin(nil)
	p.Capture = path
	ctx, cancel := ctx2s(t)
	defer cancel()
	if err := p.Connect(ctx, fs.host(), fs.port()); err != nil {
		t.Fatalf("connect: %v", err)
	}
	if _, err := p.Session().Poll(ctx); err != nil {
		t.Fatalf("poll: %v", err)
	}
	if err := p.Disconnect(); err != nil {
		t.Fatalf("disconnect: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read capture: %v", err)
	}
	var tx, rx int
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		if line == "" {
			continue
		}
		var r captureRec
		if err := json.Unmarshal([]byte(line), &r); err != nil {
			t.Fatalf("bad JSONL line %q: %v", line, err)
		}
		if r.Proto != "cerebrum-nb" {
			t.Fatalf("proto = %q, want cerebrum-nb", r.Proto)
		}
		raw, err := hex.DecodeString(r.Hex)
		if err != nil || len(raw) != r.Len {
			t.Fatalf("hex/len mismatch on %q: %v", line, err)
		}
		doc := string(raw)
		switch r.Dir {
		case "tx":
			tx++
			if !strings.Contains(doc, "POLL") {
				t.Fatalf("tx doc = %q, want the POLL document", doc)
			}
		case "rx":
			rx++
			if !strings.Contains(doc, "POLL_REPLY") {
				t.Fatalf("rx doc = %q, want the POLL_REPLY document", doc)
			}
		default:
			t.Fatalf("dir = %q", r.Dir)
		}
	}
	if tx < 1 || rx < 1 {
		t.Fatalf("capture holds tx=%d rx=%d records, want at least one each", tx, rx)
	}
}

// TestConnect_CaptureCreateError: an uncreatable capture path fails
// Connect up front with a --capture error (nothing dialed).
func TestConnect_CaptureCreateError(t *testing.T) {
	p := NewPlugin(nil)
	p.Capture = filepath.Join(t.TempDir(), "no", "such", "dir", "x.jsonl")
	ctx, cancel := ctx2s(t)
	defer cancel()
	err := p.Connect(ctx, "127.0.0.1", 1)
	if err == nil || !strings.Contains(err.Error(), "--capture") {
		t.Fatalf("want --capture create error, got %v", err)
	}
}

// TestConnect_CaptureDialError: a dial failure with capture set closes
// the recorder (no leaked handle) and surfaces the dial error.
func TestConnect_CaptureDialError(t *testing.T) {
	path := filepath.Join(t.TempDir(), "capture.jsonl")
	p := NewPlugin(nil)
	p.Capture = path
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	// Port 0 → DefaultPort 40007 on localhost, nothing listening (same
	// assumption as TestConnect_DialError).
	err := p.Connect(ctx, "127.0.0.1", 0)
	if err == nil || !strings.Contains(err.Error(), "ws dial") {
		t.Fatalf("want ws dial error, got %v", err)
	}
	// Recorder must be closed — on Windows an open handle would make
	// the TempDir cleanup fail; removing explicitly proves it is free.
	if rmErr := os.Remove(path); rmErr != nil {
		t.Fatalf("capture file still locked after dial failure: %v", rmErr)
	}
}
