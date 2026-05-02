package dnssd

import (
	"log/slog"
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
	if logger == nil {
		logger = slog.Default()
	}
	if br, ok := tryDaemonBrowser(logger); ok {
		return br, nil
	}
	logger.Info("dnssd: using stdlib browser (no system daemon detected)")
	return newStdlibBrowser(logger)
}

// NewResponder returns the best [Responder] implementation for the host
// using the same selection rule as [NewBrowser].
func NewResponder(logger *slog.Logger) (Responder, error) {
	if logger == nil {
		logger = slog.Default()
	}
	if rs, ok := tryDaemonResponder(logger); ok {
		return rs, nil
	}
	logger.Info("dnssd: using stdlib responder (no system daemon detected)")
	return newStdlibResponder(logger)
}
