package monitor

import (
	"context"
	"errors"
	"testing"
	"time"

	"dhs/internal/clock"
	"dhs/internal/consumer"
)

func newTestWorker(fp *fakeProto) *worker {
	return &worker{
		name:  "d",
		proto: fp,
		bus:   newBus(),
		clk:   clock.NewFake(time.Time{}),
		log:   discardLog(),
		last:  make(map[string]consumer.Value),
	}
}

func TestWorkerDoWriteConfirmReadFails(t *testing.T) {
	fp := newFakeProto()
	req := consumer.ValueRequest{Path: "ctrl"}
	key := addrKey(req)
	// SetValue succeeds, the confirm read-back fails.
	fp.getErr[key] = errors.New("no response")

	w := newTestWorker(fp)
	res := make(chan writeResult, 1)
	w.doWrite(context.Background(), command{kind: cmdWrite, key: key, req: req, val: intVal(4), result: res})

	r := <-res
	if r.err == nil {
		t.Fatal("expected confirm-read-failed error, got nil")
	}
	if fp.setCount() != 1 {
		t.Errorf("SetValue calls = %d, want 1", fp.setCount())
	}
}

func TestWorkerDoWriteSetFails(t *testing.T) {
	fp := newFakeProto()
	req := consumer.ValueRequest{Path: "ctrl"}
	key := addrKey(req)
	fp.setErr[key] = errors.New("readOnly")

	w := newTestWorker(fp)
	res := make(chan writeResult, 1)
	w.doWrite(context.Background(), command{kind: cmdWrite, key: key, req: req, val: intVal(4), result: res})

	if r := <-res; r.err == nil {
		t.Fatal("expected set error, got nil")
	}
	if w.mErrors.Load() != 1 {
		t.Errorf("mErrors = %d, want 1", w.mErrors.Load())
	}
}

func TestWorkerDoWriteNoResultChannel(t *testing.T) {
	fp := newFakeProto()
	req := consumer.ValueRequest{Path: "ctrl"}
	key := addrKey(req)
	fp.set(key, intVal(1))

	w := newTestWorker(fp)
	_, ch := w.bus.Subscribe(2, nil)
	// result nil must not panic and should still emit the confirmed value.
	w.doWrite(context.Background(), command{kind: cmdWrite, key: key, req: req, val: intVal(8), result: nil})

	ev := recvEvent(t, ch)
	if ev.Value.Int != 8 {
		t.Errorf("confirmed emit = %d, want 8", ev.Value.Int)
	}
}

func TestWorkerDoAssert(t *testing.T) {
	// nil hook is a no-op.
	w := newTestWorker(newFakeProto())
	w.doAssert(context.Background())
	if w.mErrors.Load() != 0 {
		t.Errorf("nil assert hook should not count an error")
	}

	// a failing hook counts one error.
	w2 := newTestWorker(newFakeProto())
	w2.assert = func(ctx context.Context) error { return errors.New("drift") }
	w2.doAssert(context.Background())
	if w2.mErrors.Load() != 1 {
		t.Errorf("failing assert hook mErrors = %d, want 1", w2.mErrors.Load())
	}

	// a passing hook counts none.
	w3 := newTestWorker(newFakeProto())
	w3.assert = func(ctx context.Context) error { return nil }
	w3.doAssert(context.Background())
	if w3.mErrors.Load() != 0 {
		t.Errorf("passing assert hook should not count an error")
	}
}
