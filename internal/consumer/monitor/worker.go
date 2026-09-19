package monitor

import (
	"context"
	"fmt"
	"log/slog"
	"sync/atomic"
	"time"

	"dhs/internal/clock"
	"dhs/internal/consumer"
)

type cmdKind int

const (
	cmdRead cmdKind = iota
	cmdWrite
	cmdAssert
)

type command struct {
	kind     cmdKind
	key      string
	req      consumer.ValueRequest
	val      consumer.Value // cmdWrite
	onChange bool           // cmdRead
	done     func()         // called after execution (clears scheduler pending)
	result   chan writeResult
}

type writeResult struct {
	val consumer.Value
	err error
}

// worker is the per-device actor (ADR-0030 §1-2). It owns one
// connection and executes commands serially, operator writes before
// scheduled reads.
type worker struct {
	name   string
	proto  consumer.Protocol
	bus    *Bus
	clk    clock.Clock
	log    *slog.Logger
	minGap time.Duration
	assert func(context.Context) error

	ops   chan command // operator writes / control asserts — priority
	reads chan command // scheduled reads

	last   map[string]consumer.Value
	lastOp time.Time

	mReads, mWrites, mChanges, mErrors atomic.Uint64
}

// run is the serial loop. It drains the priority (ops) queue ahead of
// scheduled reads so a human is never starved behind the poll.
func (w *worker) run(ctx context.Context) {
	if w.last == nil {
		w.last = make(map[string]consumer.Value)
	}
	for {
		select {
		case <-ctx.Done():
			return
		case c := <-w.ops:
			w.exec(ctx, c)
		default:
			select {
			case <-ctx.Done():
				return
			case c := <-w.ops:
				w.exec(ctx, c)
			case c := <-w.reads:
				w.exec(ctx, c)
			}
		}
	}
}

// exec applies the per-device rate gap ("let the device breathe") then
// runs one command on the wire.
func (w *worker) exec(ctx context.Context, c command) {
	if c.done != nil {
		defer c.done()
	}
	if w.minGap > 0 {
		if elapsed := w.clk.Now().Sub(w.lastOp); elapsed < w.minGap {
			if err := w.clk.Sleep(ctx, w.minGap-elapsed); err != nil {
				if c.result != nil {
					c.result <- writeResult{err: err}
				}
				return
			}
		}
	}
	w.lastOp = w.clk.Now()
	switch c.kind {
	case cmdRead:
		w.doRead(ctx, c)
	case cmdWrite:
		w.doWrite(ctx, c)
	case cmdAssert:
		w.doAssert(ctx)
	}
}

func (w *worker) doRead(ctx context.Context, c command) {
	v, err := w.proto.GetValue(ctx, c.req)
	w.mReads.Add(1)
	if err != nil {
		w.mErrors.Add(1)
		w.log.Debug("monitor: read failed", "device", w.name, "addr", c.key, "err", err)
		return
	}
	w.observe(c.key, c.req, v, c.onChange, "live")
}

// doWrite is the confirmed write (ADR-0030 §4): set, then read back to
// confirm before the value is trusted. The SetValue echo alone is never
// enough.
func (w *worker) doWrite(ctx context.Context, c command) {
	w.mWrites.Add(1)
	if _, err := w.proto.SetValue(ctx, c.req, c.val); err != nil {
		w.mErrors.Add(1)
		if c.result != nil {
			c.result <- writeResult{err: err}
		}
		return
	}
	got, err := w.proto.GetValue(ctx, c.req)
	if err != nil {
		w.mErrors.Add(1)
		if c.result != nil {
			c.result <- writeResult{err: fmt.Errorf("monitor: set applied but confirm read failed: %w", err)}
		}
		return
	}
	// A confirmed write always emits, even if the value did not move.
	w.observe(c.key, c.req, got, false, "live")
	if c.result != nil {
		c.result <- writeResult{val: got}
	}
}

func (w *worker) doAssert(ctx context.Context) {
	if w.assert == nil {
		return
	}
	if err := w.assert(ctx); err != nil {
		w.mErrors.Add(1)
		w.log.Debug("monitor: control assert failed", "device", w.name, "err", err)
	}
}

// observe runs change detection and publishes a delta. With onChange
// set it stays silent when the value has not moved.
func (w *worker) observe(key string, req consumer.ValueRequest, v consumer.Value, onChange bool, fresh string) {
	prev, had := w.last[key]
	moved := !had || !valueEqual(prev, v)
	if onChange && !moved {
		return
	}
	w.last[key] = v
	if moved {
		w.mChanges.Add(1)
	}
	ev := consumer.Event{
		Slot:      req.Slot,
		Group:     req.Group,
		ID:        req.ID,
		Path:      req.Path,
		Label:     req.Label,
		Value:     v,
		Timestamp: w.clk.Now(),
		Freshness: fresh,
	}
	if moved && had {
		ev.Changes = []consumer.FieldChange{{Name: "value", Old: valueString(prev), New: valueString(v)}}
	}
	w.bus.Publish(ev)
}
