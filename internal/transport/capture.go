// Package transport — capture.go provides a JSONL traffic recorder for
// generating unit test fixtures and mock data from real device sessions.
//
// Usage:
//
//	rec, err := transport.NewRecorder("capture_acp1_slot0.jsonl")
//	defer rec.Close()
//	// ACP1: wrap transport
//	wrapped := rec.WrapTransport(udpConn, "acp1")
//	// ACP2: call rec.Record() directly from session send/receive
//
// Each line in the output file is a JSON object:
//
//	{"ts":"2026-04-16T14:30:00.123Z","proto":"acp2","dir":"tx","hex":"c63502..."}
//	{"ts":"2026-04-16T14:30:00.125Z","proto":"acp2","dir":"rx","hex":"c63502..."}
//
// The hex field contains the COMPLETE wire bytes:
//   - ACP1 UDP: raw datagram (7-byte header + MDATA)
//   - ACP1 TCP: MLEN prefix + payload
//   - ACP2: full AN2 frame (8-byte header + payload)
package transport

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"sync"
	"time"
)

// CaptureRecord is one line in the JSONL capture file.
type CaptureRecord struct {
	Timestamp string `json:"ts"`
	Proto     string `json:"proto"`
	Direction string `json:"dir"` // "tx" or "rx"
	Hex       string `json:"hex"`
	Len       int    `json:"len"`
}

// Recorder writes raw wire bytes to a JSONL file for later use in unit
// tests. Thread-safe: multiple goroutines can call Record concurrently.
type Recorder struct {
	mu   sync.Mutex
	file *os.File
	enc  *json.Encoder
}

// NewRecorder creates a capture file at path. Truncates if exists.
func NewRecorder(path string) (*Recorder, error) {
	f, err := os.Create(path)
	if err != nil {
		return nil, fmt.Errorf("capture: create %s: %w", path, err)
	}
	return &Recorder{
		file: f,
		enc:  json.NewEncoder(f),
	}, nil
}

// Record writes one capture record. proto is "acp1" or "acp2".
// dir is "tx" or "rx". data is the raw wire bytes.
func (r *Recorder) Record(proto, dir string, data []byte) {
	if r == nil || r.file == nil {
		return
	}
	rec := CaptureRecord{
		Timestamp: time.Now().UTC().Format(time.RFC3339Nano),
		Proto:     proto,
		Direction: dir,
		Hex:       hex.EncodeToString(data),
		Len:       len(data),
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	_ = r.enc.Encode(rec) // best-effort; don't fail the real operation
}

// Close flushes and closes the capture file.
func (r *Recorder) Close() error {
	if r == nil || r.file == nil {
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	err := r.file.Close()
	r.file = nil
	return err
}

// ---- ACP1 Transport wrapper ----

// RecordingTransport wraps an ACP1 Transport and records every Send/Receive.
// It satisfies the acp1.Transport interface (Send, Receive, Close).
type RecordingTransport struct {
	inner    interface {
		Send(ctx context.Context, payload []byte) error
		Receive(ctx context.Context, maxSize int) ([]byte, error)
		Close() error
	}
	recorder *Recorder
	proto    string // "acp1"
}

// WrapTransport wraps an existing transport with capture recording.
// proto should be "acp1" (identifies records in the JSONL output).
func (r *Recorder) WrapTransport(tr interface {
	Send(ctx context.Context, payload []byte) error
	Receive(ctx context.Context, maxSize int) ([]byte, error)
	Close() error
}, proto string) *RecordingTransport {
	return &RecordingTransport{
		inner:    tr,
		recorder: r,
		proto:    proto,
	}
}

func (rt *RecordingTransport) Send(ctx context.Context, payload []byte) error {
	rt.recorder.Record(rt.proto, "tx", payload)
	return rt.inner.Send(ctx, payload)
}

func (rt *RecordingTransport) Receive(ctx context.Context, maxSize int) ([]byte, error) {
	data, err := rt.inner.Receive(ctx, maxSize)
	if err == nil && len(data) > 0 {
		rt.recorder.Record(rt.proto, "rx", data)
	}
	return data, err
}

func (rt *RecordingTransport) Close() error {
	return rt.inner.Close()
}
