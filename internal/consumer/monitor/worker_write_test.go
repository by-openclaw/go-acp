package monitor

import (
	"context"
	"errors"
	"sync/atomic"
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

// A read is over once the wire answers: its in-flight mark is released
// BEFORE the event is published, so whoever reacts to the event finds
// the scheduler free to queue the next read of that address. The bus
// runs a subscriber's filter inside Publish — that is the instant this
// test looks at the mark. Released after the publication, a quick
// subscriber (a test on a fake clock) had the next due read skipped as
// "still in flight", and the event it waited for never came (#1191).
func TestWorkerReleasesAReadBeforeItPublishesIt(t *testing.T) {
	fp := newFakeProto()
	req := consumer.ValueRequest{Path: "port.1.link"}
	key := addrKey(req)
	fp.set(key, intVal(0))
	w := newTestWorker(fp)

	var released, releases atomic.Int32
	atPublish := int32(-1)
	_, _ = w.bus.Subscribe(4, func(consumer.Event) bool {
		atPublish = released.Load()
		return true
	})

	w.exec(context.Background(), command{kind: cmdRead, key: key, req: req, done: func() {
		released.Store(1)
		releases.Add(1)
	}})

	if atPublish != 1 {
		t.Errorf("at publication the read was still marked in flight (released=%d)", atPublish)
	}
	if n := releases.Load(); n != 1 {
		t.Errorf("the in-flight mark was released %d times, want once", n)
	}

	// A read that fails publishes nothing and still releases its mark.
	fp.getErr[key] = errors.New("no response")
	released.Store(0)
	w.exec(context.Background(), command{kind: cmdRead, key: key, req: req, done: func() { released.Store(1) }})
	if released.Load() != 1 {
		t.Error("a failed read must release its in-flight mark")
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
