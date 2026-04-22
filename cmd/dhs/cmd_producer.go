// runProducer implements `dhs producer <protocol> serve [flags]`.
// Loads a canonical tree.json and serves it to consumers via the named
// provider plugin. Plugin registration happens in init() of each imported
// provider package — see main.go for the blank-import list.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"acp/internal/export/canonical"
	"acp/internal/metrics"
	"acp/internal/provider"

	acp1provider "acp/internal/acp1/provider"
	acp2provider "acp/internal/acp2/provider"
)

// metricsExposer is the optional interface provider servers implement
// to participate in the /metrics scrape. Probel provider's *Server
// satisfies it today; other protocols add it in D8.
type metricsExposer interface {
	Metrics() *metrics.Connector
}

// runProducer is called by the top-level dispatcher with the protocol name
// already parsed out of the argv. The remaining args follow an optional verb
// (currently only `serve`).
func runProducer(ctx context.Context, protoName string, args []string) error {
	fs := flag.NewFlagSet("producer "+protoName+" serve", flag.ContinueOnError)
	var (
		treePath      = fs.String("tree", "", "path to canonical tree.json (required)")
		port          = fs.Int("port", 0, "TCP listen port (0 = plugin default)")
		host          = fs.String("host", "0.0.0.0", "TCP listen host")
		logLevel      = fs.String("log-level", "info", "log level: debug, info, warn, error")
		announceDemo  = fs.Bool("announce-demo", false, "oscillate a target value every --announce-demo-interval and broadcast announces (acp1/acp2 only)")
		announceSlot  = fs.Int("announce-demo-slot", 1, "slot for --announce-demo target")
		announceGroup = fs.Int("announce-demo-group", 2, "acp1: object group for --announce-demo target (2=Control)")
		announceID    = fs.Int("announce-demo-id", 0, "acp1: object id for --announce-demo target (must be Integer type)")
		announceObj   = fs.Int("announce-demo-obj", 18, "acp2: obj-id for --announce-demo target (must be Number+Float)")
		announceEvery = fs.Duration("announce-demo-interval", 2*time.Second, "--announce-demo tick interval")
		metricsAddr   = fs.String("metrics-addr", "", "if set (e.g. ':9100'), serve Prometheus /metrics + Go/process collectors on this address")
	)
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *treePath == "" {
		return fmt.Errorf("--tree is required")
	}

	logger := newLogger(*logLevel)

	factory, ok := provider.Lookup(protoName)
	if !ok {
		return fmt.Errorf("unknown provider %q. available: %v", protoName, provider.List())
	}

	tree, err := loadTree(*treePath)
	if err != nil {
		return fmt.Errorf("load tree: %w", err)
	}

	listenPort := *port
	if listenPort == 0 {
		listenPort = factory.Meta().DefaultPort
	}
	addr := fmt.Sprintf("%s:%d", *host, listenPort)

	srv := factory.New(logger, tree)

	srvCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	// --metrics-addr mounts Prometheus /metrics if the provider
	// exposes a *metrics.Connector (optional interface). Plugins that
	// haven't landed metrics wiring yet silently skip with a warn.
	if *metricsAddr != "" {
		if mp, ok := srv.(metricsExposer); ok {
			proc := metrics.NewProcess()
			go proc.Run(5*time.Second, srvCtx.Done())
			reg := metrics.NewPromRegistry()
			if err := reg.Attach(mp.Metrics(), map[string]string{
				"proto": protoName,
				"role":  "provider",
				"addr":  addr,
			}); err != nil {
				logger.Warn("metrics attach failed", slog.String("err", err.Error()))
			}
			if err := reg.AttachProcess(proc); err != nil {
				logger.Warn("metrics attach process failed", slog.String("err", err.Error()))
			}
			mux := http.NewServeMux()
			mux.Handle("/metrics", reg.Handler())
			metricsSrv := &http.Server{
				Addr:              *metricsAddr,
				Handler:           mux,
				ReadHeaderTimeout: 5 * time.Second,
			}
			go func() {
				logger.Info("metrics endpoint serving",
					slog.String("addr", *metricsAddr),
					slog.String("path", "/metrics"))
				if err := metricsSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
					logger.Error("metrics server failed", slog.String("err", err.Error()))
				}
			}()
			go func() {
				<-srvCtx.Done()
				shutdownCtx, cancelShutdown := context.WithTimeout(context.Background(), 2*time.Second)
				defer cancelShutdown()
				_ = metricsSrv.Shutdown(shutdownCtx)
			}()
		} else {
			logger.Warn("--metrics-addr set but provider does not expose Metrics() — skipping",
				slog.String("protocol", protoName))
		}
	}

	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-sigs
		logger.Info("shutdown signal received")
		cancel()
		_ = srv.Stop()
	}()

	if *announceDemo {
		switch s := srv.(type) {
		case *acp1provider.Server:
			go s.RunAnnounceDemo(srvCtx,
				uint8(*announceSlot),
				uint8(*announceGroup),
				uint8(*announceID),
				*announceEvery,
			)
		case *acp2provider.Server:
			go s.RunAnnounceDemo(srvCtx, uint8(*announceSlot), uint32(*announceObj), *announceEvery)
		default:
			logger.Warn("--announce-demo ignored: current provider has no demo hook",
				slog.String("protocol", protoName),
			)
		}
	}

	if err := srv.Serve(srvCtx, addr); err != nil && !errors.Is(err, context.Canceled) {
		return fmt.Errorf("serve: %w", err)
	}
	return nil
}

func loadTree(path string) (*canonical.Export, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var exp canonical.Export
	if err := json.Unmarshal(data, &exp); err != nil {
		return nil, fmt.Errorf("parse canonical: %w", err)
	}
	return &exp, nil
}

func newLogger(level string) *slog.Logger {
	var lvl slog.Level
	switch level {
	case "debug":
		lvl = slog.LevelDebug
	case "warn":
		lvl = slog.LevelWarn
	case "error":
		lvl = slog.LevelError
	default:
		lvl = slog.LevelInfo
	}
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: lvl}))
}
