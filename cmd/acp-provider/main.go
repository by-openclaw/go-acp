// Command acp-provider runs an Ember+ (or future protocol) provider
// server. Loads a canonical tree.json and serves it to consumers.
//
// Usage:
//
//	acp-provider --tree <file.json> [--protocol emberplus] [--port 9010]
//
// The binary is thin: parse flags, read the tree, resolve the provider
// plugin, Serve, wait for SIGINT. Plugin registration happens in
// init() of each imported provider package.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"acp/internal/export/canonical"
	"acp/internal/provider"

	acp1provider "acp/internal/provider/acp1"
	acp2provider "acp/internal/provider/acp2"
	_ "acp/internal/provider/emberplus"
	_ "acp/internal/provider/probel"
)

func main() {
	var (
		treePath      = flag.String("tree", "", "path to canonical tree.json (required)")
		proto         = flag.String("protocol", "emberplus", "provider plugin name")
		port          = flag.Int("port", 0, "TCP listen port (0 = plugin default)")
		host          = flag.String("host", "0.0.0.0", "TCP listen host")
		logLevel      = flag.String("log-level", "info", "log level: debug, info, warn, error")
		announceDemo  = flag.Bool("announce-demo", false, "oscillate a target value every --announce-demo-interval and broadcast announces (demonstrates device-initiated state changes; acp1/acp2 only)")
		announceSlot  = flag.Int("announce-demo-slot", 1, "slot for --announce-demo target")
		announceGroup = flag.Int("announce-demo-group", 2, "acp1: object group for --announce-demo target (2=Control)")
		announceID    = flag.Int("announce-demo-id", 0, "acp1: object id for --announce-demo target (must be Integer type)")
		announceObj   = flag.Int("announce-demo-obj", 18, "acp2: obj-id for --announce-demo target (must be Number+Float)")
		announceEvery = flag.Duration("announce-demo-interval", 2*time.Second, "--announce-demo tick interval")
	)
	flag.Parse()

	if *treePath == "" {
		fmt.Fprintln(os.Stderr, "error: --tree is required")
		flag.Usage()
		os.Exit(2)
	}

	logger := newLogger(*logLevel)

	factory, ok := provider.Lookup(*proto)
	if !ok {
		fmt.Fprintf(os.Stderr, "error: unknown provider %q. available: %v\n", *proto, provider.List())
		os.Exit(2)
	}

	tree, err := loadTree(*treePath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: load tree: %v\n", err)
		os.Exit(1)
	}

	listenPort := *port
	if listenPort == 0 {
		listenPort = factory.Meta().DefaultPort
	}
	addr := fmt.Sprintf("%s:%d", *host, listenPort)

	srv := factory.New(logger, tree)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Graceful shutdown on SIGINT / SIGTERM.
	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-sigs
		logger.Info("shutdown signal received")
		cancel()
		_ = srv.Stop()
	}()

	// Optional: spawn a device-state-change simulator. Dispatched to the
	// loaded plugin's own demo hook; ignored for Ember+ and any other
	// plugin that does not implement an announce demo.
	if *announceDemo {
		switch s := srv.(type) {
		case *acp1provider.Server:
			go s.RunAnnounceDemo(ctx,
				uint8(*announceSlot),
				uint8(*announceGroup),
				uint8(*announceID),
				*announceEvery,
			)
		case *acp2provider.Server:
			go s.RunAnnounceDemo(ctx, uint8(*announceSlot), uint32(*announceObj), *announceEvery)
		default:
			logger.Warn("--announce-demo ignored: current provider has no demo hook",
				slog.String("protocol", *proto),
			)
		}
	}

	if err := srv.Serve(ctx, addr); err != nil && !errors.Is(err, context.Canceled) {
		logger.Error("serve", slog.String("err", err.Error()))
		os.Exit(1)
	}
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
