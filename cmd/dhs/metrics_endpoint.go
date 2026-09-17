package main

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"time"

	"dhs/internal/metrics"
)

// serveMetricsEndpoint mounts Prometheus /metrics (connector + Go/process
// collectors) and /snapshot.json (the JSON `dhs metrics export` and
// `producer <proto> status` read) on addr, for ONE connector's counter set,
// until ctx is cancelled. Every serving verb — producer serve, nmos serve,
// registry serve — mounts through here, so --metrics-addr means the same
// thing and exposes the same shape whatever the protocol. labels ride on
// both views (proto, role, addr).
func serveMetricsEndpoint(ctx context.Context, logger *slog.Logger, addr string, met *metrics.Connector, labels map[string]string) {
	proc := metrics.NewProcess()
	go proc.Run(5*time.Second, ctx.Done())
	reg := metrics.NewPromRegistry()
	if err := reg.Attach(met, labels); err != nil {
		logger.Warn("metrics attach failed", slog.String("err", err.Error()))
	}
	if err := reg.AttachProcess(proc); err != nil {
		logger.Warn("metrics attach process failed", slog.String("err", err.Error()))
	}
	mux := http.NewServeMux()
	mux.Handle("/metrics", reg.Handler())
	mux.HandleFunc("/snapshot.json", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		payload := map[string]any{
			"connector": met.Snapshot(),
			"process":   proc.Snapshot(),
			"labels":    labels,
		}
		_ = json.NewEncoder(w).Encode(payload)
	})
	metricsSrv := &http.Server{
		Addr:              addr,
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}
	go func() {
		logger.Info("metrics endpoint serving",
			slog.String("addr", addr),
			slog.String("path", "/metrics"))
		if err := metricsSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Error("metrics server failed", slog.String("err", err.Error()))
		}
	}()
	go func() {
		<-ctx.Done()
		shutdownCtx, cancelShutdown := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancelShutdown()
		_ = metricsSrv.Shutdown(shutdownCtx)
	}()
}
