package acp1

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	"dhs/internal/acp1/codec"
)

// confirmTransport scripts a lost-reply scenario: the FIRST request (the
// mutating op) gets no reply — Receive blocks until the attempt's context
// times out — and every subsequent request (the GET-confirm) is answered with
// a reply echoing replyValue under the request's own MTID. Records every Send
// so a test can assert the mutating op was sent exactly once (no blind
// retransmit / double-apply).
type confirmTransport struct {
	sent       [][]byte
	replyValue []byte
}

func (tr *confirmTransport) Send(_ context.Context, p []byte) error {
	tr.sent = append(tr.sent, append([]byte(nil), p...))
	return nil
}

func (tr *confirmTransport) Receive(ctx context.Context, _ int) ([]byte, error) {
	// First in-flight request (the mutating op) → reply is "lost": wait out
	// the attempt deadline so Do sees a timeout.
	if len(tr.sent) <= 1 {
		<-ctx.Done()
		return nil, ctx.Err()
	}
	// GET-confirm (or any later request) → answer under its MTID.
	last := tr.sent[len(tr.sent)-1]
	m, err := codec.Decode(last)
	if err != nil {
		return nil, err
	}
	reply := &codec.Message{
		MTID: m.MTID, MType: codec.MTypeReply, MAddr: m.MAddr,
		MCode: m.MCode, ObjGroup: m.ObjGroup, ObjID: m.ObjID, Value: tr.replyValue,
	}
	return reply.Encode()
}

func (tr *confirmTransport) Close() error { return nil }

func newConfirmClient(replyValue []byte) (*Client, *confirmTransport) {
	tr := &confirmTransport{replyValue: replyValue}
	c := NewClient(tr, slog.New(slog.NewTextHandler(io.Discard, nil)), ClientConfig{
		MaxRetries:     3,
		ReceiveTimeout: 50 * time.Millisecond,
		InitialBackoff: time.Millisecond,
		MaxBackoff:     time.Millisecond,
	})
	return c, tr
}

// countMethod returns how many recorded sends used the given method byte.
func countMethod(tr *confirmTransport, mcode byte) int {
	n := 0
	for _, p := range tr.sent {
		if m, err := codec.Decode(p); err == nil && m.MCode == mcode {
			n++
		}
	}
	return n
}

// TestRetry_SetInc_LostReply_NoDoubleApply is the core guarantee: a setInc
// whose reply is lost must NOT be retransmitted (that would increment twice).
// Instead Do GET-confirms and returns the device's actual current value.
func TestRetry_SetInc_LostReply_NoDoubleApply(t *testing.T) {
	c, tr := newConfirmClient([]byte{0x00, 0x06}) // device's current value after the inc
	rep, err := c.Do(context.Background(), &codec.Message{
		MType: codec.MTypeRequest, MAddr: 0, MCode: byte(codec.MethodSetIncValue),
		ObjGroup: codec.GroupControl, ObjID: 0,
	})
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	if string(rep.Value) != string([]byte{0x00, 0x06}) {
		t.Fatalf("confirmed value = %x, want 0006", rep.Value)
	}
	if got := countMethod(tr, byte(codec.MethodSetIncValue)); got != 1 {
		t.Fatalf("setInc sent %d times, want exactly 1 (no double-apply)", got)
	}
	if got := countMethod(tr, byte(codec.MethodGetValue)); got != 1 {
		t.Fatalf("GET-confirm sent %d times, want 1", got)
	}
}

// TestRetry_SetValue_LostReply_LandedConfirmed: an absolute setValue whose
// reply is lost but whose write landed is confirmed via GET (value matches the
// desired bytes) and returned as success — without retransmitting.
func TestRetry_SetValue_LostReply_LandedConfirmed(t *testing.T) {
	desired := []byte{0x00, 0x05}
	c, tr := newConfirmClient(desired) // GET shows the desired value → it landed
	rep, err := c.Do(context.Background(), &codec.Message{
		MType: codec.MTypeRequest, MAddr: 0, MCode: byte(codec.MethodSetValue),
		ObjGroup: codec.GroupControl, ObjID: 0, Value: desired,
	})
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	if string(rep.Value) != string(desired) {
		t.Fatalf("confirmed = %x, want %x", rep.Value, desired)
	}
	if got := countMethod(tr, byte(codec.MethodSetValue)); got != 1 {
		t.Fatalf("setValue sent %d times, want 1 (landed, not retransmitted)", got)
	}
}

// TestRetry_GetValue_StillRetransmits: a non-mutating getValue keeps the
// original keep-MTID retransmit behaviour (no GET-confirm short-circuit).
func TestRetry_GetValue_LostReply_Retransmits(t *testing.T) {
	c, tr := newConfirmClient([]byte{0x00, 0x09})
	// First getValue reply is "lost"; the retransmit (same MTID) is answered.
	rep, err := c.Do(context.Background(), &codec.Message{
		MType: codec.MTypeRequest, MAddr: 0, MCode: byte(codec.MethodGetValue),
		ObjGroup: codec.GroupControl, ObjID: 0,
	})
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	if string(rep.Value) != string([]byte{0x00, 0x09}) {
		t.Fatalf("value = %x, want 0009", rep.Value)
	}
	if got := countMethod(tr, byte(codec.MethodGetValue)); got < 2 {
		t.Fatalf("getValue sent %d times, want >=2 (retransmit on timeout)", got)
	}
}
