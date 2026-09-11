package main

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net"
	"net/http"
	"testing"
	"time"

	"dhs/internal/metrics"
)

// serveMetricsEndpoint is the one metrics mount every serving verb uses:
// /metrics (Prometheus) and /snapshot.json (connector + process + labels)
// come up on the address and go away with the context.
func TestServeMetricsEndpointMountsBothViews(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	_ = ln.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	met := metrics.NewConnector()
	met.ObserveRx(12)
	serveMetricsEndpoint(ctx, slog.New(slog.NewTextHandler(io.Discard, nil)), addr, met,
		map[string]string{"proto": "nmos", "role": "node", "addr": ":8080"})

	var body []byte
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		resp, err := http.Get("http://" + addr + "/snapshot.json")
		if err == nil {
			body, _ = io.ReadAll(resp.Body)
			_ = resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				break
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	var snap struct {
		Connector struct{ RxBytes uint64 } `json:"connector"`
		Labels    map[string]string        `json:"labels"`
	}
	if err := json.Unmarshal(body, &snap); err != nil {
		t.Fatalf("snapshot.json not served: %v (%s)", err, body)
	}
	if snap.Connector.RxBytes != 12 || snap.Labels["role"] != "node" {
		t.Errorf("snapshot = %+v, want the connector's counters and the labels", snap)
	}
	resp, err := http.Get("http://" + addr + "/metrics")
	if err != nil || resp.StatusCode != http.StatusOK {
		t.Fatalf("/metrics = %v, %v", resp, err)
	}
	_ = resp.Body.Close()

	cancel()
	deadline = time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := http.Get("http://" + addr + "/metrics"); err != nil {
			return // gone with the context
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Error("metrics endpoint still serving after the context was cancelled")
}
