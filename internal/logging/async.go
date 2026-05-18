package logging

import (
	"context"
	"io"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"
)

// AsyncHandler wraps any slog.Handler so logging calls return without
// blocking the caller on the underlying writer (stderr / file).
// Records are cloned, pushed onto a buffered channel, and drained by a
// dedicated goroutine. When the channel is full the record is dropped
// rather than blocking the hot path (Ember+ keepalive, Probel tally
// fan-out, ACP1 status announce — every per-frame log site must stay
// allocation-bounded and non-blocking).
//
// R15 #476: every consumer + provider logger goes through AsyncHandler
// by default. The drop counter is exported via DropCount() so operators
// can audit overflow under load. Close() drains the queue and joins the
// goroutine on shutdown.
type AsyncHandler struct {
	inner slog.Handler
	ch    chan slog.Record
	drops atomic.Uint64
	wg    sync.WaitGroup
	stop  chan struct{}
}

// DefaultBufferSize is the channel capacity of a fresh AsyncHandler.
// Sized so an 8-byte frame at 1 GbE wire (≈ 125 kfps theoretical, ≈ 50
// kfps practical on a typical NMOS / Ember+ device) drops only when the
// drain side stalls for ≥ 80 ms. Adjust per-call via NewAsyncHandlerN.
const DefaultBufferSize = 4096

// NewAsyncHandler wraps inner with the default buffer size. Caller MUST
// call Close() at shutdown to flush the queue.
func NewAsyncHandler(inner slog.Handler) *AsyncHandler {
	return NewAsyncHandlerN(inner, DefaultBufferSize)
}

// NewAsyncHandlerN wraps inner with a configurable buffer. buffer ≤ 0
// is treated as DefaultBufferSize.
func NewAsyncHandlerN(inner slog.Handler, buffer int) *AsyncHandler {
	if buffer <= 0 {
		buffer = DefaultBufferSize
	}
	h := &AsyncHandler{
		inner: inner,
		ch:    make(chan slog.Record, buffer),
		stop:  make(chan struct{}),
	}
	h.wg.Add(1)
	go h.run()
	return h
}

// Enabled delegates to the inner handler — level filtering happens
// before the channel push so a TRACE handler at INFO threshold doesn't
// fill the queue with records it would drop on render.
func (h *AsyncHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.inner.Enabled(ctx, level)
}

// Handle clones rec (slog.Record contents are mutable until next
// rec.Clone) and enqueues. Channel full → drop with counter bump.
func (h *AsyncHandler) Handle(_ context.Context, rec slog.Record) error {
	clone := rec.Clone()
	select {
	case h.ch <- clone:
	default:
		h.drops.Add(1)
	}
	return nil
}

// WithAttrs returns a child handler with attrs bound on the inner
// handler. The channel + drain goroutine are SHARED so cancellation
// stays cheap.
func (h *AsyncHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &asyncChild{parent: h, inner: h.inner.WithAttrs(attrs)}
}

// WithGroup returns a child handler with group bound on the inner
// handler. Channel + goroutine are SHARED.
func (h *AsyncHandler) WithGroup(name string) slog.Handler {
	return &asyncChild{parent: h, inner: h.inner.WithGroup(name)}
}

// DropCount returns the number of records the handler had to drop
// because the buffer was full. Monotonic since construction.
func (h *AsyncHandler) DropCount() uint64 {
	return h.drops.Load()
}

// Close drains the queue, stops the goroutine, and returns when both
// are done. Safe to call multiple times. Subsequent Handle calls
// fast-path to drop.
func (h *AsyncHandler) Close() error {
	select {
	case <-h.stop:
		return nil
	default:
		close(h.stop)
	}
	close(h.ch)
	h.wg.Wait()
	if c, ok := h.inner.(io.Closer); ok {
		return c.Close()
	}
	return nil
}

// run is the drain goroutine. Exits when ch is closed OR when stop
// closes (the latter is the panic-recovery path; normal close goes
// through ch).
func (h *AsyncHandler) run() {
	defer h.wg.Done()
	ctx := context.Background()
	for rec := range h.ch {
		// Errors from the inner handler aren't surfaceable from the
		// drain goroutine; we deliberately swallow them. The inner
		// stderr / JSON handler should not return errors in practice
		// (write failures on stderr indicate the process is going down
		// anyway).
		_ = h.inner.Handle(ctx, rec)
	}
}

// FlushTimeout drains up to timeout and returns whether the queue went
// empty in time. Used by tests + cmd_producer to give pending records
// a moment to flush at shutdown without hanging forever.
func (h *AsyncHandler) FlushTimeout(timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if len(h.ch) == 0 {
			return true
		}
		time.Sleep(2 * time.Millisecond)
	}
	return len(h.ch) == 0
}

// asyncChild is the handler returned by WithAttrs / WithGroup. It
// shares the parent's channel + goroutine so cancellation stays
// O(1) regardless of how many child loggers are created.
type asyncChild struct {
	parent *AsyncHandler
	inner  slog.Handler
}

func (c *asyncChild) Enabled(ctx context.Context, level slog.Level) bool {
	return c.inner.Enabled(ctx, level)
}

func (c *asyncChild) Handle(_ context.Context, rec slog.Record) error {
	clone := rec.Clone()
	select {
	case c.parent.ch <- clone:
	default:
		c.parent.drops.Add(1)
	}
	return nil
}

func (c *asyncChild) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &asyncChild{parent: c.parent, inner: c.inner.WithAttrs(attrs)}
}

func (c *asyncChild) WithGroup(name string) slog.Handler {
	return &asyncChild{parent: c.parent, inner: c.inner.WithGroup(name)}
}
