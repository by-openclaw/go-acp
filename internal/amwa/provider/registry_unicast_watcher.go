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
	"dhs/internal/plugin"
)

// unicastReresolveInterval is how often the DNS zone is re-asked.
// DNS-SD gives no push channel, so this is the staleness bound on
// registry changes reaching the Node. A var only so a test can drive
// the re-ask without sitting out a minute; production never
// reassigns it.
var unicastReresolveInterval = 60 * time.Second

// While the zone has named nothing usable the question is re-asked on
// a short backoff instead: a Node with no Registry is doing nothing
// else, and one that asks once a minute joins a plant up to a minute
// after its record is published (the AMWA suite, in unicast mode,
// gives a Node 30 s to ask). 1 s growing by half to 30 s is nmos-cpp's
// discovery backoff; IS-04 names no figure. Vars for the same reason
// the interval is one.
var (
	unicastBackoffMin = 1 * time.Second
	unicastBackoffMax = 30 * time.Second
)

const unicastBackoffFactor = 1.5

// runUnicastLoop asks at once — a Node must not sit an interval before
// its first attempt — then again after a delay the answer chooses: the
// re-resolve interval once something is known, the growing backoff
// while nothing is. Returns when ctx is cancelled.
func runUnicastLoop(ctx context.Context, ask func(context.Context) (known bool)) {
	backoff := unicastBackoffMin
	for {
		var delay time.Duration
		delay, backoff = nextUnicastDelay(ask(ctx), backoff)
		t := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			t.Stop()
			return
		case <-t.C:
		}
	}
}

// nextUnicastDelay is the loop's schedule as a function: how long to
// wait before the next question, and the backoff to carry into the one
// after. Something known waits the interval and resets the backoff;
// nothing known waits the backoff and grows it to its ceiling.
func nextUnicastDelay(known bool, backoff time.Duration) (delay, next time.Duration) {
	if known {
		return unicastReresolveInterval, unicastBackoffMin
	}
	next = time.Duration(float64(backoff) * unicastBackoffFactor)
	if next > unicastBackoffMax {
		next = unicastBackoffMax
	}
	return backoff, next
}

// unicastDisqualifyTTL matches the mDNS watcher's failover penalty —
// the two discovery modes must yield the same failover behaviour.
const unicastDisqualifyTTL = 30 * time.Second

// resolveUnicast is the DNS-SD lookup this watcher runs, behind a package
// var: a unicast lookup needs an authoritative DNS server, which a unit
// test has no business standing up, so a test scripts the zone's answers
// here instead. Production never reassigns it.
var resolveUnicast = dnssdsession.ResolveUnicast

// UnicastRegistryWatcher resolves `_nmos-register._tcp.<domain>` (and
// the pre-v1.2 legacy name) against one DNS resolver on an interval.
type UnicastRegistryWatcher struct {
	logger        *slog.Logger
	resolver      string // host[:port] of the DNS server
	domain        string // search domain the SRV records live under
	preferAPIVer  string
	disqualifyTTL time.Duration

	cancel context.CancelFunc
	// done is closed when the resolve loop has exited, so Close can
	// say the loop has STOPPED rather than only that it has been asked
	// to. Without the join a caller — or a test asserting no further
	// lookups — races an in-flight resolve that is on its way out.
	done chan struct{}

	mu           sync.Mutex
	byFull       map[string]RegistryCandidate
	disqualified map[string]time.Time
}

// NewUnicastRegistryWatcher builds the watcher. It does not resolve.
func NewUnicastRegistryWatcher(logger *slog.Logger, resolver, domain, preferAPIVer string) *UnicastRegistryWatcher {
	logger = plugin.LoggerOrDefault(logger)
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

// Run resolves once immediately, then re-resolves — on the interval
// while a Registry is known, on the backoff while none is — until ctx
// is cancelled. Returns immediately.
func (w *UnicastRegistryWatcher) Run(ctx context.Context) {
	loopCtx, cancel := context.WithCancel(ctx)
	w.cancel = cancel
	done := make(chan struct{})
	w.done = done
	go func() {
		defer close(done)
		runUnicastLoop(loopCtx, func(ctx context.Context) bool {
			w.resolveOnce(ctx)
			w.mu.Lock()
			defer w.mu.Unlock()
			return len(w.byFull) > 0
		})
	}()
}

// Close stops the resolve loop and waits for it. Idempotent, and safe
// on a watcher that was never Run.
//
// It waits because "stopped" and "asked to stop" are different facts
// to the caller: a Node tearing down still has a goroutine resolving
// against a zone if Close only cancelled.
func (w *UnicastRegistryWatcher) Close() error {
	if w.cancel != nil {
		w.cancel()
		w.cancel = nil
	}
	if w.done != nil {
		<-w.done
		w.done = nil
	}
	return nil
}

// resolveOnce asks the zone for BOTH registration service names — the
// same both-names rule the mDNS watcher follows, because a v1.0/v1.1
// Registry is published under the legacy name only and a zone during a
// v1.2 transition may carry both.
func (w *UnicastRegistryWatcher) resolveOnce(ctx context.Context) {
	for _, service := range []string{dnssdcodec.ServiceRegister, dnssdcodec.ServiceRegisterLegacy} {
		instances, err := resolveUnicast(ctx, w.resolver, service, w.domain, 0)
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
