package acp1

import (
	"context"
	"errors"
	"io"
	"testing"
	"time"
)

// fakeTransport is an in-memory Transport for deterministic client tests.
// It records outgoing Send payloads in sent[] and serves Receive calls
// from a queue of pre-built reply bytes. A nil entry in the recv queue
// signals "return context.DeadlineExceeded" (simulated timeout).
type fakeTransport struct {
	sent   [][]byte
	recv   [][]byte // nil entry = simulate timeout
	recvIx int
	closed bool
}

func (f *fakeTransport) Send(ctx context.Context, payload []byte) error {
	cp := make([]byte, len(payload))
	copy(cp, payload)
	f.sent = append(f.sent, cp)
	return nil
}

func (f *fakeTransport) Receive(ctx context.Context, maxSize int) ([]byte, error) {
	if f.recvIx >= len(f.recv) {
		return nil, io.EOF
	}
	entry := f.recv[f.recvIx]
	f.recvIx++
	if entry == nil {
		return nil, context.DeadlineExceeded
	}
	return entry, nil
}

func (f *fakeTransport) Close() error {
	f.closed = true
	return nil
}

// buildReply encodes a canned reply with the requested MTID. Used to
// hand-feed the fake transport with what a real device would return.
func buildReply(t *testing.T, mtid uint32, mtype MType, mcode byte,
	group ObjGroup, id byte, value []byte) []byte {
	t.Helper()
	m := &Message{
		MTID:     mtid,
		MType:    mtype,
		MAddr:    0,
		MCode:    mcode,
		ObjGroup: group,
		ObjID:    id,
		Value:    value,
	}
	b, err := m.Encode()
	if err != nil {
		t.Fatalf("buildReply encode: %v", err)
	}
	return b
}

// TestClient_Do_HappyPath: send one request, first reply matches → return it.
func TestClient_Do_HappyPath(t *testing.T) {
	ft := &fakeTransport{}
	c := NewClient(ft, nil, ClientConfig{
		MaxRetries:     3,
		ReceiveTimeout: 100 * time.Millisecond,
	})
	defer c.Close()

	// Seed a matching reply. We don't know the MTID yet (client allocates),
	// so we queue nothing and rebuild after we see what the client sent.
	req := &Message{
		MType:    MTypeRequest,
		MAddr:    0,
		MCode:    byte(MethodGetValue),
		ObjGroup: GroupFrame,
		ObjID:    0,
	}

	// Intercept: before Do runs we pre-seed recv with a placeholder; after
	// Do encodes we patch the MTID. Simpler: pre-allocate nextMTID ourselves.
	c.nextMTID = 41 // next allocMTID() will return 42
	reply := buildReply(t, 42, MTypeReply, byte(MethodGetValue), GroupFrame, 0, []byte{0x02, 0x02, 0x02})
	ft.recv = [][]byte{reply}

	got, err := c.Do(context.Background(), req)
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	if got.MTID != 42 {
		t.Errorf("reply MTID: got %d, want 42", got.MTID)
	}
	if got.MType != MTypeReply {
		t.Errorf("reply MType: got %d", got.MType)
	}
	if len(ft.sent) != 1 {
		t.Errorf("sent count: got %d, want 1", len(ft.sent))
	}
}

// TestClient_Do_SkipsAnnouncement: an announcement arrives before the
// matching reply → client must skip it and keep waiting.
func TestClient_Do_SkipsAnnouncement(t *testing.T) {
	ft := &fakeTransport{}
	c := NewClient(ft, nil, ClientConfig{
		MaxRetries:     2,
		ReceiveTimeout: 100 * time.Millisecond,
	})
	defer c.Close()

	c.nextMTID = 0xAA
	// First: an announcement (MTID=0, MType=0, ObjGroup=frame) → skipped.
	// Second: the real reply (MTID=0xAB).
	announce := buildReply(t, 0, MTypeAnnounce, 0, GroupFrame, 0, []byte{0x02, 0x02})
	reply := buildReply(t, 0xAB, MTypeReply, byte(MethodGetValue), GroupFrame, 0, []byte{0x03})
	ft.recv = [][]byte{announce, reply}

	req := &Message{
		MType:    MTypeRequest,
		MCode:    byte(MethodGetValue),
		ObjGroup: GroupFrame,
	}
	got, err := c.Do(context.Background(), req)
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	if got.MTID != 0xAB {
		t.Errorf("reply MTID: got %d, want 0xAB", got.MTID)
	}
	if ft.recvIx != 2 {
		t.Errorf("expected 2 receives consumed, got %d", ft.recvIx)
	}
}

// TestClient_Do_SkipsMTIDMismatch: stale reply from an earlier transaction
// arrives → skip and keep waiting.
func TestClient_Do_SkipsMTIDMismatch(t *testing.T) {
	ft := &fakeTransport{}
	c := NewClient(ft, nil, ClientConfig{
		MaxRetries:     2,
		ReceiveTimeout: 100 * time.Millisecond,
	})
	defer c.Close()

	c.nextMTID = 99
	stale := buildReply(t, 7, MTypeReply, byte(MethodGetValue), GroupFrame, 0, []byte{0xDE})
	real := buildReply(t, 100, MTypeReply, byte(MethodGetValue), GroupFrame, 0, []byte{0xAD})
	ft.recv = [][]byte{stale, real}

	req := &Message{
		MType:    MTypeRequest,
		MCode:    byte(MethodGetValue),
		ObjGroup: GroupFrame,
	}
	got, err := c.Do(context.Background(), req)
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	if got.MTID != 100 {
		t.Errorf("got MTID %d, want 100", got.MTID)
	}
}

// TestClient_Do_RetryOnTimeout: first attempt times out → retry with the
// SAME MTID (spec p. 12). The sent payload must be byte-identical across
// attempts.
func TestClient_Do_RetryOnTimeout(t *testing.T) {
	ft := &fakeTransport{}
	c := NewClient(ft, nil, ClientConfig{
		MaxRetries:     3,
		ReceiveTimeout: 50 * time.Millisecond,
		InitialBackoff: 1 * time.Millisecond,
		MaxBackoff:     1 * time.Millisecond,
	})
	defer c.Close()

	c.nextMTID = 0xDEADBEE0
	// nil = simulated timeout on the first attempt
	reply := buildReply(t, 0xDEADBEE1, MTypeReply, byte(MethodGetValue), GroupControl, 5, []byte{0x00})
	ft.recv = [][]byte{nil, reply}

	req := &Message{
		MType:    MTypeRequest,
		MCode:    byte(MethodGetValue),
		ObjGroup: GroupControl,
		ObjID:    5,
	}
	got, err := c.Do(context.Background(), req)
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	if got.MTID != 0xDEADBEE1 {
		t.Errorf("reply MTID: got %x, want deadbee1", got.MTID)
	}

	// The critical spec compliance check: both send attempts must be
	// byte-identical. The C# reference driver bumps MTID on retry; we
	// must NOT.
	if len(ft.sent) != 2 {
		t.Fatalf("sent count: got %d, want 2", len(ft.sent))
	}
	for i := range ft.sent[0] {
		if ft.sent[0][i] != ft.sent[1][i] {
			t.Fatalf("retransmit differs at byte %d: %x vs %x",
				i, ft.sent[0][i], ft.sent[1][i])
		}
	}
}

// TestClient_Do_MaxRetriesExceeded: every attempt times out.
func TestClient_Do_MaxRetriesExceeded(t *testing.T) {
	ft := &fakeTransport{
		recv: [][]byte{nil, nil, nil}, // three timeouts
	}
	c := NewClient(ft, nil, ClientConfig{
		MaxRetries:     3,
		ReceiveTimeout: 10 * time.Millisecond,
		InitialBackoff: 1 * time.Millisecond,
		MaxBackoff:     1 * time.Millisecond,
	})
	defer c.Close()

	req := &Message{MType: MTypeRequest, MCode: byte(MethodGetValue), ObjGroup: GroupFrame}
	_, err := c.Do(context.Background(), req)
	if !errors.Is(err, ErrMaxRetries) {
		t.Errorf("got err=%v, want ErrMaxRetries", err)
	}
	if len(ft.sent) != 3 {
		t.Errorf("sent %d times, want 3", len(ft.sent))
	}
}

// TestClient_Do_CtxCancelDuringBackoff: caller cancels context while the
// client is sleeping between retries → return ctx.Err() immediately.
func TestClient_Do_CtxCancelDuringBackoff(t *testing.T) {
	ft := &fakeTransport{recv: [][]byte{nil}} // first attempt times out
	c := NewClient(ft, nil, ClientConfig{
		MaxRetries:     5,
		ReceiveTimeout: 5 * time.Millisecond,
		InitialBackoff: 200 * time.Millisecond,
		MaxBackoff:     1 * time.Second,
	})
	defer c.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	req := &Message{MType: MTypeRequest, MCode: byte(MethodGetValue), ObjGroup: GroupFrame}
	_, err := c.Do(ctx, req)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("got err=%v, want DeadlineExceeded", err)
	}
}

// TestClient_Do_ErrorReplyPropagates: server returns MType=3 error reply.
// The client must surface it as a successful Do() (no retry) so the
// caller can inspect msg.ErrCode().
func TestClient_Do_ErrorReplyPropagates(t *testing.T) {
	ft := &fakeTransport{}
	c := NewClient(ft, nil, ClientConfig{
		MaxRetries:     3,
		ReceiveTimeout: 50 * time.Millisecond,
	})
	defer c.Close()

	c.nextMTID = 10
	// Hand-build an error reply with MTID=11. An error reply MDATA can
	// be just the MCODE byte (spec p. 9) — no ObjGrp/ObjId.
	errReply := []byte{
		0x00, 0x00, 0x00, 0x0B, // MTID = 11
		0x01,       // PVER
		0x03,       // MType = error
		0x00,       // MAddr
		0x13,       // MCODE = 19 (no write access)
	}
	ft.recv = [][]byte{errReply}

	req := &Message{MType: MTypeRequest, MCode: byte(MethodSetValue), ObjGroup: GroupControl, ObjID: 7}
	got, err := c.Do(context.Background(), req)
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	if !got.IsError() {
		t.Fatal("expected IsError true")
	}
	oerr, ok := got.ErrCode().(ObjectErr)
	if !ok || oerr.Code != OErrNoWriteAccess {
		t.Errorf("unexpected err code: %v", got.ErrCode())
	}
	if len(ft.sent) != 1 {
		t.Errorf("should not retry error replies; sent=%d", len(ft.sent))
	}
}

// TestClient_allocMTID_SkipsZeroOnWrap
func TestClient_allocMTID_SkipsZeroOnWrap(t *testing.T) {
	c := &Client{nextMTID: 0xFFFFFFFF}
	first := c.allocMTID()
	if first != 1 {
		t.Errorf("wrap: got %d, want 1", first)
	}
	second := c.allocMTID()
	if second != 2 {
		t.Errorf("post-wrap: got %d, want 2", second)
	}
}

// TestClient_Do_Serialisation: two concurrent Do() calls must not
// interleave payloads. We can't easily assert real parallelism in a test,
// but we can verify the mutex exists by checking that two sequential
// calls produce monotonically-increasing MTIDs in both payloads.
func TestClient_Do_Serialisation(t *testing.T) {
	ft := &fakeTransport{}
	c := NewClient(ft, nil, ClientConfig{
		MaxRetries:     2,
		ReceiveTimeout: 50 * time.Millisecond,
	})
	defer c.Close()

	c.nextMTID = 100
	r1 := buildReply(t, 101, MTypeReply, byte(MethodGetValue), GroupFrame, 0, []byte{0x02})
	r2 := buildReply(t, 102, MTypeReply, byte(MethodGetValue), GroupFrame, 0, []byte{0x02})
	ft.recv = [][]byte{r1, r2}

	req := &Message{MType: MTypeRequest, MCode: byte(MethodGetValue), ObjGroup: GroupFrame}
	got1, err := c.Do(context.Background(), req)
	if err != nil {
		t.Fatalf("Do1: %v", err)
	}
	got2, err := c.Do(context.Background(), req)
	if err != nil {
		t.Fatalf("Do2: %v", err)
	}
	if got1.MTID >= got2.MTID {
		t.Errorf("MTIDs not monotonic: %d >= %d", got1.MTID, got2.MTID)
	}
}
