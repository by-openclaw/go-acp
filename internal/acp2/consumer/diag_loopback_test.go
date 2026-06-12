package acp2

import (
	"context"
	"net"
	"testing"
	"time"

	"dhs/internal/acp2/codec"
)

// diag_loopback_test.go drives RunDiagnostics against the loopback peer.
//
// The happy path (TestRunDiagnostics) answers ACP2 data probes and is
// gated behind -short being OFF because RunDiagnostics has hard-coded
// 3 s per-probe timeouts plus a 2 s announce-listen window, and the
// session readLoop drops proto=3 (ACMP) / proto=4 frames, so those
// probes always run to their full 3 s timeout. Total ~15 s — kept out
// of the fast path. The cheap dial-error path always runs.
func TestRunDiagnostics(t *testing.T) {
	if testing.Short() {
		t.Skip("RunDiagnostics has hard-coded multi-second probe timeouts; skipped in -short")
	}
	srv, host, port := newFakeServer(t)

	// Answer every ACP2 data request with a generic reply echoing mtid.
	srv.onACP2 = func(slot uint8, req *codec.ACP2Message) *codec.ACP2Message {
		return &codec.ACP2Message{
			Type: codec.ACP2TypeReply,
			Func: req.Func,
			Body: objectBody(req.ObjID, nil),
		}
	}

	// Answer ACMP / vendor-proto data frames with a reply on the same
	// proto, echoing the ACP2 mtid from byte 1 of the payload. (These
	// replies are dropped by the session readLoop, which only routes
	// proto 0/2 — so the probes still time out; the handler keeps the
	// server side from blocking.)
	srv.onOtherProto = func(c net.Conn, f *codec.AN2Frame) {
		if f.Type != codec.AN2TypeData || len(f.Payload) < codec.ACP2HeaderSize {
			return
		}
		mtid := f.Payload[1]
		replyPayload := []byte{byte(codec.ACP2TypeReply), mtid, f.Payload[2], 0}
		srv.sendFrame(c, &codec.AN2Frame{
			Proto:   f.Proto,
			Slot:    f.Slot,
			MTID:    0,
			Type:    codec.AN2TypeData,
			Payload: replyPayload,
		})
	}
	defer srv.stop()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	results, err := RunDiagnostics(ctx, host, port, 1, testLogger())
	if err != nil {
		t.Fatalf("RunDiagnostics: %v", err)
	}
	if len(results) == 0 {
		t.Fatal("RunDiagnostics returned no results")
	}
	// At least the first ACP2 get_object probe should report ok.
	var sawOK bool
	for _, r := range results {
		if len(r.Status) >= 2 && r.Status[:2] == "ok" {
			sawOK = true
			break
		}
	}
	if !sawOK {
		t.Errorf("no probe reported ok; results=%+v", results)
	}
}

// TestRunDiagnostics_DialError covers the early-return path when the
// device is unreachable.
func TestRunDiagnostics_DialError(t *testing.T) {
	srv, host, port := newFakeServer(t)
	srv.stop() // free the port so the dial is refused

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if _, err := RunDiagnostics(ctx, host, port, 0, testLogger()); err == nil {
		t.Fatal("RunDiagnostics to dead port: want error")
	}
}
