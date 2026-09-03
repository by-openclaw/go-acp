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
	"strconv"
	"strings"
	"syscall"
	"time"

	"dhs/internal/devicemodel"
	"dhs/internal/export/canonical"
	"dhs/internal/manifest"
	"dhs/internal/metrics"
	"dhs/internal/provider"

	acp1provider "dhs/internal/acp1/provider"
	acp2provider "dhs/internal/acp2/provider"
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
		treePath       = fs.String("tree", "", "path to canonical tree.json (one of --tree | --manifest required)")
		manifestPath   = fs.String("manifest", "", "path to manifest JSON (.cache/manifest/<device>.json) — assembles the tree from referenced DMs under .cache/dm/<proto>/ per ADR-0022. Mutually exclusive with --tree.")
		cacheDir       = fs.String("cache-dir", ".cache", "root of the cache tree (`.cache/dm/<proto>/<Model@SwRev>.json` lookup base; only used with --manifest)")
		port           = fs.Int("port", 0, "TCP listen port (0 = plugin default)")
		host           = fs.String("host", "0.0.0.0", "TCP/UDP listen host (alias: --bind)")
		bind           = fs.String("bind", "", "alternate spelling of --host. e.g. --bind 10.6.239.200 binds the listener AND pins the broadcast source IP to the VIP, so multi-instance emulators on the same machine appear as distinct From: addresses to consumers (#263).")
		logLevel       = fs.String("log-level", "info", "log level: debug, info, warn, error")
		logFormat      = fs.String("log-format", DefaultLogFormat, "log format: syslog (RFC 5424, default; severity mapped incl. critical — #751 G6) | json (Loki/Promtail) | text (human) — epic #987")
		syslogAddr     = fs.String("syslog-addr", "", "also forward logs as RFC 5424 UDP datagrams to host:port (non-blocking: a slow collector drops records; drops are counted and reported on stderr — #934)")
		announceDemo   = fs.Bool("announce-demo", false, "oscillate a target value every --announce-demo-interval and broadcast announces (acp1/acp2 only)")
		announceSlot   = fs.Int("announce-demo-slot", 1, "slot for --announce-demo target")
		announceGroup  = fs.Int("announce-demo-group", 2, "acp1: object group for --announce-demo target (2=Control)")
		announceID     = fs.Int("announce-demo-id", 0, "acp1: object id for --announce-demo target (must be Integer type)")
		announceObj    = fs.Int("announce-demo-obj", 18, "acp2: obj-id for --announce-demo target (must be Number+Float)")
		announceEvery  = fs.Duration("announce-demo-interval", 2*time.Second, "--announce-demo tick interval")
		metricsAddr    = fs.String("metrics-addr", "", "if set (e.g. ':9100'), serve Prometheus /metrics + Go/process collectors on this address")
		transport      = fs.String("transport", "udp", "acp1 only: udp (Mode A), tcp (Mode B), an2 (Mode C, port 2072), udp+tcp (Mode A + Mode B, no AN2), or all (every transport). Other protocols ignore this flag.")
		tcpPort        = fs.Int("tcp-port", 0, "acp1 only: TCP listen port for --transport tcp/all (0 = same as --port)")
		an2Port        = fs.Int("an2-port", 2072, "acp1 only: AN2/TCP listen port for --transport an2/all")
		adminName      = fs.String("name", "dhs-acp1", "acp1 only: instance name for admin RPC discovery file")
		insertTiming   = fs.String("insert-timing", "real", "acp1 only: cascade timing for slot insert (real / fast)")
		dmLibraryRoot  = fs.String("dm-library", "", "acp1 only: DM library root for admin slot.load (#260)")
		preload        = fs.String("preload", "", "acp1 only: pre-populate slots at boot with NO cascade. External controllers see a stable device from first walk and don't discard the cached template on producer restart. Format: slot=card[,slot=card,...] e.g. 0=axon/synapse/RRS18-1601/acp1,1=axon/synapse/2GS110-2728/acp1")
		play           = fs.String("play", "", "acp1 only: oscillate objects with random values; each tick fires a spontaneous status announce. Pass `all` to oscillate every oscillatable object on every slot (slot 0 included), or a comma-separated path list 1.<slot+1>.<group>.<id>[,...] e.g. 1.1.3.6,1.1.3.7 oscillates Temp_Left + Temp_Right on slot 0")
		playEvery      = fs.Duration("play-interval", 2*time.Second, "acp1 only: tick interval for --play")
		playMode       = fs.String("play-mode", "walk", "acp1 only: --play value strategy — `walk` (mean-reverting drift, realistic) or `random` (force a uniform value across the object's full [min,max] each tick)")
		pidfile        = fs.String("pidfile", "", "if set, write this process's PID to PATH on start (removed on exit) so `dhs producer <proto> stop --pidfile PATH` can signal it")
		announceReplay = fs.String("announce-replay", "", "acp2: loop a recorded announce stream (.jsonl from a real-device capture) to subscribed sessions — real cards announce continuously and controllers derive module liveness from it")
	)
	if err := parseVerbFlags(fs, args); err != nil {
		return err
	}
	if *treePath == "" && *manifestPath == "" {
		return fmt.Errorf("one of --tree | --manifest is required")
	}
	if *treePath != "" && *manifestPath != "" {
		return fmt.Errorf("--tree and --manifest are mutually exclusive")
	}
	// --bind is the canonical name in the design discussion (#263);
	// --host stays for backwards-compat. When both are set, --bind wins.
	if *bind != "" {
		*host = *bind
	}

	// Write a PID file for `producer stop` to signal, removed on exit. Atomic
	// (.tmp + rename) per the repo's file-write convention.
	if *pidfile != "" {
		if err := writePIDFile(*pidfile); err != nil {
			return fmt.Errorf("write pidfile: %w", err)
		}
		defer func() { _ = os.Remove(*pidfile) }()
	}

	logger := newLogger(*logLevel, *logFormat)
	if *syslogAddr != "" {
		udp, err := dialSyslogUDP(*syslogAddr)
		if err != nil {
			return err
		}
		defer udp.Close()
		logger = slog.New(teeHandler{logger.Handler(), udp.Handler(parseLogLevel(*logLevel))})
	}

	factory, ok := provider.Lookup(protoName)
	if !ok {
		return fmt.Errorf("unknown provider %q. available: %v", protoName, provider.List())
	}

	var tree *canonical.Export
	var mf *manifest.Manifest
	if *manifestPath != "" {
		var err error
		mf, err = manifest.Load(*manifestPath)
		if err != nil {
			return fmt.Errorf("load manifest: %w", err)
		}
		if mf.Device.Protocol != protoName {
			return fmt.Errorf("manifest protocol %q != requested %q", mf.Device.Protocol, protoName)
		}
		tree, err = manifest.BuildExport(mf, *cacheDir)
		if err != nil {
			return fmt.Errorf("build tree from manifest: %w", err)
		}
		logger.Info("producer manifest loaded",
			slog.String("path", *manifestPath),
			slog.String("device", mf.Device.Name),
			slog.Int("endpoints", len(mf.Device.Endpoints)),
			slog.Int("frames", len(mf.Frames)),
		)
	} else {
		var err error
		tree, err = loadTree(*treePath)
		if err != nil {
			return fmt.Errorf("load tree: %w", err)
		}
	}

	listenPort := *port
	if listenPort == 0 {
		listenPort = factory.Meta().DefaultPort
	}
	addr := fmt.Sprintf("%s:%d", *host, listenPort)

	srv := factory.New(logger, tree)
	// Manifest slots may declare per-slot GetSlotInfo proto lists
	// (emulation fidelity — e.g. the real Neuron advertises [2,3,4]/[2,3]
	// and Cerebrum's driver gates on it). Providers that support the
	// override expose SetSlotProtos on the concrete type.
	if mf != nil {
		if sp := mf.SlotProtos(); len(sp) > 0 {
			if o, ok := srv.(interface{ SetSlotProtos(map[uint8][]uint8) }); ok {
				o.SetSlotProtos(sp)
				logger.Info("slot proto advertisements from manifest",
					slog.Int("slots", len(sp)))
			}
		}
	}
	// Initialise the rack-controller frame-status from the served tree so a
	// multi-card frame (via --tree OR --manifest) reports its populated slots
	// as present from the first walk, instead of an empty rack. Derived from
	// the tree itself, so a hand-authored multi-slot tree.json works without a
	// manifest. --preload installs further slots after this with its own
	// present-marking.
	if protoName == "acp1" {
		if a1, ok := srv.(*acp1provider.Server); ok {
			if err := a1.MarkTreeSlotsPresent(); err != nil {
				return fmt.Errorf("acp1 frame-status init: %w", err)
			}
		}
	}

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
			connSnap := mp.Metrics()
			mux := http.NewServeMux()
			mux.Handle("/metrics", reg.Handler())
			// /snapshot.json returns the Connector + Process snapshots
			// as JSON so `dhs metrics export` can convert to CSV/MD
			// without parsing Prom text.
			mux.HandleFunc("/snapshot.json", func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				payload := map[string]any{
					"connector": connSnap.Snapshot(),
					"process":   proc.Snapshot(),
					"labels": map[string]string{
						"proto": protoName,
						"role":  "provider",
						"addr":  addr,
					},
				}
				_ = json.NewEncoder(w).Encode(payload)
			})
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

	// --announce-replay: loop a real-device announce recording (acp2).
	// Optional concrete-type interface; providers without it warn.
	if *announceReplay != "" {
		type announceReplayer interface {
			RunAnnounceReplay(ctx context.Context, items []acp2provider.AnnounceItem)
		}
		if ar, ok := srv.(announceReplayer); ok {
			items, err := acp2provider.LoadAnnounceReplay(*announceReplay)
			if err != nil {
				return fmt.Errorf("announce replay: %w", err)
			}
			go ar.RunAnnounceReplay(srvCtx, items)
		} else {
			logger.Warn("--announce-replay set but provider does not support it — skipping",
				slog.String("protocol", protoName))
		}
	}

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

	// ACP1 multi-transport dispatch (Mode A UDP / Mode B TCP / both).
	// Other protocols silently ignore --transport / --tcp-port and run
	// their default Serve.
	if protoName == "acp1" {
		acp1Srv, ok := srv.(*acp1provider.Server)
		if !ok {
			return fmt.Errorf("acp1 producer: wrong server type %T", srv)
		}

		// Slot state-machine timing: --insert-timing real (default) or
		// fast (50ms per phase, for CI integration tests).
		timing, err := acp1provider.ParseInsertTiming(*insertTiming)
		if err != nil {
			return err
		}
		acp1Srv.SetInsertTiming(timing)

		// DM library: --dm-library points at tests/fixtures/products/
		// so the admin slot.load verb (#260) can resolve cards.
		if *dmLibraryRoot != "" {
			acp1Srv.SetDMLibrary(devicemodel.New(*dmLibraryRoot))
		}

		// --preload installs schemas onto slots BEFORE Serve binds the
		// listening socket. External controllers (Cerebrum, VSM Studio)
		// then see a stable device on first walk and don't discard the
		// cached template across producer restarts. Order:
		//   1. Tree.json starter loaded (frame-status array sized).
		//   2. SetDMLibrary attached (resolver ready).
		//   3. Preload entries installed (ReplaceSlot + present).
		//   4. Serve begins.
		if *preload != "" {
			if *dmLibraryRoot == "" {
				return fmt.Errorf("--preload requires --dm-library")
			}
			for _, entry := range strings.Split(*preload, ",") {
				entry = strings.TrimSpace(entry)
				if entry == "" {
					continue
				}
				kv := strings.SplitN(entry, "=", 2)
				if len(kv) != 2 {
					return fmt.Errorf("--preload: entry %q must be slot=card", entry)
				}
				slotN, perr := strconv.Atoi(strings.TrimSpace(kv[0]))
				if perr != nil || slotN < 0 || slotN > 31 {
					return fmt.Errorf("--preload: invalid slot %q", kv[0])
				}
				cardPath := strings.TrimSpace(kv[1])
				if perr := acp1Srv.PreloadSlot(uint8(slotN), cardPath); perr != nil {
					return fmt.Errorf("--preload slot %d (%s): %w", slotN, cardPath, perr)
				}
				logger.Info("acp1 preload",
					slog.Int("slot", slotN),
					slog.String("card", cardPath))
			}
		}

		// --play kicks off a producer-internal random oscillator on
		// the listed object paths. Models real-hardware drift
		// (temperature, PSU rails, packet counters): the server
		// publishes spontaneous status announces on its own without
		// any external trigger.
		if *play != "" {
			var fullRange bool
			switch strings.ToLower(strings.TrimSpace(*playMode)) {
			case "", "walk":
				fullRange = false
			case "random", "force":
				fullRange = true
			default:
				return fmt.Errorf("--play-mode %q invalid (use walk | random)", *playMode)
			}
			if strings.EqualFold(strings.TrimSpace(*play), "all") {
				acp1Srv.RunStatusPlayAll(srvCtx, *playEvery, fullRange)
				// "all" also drives the rack frame-status so consumers can
				// detect slot insert/remove/error events on slot 0.
				acp1Srv.RunFrameStatusPlay(srvCtx, *playEvery)
			} else {
				acp1Srv.RunStatusPlay(srvCtx, strings.Split(*play, ","), *playEvery, fullRange)
			}
		}

		// Admin RPC always runs alongside the wire transports so the
		// `dhs producer acp1 admin <verb>` CLI can talk to this
		// instance (#258).
		go func() {
			if err := acp1Srv.ServeAdmin(srvCtx, *adminName); err != nil &&
				!errors.Is(err, context.Canceled) {
				logger.Warn("acp1 admin server stopped",
					slog.String("err", err.Error()))
			}
		}()
		switch *transport {
		case "udp":
			if err := acp1Srv.Serve(srvCtx, addr); err != nil && !errors.Is(err, context.Canceled) {
				return fmt.Errorf("serve udp: %w", err)
			}
		case "tcp":
			tcpAddr := tcpListenAddr(*host, listenPort, *tcpPort)
			if err := acp1Srv.ServeTCP(srvCtx, tcpAddr); err != nil && !errors.Is(err, context.Canceled) {
				return fmt.Errorf("serve tcp: %w", err)
			}
		case "an2":
			an2Addr := fmt.Sprintf("%s:%d", *host, *an2Port)
			if err := acp1Srv.ServeAN2(srvCtx, an2Addr); err != nil && !errors.Is(err, context.Canceled) {
				return fmt.Errorf("serve an2: %w", err)
			}
		case "all":
			tcpAddr := tcpListenAddr(*host, listenPort, *tcpPort)
			an2Addr := fmt.Sprintf("%s:%d", *host, *an2Port)
			udpErrCh := make(chan error, 1)
			tcpErrCh := make(chan error, 1)
			an2ErrCh := make(chan error, 1)
			go func() { udpErrCh <- acp1Srv.Serve(srvCtx, addr) }()
			go func() { tcpErrCh <- acp1Srv.ServeTCP(srvCtx, tcpAddr) }()
			go func() { an2ErrCh <- acp1Srv.ServeAN2(srvCtx, an2Addr) }()
			drain := func(ch chan error) error {
				err := <-ch
				if err != nil && !errors.Is(err, context.Canceled) {
					return err
				}
				return nil
			}
			select {
			case err := <-udpErrCh:
				cancel()
				_ = drain(tcpErrCh)
				_ = drain(an2ErrCh)
				if err != nil && !errors.Is(err, context.Canceled) {
					return fmt.Errorf("serve udp: %w", err)
				}
			case err := <-tcpErrCh:
				cancel()
				_ = drain(udpErrCh)
				_ = drain(an2ErrCh)
				if err != nil && !errors.Is(err, context.Canceled) {
					return fmt.Errorf("serve tcp: %w", err)
				}
			case err := <-an2ErrCh:
				cancel()
				_ = drain(udpErrCh)
				_ = drain(tcpErrCh)
				if err != nil && !errors.Is(err, context.Canceled) {
					return fmt.Errorf("serve an2: %w", err)
				}
			}
		case "udp+tcp":
			// Mode A + Mode B without AN2 (Mode C). Spec-strict ACP1 only;
			// avoids the auto-negotiation pitfall where some controllers
			// pick AN2 the moment 2072 is open.
			tcpAddr := tcpListenAddr(*host, listenPort, *tcpPort)
			udpErrCh := make(chan error, 1)
			tcpErrCh := make(chan error, 1)
			go func() { udpErrCh <- acp1Srv.Serve(srvCtx, addr) }()
			go func() { tcpErrCh <- acp1Srv.ServeTCP(srvCtx, tcpAddr) }()
			drain := func(ch chan error) error {
				err := <-ch
				if err != nil && !errors.Is(err, context.Canceled) {
					return err
				}
				return nil
			}
			select {
			case err := <-udpErrCh:
				cancel()
				_ = drain(tcpErrCh)
				if err != nil && !errors.Is(err, context.Canceled) {
					return fmt.Errorf("serve udp: %w", err)
				}
			case err := <-tcpErrCh:
				cancel()
				_ = drain(udpErrCh)
				if err != nil && !errors.Is(err, context.Canceled) {
					return fmt.Errorf("serve tcp: %w", err)
				}
			}
		default:
			return fmt.Errorf("acp1 producer: unknown --transport %q (use udp / tcp / an2 / udp+tcp / all)", *transport)
		}
		return nil
	}

	if err := srv.Serve(srvCtx, addr); err != nil && !errors.Is(err, context.Canceled) {
		return fmt.Errorf("serve: %w", err)
	}
	return nil
}

// tcpListenAddr resolves the TCP listen address. Defaults to the same
// host:port pair as UDP when --tcp-port is unset.
func tcpListenAddr(host string, udpPort, override int) string {
	port := override
	if port == 0 {
		port = udpPort
	}
	return fmt.Sprintf("%s:%d", host, port)
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

func parseLogLevel(level string) slog.Level {
	switch level {
	case "debug":
		return slog.LevelDebug
	case "warn":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	case "critical":
		return LevelCritical
	default:
		return slog.LevelInfo
	}
}

func newLogger(level, format string) *slog.Logger {
	// Delegate to the shared format chooser (epic #987) so producer and
	// consumer pick the log FORMAT identically. Sinks (stderr here, +file/
	// +syslog-addr) are layered by the caller.
	return newLoggerTo(os.Stderr, parseLogLevel(level), format)
}
