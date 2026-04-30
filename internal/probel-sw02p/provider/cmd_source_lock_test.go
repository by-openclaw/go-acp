package probelsw02p

import (
	"testing"

	"acp/internal/probel-sw02p/codec"
)

// TestSourceLockStatusRequestRepliesAllHealthy locks in the rx 014 /
// tx 015 contract — §3.2.16/17. Provider reports every declared
// source as locked=true (clean signal) by default since it has no
// hardware to monitor. Uses a 4-source canonical matrix so the
// bitmap fits in a single byte.
func TestSourceLockStatusRequestRepliesAllHealthy(t *testing.T) {
	srv := newTestServer(t) // 4x4 matrix from newTestServer

	in := codec.EncodeSourceLockStatusRequest(codec.SourceLockStatusRequestParams{Controller: codec.ControllerLH})
	res, err := srv.dispatch(in)
	if err != nil {
		t.Fatalf("dispatch rx 014: %v", err)
	}
	if res.reply == nil || res.reply.ID != codec.TxSourceLockStatusResponse {
		t.Fatalf("reply missing / wrong ID: %+v", res.reply)
	}
	resp, err := codec.DecodeSourceLockStatusResponse(*res.reply)
	if err != nil {
		t.Fatalf("decode tx 015: %v", err)
	}
	// 4 declared sources → bitmap rounds up to 4 entries exactly.
	if len(resp.Locked) < 4 {
		t.Fatalf("Locked len = %d; want at least 4", len(resp.Locked))
	}
	for i := 0; i < 4; i++ {
		if !resp.Locked[i] {
			t.Errorf("Locked[%d] = false; want true (all-healthy default)", i)
		}
	}
}

// TestSourceLockStatusRequestEmptyTree verifies the bare-tree case —
// no sources declared → empty bitmap (header-only response).
func TestSourceLockStatusRequestEmptyTree(t *testing.T) {
	srv := newBareTestServer(t)
	res, err := srv.dispatch(codec.EncodeSourceLockStatusRequest(codec.SourceLockStatusRequestParams{}))
	if err != nil {
		t.Fatalf("dispatch rx 014: %v", err)
	}
	if res.reply == nil {
		t.Fatal("reply is nil; want tx 015")
	}
	resp, err := codec.DecodeSourceLockStatusResponse(*res.reply)
	if err != nil {
		t.Fatalf("decode tx 015: %v", err)
	}
	if len(resp.Locked) != 0 {
		t.Errorf("Locked len = %d; want 0 (no sources declared)", len(resp.Locked))
	}
}

// TestSourceLockStatusRequestHandlerDecodeError confirms a short-
// payload rx 014 flows back as a decode error.
func TestSourceLockStatusRequestHandlerDecodeError(t *testing.T) {
	srv := newTestServer(t)
	bad := codec.Frame{ID: codec.RxSourceLockStatusRequest, Payload: nil}
	res, err := srv.dispatch(bad)
	if err == nil {
		t.Fatal("want ErrShortPayload; got nil")
	}
	if res.reply != nil {
		t.Errorf("got reply on decode error; want none")
	}
}
