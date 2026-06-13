package acp1

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"

	"dhs/internal/acp1/codec"
)

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// TestNonZeroSeed covers both arms of the MTID seed sanitiser.
func TestNonZeroSeed(t *testing.T) {
	if nonZeroSeed(0) != 1 {
		t.Error("zero seed should bump to 1")
	}
	if nonZeroSeed(42) != 42 {
		t.Error("non-zero seed should pass through")
	}
}

// TestClient_Close_NilTransport covers the nil-transport early return.
func TestClient_Close_NilTransport(t *testing.T) {
	c := &Client{}
	if err := c.Close(); err != nil {
		t.Fatalf("Close nil tr: %v", err)
	}
}

// TestClient_Do_GuardErrors covers nil-request and closed-client guards
// plus the encode-error path (MAddr out of range).
func TestClient_Do_GuardErrors(t *testing.T) {
	ft := &fakeTransport{}
	c := NewClient(ft, nil, ClientConfig{})
	if _, err := c.Do(context.Background(), nil); err == nil {
		t.Error("nil request: want error")
	}

	// Encode error: MAddr > 31.
	if _, err := c.Do(context.Background(), &codec.Message{
		MType: codec.MTypeRequest, MAddr: 99, MCode: byte(codec.MethodGetValue), ObjGroup: codec.GroupFrame,
	}); err == nil {
		t.Error("MAddr out of range: want encode error")
	}

	// Closed client.
	_ = c.Close()
	if _, err := c.Do(context.Background(), &codec.Message{MType: codec.MTypeRequest}); err == nil {
		t.Error("closed client: want error")
	}
}

// errSendTransport returns a non-timeout error from Send so Do's send-error
// arm (fatal, no retry) is exercised.
type errSendTransport struct{}

func (errSendTransport) Send(context.Context, []byte) error { return errors.New("socket broken") }
func (errSendTransport) Receive(context.Context, int) ([]byte, error) {
	return nil, io.EOF
}
func (errSendTransport) Close() error { return nil }

func TestClient_Do_SendError(t *testing.T) {
	c := NewClient(errSendTransport{}, discardLogger(), ClientConfig{
		MaxRetries: 2, ReceiveTimeout: 20 * time.Millisecond,
		InitialBackoff: time.Millisecond, MaxBackoff: time.Millisecond,
	})
	if _, err := c.Do(context.Background(), &codec.Message{
		MType: codec.MTypeRequest, MCode: byte(codec.MethodGetValue), ObjGroup: codec.GroupFrame,
	}); err == nil {
		t.Fatal("send error: want error")
	}
}

// TestClient_Do_SkipsMalformedReply queues garbage bytes (decode failure)
// before a good reply so doOneAttempt's malformed-skip arm is hit.
func TestClient_Do_SkipsMalformedReply(t *testing.T) {
	ft := &fakeTransport{}
	c := NewClient(ft, discardLogger(), ClientConfig{
		MaxRetries: 2, ReceiveTimeout: 100 * time.Millisecond,
	})
	c.nextMTID = 700
	good := buildReply(t, 701, codec.MTypeReply, byte(codec.MethodGetValue), codec.GroupFrame, 0, []byte{0x01})
	ft.recv = [][]byte{{0x00, 0x01}, good} // 2 bytes → too short → decode error
	got, err := c.Do(context.Background(), &codec.Message{
		MType: codec.MTypeRequest, MCode: byte(codec.MethodGetValue), ObjGroup: codec.GroupFrame,
	})
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	if got.MTID != 701 {
		t.Fatalf("got MTID %d, want 701", got.MTID)
	}
}

// TestGetConfirm_EncodeError calls getConfirm directly with a request whose
// MAddr is out of range so get.Encode() fails (line ~325).
func TestGetConfirm_EncodeError(t *testing.T) {
	ft := &fakeTransport{}
	c := NewClient(ft, discardLogger(), ClientConfig{})
	_, err := c.getConfirm(context.Background(), &codec.Message{
		MType: codec.MTypeRequest, MAddr: 99, MCode: byte(codec.MethodSetValue),
		ObjGroup: codec.GroupControl, ObjID: 0,
	})
	if err == nil {
		t.Fatal("getConfirm with bad MAddr: want encode error")
	}
}

// notLandedTransport: the mutating setValue reply is lost; the GET-confirm
// returns a value DIFFERENT from the desired write (not landed); the
// retransmit of the setValue is then answered. Drives Do's "absolute write
// not applied → continue (retransmit)" arm (line ~209).
type notLandedTransport struct {
	sent        [][]byte
	desired     []byte
	confirmVal  []byte
	retransmits int
}

func (tr *notLandedTransport) Send(_ context.Context, p []byte) error {
	tr.sent = append(tr.sent, append([]byte(nil), p...))
	return nil
}
func (tr *notLandedTransport) Receive(ctx context.Context, _ int) ([]byte, error) {
	last := tr.sent[len(tr.sent)-1]
	m, err := codec.Decode(last)
	if err != nil {
		return nil, err
	}
	if m.MCode == byte(codec.MethodSetValue) {
		tr.retransmits++
		if tr.retransmits == 1 {
			// First setValue: reply is lost.
			<-ctx.Done()
			return nil, ctx.Err()
		}
		// Retransmit setValue → answer with the desired value (landed now).
		reply := &codec.Message{MTID: m.MTID, MType: codec.MTypeReply, MAddr: m.MAddr,
			MCode: m.MCode, ObjGroup: m.ObjGroup, ObjID: m.ObjID, Value: tr.desired}
		return reply.Encode()
	}
	// GET-confirm → return a DIFFERENT value (write did not land).
	reply := &codec.Message{MTID: m.MTID, MType: codec.MTypeReply, MAddr: m.MAddr,
		MCode: m.MCode, ObjGroup: m.ObjGroup, ObjID: m.ObjID, Value: tr.confirmVal}
	return reply.Encode()
}
func (tr *notLandedTransport) Close() error { return nil }

func TestRetry_SetValue_NotLanded_Retransmits(t *testing.T) {
	tr := &notLandedTransport{desired: []byte{0x00, 0x05}, confirmVal: []byte{0x00, 0x01}}
	c := NewClient(tr, discardLogger(), ClientConfig{
		MaxRetries: 4, ReceiveTimeout: 30 * time.Millisecond,
		InitialBackoff: time.Millisecond, MaxBackoff: time.Millisecond,
	})
	rep, err := c.Do(context.Background(), &codec.Message{
		MType: codec.MTypeRequest, MAddr: 0, MCode: byte(codec.MethodSetValue),
		ObjGroup: codec.GroupControl, ObjID: 0, Value: []byte{0x00, 0x05},
	})
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	if string(rep.Value) != string([]byte{0x00, 0x05}) {
		t.Fatalf("final value = %x, want 0005", rep.Value)
	}
	if tr.retransmits < 2 {
		t.Fatalf("setValue retransmits = %d, want >=2", tr.retransmits)
	}
}

// confirmFailTransport: a relative setInc whose reply is lost AND whose
// GET-confirm also fails (every receive times out). Drives Do's
// "relative op + GET-confirm failed → return error, no retransmit" arm
// (line ~218).
type confirmFailTransport struct{ sent [][]byte }

func (tr *confirmFailTransport) Send(_ context.Context, p []byte) error {
	tr.sent = append(tr.sent, append([]byte(nil), p...))
	return nil
}
func (tr *confirmFailTransport) Receive(ctx context.Context, _ int) ([]byte, error) {
	<-ctx.Done() // everything times out
	return nil, ctx.Err()
}
func (tr *confirmFailTransport) Close() error { return nil }

func TestRetry_SetInc_ConfirmFails_NoRetransmit(t *testing.T) {
	tr := &confirmFailTransport{}
	c := NewClient(tr, discardLogger(), ClientConfig{
		MaxRetries: 3, ReceiveTimeout: 15 * time.Millisecond,
		InitialBackoff: time.Millisecond, MaxBackoff: time.Millisecond,
	})
	_, err := c.Do(context.Background(), &codec.Message{
		MType: codec.MTypeRequest, MAddr: 0, MCode: byte(codec.MethodSetIncValue),
		ObjGroup: codec.GroupControl, ObjID: 0,
	})
	if !errors.Is(err, ErrMaxRetries) {
		t.Fatalf("err = %v, want ErrMaxRetries", err)
	}
	// setInc must be sent exactly once (no blind retransmit on a relative op).
	got := 0
	for _, p := range tr.sent {
		if m, e := codec.Decode(p); e == nil && m.MCode == byte(codec.MethodSetIncValue) {
			got++
		}
	}
	if got != 1 {
		t.Fatalf("setInc sent %d times, want exactly 1", got)
	}
}
