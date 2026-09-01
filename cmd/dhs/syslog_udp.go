package main

// Non-blocking RFC 5424 forwarding over UDP (#934). `--syslog-addr
// host:port` tees producer logs to a collector as one datagram per
// record (RFC 5426 §3.1). The contract is that logging NEVER stalls a
// frame path: Handle() enqueues into a bounded channel and returns; a
// single writer goroutine drains to the socket. When the collector
// cannot keep up the queue fills, the record is dropped, and the drop
// is counted — the first drop and the close-time total are reported on
// stderr, so lost records are audited rather than silent.

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net"
	"os"
	"sync"
	"sync/atomic"
)

// syslogQueueDepth bounds how many formatted records may wait for the
// socket before new records are dropped. 1024 lines rides out collector
// hiccups of a few hundred ms at debug-level chatter without letting an
// unreachable collector grow the heap unbounded.
const syslogQueueDepth = 1024

// syslogUDP owns the socket, the bounded queue, and the writer
// goroutine. One instance is shared by every derived handler
// (WithAttrs/WithGroup copies), so the drop accounting is per-target.
type syslogUDP struct {
	addr      string
	conn      net.Conn
	ch        chan string
	dropped   atomic.Uint64
	writeErrs atomic.Uint64
	wg        sync.WaitGroup
	closeOnce sync.Once
}

// dialSyslogUDP resolves the collector address and starts the writer
// goroutine. UDP "dialing" only fails on resolution — an unreachable
// collector is invisible at this layer, which is exactly why drops and
// write errors are counted instead of assumed impossible.
func dialSyslogUDP(addr string) (*syslogUDP, error) {
	conn, err := net.Dial("udp", addr)
	if err != nil {
		return nil, fmt.Errorf("syslog dial %s: %w", addr, err)
	}
	q := &syslogUDP{addr: addr, conn: conn, ch: make(chan string, syslogQueueDepth)}
	q.wg.Add(1)
	go func() {
		defer q.wg.Done()
		for line := range q.ch {
			if _, err := q.conn.Write([]byte(line)); err != nil {
				q.writeErrs.Add(1)
			}
		}
	}()
	return q, nil
}

// enqueue hands one formatted line to the writer without blocking.
func (q *syslogUDP) enqueue(line string) {
	select {
	case q.ch <- line:
	default:
		if q.dropped.Add(1) == 1 {
			fmt.Fprintf(os.Stderr, "syslog: collector %s not keeping up; dropping records (totals reported at shutdown)\n", q.addr)
		}
	}
}

// Close drains the queue, closes the socket, and reports totals. Safe
// to call more than once.
func (q *syslogUDP) Close() {
	q.closeOnce.Do(func() {
		close(q.ch)
		q.wg.Wait()
		_ = q.conn.Close()
		if d := q.dropped.Load(); d > 0 {
			fmt.Fprintf(os.Stderr, "syslog: dropped %d record(s) for %s (queue full)\n", d, q.addr)
		}
		if e := q.writeErrs.Load(); e > 0 {
			fmt.Fprintf(os.Stderr, "syslog: %d send error(s) to %s\n", e, q.addr)
		}
	})
}

// Handler returns a slog.Handler forwarding to this queue at min level.
func (q *syslogUDP) Handler(min slog.Level) slog.Handler {
	// io.Discard: the formatting state never writes — Handle goes
	// through formatRecord + enqueue, not syslogHandler.Handle.
	return &syslogUDPHandler{fmtH: newSyslogHandler(io.Discard, min), q: q}
}

// syslogUDPHandler pairs the shared queue with per-handler formatting
// state. Formatting reuses syslogHandler so the wire line is
// byte-identical to the `--log-format syslog` stderr line.
type syslogUDPHandler struct {
	fmtH *syslogHandler
	q    *syslogUDP
}

func (h *syslogUDPHandler) Enabled(ctx context.Context, l slog.Level) bool {
	return h.fmtH.Enabled(ctx, l)
}

func (h *syslogUDPHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &syslogUDPHandler{fmtH: h.fmtH.WithAttrs(attrs).(*syslogHandler), q: h.q}
}

func (h *syslogUDPHandler) WithGroup(name string) slog.Handler {
	return &syslogUDPHandler{fmtH: h.fmtH.WithGroup(name).(*syslogHandler), q: h.q}
}

func (h *syslogUDPHandler) Handle(_ context.Context, r slog.Record) error {
	h.q.enqueue(h.fmtH.formatRecord(r))
	return nil
}

// teeHandler fans one record out to every underlying handler. Used to
// keep the operator's stderr format while also forwarding to syslog.
type teeHandler []slog.Handler

func (t teeHandler) Enabled(ctx context.Context, l slog.Level) bool {
	for _, h := range t {
		if h.Enabled(ctx, l) {
			return true
		}
	}
	return false
}

func (t teeHandler) Handle(ctx context.Context, r slog.Record) error {
	var first error
	for _, h := range t {
		if h.Enabled(ctx, r.Level) {
			if err := h.Handle(ctx, r.Clone()); err != nil && first == nil {
				first = err
			}
		}
	}
	return first
}

func (t teeHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	out := make(teeHandler, len(t))
	for i, h := range t {
		out[i] = h.WithAttrs(attrs)
	}
	return out
}

func (t teeHandler) WithGroup(name string) slog.Handler {
	out := make(teeHandler, len(t))
	for i, h := range t {
		out[i] = h.WithGroup(name)
	}
	return out
}
