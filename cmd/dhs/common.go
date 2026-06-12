package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"dhs/internal/logging"
	"dhs/internal/consumer"
	"dhs/internal/acp1/consumer"
	"dhs/internal/datastore"
	"dhs/internal/transport"
)

// treeStore is the global file-backed tree store, initialized once.
// Per ADR-0020 Bucket 4: rooted at <project>/.cache/devices/{ip}/slot_{n}.json
// (gitignored, regeneratable; separate from manual captures under captures/).
var treeStore *datastore.TreeStore

func init() {
	store, err := datastore.NewTreeStoreInProjectCache()
	if err == nil {
		treeStore = store
	}
}

// commonFlags holds the flags every subcommand accepts. Parsed per
// subcommand so positional args (the host) stay in position 1.
type commonFlags struct {
	protocol  string
	transport         string
	port              int
	timeout           time.Duration
	keepalive         time.Duration
	keepaliveTimeout  time.Duration
	verbose           bool
	logLevel          string
	capture           string

	// captureDir is populated by connect() when --capture points at a
	// directory (or at a path without a .jsonl extension). In that
	// mode, raw frames go to <captureDir>/raw.<transport>.jsonl named
	// after the wire-format carried — raw.acp1.jsonl for ACP1 UDP/TCP,
	// raw.an2.jsonl for ACP2 AN2 frames, raw.s101.jsonl for Ember+
	// S101 framing — and the post-walk step additionally writes
	// glow.json + tree.json alongside. Plain single-file --capture
	// keeps the legacy flat-JSONL behaviour.
	captureDir string

	// canonical-export mode flags (Ember+ only). Each controls one
	// piece of the resolver contract — see docs/protocols/schema.md §4
	// and internal/emberplus/consumer/resolver.go. Default "pointer"
	// emits the wire-faithful shape; "inline" absorbs the referenced
	// subtree into the referring element; "both" keeps both.
	canonTemplates string
	canonLabels    string
	canonGain      string
}

func addCommonFlags(fs *flag.FlagSet) *commonFlags {
	cf := &commonFlags{}
	fs.StringVar(&cf.protocol, "protocol", "acp1", "protocol plugin name")
	fs.StringVar(&cf.transport, "transport", "auto",
		"transport: auto (default, TCP-first with UDP fallback like real "+
			"ACP1 controllers), udp (subnet broadcast announcements only) "+
			"or tcp (ACP1 v1.4 TCP direct, crosses VLANs)")
	fs.IntVar(&cf.port, "port", 0, "override default port (0 = plugin default)")
	fs.DurationVar(&cf.timeout, "timeout", 1*time.Second, "per-operation timeout (single get/set/connect; walks ignore this and run until done)")
	fs.DurationVar(&cf.keepalive, "keepalive", 0,
		"keep-alive probe interval (0 = plugin default — acp1: 5s, "+
			"emberplus: 10s — match real-controller cadence; -1 = disable probes)")
	fs.DurationVar(&cf.keepaliveTimeout, "keepalive-timeout", 0,
		"keep-alive dead-man threshold (0 = 3× --keepalive; -1 = never "+
			"declare session dead). Watch verb shows freshness=cache once "+
			"this elapses without rx; values stay decoded against the cached schema.")
	fs.BoolVar(&cf.verbose, "verbose", false, "debug log output (shortcut for --log-level debug)")
	fs.StringVar(&cf.logLevel, "log-level", "info", "log level: trace, debug, info, warn, error, critical")
	fs.StringVar(&cf.capture, "capture", "",
		"capture traffic. Path ending in .jsonl → single-file raw frame log "+
			"(ACP1/ACP2/Ember+). Any other path → directory mode: writes "+
			"raw.<transport>.jsonl (raw.acp1 / raw.an2 / raw.s101 per protocol) "+
			"+ tree.json (all 3 protocols) + glow.json (Ember+ only).")
	fs.StringVar(&cf.canonTemplates, "templates", "pointer",
		"canonical export mode for templateReference (Ember+ only): "+
			"pointer (wire-faithful), inline (absorb template into element), "+
			"both (keep ref + absorbed shape).")
	fs.StringVar(&cf.canonLabels, "labels", "pointer",
		"canonical export mode for matrix labels (Ember+ only): "+
			"pointer (wire-faithful, multi-level array preserved), "+
			"inline (absorb label subtree, populate targetLabels/sourceLabels), "+
			"both (keep pointer + absorbed maps).")
	fs.StringVar(&cf.canonGain, "gain", "pointer",
		"canonical export mode for parametersLocation (Ember+ only): "+
			"pointer (wire-faithful), inline (absorb params subtree, populate "+
			"targetParams/sourceParams/connectionParams), both (keep both).")
	return cf
}

// rawFrameFilename picks the capture-dir filename that matches the
// wire-format the plugin carries. Kept here (not in the plugin) so
// directory-mode captures get the correct filename even when the
// recorder is wrapped before the plugin instance exists.
//
//	acp1       → raw.acp1.jsonl   (UDP datagrams or TCP/AN2 ACP1 frames)
//	acp2       → raw.an2.jsonl    (AN2 frames wrapping ACP2 payload)
//	emberplus  → raw.s101.jsonl   (S101-framed BER)
//	other      → raw.jsonl        (generic fallback — should not happen
//	                               on registered plugins)
func rawFrameFilename(proto string) string {
	switch proto {
	case "acp1":
		return "raw.acp1.jsonl"
	case "acp2":
		return "raw.an2.jsonl"
	case "emberplus":
		return "raw.s101.jsonl"
	}
	return "raw.jsonl"
}

// isDirectoryCapture decides whether the --capture path should be
// treated as a directory (three-file mode) or a single JSONL file.
// Rules, in order:
//
//  1. Existing directory → dir mode.
//  2. Existing file     → file mode.
//  3. Path ends in ".jsonl" or ".json" → file mode.
//  4. Otherwise         → dir mode (the path will be created).
func isDirectoryCapture(path string) bool {
	if path == "" {
		return false
	}
	if st, err := os.Stat(path); err == nil {
		return st.IsDir()
	}
	switch strings.ToLower(filepath.Ext(path)) {
	case ".jsonl", ".json":
		return false
	}
	return true
}

// connect builds a fresh plugin instance, dials the host, and returns the
// live Protocol along with a cleanup function. Every subcommand starts
// with this; the cleanup runs on function exit.
func connect(ctx context.Context, host string, cf *commonFlags) (consumer.Protocol, func(), error) {
	if host == "" {
		return nil, nil, fmt.Errorf("host argument is required")
	}

	lvl := logging.ParseLevel(cf.logLevel)
	if cf.verbose && lvl > logging.LevelDebug {
		lvl = logging.LevelDebug // --verbose is shortcut for --log-level debug
	}
	logger := logging.NewTextLogger(lvl)

	// Optional traffic capture for test data generation.
	var recorder *transport.Recorder
	if cf.capture != "" {
		recorderPath := cf.capture
		if isDirectoryCapture(cf.capture) {
			cf.captureDir = cf.capture
			if err := os.MkdirAll(cf.captureDir, 0o755); err != nil {
				return nil, nil, fmt.Errorf("capture dir: %w", err)
			}
			recorderPath = filepath.Join(cf.captureDir, rawFrameFilename(cf.protocol))
		}
		var recErr error
		recorder, recErr = transport.NewRecorder(recorderPath)
		if recErr != nil {
			return nil, nil, fmt.Errorf("capture: %w", recErr)
		}
	}

	factory, err := consumer.Get(cf.protocol)
	if err != nil {
		if recorder != nil {
			_ = recorder.Close()
		}
		return nil, nil, err
	}
	plug := factory.New(logger)

	// Attach recorder if --capture was given.
	if recorder != nil {
		if p, ok := plug.(interface{ SetRecorder(*transport.Recorder) }); ok {
			p.SetRecorder(recorder)
		}
	}

	// Transport selection is plugin-specific; cast when possible and
	// apply. Protocols that don't expose SetTransport just ignore it.
	if tcfg, ok := plug.(interface{ SetTransport(acp1.TransportKind) }); ok {
		switch strings.ToLower(cf.transport) {
		case "tcp", "tcp-direct", "tcpdirect":
			tcfg.SetTransport(acp1.TransportTCPDirect)
		case "udp":
			tcfg.SetTransport(acp1.TransportUDP)
		case "an2", "an2-tcp", "mode-c":
			tcfg.SetTransport(acp1.TransportAN2)
		case "auto", "":
			tcfg.SetTransport(acp1.TransportAuto)
		default:
			return nil, nil, fmt.Errorf("unknown --transport %q (use auto / udp / tcp / an2)", cf.transport)
		}
	}

	// Keep-alive selection is also plugin-specific via the optional
	// consumer.KeepAliver capability. Plugins that don't implement it
	// silently ignore the flags.
	if ka, ok := plug.(consumer.KeepAliver); ok {
		ka.SetKeepAlive(consumer.KeepAliveConfig{
			Interval: cf.keepalive,
			Timeout:  cf.keepaliveTimeout,
		})
	}

	port := cf.port
	if port == 0 {
		port = factory.Meta().DefaultPort
	}

	// Connect needs a longer floor than a single-op --timeout: ACP2
	// does AN2 GetVersion + GetDeviceInfo + GetSlotInfo(n) + EnableEvents
	// + ACP2 GetVersion before returning ready (~5 round trips on LAN),
	// and TCP dial itself can take 100-300 ms on first attempt. Use
	// max(cf.timeout, 5s) so a tight --timeout doesn't kill connect.
	dialTimeout := cf.timeout
	if dialTimeout < 5*time.Second {
		dialTimeout = 5 * time.Second
	}
	dialCtx, cancel := context.WithTimeout(ctx, dialTimeout)
	defer cancel()
	if err := plug.Connect(dialCtx, host, port); err != nil {
		return nil, nil, err
	}
	cleanup := func() {
		_ = plug.Disconnect()
		if recorder != nil {
			_ = recorder.Close()
		}
	}
	return plug, cleanup, nil
}

// connectWithRetry wraps connect() with the same exponential backoff
// the per-plugin warm-restart watcher uses (1s → 2s → 4s → 8s → 16s →
// 30s cap). Used by long-running verbs like `watch` so the operator can
// launch them before the producer is up — they wait, log progress, and
// transparently proceed once the server appears. Bails on ctx cancel
// (Ctrl-C); never bails on transport error alone.
//
// One-shot verbs (info, walk, get, set, health) keep the fail-fast
// connect() so a typo'd IP doesn't hang forever.
func connectWithRetry(ctx context.Context, host string, cf *commonFlags) (consumer.Protocol, func(), error) {
	const (
		initial = 1 * time.Second
		cap_    = 30 * time.Second
	)
	delay := initial
	attempt := 0
	for {
		attempt++
		plug, cleanup, err := connect(ctx, host, cf)
		if err == nil {
			if attempt > 1 {
				fmt.Fprintf(os.Stderr, "connect: succeeded on attempt %d\n", attempt)
			}
			return plug, cleanup, nil
		}
		if ctx.Err() != nil {
			return nil, nil, ctx.Err()
		}
		fmt.Fprintf(os.Stderr, "connect: attempt %d failed: %v (retry in %s)\n",
			attempt, err, delay)

		select {
		case <-ctx.Done():
			return nil, nil, ctx.Err()
		case <-time.After(delay):
		}
		delay *= 2
		if delay > cap_ {
			delay = cap_
		}
	}
}

// resolvePathFromCache tries to find an object ID from the disk cache
// for path-based addressing. Path is a dot-separated label sequence
// (e.g. "IDENTITY.User Label 1"); the lookup matches on the **suffix**
// of the cached object's Path so partial parents work too. Returns
// the ID if found, -1 otherwise.
//
// Avoids a full slot walk when the path is already in the cache —
// previously the set / get verbs always issued plug.Walk(ctx, slot)
// up-front, blocking for minutes on a 49 000-object Neuron tree.
func resolvePathFromCache(host, proto string, slot int, path string) int {
	if treeStore == nil || path == "" {
		return -1
	}
	snap, err := treeStore.Load(host, slot)
	if err != nil || snap == nil {
		return -1
	}
	if snap.Device.Protocol != "" && snap.Device.Protocol != proto {
		return -1
	}
	wanted := strings.Split(path, ".")
	for _, sd := range snap.Slots {
		for _, o := range sd.Objects {
			if matchPathSuffix(o.Path, wanted) {
				return o.ID
			}
		}
	}
	return -1
}

// matchPathSuffix reports whether wanted is a suffix of objPath
// (element-wise, case-sensitive). Empty wanted matches nothing.
func matchPathSuffix(objPath, wanted []string) bool {
	if len(wanted) == 0 || len(objPath) < len(wanted) {
		return false
	}
	off := len(objPath) - len(wanted)
	for i := range wanted {
		if objPath[off+i] != wanted[i] {
			return false
		}
	}
	return true
}

// resolveLabelFromCache tries to find an object ID from the disk cache
// for label-based addressing. Returns the ID if found, -1 otherwise.
// This avoids a full walk when the label is in the disk cache.
func resolveLabelFromCache(host, proto string, slot int, group, label string) int {
	if treeStore == nil || label == "" {
		return -1
	}
	snap, err := treeStore.Load(host, slot)
	if err != nil || snap == nil {
		return -1
	}
	// Verify protocol matches to avoid cross-protocol collisions
	// (e.g. ACP1 and Ember+ on same host).
	if snap.Device.Protocol != "" && snap.Device.Protocol != proto {
		return -1
	}
	for _, sd := range snap.Slots {
		for _, o := range sd.Objects {
			if o.Label == label {
				if group == "" || o.Group == group {
					return o.ID
				}
			}
		}
	}
	return -1
}

// withTimeout wraps ctx with the subcommand's --timeout.
func withTimeout(ctx context.Context, d time.Duration) (context.Context, context.CancelFunc) {
	return context.WithTimeout(ctx, d)
}

// popHost extracts the first non-flag argument as the host and returns
// the remainder for flag.Parse. Go's stdlib flag package stops parsing at
// the first non-flag token, so we separate the positional host argument
// from the flags manually. This lets users write the natural order:
//
//	acp walk 10.6.239.113 --slot 0
//
// instead of being forced to put flags before positional args.
func popHost(args []string) (string, []string, error) {
	// Skip flags AND their values. A flag like "--capture FILE" means
	// the next arg is the flag's value, not the host. Flags that use
	// "=" syntax (--capture=FILE) are handled by the HasPrefix check.
	// Boolean flags (--verbose, --all, --dry-run, --active) have no
	// separate value arg.
	boolFlags := map[string]bool{
		"-verbose": true, "--verbose": true,
		"-all": true, "--all": true,
		"-dry-run": true, "--dry-run": true,
		"-active": true, "--active": true,
	}
	skipNext := false
	for i, a := range args {
		if skipNext {
			skipNext = false
			continue
		}
		if strings.HasPrefix(a, "-") {
			// If it's not a boolean flag AND doesn't use = syntax,
			// the next arg is the flag's value — skip it too.
			if !boolFlags[a] && !strings.Contains(a, "=") {
				skipNext = true
			}
			continue
		}
		rest := make([]string, 0, len(args)-1)
		rest = append(rest, args[:i]...)
		rest = append(rest, args[i+1:]...)
		return a, rest, nil
	}
	return "", nil, fmt.Errorf("host argument missing")
}
