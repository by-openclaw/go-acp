package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"time"

	acp1provider "dhs/internal/acp1/provider"
	"dhs/internal/acp1/codec"
	"dhs/internal/provider"
)

// runACP1Fuzz implements `dhs producer acp1 fuzz [flags]`. Loads a
// tree, spins up the standard ACP1 provider over UDP, and runs the
// random-mutation generator against the writable objects.
func runACP1Fuzz(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("producer acp1 fuzz", flag.ContinueOnError)
	var (
		treePath = fs.String("tree", "", "path to canonical tree.json (required)")
		port     = fs.Int("port", 0, "UDP listen port (0 = plugin default)")
		host     = fs.String("host", "0.0.0.0", "UDP listen host")

		seed         = fs.Int64("seed", 0, "fuzz RNG seed (0 = time-derived)")
		rate         = fs.Float64("rate", 1.0, "events per second")
		duration     = fs.Duration("duration", 0, "stop after this duration (0 = until ctrl-c)")
		slot         = fs.Int("slot", -1, "filter: only this slot (-1 = all)")
		group        = fs.String("group", "", "filter: only this group (control / status / ...) — empty = all")
		id           = fs.Int("id", -1, "filter: only this object id (-1 = all)")
		includeEdges = fs.Bool("include-edges", false, "bias every 4th cycle to a min/max boundary")

		// R15 #476: ladder + format + log-only via the shared helper.
		lf = addLogFlags(fs, "info")
	)
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *treePath == "" {
		return fmt.Errorf("--tree is required")
	}

	cfg := acp1provider.FuzzConfig{
		Seed:         *seed,
		Rate:         *rate,
		Duration:     *duration,
		Slot:         *slot,
		ID:           *id,
		IncludeEdges: *includeEdges,
	}
	if *group != "" {
		g, ok := codec.ParseGroup(*group)
		if !ok {
			return fmt.Errorf("--group %q: unknown group name", *group)
		}
		cfg.Group = g
		cfg.GroupSet = true
	}

	logger, err := lf.resolve("info")
	if err != nil {
		return err
	}

	factory, ok := provider.Lookup("acp1")
	if !ok {
		return fmt.Errorf("acp1 provider not registered")
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

	srv, ok := factory.New(logger, tree).(*acp1provider.Server)
	if !ok {
		return fmt.Errorf("acp1 fuzz: unexpected server type")
	}

	srvCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	serveErr := make(chan error, 1)
	go func() { serveErr <- srv.Serve(srvCtx, addr) }()

	// Give Serve a beat to bind before fuzzing kicks off — the fuzzer
	// emits announces via broadcastAnnounce which tolerates a missing
	// UDP socket, but in practice we want the listener active first
	// so initial announces reach connected consumers.
	time.Sleep(100 * time.Millisecond)

	logger.Info("acp1 fuzz starting",
		slog.String("tree", *treePath),
		slog.String("addr", addr),
		slog.Float64("rate", *rate),
		slog.Duration("duration", *duration))

	fuzzErr := srv.RunFuzz(srvCtx, cfg)

	// Wait for serve to wind down (or already errored).
	cancel()
	if err := <-serveErr; err != nil && !errors.Is(err, context.Canceled) {
		return fmt.Errorf("serve: %w", err)
	}
	return fuzzErr
}
