package http

import (
	"context"
	"io"
	stdhttp "net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"dhs/internal/metrics"
)

// With a Metrics connector set, every request the client sends is counted
// as tx with its round-trip time, and every response as rx once its body
// is read and closed — across all four GET helpers, which share one do().
// A failed dial counts nothing; without a connector nothing changes.
func TestClientMetricsCountRequestsAndResponses(t *testing.T) {
	body := `{"a":1}`
	srv := httptest.NewServer(stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, body)
	}))
	defer srv.Close()

	c := NewClient()
	c.Metrics = metrics.NewConnector()
	var dst map[string]int
	if err := c.GetJSON(context.Background(), srv.URL, &dst); err != nil {
		t.Fatal(err)
	}
	snap := c.Metrics.Snapshot()
	if snap.TxFrames != 1 || snap.RxFrames != 1 || snap.RxBytes != uint64(len(body)) {
		t.Errorf("after GetJSON: tx %d rx %d/%dB, want 1 / 1 / %d", snap.TxFrames, snap.RxFrames, snap.RxBytes, len(body))
	}
	if _, err := c.GetBytes(context.Background(), srv.URL); err != nil {
		t.Fatal(err)
	}
	if got := c.Metrics.Snapshot(); got.TxFrames != 2 || got.RxFrames != 2 {
		t.Errorf("after GetBytes: tx %d rx %d, want 2 / 2", got.TxFrames, got.RxFrames)
	}

	// A dial failure is not a request the peer saw: nothing is counted.
	if err := c.GetJSON(context.Background(), "http://127.0.0.1:1/nope", &dst); err == nil {
		t.Fatal("expected a dial error")
	}
	if got := c.Metrics.Snapshot(); got.TxFrames != 2 {
		t.Errorf("a failed dial must not count as tx: %d", got.TxFrames)
	}

	// Close is idempotent: the rx frame is reported once.
	cb := &countingBody{ReadCloser: io.NopCloser(strings.NewReader("abcd")), met: metrics.NewConnector()}
	_, _ = io.ReadAll(cb)
	_ = cb.Close()
	_ = cb.Close()
	if got := cb.met.Snapshot(); got.RxFrames != 1 || got.RxBytes != 4 {
		t.Errorf("counting body reported %d frames / %d bytes, want 1 / 4", got.RxFrames, got.RxBytes)
	}

	plain := NewClient()
	if err := plain.GetJSON(context.Background(), srv.URL, &dst); err != nil {
		t.Errorf("no metrics: %v", err)
	}
}
