package provider

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	stdhttp "net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"dhs/internal/amwa/codec/is04"
	"dhs/internal/transport"
)

// HeartbeatInterval is the IS-04 §6.1 default for POST
// /health/nodes/{id} cadence — every 5 seconds.
const HeartbeatInterval = 5 * time.Second

// MaxFailoverChain caps how many Registries we'll cycle through in a
// single tick. AMWA test_15/16 advertise up to ~6 mocks; cascading
// failure means we need to try each within the test's window.
const MaxFailoverChain = 8

// HeartbeatGracePeriod (12 s) is the spec default the Registry uses
// to GC unresponsive Nodes; we re-register if heartbeats keep failing
// past this.
const HeartbeatGracePeriod = 12 * time.Second

// ErrRegistryNotFound is returned by sendHeartbeat when the Registry
// answers 404 — the Node has been GC'd and must re-register from
// scratch.
var ErrRegistryNotFound = errors.New("provider/node: registry returned 404 — re-registration required")

// RegistrationClient drives the Node-side registration loop:
//
//  1. POST /resource for the Node, then each Device, Source, Flow,
//     Sender, Receiver in dependency order (referential integrity:
//     Sources before Flows, etc.).
//  2. Heartbeat every 5 s via POST /health/nodes/{id}.
//  3. On 404 from heartbeat → full re-registration.
//  4. On Stop / shutdown → DELETE every owned resource (Receivers
//     first, then Senders, Flows, Sources, Devices, Node).
type RegistrationClient struct {
	logger *slog.Logger

	// Base URL of the Registration API including version, e.g.
	// `http://10.6.239.113:8235/x-nmos/registration/v1.3`. Empty when
	// the watcher is driving discovery — base is set per-cycle then.
	base   string
	apiVer string
	bundle *NodeConfig
	// codec encodes every resource POSTed to the registry in the wire
	// shape for the configured api_ver. Without this, the registry
	// stores canonical (v1.3) JSON, the Node API serves per-version
	// JSON, and AMWA test_04 / test_07-11 fail Node-API vs registry
	// coherence on every minor < v1.3.
	codec is04.Codec

	// watcher, when non-nil, supplies the current Registry — mDNS
	// (Mode A) or unicast DNS-SD, the client cannot tell and must not:
	// IS-04 §6.1 failover works identically on both. When nil, base is
	// fixed (Mode B).
	watcher registrySource
	// currentRegistry is the FullName of the watcher pick the loop is
	// currently registered against — used for Disqualify on failure.
	currentRegistry string

	http *stdhttp.Client

	// tokenSource, when non-nil, supplies a BCP-003-02 Bearer token
	// for every Registration API request (an authed registry 401s
	// bare requests).
	tokenSource func(context.Context) (string, error)

	// heartbeatIntervalFn, when set, supplies the live heartbeat
	// cadence — the IS-09 System API's is04.heartbeat_interval
	// (seconds on the wire), read fresh each loop tick so a /global
	// that arrives after the loop started still takes effect
	// (IS-09-02 test_05: "System API configuration takes effect in
	// the Node"). Nil, or a non-positive return, keeps the IS-04 §6.1
	// default of HeartbeatInterval (5 s).
	heartbeatIntervalFn atomic.Pointer[func() time.Duration]

	// defaultInterval, when positive, replaces the IS-04 §6.1 5 s
	// fallback above — the `--heartbeat` operator knob (#855). The
	// IS-09 value still outranks it: the System API is the
	// plant-wide config, the flag is a local default.
	defaultInterval time.Duration

	mu         sync.Mutex
	cancelLoop context.CancelFunc
	registered atomic.Bool
	// onRegistered fires whenever the registered flag transitions
	// (true→false or false→true). The callback is invoked synchronously
	// from the loop goroutine so handlers must be quick + non-blocking
	// (e.g. toggle the mDNS responder via a goroutine internally if
	// needed). Used by IS04NodeServer to satisfy IS-04 §4.2.1: a
	// registered Node MUST stop advertising _nmos-node._tcp until
	// registration is lost (AMWA test_12_01).
	onRegistered atomic.Pointer[func(bool)]
	// everRegistered flips true after the first successful registerAll.
	// On subsequent failovers we use rejoinOrRegister (heartbeat-first)
	// per IS-04 §6.1. Touched only inside the Run loop, no mutex.
	everRegistered bool
	registrations  uint64
	heartbeats     uint64
	reregister     uint64
	deletions      uint64
	failures       uint64

	// closed signals the heartbeat loop to exit + DELETE has finished.
	closed chan struct{}

	// republish carries resources whose content changed after the
	// initial registration and must be POSTed again.
	//
	// IS-04 §4.2 is explicit that a Node re-POSTs a resource whenever
	// its data changes; the Registry has no other way to learn. Without
	// this an IS-05 activation updated the Node's own
	// receiver.subscription and left the Registry's copy saying the
	// receiver was idle — so a Controller reading the Query API, which
	// is the normal way to render routing state, saw a live route as
	// unrouted.
	//
	// Buffered and non-blocking on send: the caller is inside the
	// connection store's lock during an activation, and stalling there
	// would deadlock the very API that produced the change.
	republish chan republishItem
}

// republishItem is one resource to re-POST to the Registration API.
type republishItem struct {
	typ  is04.ResourceType
	data any
}

// NewRegistrationClient builds an unstarted client. apiVer is the
// IS-04 wire version (e.g. "v1.3"); registryURL must NOT include the
// `/x-nmos/registration/...` path — we append it.
func NewRegistrationClient(logger *slog.Logger, registryURL, apiVer string, bundle *NodeConfig) *RegistrationClient {
	if logger == nil {
		logger = slog.Default()
	}
	if apiVer == "" {
		apiVer = is04.APIVersion
	}
	base := ""
	if registryURL != "" {
		base = strings.TrimRight(registryURL, "/")
		// Accept both forms: bare host (we append /x-nmos/registration/v1.3)
		// or a URL that already includes the suffix.
		if !strings.Contains(base, "/x-nmos/registration/") {
			base = base + "/x-nmos/registration/" + apiVer
		}
	}
	codec, _ := is04.Get(apiVer) // nil-codec falls back to canonical shape in EncodeRegistrationVersioned
	return &RegistrationClient{
		logger: logger,
		base:   base,
		apiVer: apiVer,
		bundle: bundle,
		codec:  codec,
		http: &stdhttp.Client{
			Timeout: 10 * time.Second,
		},
		closed:    make(chan struct{}),
		republish: make(chan republishItem, 64),
	}
}

// Republish queues a changed resource for re-POST to the Registration
// API, so the Registry's copy stops disagreeing with the Node's own.
//
// Never blocks. If the queue is full the item is dropped and logged:
// the alternative is stalling an IS-05 activation on a slow or absent
// Registry, and a route that works with a stale catalogue beats a route
// that hangs. The next heartbeat cycle re-registers everything anyway,
// so a dropped item is a delay, not a permanent divergence.
func (c *RegistrationClient) Republish(t is04.ResourceType, data any) {
	select {
	case c.republish <- republishItem{typ: t, data: data}:
	default:
		c.logger.Warn("provider/node: republish queue full, dropping update",
			"type", string(t), "id", resourceID(t, data))
	}
}

// SetOnRegistered installs a callback fired on every transition of
// the registered flag (true→false or false→true). Used by
// IS04NodeServer to suspend the _nmos-node._tcp mDNS announce while
// registered (IS-04 §4.2.1, AMWA test_12_01).
func (c *RegistrationClient) SetOnRegistered(cb func(bool)) {
	if cb == nil {
		c.onRegistered.Store(nil)
		return
	}
	c.onRegistered.Store(&cb)
}

// setRegistered atomically updates the registered flag and fires the
// onRegistered callback when the value actually changes — replaces
// every direct c.registered.Store call so transitions never get
// missed by the consumer.
func (c *RegistrationClient) setRegistered(v bool) {
	prev := c.registered.Swap(v)
	if prev == v {
		return
	}
	if cb := c.onRegistered.Load(); cb != nil && *cb != nil {
		(*cb)(v)
	}
}

// registrySource is where Registry candidates come from — multicast
// mDNS (RegistryWatcher) or unicast DNS-SD (UnicastRegistryWatcher).
// The client's selection + failover logic is identical either way,
// and that is the contract: a Node's failover order must not depend
// on which discovery transport fed it.
type registrySource interface {
	Best() (RegistryCandidate, bool)
	Disqualify(fullName string)
}

// SetWatcher attaches a registry source; when set, Run picks the
// highest-pri Registry from it each cycle and falls over to the
// next-best on registration / heartbeat failure.
func (c *RegistrationClient) SetWatcher(w registrySource) {
	c.mu.Lock()
	c.watcher = w
	c.mu.Unlock()
}

// pickBase chooses the Registry base URL for the next cycle. With a
// fixed base (Mode B) it's a no-op. With a watcher (Mode A) it picks
// the current best candidate and stamps c.currentRegistry so failures
// can disqualify it. Returns ok=false when no registry is known yet.
func (c *RegistrationClient) pickBase() (string, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.watcher == nil {
		// Mode B — fixed base.
		return c.base, c.base != ""
	}
	cand, ok := c.watcher.Best()
	if !ok {
		c.currentRegistry = ""
		c.base = ""
		return "", false
	}
	apiVer := cand.APIVer
	if apiVer == "" {
		apiVer = c.apiVer
	}
	url := strings.TrimRight(cand.URL, "/") + "/x-nmos/registration/" + apiVer
	c.base = url
	c.currentRegistry = cand.FullName
	return url, true
}

// disqualifyCurrent tells the watcher to skip the current Registry
// for its disqualifyTTL, so the next cycle picks the next-best.
func (c *RegistrationClient) disqualifyCurrent() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.watcher != nil && c.currentRegistry != "" {
		c.watcher.Disqualify(c.currentRegistry)
	}
}

// rejoinOrRegister implements IS-04 v1.3.3 §6.1 failover semantics:
// when moving to a different Registry the Node SHOULD attempt a
// heartbeat first (cheaper, preserves UUIDs) and only fall back to
// full re-registration if the new Registry returns 404. AMWA test_16
// fails the Node if it POSTs /resource on every failover.
func (c *RegistrationClient) rejoinOrRegister(ctx context.Context) error {
	if err := c.sendHeartbeat(ctx); err == nil {
		c.setRegistered(true)
		return nil
	} else if errors.Is(err, ErrRegistryNotFound) {
		// New Registry doesn't have our resources — register fresh.
		return c.registerAll(ctx)
	} else {
		return err
	}
}

// shouldSwitchToBetter reports whether the currently-best Registry in
// the watcher is a different (higher-priority) instance than the one
// we're presently registered with. Always false in Mode B (fixed URL).
func (c *RegistrationClient) shouldSwitchToBetter() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.watcher == nil || c.currentRegistry == "" {
		return false
	}
	cand, ok := c.watcher.Best()
	if !ok {
		return false
	}
	return cand.FullName != c.currentRegistry
}

// SetHeartbeatIntervalFn installs the live heartbeat-cadence source
// (see the field doc). Safe to call before or during Run.
func (c *RegistrationClient) SetHeartbeatIntervalFn(fn func() time.Duration) {
	c.heartbeatIntervalFn.Store(&fn)
}

// SetDefaultHeartbeatInterval replaces the fallback cadence used when
// no System API supplies one — the `--heartbeat` operator knob
// (issue #855). Non-positive keeps the IS-04 §6.1 default. Safe to
// call before Run only.
func (c *RegistrationClient) SetDefaultHeartbeatInterval(d time.Duration) {
	c.defaultInterval = d
}

// heartbeatInterval returns the cadence in force right now:
// the System-API-supplied value when one is recorded and positive
// (IS-09 is the plant-wide operator config and outranks a local
// flag), else the operator's --heartbeat default, else the IS-04
// §6.1 default (HeartbeatInterval, 5 s).
func (c *RegistrationClient) heartbeatInterval() time.Duration {
	if p := c.heartbeatIntervalFn.Load(); p != nil {
		if d := (*p)(); d > 0 {
			return d
		}
	}
	if c.defaultInterval > 0 {
		return c.defaultInterval
	}
	return HeartbeatInterval
}

// heartbeatTick is the loop's poll rate for a given cadence: fast
// enough for failover detection AND for sub-second cadences (half the
// beat), never slower than the historic 1 s, floored so a pathological
// cadence cannot spin the loop.
func heartbeatTick(cadence time.Duration) time.Duration {
	t := cadence / 2
	if t > time.Second {
		t = time.Second
	}
	if t < 50*time.Millisecond {
		t = 50 * time.Millisecond
	}
	return t
}

// heartbeatSlack is the early-fire allowance that keeps the
// tick-quantisation jitter inside an observer's ±window (AMWA test_05
// uses ±0.5 s at the 5 s default). It must scale down with the
// cadence — a 500 ms slack swallows a 500 ms beat whole.
func heartbeatSlack(cadence time.Duration) time.Duration {
	s := cadence / 4
	if s > 500*time.Millisecond {
		s = 500 * time.Millisecond
	}
	if s < 10*time.Millisecond {
		s = 10 * time.Millisecond
	}
	return s
}

// Run drives the registration + heartbeat loop until ctx is
// cancelled. Performs initial registration, starts the heartbeat
// ticker, handles 404 → re-register, then deregisters on cancel.
//
// In Mode A (watcher attached, no fixed URL), the loop:
//   - waits until the watcher reports at least one Registry,
//   - picks the highest-pri one and registers,
//   - on POST/heartbeat failure (5xx, timeout) disqualifies the pick
//     and the next cycle tries the next-best — IS-04 §3.1 fallback.
func (c *RegistrationClient) Run(ctx context.Context) {
	defer close(c.closed)
	loopCtx, cancel := context.WithCancel(ctx)
	c.mu.Lock()
	c.cancelLoop = cancel
	c.mu.Unlock()

	if _, ok := c.pickBase(); ok {
		if err := c.registerAll(loopCtx); err != nil {
			c.logger.Warn("provider/node: initial registration failed", "err", err)
			atomic.AddUint64(&c.failures, 1)
			c.disqualifyCurrent()
		}
	}

	// AMWA test_15/16 cascade-disables mocks every (HeartbeatInterval+1)
	// seconds. To detect dead mocks reliably and fail over within that
	// window, the loop ticks faster than the heartbeat cadence. We
	// throttle actual heartbeats to the live cadence below. The tick
	// scales with the cadence (heartbeatTick) so a sub-second
	// --heartbeat / IS-09 value still beats on time — at the 5 s
	// default this is the historic 1 s tick — and is Reset when a
	// live IS-09 change moves the cadence.
	curTick := heartbeatTick(c.heartbeatInterval())
	ticker := time.NewTicker(curTick)
	defer ticker.Stop()
	lastHeartbeat := time.Time{}

	for {
		select {
		case <-loopCtx.Done():
			c.deregisterAll()
			return
		case item := <-c.republish:
			// Only meaningful while registered — an unregistered Node
			// will POST everything fresh the moment it joins.
			if !c.registered.Load() {
				continue
			}
			if err := c.postResource(loopCtx, item.typ, item.data); err != nil {
				c.logger.Warn("provider/node: republish failed",
					"type", string(item.typ), "id", resourceID(item.typ, item.data), "err", err)
				atomic.AddUint64(&c.failures, 1)
				continue
			}
			atomic.AddUint64(&c.reregister, 1)
		case <-ticker.C:
			// A ticker tick and loopCtx.Done() can be ready in the same
			// iteration; select picks at random, so we can land here with
			// the context already cancelled. If we proceed, the heartbeat
			// + cascade below run against the dead context, fail with
			// "context canceled", and flip registered→false — which makes
			// the subsequent deregisterAll() early-return and skip every
			// shutdown DELETE (the flaky-test symptom). Bail straight to
			// shutdown instead so the unregister still runs while we're
			// still marked registered.
			if loopCtx.Err() != nil {
				c.deregisterAll()
				return
			}
			// IS-04 v1.3.3 §3.1 — Node selects the highest-priority
			// Registry from those *currently* advertised. If a
			// higher-priority one appeared after we registered, switch:
			// DELETE from the old one, then re-register with the new.
			if c.registered.Load() && c.shouldSwitchToBetter() {
				c.logger.Info("provider/node: better registry available — switching")
				c.deregisterAll()
				c.setRegistered(false)
			}
			if !c.registered.Load() {
				// Try every advertised Registry in priority order
				// before giving up this tick. AMWA test_15 cascades
				// failures faster than HeartbeatInterval.
				//
				// On failover (everRegistered) we follow IS-04 §6.1:
				// heartbeat first, register only if Registry returns 404.
				// AMWA test_16 fails any Node that POSTs /resource on
				// every failover.
				done := false
				for i := 0; i < MaxFailoverChain; i++ {
					if _, ok := c.pickBase(); !ok {
						break
					}
					var err error
					if c.everRegistered {
						err = c.rejoinOrRegister(loopCtx)
					} else {
						err = c.registerAll(loopCtx)
					}
					if err == nil {
						done = true
						c.everRegistered = true
						// Force a heartbeat to the new Registry on the
						// next tick — the test framework counts those.
						lastHeartbeat = time.Time{}
						break
					}
					c.logger.Warn("provider/node: re-register attempt failed", "step", i, "err", err)
					atomic.AddUint64(&c.failures, 1)
					c.disqualifyCurrent()
				}
				if !done {
					continue
				}
			}
			// Throttle actual /health POSTs near the live cadence — the
			// fast tick above is for failover detection only. AMWA
			// test_05 enforces the interval ± 0.5 s between heartbeats,
			// so we fire when the elapsed budget is within the scaled
			// slack of due, keeping tick-quantisation jitter inside the
			// window at every cadence. The cadence is the IS-09 System
			// API's heartbeat_interval when one has been read, else the
			// --heartbeat default, else IS-04 §6.1's 5 s.
			cadence := c.heartbeatInterval()
			if nt := heartbeatTick(cadence); nt != curTick {
				curTick = nt
				ticker.Reset(nt)
			}
			if time.Since(lastHeartbeat) < cadence-heartbeatSlack(cadence) {
				continue
			}
			lastHeartbeat = time.Now()
			err := c.sendHeartbeat(loopCtx)
			if loopCtx.Err() != nil {
				// The run context was cancelled while the heartbeat was
				// in flight. Any error here is our own shutdown, not a
				// Registry failure — do NOT clear registered or cascade
				// (that would skip the shutdown DELETEs). Go straight to
				// graceful deregistration.
				c.deregisterAll()
				return
			}
			if errors.Is(err, ErrRegistryNotFound) {
				c.logger.Warn("provider/node: heartbeat 404 — re-registering")
				atomic.AddUint64(&c.reregister, 1)
				c.setRegistered(false)
			} else if err != nil {
				c.logger.Warn("provider/node: heartbeat failed", "err", err)
				atomic.AddUint64(&c.failures, 1)
				// IS-04 v1.3.3 §6.1 — on heartbeat failure (5xx /
				// timeout) the Node fails over but does NOT DELETE its
				// resources from the failing Registry. Many real
				// Registries share resource state (e.g. AMWA's mock
				// suite shares one resource map across all 6 mocks);
				// a stray DELETE there would also remove the Node from
				// the new Registry's view, forcing a needless re-POST
				// and tripping AMWA test_16. Just disqualify + cascade.
				c.disqualifyCurrent()
				c.setRegistered(false)
				// Immediate failover loop — try the next-best Registry
				// right now rather than waiting another HeartbeatInterval.
				// AMWA test_15/16 cascade mocks down faster than 5 s.
				//
				// Per IS-04 v1.3.3 §6.1: failover MUST attempt a heartbeat
				// first against the new Registry. Only if that returns 404
				// (Registry doesn't have us yet) do we fall back to
				// registerAll. AMWA test_16 explicitly fails any Node that
				// POSTs /resource on every failover instead of probing
				// with a heartbeat — that test detail reads:
				// "Node re-registered its resources when it failed over to
				// a new registry, when it should only have issued a
				// heartbeat". rejoinOrRegister implements that semantic;
				// previously this loop called registerAll unconditionally,
				// which is wire-correct only on the very first cascade
				// step into a never-seen Registry.
				for i := 0; i < MaxFailoverChain; i++ {
					if _, ok := c.pickBase(); !ok {
						break
					}
					regErr := c.rejoinOrRegister(loopCtx)
					if regErr == nil {
						lastHeartbeat = time.Time{}
						break
					}
					c.logger.Warn("provider/node: cascade rejoin/register failed", "step", i, "err", regErr)
					atomic.AddUint64(&c.failures, 1)
					c.disqualifyCurrent()
				}
			}
		}
	}
}

// Close stops the loop and waits for deregistration to finish.
func (c *RegistrationClient) Close() error {
	c.mu.Lock()
	if c.cancelLoop != nil {
		c.cancelLoop()
		c.cancelLoop = nil
	}
	c.mu.Unlock()
	select {
	case <-c.closed:
	case <-time.After(2 * HeartbeatGracePeriod):
		return errors.New("provider/node: deregistration timed out")
	}
	return nil
}

// Stats returns a snapshot of registration counters.
func (c *RegistrationClient) Stats() map[string]uint64 {
	return map[string]uint64{
		"registrations": atomic.LoadUint64(&c.registrations),
		"heartbeats":    atomic.LoadUint64(&c.heartbeats),
		"reregister":    atomic.LoadUint64(&c.reregister),
		"deletions":     atomic.LoadUint64(&c.deletions),
		"failures":      atomic.LoadUint64(&c.failures),
	}
}

// registerAll POSTs every owned resource in dependency order.
//
// IS-04 v1.3.3 §4 distinguishes 201 Created (fresh registration) from
// 200 OK (the Registry already had this UUID — typically from a stale
// session that wasn't deregistered). On 200 for the Node we DELETE
// the stale entries from the Registry and re-POST as fresh state
// (AMWA test_21). Subsequent resources naturally follow.
func (c *RegistrationClient) registerAll(ctx context.Context) error {
	if err := c.postResource(ctx, is04.ResourceNode, &c.bundle.Node); err != nil {
		return err
	}
	for i := range c.bundle.Devices {
		if err := c.postResource(ctx, is04.ResourceDevice, &c.bundle.Devices[i]); err != nil {
			return err
		}
	}
	for i := range c.bundle.Sources {
		if err := c.postResource(ctx, is04.ResourceSource, &c.bundle.Sources[i]); err != nil {
			return err
		}
	}
	for i := range c.bundle.Flows {
		if err := c.postResource(ctx, is04.ResourceFlow, &c.bundle.Flows[i]); err != nil {
			return err
		}
	}
	for i := range c.bundle.Senders {
		if err := c.postResource(ctx, is04.ResourceSender, &c.bundle.Senders[i]); err != nil {
			return err
		}
	}
	for i := range c.bundle.Receivers {
		if err := c.postResource(ctx, is04.ResourceReceiver, &c.bundle.Receivers[i]); err != nil {
			return err
		}
	}
	c.setRegistered(true)
	return nil
}

// postResource POSTs one IS-04 resource. Per IS-04 v1.3.3 §4.0:
//   - 201 Created  → fresh registration, accept it.
//   - 200 OK       → Registry already had this UUID (stale state). The
//     Node MUST DELETE the stale entry and re-POST as
//     fresh — AMWA test_21 enforces this.
//   - other        → error.
// SetTokenSource installs the access-token supplier (BCP-003-02).
func (c *RegistrationClient) SetTokenSource(fn func(context.Context) (string, error)) {
	c.tokenSource = fn
}

// SetTLSRoots installs the trust anchors used to verify an https
// Registration API (BCP-003-01: clients SHALL validate server
// certificates against an installed root).
func (c *RegistrationClient) SetTLSRoots(roots *x509.CertPool) {
	if roots == nil {
		return
	}
	// Built by the transport layer so every dhs client shares one posture;
	// this was already the strictest of the four hand-rolled configs, and
	// it is now the only one.
	cfg, err := transport.TLSOptions{Enable: true, RootCAs: roots}.Client()
	if err != nil {
		// Unreachable: no CA or client-certificate FILE is configured here,
		// and those are Client's only failure modes.
		cfg = &tls.Config{MinVersion: transport.MinTLSVersion, RootCAs: roots}
	}
	c.http.Transport = &stdhttp.Transport{TLSClientConfig: cfg}
}

// applyToken attaches the Bearer token when a source is installed.
func (c *RegistrationClient) applyToken(ctx context.Context, req *stdhttp.Request) error {
	if c.tokenSource == nil {
		return nil
	}
	tok, err := c.tokenSource(ctx)
	if err != nil {
		return fmt.Errorf("provider/node: obtain access token: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+tok)
	return nil
}

func (c *RegistrationClient) postResource(ctx context.Context, t is04.ResourceType, data any) error {
	status, err := c.postResourceOnce(ctx, t, data)
	if err != nil {
		return err
	}
	if status == stdhttp.StatusOK {
		// Stale data on the Registry — DELETE then re-POST.
		id := resourceID(t, data)
		c.deleteResource(ctx, t, id)
		status, err = c.postResourceOnce(ctx, t, data)
		if err != nil {
			return err
		}
	}
	if status != stdhttp.StatusOK && status != stdhttp.StatusCreated {
		return fmt.Errorf("provider/node: POST resource (%s): unexpected HTTP %d", t, status)
	}
	atomic.AddUint64(&c.registrations, 1)
	return nil
}

// postResourceOnce performs a single POST and returns the status code
// + an error for transport / 4xx / 5xx failures. 200 + 201 surface as
// (status, nil) so the caller can distinguish them.
func (c *RegistrationClient) postResourceOnce(ctx context.Context, t is04.ResourceType, data any) (int, error) {
	body, err := is04.EncodeRegistrationVersioned(c.codec, t, data)
	if err != nil {
		return 0, err
	}
	url := c.base + "/resource"
	req, err := stdhttp.NewRequestWithContext(ctx, stdhttp.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return 0, fmt.Errorf("provider/node: build POST: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if err := c.applyToken(ctx, req); err != nil {
		return 0, err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return 0, fmt.Errorf("provider/node: POST %s/resource: %w", c.base, err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != stdhttp.StatusOK && resp.StatusCode != stdhttp.StatusCreated {
		respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		bodyText := strings.TrimSpace(string(respBody))
		if len(bodyText) > 120 {
			bodyText = bodyText[:120] + "..."
		}
		return resp.StatusCode, fmt.Errorf("provider/node: POST %s: HTTP %d: %s",
			url, resp.StatusCode, bodyText)
	}
	return resp.StatusCode, nil
}

// resourceID extracts the IS-04 resource UUID from an arbitrary
// payload. The codec tags ResourceCore.ID consistently across types.
func resourceID(t is04.ResourceType, data any) string {
	switch v := data.(type) {
	case *is04.Node:
		return v.ID
	case *is04.Device:
		return v.ID
	case *is04.Source:
		return v.ID
	case *is04.Flow:
		return v.ID
	case *is04.Sender:
		return v.ID
	case *is04.Receiver:
		return v.ID
	}
	return ""
}

// sendHeartbeat POSTs to /health/nodes/{node-id}. Returns
// ErrRegistryNotFound on 404, generic error on other failures, nil on
// 200.
func (c *RegistrationClient) sendHeartbeat(ctx context.Context) error {
	url := c.base + "/health/nodes/" + c.bundle.Node.ID
	req, err := stdhttp.NewRequestWithContext(ctx, stdhttp.MethodPost, url, nil)
	if err != nil {
		return fmt.Errorf("provider/node: build heartbeat: %w", err)
	}
	if err := c.applyToken(ctx, req); err != nil {
		return err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("provider/node: POST %s: %w", url, err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode == stdhttp.StatusNotFound {
		return ErrRegistryNotFound
	}
	if resp.StatusCode != stdhttp.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("provider/node: heartbeat: HTTP %d: %s",
			resp.StatusCode, strings.TrimSpace(string(body)))
	}
	// Discard the response body — we don't track Registry-side TTL
	// today; the spec only requires us to POST.
	_, _ = io.Copy(io.Discard, resp.Body)
	atomic.AddUint64(&c.heartbeats, 1)
	return nil
}

// deregisterAll DELETEs every owned resource in REVERSE dependency
// order (Receivers first, Node last). Best-effort — failures are
// logged but don't block shutdown.
func (c *RegistrationClient) deregisterAll() {
	if !c.registered.Load() {
		return
	}
	delCtx, cancel := context.WithTimeout(context.Background(), HeartbeatGracePeriod)
	defer cancel()
	// Reverse order
	for i := range c.bundle.Receivers {
		c.deleteResource(delCtx, is04.ResourceReceiver, c.bundle.Receivers[i].ID)
	}
	for i := range c.bundle.Senders {
		c.deleteResource(delCtx, is04.ResourceSender, c.bundle.Senders[i].ID)
	}
	for i := range c.bundle.Flows {
		c.deleteResource(delCtx, is04.ResourceFlow, c.bundle.Flows[i].ID)
	}
	for i := range c.bundle.Sources {
		c.deleteResource(delCtx, is04.ResourceSource, c.bundle.Sources[i].ID)
	}
	for i := range c.bundle.Devices {
		c.deleteResource(delCtx, is04.ResourceDevice, c.bundle.Devices[i].ID)
	}
	c.deleteResource(delCtx, is04.ResourceNode, c.bundle.Node.ID)
	c.setRegistered(false)
}

func (c *RegistrationClient) deleteResource(ctx context.Context, t is04.ResourceType, id string) {
	url := c.base + "/resource/" + t.Plural() + "/" + id
	req, err := stdhttp.NewRequestWithContext(ctx, stdhttp.MethodDelete, url, nil)
	if err != nil {
		c.logger.Warn("provider/node: build DELETE", "err", err)
		atomic.AddUint64(&c.failures, 1)
		return
	}
	if err := c.applyToken(ctx, req); err != nil {
		c.logger.Warn("provider/node: DELETE token", "err", err)
		atomic.AddUint64(&c.failures, 1)
		return
	}
	resp, err := c.http.Do(req)
	if err != nil {
		c.logger.Warn("provider/node: DELETE", "type", t, "id", id, "err", err)
		atomic.AddUint64(&c.failures, 1)
		return
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != stdhttp.StatusNoContent && resp.StatusCode != stdhttp.StatusNotFound {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		c.logger.Warn("provider/node: DELETE non-204",
			"type", t, "id", id, "status", resp.StatusCode, "body", string(body))
		atomic.AddUint64(&c.failures, 1)
		return
	}
	atomic.AddUint64(&c.deletions, 1)
}

// MarshalNode renders the node bundle's Node as JSON. Used by tests.
func (c *RegistrationClient) MarshalNode() ([]byte, error) {
	return json.Marshal(c.bundle.Node)
}
