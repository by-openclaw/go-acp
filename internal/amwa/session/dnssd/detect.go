package dnssd

import (
	"dhs/internal/plugin"
	"log/slog"
)

// tryDaemonBrowserFn / tryDaemonResponderFn indirect the build-tagged daemon
// probes through package vars so a test can drive the daemon-success return —
// otherwise dead code on any build whose probe is the no-daemon stub
// (everything but Linux-with-avahi). Same testability seam pattern the rest of
// the tree uses; the runtime default is the real probe.
var (
	tryDaemonBrowserFn   = tryDaemonBrowser
	tryDaemonResponderFn = tryDaemonResponder
)

// NewBrowser returns the best [Browser] implementation for the host:
//
//   - Linux with avahi-daemon reachable on the system DBus → [avahiBrowser]
//   - macOS / Windows with the Bonjour service available → [bonjourBrowser]
//     (planned; falls through to stdlib until that lands)
//   - everywhere else → [stdlibBrowser]
//
// The selection happens at process start and is logged so operators
// can confirm which path is active. The [Browser] interface is the
// only contract callers depend on — backend selection is opaque.
//
// On any error from the daemon path, this falls back to stdlib rather
// than failing the whole producer; a Node that ships without a system
// daemon should still register against an mDNS Registry, just without
// the sub-millisecond cascade-timing precision a daemon would provide.
func NewBrowser(logger *slog.Logger) (Browser, error) {
	logger = plugin.LoggerOrDefault(logger)
	if br, ok := tryDaemonBrowserFn(logger); ok {
		return br, nil
	}
	logger.Info("dnssd: using stdlib browser (no system daemon detected)")
	return newStdlibBrowser(logger)
}

// NewResponder returns the best [Responder] implementation for the host
// using the same selection rule as [NewBrowser].
func NewResponder(logger *slog.Logger) (Responder, error) {
	logger = plugin.LoggerOrDefault(logger)
	if rs, ok := tryDaemonResponderFn(logger); ok {
		return rs, nil
	}
	logger.Info("dnssd: using stdlib responder (no system daemon detected)")
	return newStdlibResponder(logger)
}
