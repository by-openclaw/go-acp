package acp1

import (
	"context"
	"encoding/binary"
	"io"
	"net"
	"testing"
	"time"
)

func TestAdminSlotInsert(t *testing.T) {
	s := newTestServer(t)
	s.SetInsertTiming(InsertTimingFast)
	c := startAdminServer(t, s, "test-insert")
	resp, err := c.Call(context.Background(), &AdminRequest{
		Verb: "slot.insert", Args: map[string]any{"slot": 1},
	})
	if err != nil {
		t.Fatalf("Call: %v", err)
	}
	if resp.Status != "ok" {
		t.Fatalf("status = %q (msg=%s)", resp.Status, resp.Message)
	}
}

func TestAdminSlotExtract(t *testing.T) {
	s := newTestServer(t)
	c := startAdminServer(t, s, "test-extract")
	resp, err := c.Call(context.Background(), &AdminRequest{
		Verb: "slot.extract", Args: map[string]any{"slot": 1},
	})
	if err != nil {
		t.Fatalf("Call: %v", err)
	}
	if resp.Status != "ok" {
		t.Fatalf("status = %q (msg=%s)", resp.Status, resp.Message)
	}
}

func TestAdminSlotUnload(t *testing.T) {
	s := newTestServer(t)
	c := startAdminServer(t, s, "test-unload")
	resp, err := c.Call(context.Background(), &AdminRequest{
		Verb: "slot.unload", Args: map[string]any{"slot": 1},
	})
	if err != nil {
		t.Fatalf("Call: %v", err)
	}
	if resp.Status != "ok" {
		t.Fatalf("status = %q (msg=%s)", resp.Status, resp.Message)
	}
}

func TestAdminReload_NotImplemented(t *testing.T) {
	s := newTestServer(t)
	c := startAdminServer(t, s, "test-reload")
	resp, err := c.Call(context.Background(), &AdminRequest{Verb: "reload"})
	if err != nil {
		t.Fatalf("Call: %v", err)
	}
	if resp.Status != "error" {
		t.Fatalf("reload should report error (stub): status=%q", resp.Status)
	}
}

func TestAdminMissingArgs(t *testing.T) {
	s := newTestServer(t)
	c := startAdminServer(t, s, "test-missing")
	// slot.insert without "slot" → readIntArg error path.
	resp, err := c.Call(context.Background(), &AdminRequest{Verb: "slot.insert"})
	if err != nil {
		t.Fatalf("Call: %v", err)
	}
	if resp.Status != "error" {
		t.Fatalf("missing slot arg should error, got %q", resp.Status)
	}
	// value.set without "value" → handleValueSet missing-value error.
	resp, err = c.Call(context.Background(), &AdminRequest{
		Verb: "value.set", Args: map[string]any{"path": "1.2.2.0"},
	})
	if err != nil {
		t.Fatalf("Call: %v", err)
	}
	if resp.Status != "error" {
		t.Fatalf("missing value arg should error, got %q", resp.Status)
	}
}

func TestAdminValueGet_MissingObject(t *testing.T) {
	s := newTestServer(t)
	c := startAdminServer(t, s, "test-get-miss")
	// group=4 (alarm) has no entries → handleValueGet error branch.
	resp, err := c.Call(context.Background(), &AdminRequest{
		Verb: "value.get", Args: map[string]any{"slot": 1, "group": 4, "id": 0},
	})
	if err != nil {
		t.Fatalf("Call: %v", err)
	}
	if resp.Status != "error" {
		t.Fatalf("value.get on missing object should error, got %q", resp.Status)
	}
}

// TestAdminBadFrame drives writeAdminErr: a zero-length frame header is
// rejected before any JSON is read.
func TestAdminBadFrame(t *testing.T) {
	s := newTestServer(t)
	c := startAdminServer(t, s, "test-badframe")

	conn, err := net.DialTimeout("tcp4", c.addr, 2*time.Second)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer func() { _ = conn.Close() }()
	_ = conn.SetDeadline(time.Now().Add(2 * time.Second))

	var lenBuf [4]byte
	binary.BigEndian.PutUint32(lenBuf[:], 0) // mlen=0 → out of range
	if _, err := conn.Write(lenBuf[:]); err != nil {
		t.Fatalf("write: %v", err)
	}
	if _, err := io.ReadFull(conn, lenBuf[:]); err != nil {
		t.Fatalf("read reply len: %v", err)
	}
	rlen := binary.BigEndian.Uint32(lenBuf[:])
	if rlen == 0 || rlen > adminMaxFrame {
		t.Fatalf("reply length %d out of range", rlen)
	}
	body := make([]byte, rlen)
	if _, err := io.ReadFull(conn, body); err != nil {
		t.Fatalf("read reply body: %v", err)
	}
	if len(body) == 0 {
		t.Fatal("empty error reply")
	}
}
