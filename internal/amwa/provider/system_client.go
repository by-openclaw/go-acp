// Layer-3 -- the Node as an IS-09 CLIENT.
//
// Every other API on this Node is something it SERVES. This is the one
// it consumes: IS-09 §4 has a Node discover a System API at startup
// and take its global configuration from it -- the PTP domain to lock
// to, where to send syslog, and which Registries to prefer.
//
// That last item is why this is not optional garnish. A Node that
// ignores the System API picks its Registry from mDNS priority alone,
// so a plant that has deliberately pointed its devices at one Registry
// through IS-09 finds this Node quietly registered somewhere else. The
// AMWA IS-09-02 suite stands up a System API and checks the Node
// actually went and read it.
//
// Failure is NOT fatal. IS-09 §4 makes the System API optional, and a
// Node that refuses to start because no System API answered would be
// unusable on every network that does not run one -- which is most of
// them. The outcome is logged either way, because "we could not find
// one" and "we never looked" are different facts and only one of them
// is a configuration problem.

package provider

import (
	"context"
	"time"

	"dhs/internal/amwa/codec/is09"
	systemsession "dhs/internal/amwa/session/system"
)

// systemDiscoveryTimeout bounds the mDNS browse. IS-09 §3 gives no
// number; this one is long enough for a responder on a quiet link and
// short enough that a Node on a network with no System API is serving
// within a couple of seconds.
const systemDiscoveryTimeout = 2 * time.Second

// fetchSystemGlobal discovers a System API and reads its global
// resource, applying what it finds. Returns the Global, or nil when
// none was found.
func (s *IS04NodeServer) fetchSystemGlobal(ctx context.Context) *is09.Global {
	// The watch starts FIRST, before the one-shot lookup below.
	//
	// Order matters more than it looks. The one-shot lookup blocks for
	// a discovery timeout, and anything advertised during that window
	// is announced to nobody -- the watcher does not exist yet, and a
	// browse opened afterwards is only told about what happens NEXT.
	// Starting the watch first means the Node is listening from the
	// moment it serves, and the one-shot lookup becomes what it should
	// be: a fast path for a System API that is already there, not the
	// only path.
	s.watchForSystem(ctx)

	opts := systemsession.IS09FetchOptions{Logger: s.logger}

	// An explicitly configured System API beats discovery. An operator
	// who named one has already made the choice discovery exists to
	// make.
	if s.cfg.SystemURL != "" {
		opts.Direct = s.cfg.SystemURL
	} else {
		if s.cfg.DiscoveryMode == "static" {
			// Discovery is off by configuration, not by failure.
			// Saying so once is worth more than silence: it is the
			// answer to "why did this Node ignore the System API".
			s.logger.Info("provider/node: System API discovery skipped (static mode)",
				"plugin", "amwa", "api", "is-09")
			return nil
		}
		found, err := systemsession.DiscoverMDNS(ctx, systemDiscoveryTimeout, s.logger)
		if err != nil || len(found) == 0 {
			// Not an error, and not the end of it. The System API may
			// simply not be advertised yet -- the config server can be
			// restarted, or this Node can have booted first -- so a
			// watcher keeps listening and fetches when one appears.
			// IS-09 §4 expects the Node to re-resolve on change; a
			// Node that looked once never learns.
			s.logger.Info("provider/node: no System API yet, watching for one",
				"plugin", "amwa", "api", "is-09", "err", err)
			s.watchForSystem(ctx)
			return nil
		}
		opts.Discovered = found
	}

	res, err := systemsession.Fetch(ctx, opts)
	if err != nil || res == nil || res.Global == nil {
		s.logger.Warn("provider/node: System API found but /global could not be read",
			"plugin", "amwa", "api", "is-09", "err", err)
		s.watchForSystem(ctx)
		return nil
	}
	s.applySystemGlobal(res.Global, res.URL)
	// Keep watching even after a successful read: `pri` exists so an
	// operator can advertise a better System API and have devices move
	// to it without being restarted.
	s.watchForSystem(ctx)
	return res.Global
}

// watchForSystem starts the ongoing mDNS watch, once.
func (s *IS04NodeServer) watchForSystem(ctx context.Context) {
	if s.cfg.SystemURL != "" || s.cfg.DiscoveryMode == "static" {
		// An explicitly configured System API is not up for
		// rediscovery, and a Node with discovery switched off is not
		// browsing for anything.
		return
	}
	s.mu.Lock()
	if s.systemWatcher != nil {
		s.mu.Unlock()
		return
	}
	w, err := NewSystemWatcher(s.logger, is09WireVersion, func(g any, url string) {
		global, ok := g.(*is09.Global)
		if !ok {
			return
		}
		s.applySystemGlobal(global, url)
	})
	if err != nil {
		s.mu.Unlock()
		s.logger.Warn("provider/node: cannot watch for a System API",
			"plugin", "amwa", "api", "is-09", "err", err)
		return
	}
	s.systemWatcher = w
	s.mu.Unlock()

	if err := w.Run(ctx); err != nil {
		s.logger.Warn("provider/node: System API watch failed to start",
			"plugin", "amwa", "api", "is-09", "err", err)
	}
}

// is09WireVersion is the IS-09 minor the Node speaks. One published
// minor today; a constant beats a lookup that can only ever return it.
const is09WireVersion = "v1.0"

// applySystemGlobal records what the System API told this Node.
//
// Recording rather than enforcing, deliberately. The PTP domain and
// syslog host are facts about the plant that belong to whatever
// subsystem uses them, and a Node that silently reconfigured its clock
// from a resource it fetched over unauthenticated HTTP would be a
// worse citizen than one that reports what it was told. The Registry
// hint is the part this Node acts on, and only when the operator has
// not already named a Registry.
func (s *IS04NodeServer) applySystemGlobal(g *is09.Global, url string) {
	s.mu.Lock()
	s.systemGlobal = g
	s.mu.Unlock()

	s.logger.Info("provider/node: System API global applied",
		"plugin", "amwa", "api", "is-09",
		"url", url,
		"id", g.ID,
		"version", g.Version)
}

// SystemGlobal returns the last global resource read from a System API,
// or nil if none was.
func (s *IS04NodeServer) SystemGlobal() *is09.Global {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.systemGlobal
}
