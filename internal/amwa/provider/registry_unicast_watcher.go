// Layer-3 — Registry discovery over unicast DNS-SD (RFC 6763 §11).
//
// The mDNS watcher cannot exist on networks that block multicast, and
// IS-04 §3.1 names unicast DNS-SD as the discovery mode for exactly
// those plants: a conventional DNS server carries the PTR/SRV/TXT
// records under a search domain, and the Node asks it directly.
//
// The shape mirrors RegistryWatcher deliberately — same candidate
// type, same selection helper, same Disqualify contract — so the
// RegistrationClient cannot tell which discovery mode fed it, and
// IS-04's failover semantics (§6.1) hold identically in both.
//
// One difference is structural: mDNS is a running conversation, so the
// multicast watcher just listens; unicast DNS is question-and-answer,
// so this watcher re-asks on an interval. Re-asking is not optional —
// records change (a Registry moves, an operator re-prioritises), and a
// Node that resolved once at boot would follow yesterday's plant.

package provider

import (
	"context"
	"log/slog"
	"sync"
	"time"

	dnssdcodec "dhs/internal/amwa/codec/dnssd"
	dnssdsession "dhs/internal/amwa/session/dnssd"
)

// unicastReresolveInterval is how often the DNS zone is re-asked.
// DNS-SD gives no push channel, so this is the staleness bound on
// registry changes reaching the Node.
const unicastReresolveInterval = 60 * time.Second

// unicastDisqualifyTTL matches the mDNS watcher's failover penalty —
// the two discovery modes must yield the same failover behaviour.
const unicastDisqualifyTTL = 30 * time.Second

// UnicastRegistryWatcher resolves `_nmos-register._tcp.<domain>` (and
// the pre-v1.2 legacy name) against one DNS resolver on an interval.
type UnicastRegistryWatcher struct {
	logger        *slog.Logger
	resolver      string // host[:port] of the DNS server
	domain        string // search domain the SRV records live under
	preferAPIVer  string
	disqualifyTTL time.Duration

	cancel context.CancelFunc

	mu           sync.Mutex
	byFull       map[string]RegistryCandidate
	disqualified map[string]time.Time
}

// NewUnicastRegistryWatcher builds the watcher. It does not resolve.
func NewUnicastRegistryWatcher(logger *slog.Logger, resolver, domain, preferAPIVer string) *UnicastRegistryWatcher {
	if logger == nil {
		logger = slog.Default()
	}
	if preferAPIVer == "" {
		preferAPIVer = "v1.3"
	}
	return &UnicastRegistryWatcher{
		logger:        logger,
		resolver:      resolver,
		domain:        domain,
		preferAPIVer:  preferAPIVer,
		disqualifyTTL: unicastDisqualifyTTL,
		byFull:        map[string]RegistryCandidate{},
		disqualified:  map[string]time.Time{},
	}
}

// Run resolves once immediately — a Node must not sit a full interval
// before its first registration attempt — then re-resolves on the
// interval until ctx is cancelled. Returns immediately.
func (w *UnicastRegistryWatcher) Run(ctx context.Context) error {
	loopCtx, cancel := context.WithCancel(ctx)
	w.cancel = cancel
	go func() {
		w.resolveOnce(loopCtx)
		t := time.NewTicker(unicastReresolveInterval)
		defer t.Stop()
		for {
			select {
			case <-loopCtx.Done():
				return
			case <-t.C:
				w.resolveOnce(loopCtx)
			}
		}
	}()
	return nil
}

// Close stops the resolve loop. Idempotent.
func (w *UnicastRegistryWatcher) Close() error {
	if w.cancel != nil {
		w.cancel()
		w.cancel = nil
	}
	return nil
}

// resolveOnce asks the zone for BOTH registration service names — the
// same both-names rule the mDNS watcher follows, because a v1.0/v1.1
// Registry is published under the legacy name only and a zone during a
// v1.2 transition may carry both.
func (w *UnicastRegistryWatcher) resolveOnce(ctx context.Context) {
	for _, service := range []string{dnssdcodec.ServiceRegister, dnssdcodec.ServiceRegisterLegacy} {
		instances, err := dnssdsession.ResolveUnicast(ctx, w.resolver, service, w.domain, 0)
		if err != nil {
			// One name failing must not hide the other: a zone with no
			// legacy records answers NXDOMAIN, which is normal, not an
			// outage.
			w.logger.Debug("provider/node: unicast DNS-SD resolve failed",
				"plugin", "amwa", "api", "is-04",
				"service", service, "domain", w.domain, "resolver", w.resolver, "err", err)
			continue
		}
		for _, ins := range instances {
			cand, ok := candidateFromInstance(ins, w.preferAPIVer)
			if !ok {
				w.logger.Info("provider/node: unicast registry rejected",
					"plugin", "amwa", "api", "is-04",
					"name", ins.FullName(),
					"api_ver_advert", ins.TXT[dnssdcodec.TXTKeyAPIVer],
					"api_ver_prefer", w.preferAPIVer)
				continue
			}
			w.mu.Lock()
			_, known := w.byFull[cand.FullName]
			w.byFull[cand.FullName] = cand
			// A record re-appearing in the zone clears its penalty —
			// the operator may have just fixed the Registry, same rule
			// as an mDNS re-announcement.
			delete(w.disqualified, cand.FullName)
			w.mu.Unlock()
			if !known {
				w.logger.Info("provider/node: registry discovered (unicast DNS-SD)",
					"plugin", "amwa", "api", "is-04",
					"name", cand.FullName, "url", cand.URL,
					"pri", cand.Priority, "api_ver", cand.APIVer)
			}
		}
	}
}

// Best returns the current best candidate under the same dedupe +
// priority rules as the mDNS watcher — shared helper, one selection
// truth.
func (w *UnicastRegistryWatcher) Best() (RegistryCandidate, bool) {
	w.mu.Lock()
	defer w.mu.Unlock()
	now := time.Now()
	for k, exp := range w.disqualified {
		if now.After(exp) {
			delete(w.disqualified, k)
		}
	}
	return bestCandidate(w.byFull, w.disqualified)
}

// Disqualify marks a Registry as failed for disqualifyTTL, exactly as
// the mDNS watcher does — the client's failover path is shared.
func (w *UnicastRegistryWatcher) Disqualify(fullName string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.disqualified[fullName] = time.Now().Add(w.disqualifyTTL)
}
