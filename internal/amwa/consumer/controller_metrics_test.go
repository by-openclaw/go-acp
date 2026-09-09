package consumer

import (
	"context"
	stdhttp "net/http"
	"net/http/httptest"
	"testing"

	"dhs/internal/metrics"
	"dhs/internal/plugin"
)

// A Controller built from plugin.Deps counts every request it makes
// through the injected metrics connector, and Metrics() hands that
// connector back; with no Logger given, the Deps logger is used.
func TestControllerMetricsCountRequests(t *testing.T) {
	srv := httptest.NewServer(stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/x-nmos/node/" {
			_, _ = w.Write([]byte(`["v1.3/"]`))
			return
		}
		_, _ = w.Write([]byte(`[]`))
	}))
	defer srv.Close()

	met := metrics.NewConnector()
	c, err := NewController(context.Background(), ControllerOptions{
		NodeURL: srv.URL,
		Deps:    plugin.Deps{Metrics: met},
	})
	if err != nil {
		t.Fatal(err)
	}
	if c.Metrics() != met {
		t.Fatalf("Metrics() = %p, want the injected connector %p", c.Metrics(), met)
	}
	if c.logger == nil {
		t.Error("a Controller without an explicit Logger must fall back to the Deps logger")
	}
	_, _ = c.Walk(context.Background())
	if snap := met.Snapshot(); snap.TxFrames == 0 || snap.RxFrames == 0 {
		t.Errorf("Walk made requests that were not counted: %+v", snap)
	}
}
