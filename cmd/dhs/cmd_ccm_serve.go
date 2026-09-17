package main

// `dhs producer ccm serve` — replay a captured CCM device model so a
// controller (Cerebrum) drives dhs as if it were a real CCM (EVS BRIDGE /
// Neuron) device. The model is a dm-tree (resource path -> resource JSON),
// the file `dhs consumer ccm export` writes; the OpenAPI document is the
// device's own api.yml from the same export. The provider serves exactly the
// CCM protocol under /api/v1 and keeps every dhs addition under /x-dhs.
//
//   dhs producer ccm serve --dm-tree dm-tree.json [--api-spec api.yml] [--bind :8080]
//   dhs producer ccm serve --dm-tree dm-tree.json --tls-cert dev.crt --tls-key dev.key --bind :8443

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	ccmp "dhs/internal/ccm/provider"
	"dhs/internal/metrics"
	"dhs/internal/plugin"
	"dhs/internal/transport"
)

func runCCMProducer(ctx context.Context, args []string) error {
	if len(args) == 0 || isHelpToken(args[0]) {
		printCCMProducerHelp()
		return nil
	}
	verb := args[0]
	rest := args[1:]
	switch verb {
	case "serve":
		return runCCMServe(ctx, rest)
	case "ensure":
		// ADR-0007: converge the serving instance to --state present|absent,
		// keyed on the --pidfile serve wrote — the same generic implementation
		// every producer uses, so an Ansible play treats a CCM device like any
		// other dhs producer.
		return runProducerEnsure(ctx, "ccm", rest)
	case "stop":
		return runProducerStop(ctx, "ccm", rest)
	case "status":
		// Live runtime snapshot of a serving instance (frames/bytes/latency),
		// fetched from its --metrics-addr /snapshot.json via --url.
		return runMetricsShow(ctx, rest)
	}
	return fmt.Errorf("producer ccm: unknown verb %q (expected: serve | ensure | stop | status)", verb)
}

func printCCMProducerHelp() {
	fmt.Println(`dhs producer ccm — serve a captured CCM device model (EVS BRIDGE / Neuron REST)

USAGE
  dhs producer ccm serve --dm-tree PATH [flags]

VERBS
  serve     replay a dm-tree over HTTP(S) as a CCM device a controller can drive
  ensure    ADR-0007 converge to --state present|absent, keyed on --pidfile
  stop      signal a 'serve --pidfile PATH' instance to shut down (--pidfile PATH)
  status    live runtime snapshot of a serving instance (--url http://host:port/snapshot.json)

FLAGS (serve)
  --dm-tree PATH        device model to replay: resource path -> resource JSON,
                        as written by 'dhs consumer ccm export' (required)
  --pidfile PATH        write the PID here on start (removed on exit) for stop/ensure
  --api-spec PATH       the device's OpenAPI 3.1 api.yml: served at
                        /api/v1/docs/api.yml and the WRITE CONTRACT — only the
                        operations it declares are accepted (without it: GET only)
  --bind ADDR           listen address (default :8080; a real device is https :443)
  --tls-cert PATH       server certificate (PEM) — with --tls-key, serves HTTPS
  --tls-key PATH        private key for --tls-cert
  --metrics-addr ADDR   serve Prometheus /metrics + /snapshot.json here
  --readme PATH         Markdown to render at /x-dhs/readme (default: the provider README)

NAMESPACES
  /api/v1   the CCM protocol, 100% to the device's api.yml — the tree, the document,
            the writes it declares, {code,message} errors
  /x-dhs    dhs-only additions — landing (/x-dhs/), rendered README (/x-dhs/readme),
            capabilities (/x-dhs/capabilities); never touches /api/v1

EXAMPLES
  dhs producer ccm serve --dm-tree BRIDGE@7.0.2/dm-tree.json --api-spec BRIDGE@7.0.2/api.yml
  dhs producer ccm serve --dm-tree dm-tree.json --bind :8443 --tls-cert dev.crt --tls-key dev.key`)
}

func runCCMServe(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("producer ccm serve", flag.ContinueOnError)
	treePath := fs.String("dm-tree", "", "device model to replay (resource path -> resource JSON), from 'dhs consumer ccm export' (required)")
	specPath := fs.String("api-spec", "", "the device's OpenAPI 3.1 api.yml: served at /api/v1/docs/api.yml and the write contract (without it: GET only)")
	bind := fs.String("bind", ":8080", "listen address (a real device serves https on :443)")
	tlsCert := fs.String("tls-cert", "", "server certificate PEM; with --tls-key, serve HTTPS")
	tlsKey := fs.String("tls-key", "", "private key for --tls-cert")
	metricsAddr := fs.String("metrics-addr", "", "if set (e.g. ':9100'), serve Prometheus /metrics + /snapshot.json on this address")
	readmePath := fs.String("readme", "", "Markdown document to serve rendered at /x-dhs/readme (default: the provider's own README)")
	pidfile := fs.String("pidfile", "", "if set, write this process's PID to PATH on start (removed on exit) so `dhs producer ccm stop|ensure --pidfile PATH` can manage it")
	if err := parseVerbFlags(fs, args); err != nil {
		return err
	}
	if *treePath == "" {
		return fmt.Errorf("producer ccm serve: --dm-tree PATH is required (the model to replay, from 'dhs consumer ccm export')")
	}
	if (*tlsCert == "") != (*tlsKey == "") {
		return fmt.Errorf("producer ccm serve: --tls-cert and --tls-key must be given together")
	}
	if *pidfile != "" {
		if err := writePIDFile(*pidfile); err != nil {
			return fmt.Errorf("producer ccm serve: write pidfile: %w", err)
		}
		defer func() { _ = os.Remove(*pidfile) }()
	}

	raw, err := os.ReadFile(*treePath)
	if err != nil {
		return fmt.Errorf("producer ccm serve: read --dm-tree: %w", err)
	}
	tree, err := ccmp.LoadTree(raw)
	if err != nil {
		return fmt.Errorf("producer ccm serve: %w", err)
	}
	var spec []byte
	if *specPath != "" {
		if spec, err = os.ReadFile(*specPath); err != nil {
			return fmt.Errorf("producer ccm serve: read --api-spec: %w", err)
		}
	}
	var readme []byte
	if *readmePath != "" {
		if readme, err = os.ReadFile(*readmePath); err != nil {
			return fmt.Errorf("producer ccm serve: read --readme: %w", err)
		}
	}

	logger, _, logClean, _ := consumerLogger(ctx, "ccm", *bind, "serve")
	defer logClean()

	srv := ccmp.NewServer(plugin.Deps{Logger: logger}, tree, spec)
	// The dhs landing (this page, the rendered README, capabilities) lives
	// under /x-dhs — never under the CCM namespace.
	srv.MountLanding(readme)
	if *tlsCert != "" {
		if err := srv.WithTLS(transport.TLSOptions{Enable: true, CertFile: *tlsCert, KeyFile: *tlsKey}); err != nil {
			return fmt.Errorf("producer ccm serve: %w", err)
		}
	}

	srvCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	if *metricsAddr != "" {
		mountMetricsEndpoint(srvCtx, logger, *metricsAddr, srv.Metrics(), map[string]string{
			"proto": "ccm", "role": "provider", "addr": *bind,
		})
	}

	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(sigs)
	go func() {
		select {
		case <-sigs:
			logger.Info("shutdown signal received")
			cancel()
		case <-srvCtx.Done():
		}
	}()

	return srv.Serve(srvCtx, *bind)
}

// mountMetricsEndpoint serves Prometheus /metrics and /snapshot.json for one
// connector on addr until ctx ends — the same surface every other producer
// exposes under --metrics-addr, so `dhs metrics show` and the Grafana stack
// see a CCM device like any other connector.
func mountMetricsEndpoint(ctx context.Context, logger *slog.Logger, addr string, conn *metrics.Connector, labels map[string]string) {
	proc := metrics.NewProcess()
	go proc.Run(5*time.Second, ctx.Done())
	reg := metrics.NewPromRegistry()
	if err := reg.Attach(conn, labels); err != nil {
		logger.Warn("metrics attach failed", slog.String("err", err.Error()))
	}
	if err := reg.AttachProcess(proc); err != nil {
		logger.Warn("metrics attach process failed", slog.String("err", err.Error()))
	}
	mux := http.NewServeMux()
	mux.Handle("/metrics", reg.Handler())
	mux.HandleFunc("/snapshot.json", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"connector": conn.Snapshot(),
			"process":   proc.Snapshot(),
			"labels":    labels,
		})
	})
	msrv := &http.Server{Addr: addr, Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	go func() {
		logger.Info("metrics endpoint serving", slog.String("addr", addr), slog.String("path", "/metrics"))
		if err := msrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Error("metrics server failed", slog.String("err", err.Error()))
		}
	}()
	go func() {
		<-ctx.Done()
		sctx, c := context.WithTimeout(context.Background(), 2*time.Second)
		defer c()
		_ = msrv.Shutdown(sctx)
	}()
}
