package metrics

import (
	"io"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// TestPromRegistryScrapes exercises /metrics end-to-end: register a
// Connector + a Process, observe a few frames, hit the Handler, and
// verify the exposition contains the metric families we advertise.
func TestPromRegistryScrapes(t *testing.T) {
	reg := NewPromRegistry()

	conn := NewConnector()
	conn.RegisterCmd(0x02, "rx 002 Crosspoint Connect")
	conn.ObserveCmdRx(0x02, 11)
	conn.ObserveCmdTx(0x04, 12, 75*time.Microsecond)

	if err := reg.Attach(conn, map[string]string{
		"proto": "probel-sw08p",
		"role":  "provider",
	}); err != nil {
		t.Fatalf("Attach: %v", err)
	}

	proc := NewProcess()
	if err := reg.AttachProcess(proc); err != nil {
		t.Fatalf("AttachProcess: %v", err)
	}

	srv := httptest.NewServer(reg.Handler())
	defer srv.Close()

	resp, err := srv.Client().Get(srv.URL)
	if err != nil {
		t.Fatalf("GET /: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	text := string(body)

	wantSubstrings := []string{
		`dhs_connector_rx_frames_total{proto="probel-sw08p",role="provider"} 1`,
		`dhs_connector_tx_frames_total{proto="probel-sw08p",role="provider"} 1`,
		`dhs_connector_rx_cmd_hits_total{`,
		`cmd_id="2"`,
		`dhs_connector_handler_latency_us_bucket_total{`,
		`dhs_process_goroutines`,
		`go_memstats_heap_alloc_bytes`,  // from GoCollector
		`process_cpu_seconds_total`,     // from ProcessCollector (may be 0 on Windows in some flows)
	}
	for _, s := range wantSubstrings {
		// process_cpu_seconds_total may be absent on Windows under some
		// runtimes; tolerate that single absence.
		if strings.Contains(s, "process_cpu_seconds_total") && !strings.Contains(text, s) {
			t.Logf("optional missing: %s (OS may not expose it)", s)
			continue
		}
		if !strings.Contains(text, s) {
			t.Errorf("scrape body missing %q\n--- body ---\n%s", s, text)
		}
	}
}
