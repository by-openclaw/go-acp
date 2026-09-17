package provider

import (
	"context"
	"fmt"
	"log/slog"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	dnssdcodec "dhs/internal/amwa/codec/dnssd"
	dnssdsession "dhs/internal/amwa/session/dnssd"
)

// RegistryCandidate is one Registry instance discovered via DNS-SD,
// resolved into the URL the Node should POST against.
type RegistryCandidate struct {
	FullName string // "<instance>.<service>.<domain>"
	URL      string // e.g. "http://reg-1.local:8235"
	Priority int    // pri TXT — lower is higher priority (0-99 prod, 100+ dev)
	APIVer   string // best-matching api_ver TXT (we pick the highest mutual)
	APIProto string // "http" | "https"
	APIAuth  bool   // api_auth TXT
}

// RegistryWatcher browses BOTH `_nmos-register._tcp` (IS-04 v1.2+) and
// `_nmos-registration._tcp` (IS-04 v1.0 / v1.1 legacy name) on the
// link, keeps the set of currently-advertised Registry candidates, and
// exposes the best (highest-priority, lowest pri integer) one to
// callers.
//
// IS-04 v1.2 renamed the service; v1.0 and v1.1 Registries advertise on
// the legacy name only. A watcher that browsed only the modern name
// would miss every legacy Registry — and the AMWA NMOS Testing tool
// runs its mock Registries on the legacy name when the suite is
// invoked at `version=v1.0` or `v1.1`. See
// root CLAUDE.md "AMWA NMOS strict" and tests/integration/nmos/amwa/
// NOTES.md for the conformance evidence.
//
// Compliance with IS-04 v1.3.3 §3.1:
//   - watch shared records (PTR) on the service type;
//   - SRV.Target + Port becomes the host:port;
//   - TXT.api_proto chooses http/https;
//   - TXT.api_ver lists the wire versions advertised — we pick the
//     highest mutual with our preferred apiVer;
//   - TXT.pri orders the list (lower = higher).
//
// Failover: when the consumer signals the current pick has failed
// (Disqualify), the watcher returns the next-best on the next Best()
// call. Disqualified entries time out after disqualifyTTL.
type RegistryWatcher struct {
	logger        *slog.Logger
	preferAPIVer  string
	disqualifyTTL time.Duration

	// One shared Browser that fans every received mDNS Instance out to
	// every active subscription via Browse() — see Browser.Browse doc.
	// Two independent Browsers each binding their own sockets to
	// 224.0.0.251:5353 was tried first but breaks on Docker bridge
	// networks where the second multicast bind doesn't see the same
	// stream of packets the first one sees.
	browser   dnssdsession.Browser
	cancel    context.CancelFunc
	outModern <-chan dnssdcodec.Instance
	outLegacy <-chan dnssdcodec.Instance

	mu           sync.Mutex
	byFull       map[string]RegistryCandidate
	disqualified map[string]time.Time // FullName → expiry
	// hostIPv4 caches A records seen on the link, keyed by SRV target
	// hostname. Many mocks share one hostname; per-packet pairing
	// inside dnssdcodec.DecodeInstances only finds the A in the same
	// packet, so we aggregate across packets here. When a candidate
	// has no IPv4 in its own packet but we've seen the host's A
	// elsewhere, we substitute it into the URL.
	hostIPv4 map[string]string
}

// NewRegistryWatcher opens an mDNS browser for `_nmos-register._tcp`.
// preferAPIVer (e.g. "v1.3") is used as the highest-mutual selection
// preference when a Registry advertises multiple comma-separated
// versions in TXT.api_ver.
func NewRegistryWatcher(logger *slog.Logger, preferAPIVer string) (*RegistryWatcher, error) {
	if logger == nil {
		logger = slog.Default()
	}
	if preferAPIVer == "" {
		preferAPIVer = "v1.3"
	}
	br, err := dnssdsession.NewBrowser(logger)
	if err != nil {
		return nil, fmt.Errorf("provider/node: open mDNS browser: %w", err)
	}
	return &RegistryWatcher{
		logger:        logger,
		preferAPIVer:  preferAPIVer,
		disqualifyTTL: 30 * time.Second,
		browser:       br,
		byFull:        map[string]RegistryCandidate{},
		disqualified:  map[string]time.Time{},
		hostIPv4:      map[string]string{},
	}, nil
}

// Run starts the browse loop on BOTH the modern and legacy NMOS
// registration service names — Browser.Browse fans packets out to
// every active subscription, so two Browse calls share one read loop
// without racing. Returns immediately; cancel ctx to stop. Safe to
// call only once.
func (w *RegistryWatcher) Run(ctx context.Context) error {
	loopCtx, cancel := context.WithCancel(ctx)
	w.cancel = cancel
	outModern, err := w.browser.Browse(loopCtx, dnssdcodec.ServiceRegister)
	if err != nil {
		cancel()
		return err
	}
	outLegacy, err := w.browser.Browse(loopCtx, dnssdcodec.ServiceRegisterLegacy)
	if err != nil {
		cancel()
		return err
	}
	w.outModern = outModern
	w.outLegacy = outLegacy
	go w.consume(loopCtx)
	return nil
}

// Close stops the browse loops and the underlying sockets. Idempotent.
func (w *RegistryWatcher) Close() error {
	if w.cancel != nil {
		w.cancel()
		w.cancel = nil
	}
	if w.browser != nil {
		return w.browser.Close()
	}
	return nil
}

// Best returns the current best Registry candidate, ok=false if none
// known (or every one has been disqualified within disqualifyTTL).
//
// Dedupes by URL: the same Registry can appear twice in byFull when it
// advertises on both _nmos-register._tcp (modern) AND
// _nmos-registration._tcp (legacy) — that's spec-conformant during a
// v1.2 transition and matches Cerebrum's live behaviour. Without
// dedupe, Best() returns whichever name the map's random iteration
// happens to pick, and shouldSwitchToBetter() flap-switches every
// tick. Dedupe groups entries by URL, picks the highest-priority
// FullName per group, with ties broken alphabetically (which prefers
// the modern service name `_nmos-register` over `_nmos-registration`).
func (w *RegistryWatcher) Best() (RegistryCandidate, bool) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.gcDisqualifiedLocked()
	return bestCandidate(w.byFull, w.disqualified)
}

// bestCandidate is the ONE selection rule, shared by the mDNS and the
// unicast DNS-SD watchers — two selection implementations would drift,
// and a Node whose failover order depends on which discovery mode fed
// it is a Node no operator can reason about.
func bestCandidate(byFull map[string]RegistryCandidate, disqualified map[string]time.Time) (RegistryCandidate, bool) {
	// First pass: collect non-disqualified entries grouped by URL.
	byURL := make(map[string]RegistryCandidate, len(byFull))
	for full, c := range byFull {
		if _, dq := disqualified[full]; dq {
			continue
		}
		// Drop empty-URL entries — pre-resolution stubs that can't
		// be POSTed against anyway. Best() returning one would make
		// pickBase produce an invalid base URL.
		if c.URL == "" {
			continue
		}
		prev, seen := byURL[c.URL]
		if !seen {
			byURL[c.URL] = c
			continue
		}
		// Keep the higher-priority (lower number) entry; if equal,
		// keep the alphabetically earlier FullName for determinism.
		if c.Priority < prev.Priority ||
			(c.Priority == prev.Priority && c.FullName < prev.FullName) {
			byURL[c.URL] = c
		}
	}
	if len(byURL) == 0 {
		return RegistryCandidate{}, false
	}
	cands := make([]RegistryCandidate, 0, len(byURL))
	for _, c := range byURL {
		cands = append(cands, c)
	}
	sort.SliceStable(cands, func(i, j int) bool {
		if cands[i].Priority != cands[j].Priority {
			return cands[i].Priority < cands[j].Priority
		}
		return cands[i].FullName < cands[j].FullName
	})
	return cands[0], true
}

// Disqualify marks a Registry as failed for the next disqualifyTTL.
// Best() will skip it during the window — the consumer's next call
// returns the next-best one.
func (w *RegistryWatcher) Disqualify(fullName string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.disqualified[fullName] = time.Now().Add(w.disqualifyTTL)
}

// All returns every currently-known candidate (disqualified included)
// for diagnostics.
func (w *RegistryWatcher) All() []RegistryCandidate {
	w.mu.Lock()
	defer w.mu.Unlock()
	out := make([]RegistryCandidate, 0, len(w.byFull))
	for _, c := range w.byFull {
		out = append(out, c)
	}
	sort.SliceStable(out, func(i, j int) bool {
		return out[i].Priority < out[j].Priority
	})
	return out
}

// consume reads from BOTH browser channels (modern + legacy service
// names) and folds new Instances into byFull. Older entries refresh
// their TXT/Priority when re-advertised. A Registry that advertises on
// both names — possible during a v1.2 transition window — is
// deduplicated by FullName since the SRV target+port stays identical.
//
// The watcher also caches every A record observed (keyed by hostname)
// so that mocks sharing one SRV target — but advertising in separate
// packets — can still resolve. Rewrite candidate URLs every cycle so a
// late-arriving A record fixes earlier hostname-only entries too.
func (w *RegistryWatcher) consume(ctx context.Context) {
	for {
		var ins dnssdcodec.Instance
		var ok bool
		select {
		case <-ctx.Done():
			return
		case ins, ok = <-w.outModern:
			if !ok {
				w.outModern = nil
			}
		case ins, ok = <-w.outLegacy:
			if !ok {
				w.outLegacy = nil
			}
		}
		// If both channels are closed, exit.
		if w.outModern == nil && w.outLegacy == nil {
			return
		}
		if !ok {
			// Only one channel closed; keep looping on the other.
			continue
		}
		{
			cand, ok := candidateFromInstance(ins, w.preferAPIVer)
			if !ok {
				if w.logger != nil {
					w.logger.Info("provider/node: registry rejected",
						"name", ins.FullName(),
						"host", ins.Host, "port", ins.Port,
						"api_ver_advert", ins.TXT[dnssdcodec.TXTKeyAPIVer],
						"api_ver_prefer", w.preferAPIVer,
						"api_proto", ins.TXT[dnssdcodec.TXTKeyAPIProto])
				}
				continue
			}
			w.mu.Lock()
			// Record any A record we just saw, keyed by SRV target.
			if len(ins.IPv4) > 0 {
				host := strings.TrimSuffix(ins.Host, ".")
				w.hostIPv4[host] = ins.IPv4[0].String()
			}
			// If the candidate URL is hostname-only, substitute the
			// cached IP for that host (now or earlier).
			cand.URL = w.rewriteURLLocked(cand.URL)
			w.byFull[cand.FullName] = cand
			// Re-walk every existing candidate so an A record arriving
			// AFTER its SRV gets applied retroactively.
			for full, c := range w.byFull {
				newURL := w.rewriteURLLocked(c.URL)
				if newURL != c.URL {
					c.URL = newURL
					w.byFull[full] = c
				}
			}
			// A re-announcement removes any stale disqualification;
			// the operator may have just restored the Registry.
			delete(w.disqualified, cand.FullName)
			w.mu.Unlock()
			if w.logger != nil {
				w.logger.Info("provider/node: registry discovered",
					"name", cand.FullName, "url", cand.URL,
					"pri", cand.Priority, "api_ver", cand.APIVer)
			}
		}
	}
}

// rewriteURLLocked rewrites a hostname-only URL to use a cached IPv4
// when one is known for that hostname. Caller must hold w.mu.
func (w *RegistryWatcher) rewriteURLLocked(url string) string {
	scheme := ""
	rest := url
	if i := strings.Index(rest, "://"); i >= 0 {
		scheme = rest[:i]
		rest = rest[i+3:]
	}
	hostport := rest
	path := ""
	if j := strings.IndexAny(rest, "/?"); j >= 0 {
		hostport = rest[:j]
		path = rest[j:]
	}
	host := hostport
	port := ""
	if k := strings.LastIndex(hostport, ":"); k >= 0 {
		host = hostport[:k]
		port = hostport[k:]
	}
	// Only rewrite hostname-style hosts (skip if already an IP).
	if isIPv4Literal(host) {
		return url
	}
	ip, ok := w.hostIPv4[host]
	if !ok {
		return url
	}
	return scheme + "://" + ip + port + path
}

func isIPv4Literal(s string) bool {
	parts := strings.Split(s, ".")
	if len(parts) != 4 {
		return false
	}
	for _, p := range parts {
		if p == "" {
			return false
		}
		for _, c := range p {
			if c < '0' || c > '9' {
				return false
			}
		}
	}
	return true
}

func (w *RegistryWatcher) gcDisqualifiedLocked() {
	now := time.Now()
	for k, exp := range w.disqualified {
		if now.After(exp) {
			delete(w.disqualified, k)
		}
	}
}

// candidateFromInstance projects a dnssd.Instance into a
// RegistryCandidate, choosing the highest-mutual api_ver and decoding
// the pri TXT integer. Returns ok=false on records that lack the
// required fields.
//
// Resolution preference: when the same mDNS response carried an A
// record alongside SRV (the IPv4 the SRV target points to on this
// link), use that IP in the URL instead of the SRV.target hostname.
// SRV targets are typically `*.local` names that only mDNS can
// resolve — Docker's stub resolver (127.0.0.11) returns NXDOMAIN for
// them, so a hostname URL would never connect. The A record is
// already in our hand from the same packet.
func candidateFromInstance(ins dnssdcodec.Instance, preferAPIVer string) (RegistryCandidate, bool) {
	if ins.Host == "" || ins.Port == 0 {
		return RegistryCandidate{}, false
	}
	proto := "http"
	if v, ok := ins.TXT[dnssdcodec.TXTKeyAPIProto]; ok && v != "" {
		proto = strings.ToLower(strings.TrimSpace(v))
	}
	auth := false
	if v, ok := ins.TXT[dnssdcodec.TXTKeyAPIAuth]; ok {
		auth = strings.EqualFold(strings.TrimSpace(v), "true")
	}
	// IS-04 v1.3.3 §3.1 — `pri` 0-99 is production, 100+ development.
	// Missing or unparseable `pri` is NOT "highest priority"; treat it
	// as the dev floor so a misconfigured / left-over mock can't
	// outrank an explicitly-prioritised production Registry.
	pri := 100
	if v, ok := ins.TXT[dnssdcodec.TXTKeyPriority]; ok {
		if p, err := strconv.Atoi(strings.TrimSpace(v)); err == nil {
			pri = p
		}
	}
	apiVer := pickAPIVer(ins.TXT[dnssdcodec.TXTKeyAPIVer], preferAPIVer)
	if apiVer == "" {
		// Registry doesn't advertise a version we speak — reject.
		return RegistryCandidate{}, false
	}
	if !isSupportedProto(proto) {
		// Registry advertises https / unknown — we don't speak it yet.
		return RegistryCandidate{}, false
	}
	hostForURL := strings.TrimSuffix(ins.Host, ".")
	if len(ins.IPv4) > 0 {
		hostForURL = ins.IPv4[0].String()
	}
	url := fmt.Sprintf("%s://%s:%d", proto, hostForURL, ins.Port)
	return RegistryCandidate{
		FullName: ins.FullName(),
		URL:      url,
		Priority: pri,
		APIVer:   apiVer,
		APIProto: proto,
		APIAuth:  auth,
	}, true
}

// pickAPIVer chooses the version we'll talk. IS-04 v1.3.3 §3.1
// requires the Node to use one of the api_ver values the Registry
// advertises — never to invent one. We prefer `preferred`; if the
// Registry doesn't advertise it we return "" so the caller can REJECT
// the Registry rather than fall back to a non-mutual version (AMWA
// test_01_01).
//
// Empty advert is treated as "Registry didn't say" — we trust the
// preferred version (legacy behaviour for codec call-sites).
func pickAPIVer(advert, preferred string) string {
	if advert == "" {
		return preferred
	}
	parts := make([]string, 0, 3)
	for _, p := range strings.Split(advert, ",") {
		p = strings.TrimSpace(p)
		if p != "" {
			parts = append(parts, p)
		}
	}
	if len(parts) == 0 {
		return preferred
	}
	for _, p := range parts {
		if p == preferred {
			return p
		}
	}
	return ""
}

// isSupportedProto returns true for protocols dhs can actually speak
// today. IS-04 v1.3 advertises only http (or https when IS-10 lands).
// Anything else (rogue / mistyped) is rejected so the Node won't
// register with it — AMWA test_01_01.
func isSupportedProto(proto string) bool {
	switch strings.ToLower(strings.TrimSpace(proto)) {
	case "http":
		return true
	case "https":
		return false // IS-10 not yet supported by dhs
	}
	return false
}
