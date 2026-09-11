package http

import (
	"bufio"
	"context"
	"errors"
	"io"
	"net"
	stdhttp "net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"dhs/internal/metrics"
)

// With a Metrics connector set, every dispatched request is counted:
// Content-Length as rx, the bytes the client received as tx with the
// handler's elapsed time — including the panic barrier's 500 and error
// bodies, since what the client got is what counts. Without one, nothing
// is counted and nothing changes.
func TestMetricsCountEveryResponse(t *testing.T) {
	s := NewServer(quietLogger())
	s.Metrics = metrics.NewConnector()
	s.Handle(stdhttp.MethodPost, "/thing", func(context.Context, *stdhttp.Request) (int, any, error) {
		return stdhttp.StatusCreated, map[string]string{"id": "x"}, nil
	})
	s.Handle(stdhttp.MethodGet, "/raw", func(context.Context, *stdhttp.Request) (int, any, error) {
		return 0, &RawBody{ContentType: "text/plain", Body: []byte("hello")}, nil
	})
	s.Handle(stdhttp.MethodGet, "/boom", func(context.Context, *stdhttp.Request) (int, any, error) {
		panic("kaboom")
	})

	req := httptest.NewRequest(stdhttp.MethodPost, "/thing", strings.NewReader(`{"a":1}`))
	req.ContentLength = 7
	w := httptest.NewRecorder()
	s.dispatch(w, req)
	snap := s.Metrics.Snapshot()
	if snap.RxBytes != 7 || snap.RxFrames != 1 {
		t.Errorf("rx after POST = %d bytes / %d frames, want 7 / 1", snap.RxBytes, snap.RxFrames)
	}
	if snap.TxFrames != 1 || snap.TxBytes != uint64(w.Body.Len()) || w.Body.Len() == 0 {
		t.Errorf("tx after POST = %d bytes / %d frames, want %d / 1", snap.TxBytes, snap.TxFrames, w.Body.Len())
	}

	_ = do(s, stdhttp.MethodGet, "/raw")
	if got := s.Metrics.Snapshot(); got.TxBytes != snap.TxBytes+5 || got.RxFrames != 2 {
		t.Errorf("raw body: tx %d (want %d), rx frames %d (want 2)", got.TxBytes, snap.TxBytes+5, got.RxFrames)
	}

	w = do(s, stdhttp.MethodGet, "/boom")
	if w.Code != stdhttp.StatusInternalServerError {
		t.Fatalf("panic must still answer 500, got %d", w.Code)
	}
	if got := s.Metrics.Snapshot(); got.TxFrames != 3 || got.TxBytes <= snap.TxBytes+5 {
		t.Errorf("the 500 the client received must be counted: %+v", got)
	}

	plain := NewServer(quietLogger())
	plain.Handle(stdhttp.MethodGet, "/x", okHandler(nil))
	if w := do(plain, stdhttp.MethodGet, "/x"); w.Code != stdhttp.StatusOK {
		t.Errorf("no metrics: %d", w.Code)
	}
}

// A raw handler that hijacks the connection (a WebSocket upgrade) still
// works through the counting writer, and its flushes pass through.
func TestMetricsWriterForwardsHijackAndFlush(t *testing.T) {
	s := NewServer(quietLogger())
	s.Metrics = metrics.NewConnector()
	s.HandleRaw("/ws", stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
		if _, ok := w.(stdhttp.Flusher); !ok {
			t.Error("counting writer must expose Flusher")
		}
		h, ok := w.(stdhttp.Hijacker)
		if !ok {
			t.Error("counting writer must expose Hijacker")
			return
		}
		conn, rw, err := h.Hijack()
		if err != nil {
			t.Errorf("hijack: %v", err)
			return
		}
		defer func() { _ = conn.Close() }()
		_, _ = rw.WriteString("HTTP/1.1 101 Switching Protocols\r\n\r\n")
		_ = rw.Flush()
	}))
	hs := httptest.NewServer(s.MuxHandler())
	defer hs.Close()

	conn, err := net.Dial("tcp", strings.TrimPrefix(hs.URL, "http://"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = conn.Close() }()
	_, _ = io.WriteString(conn, "GET /ws HTTP/1.1\r\nHost: x\r\n\r\n")
	line, err := bufio.NewReader(conn).ReadString('\n')
	if err != nil || !strings.Contains(line, "101") {
		t.Fatalf("hijacked response = %q, %v; want 101", line, err)
	}
	// The count lands when dispatch returns, which races the client's read
	// of the hijacked reply; wait for it rather than assume ordering.
	deadline := time.Now().Add(2 * time.Second)
	for s.Metrics.Snapshot().RxFrames != 1 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if got := s.Metrics.Snapshot(); got.RxFrames != 1 {
		t.Errorf("the upgrade request must be counted once, got rx frames %d", got.RxFrames)
	}

	// A writer that cannot be hijacked says so instead of panicking; a
	// writer that cannot flush is a no-op.
	cw := &countingWriter{ResponseWriter: httptest.NewRecorder()}
	if _, _, err := cw.Hijack(); !errors.Is(err, stdhttp.ErrNotSupported) {
		t.Errorf("Hijack on a recorder = %v, want ErrNotSupported", err)
	}
	cw.Flush()
	if _, err := cw.Write([]byte("abc")); err != nil || cw.n != 3 {
		t.Errorf("Write counted %d, %v; want 3", cw.n, err)
	}
	plainCW := &countingWriter{ResponseWriter: noFlushWriter{}}
	plainCW.Flush() // no Flusher underneath: must not panic
}

// noFlushWriter is a ResponseWriter with neither Flusher nor Hijacker.
type noFlushWriter struct{}

func (noFlushWriter) Header() stdhttp.Header      { return stdhttp.Header{} }
func (noFlushWriter) Write(p []byte) (int, error) { return len(p), nil }
func (noFlushWriter) WriteHeader(int)             {}
