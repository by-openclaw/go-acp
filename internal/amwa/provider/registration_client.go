package provider

import (
	"bytes"
	"context"
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

	"acp/internal/amwa/codec/is04"
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
//   1. POST /resource for the Node, then each Device, Source, Flow,
//      Sender, Receiver in dependency order (referential integrity:
//      Sources before Flows, etc.).
//   2. Heartbeat every 5 s via POST /health/nodes/{id}.
//   3. On 404 from heartbeat → full re-registration.
//   4. On Stop / shutdown → DELETE every owned resource (Receivers
//      first, then Senders, Flows, Sources, Devices, Node).
type RegistrationClient struct {
	logger *slog.Logger

	// Base URL of the Registration API including version, e.g.
	// `http://10.6.239.113:8235/x-nmos/registration/v1.3`. Empty when
	// the watcher is driving discovery — base is set per-cycle then.
	base   string
	apiVer string
	bundle *NodeConfig

	// watcher, when non-nil, supplies the current Registry (Mode A).
	// When nil, base is fixed (Mode B).
	watcher *RegistryWatcher
	// currentRegistry is the FullName of the watcher pick the loop is
	// currently registered against — used for Disqualify on failure.
	currentRegistry string

	http *stdhttp.Client

	mu          sync.Mutex
	cancelLoop  context.CancelFunc
	registered  atomic.Bool
	// everRegistered flips true after the first successful registerAll.
	// On subsequent failovers we use rejoinOrRegister (heartbeat-first)
	// per IS-04 §6.1. Touched only inside the Run loop, no mutex.
	everRegistered bool
	registrations uint64
	heartbeats    uint64
	reregister    uint64
	deletions     uint64
	failures      uint64

	// closed signals the heartbeat loop to exit + DELETE has finished.
	closed chan struct{}
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
	return &RegistrationClient{
		logger: logger,
		base:   base,
		apiVer: apiVer,
		bundle: bundle,
		http: &stdhttp.Client{
			Timeout: 10 * time.Second,
		},
		closed: make(chan struct{}),
	}
}

// SetWatcher attaches a RegistryWatcher; when set, Run picks the
// highest-pri Registry from the watcher each cycle and falls over to
// the next-best on registration / heartbeat failure.
func (c *RegistrationClient) SetWatcher(w *RegistryWatcher) {
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
		c.registered.Store(true)
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
	// throttle actual heartbeats to HeartbeatInterval below.
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()
	lastHeartbeat := time.Time{}

	for {
		select {
		case <-loopCtx.Done():
			c.deregisterAll()
			return
		case <-ticker.C:
			// IS-04 v1.3.3 §3.1 — Node selects the highest-priority
			// Registry from those *currently* advertised. If a
			// higher-priority one appeared after we registered, switch:
			// DELETE from the old one, then re-register with the new.
			if c.registered.Load() && c.shouldSwitchToBetter() {
				c.logger.Info("provider/node: better registry available — switching")
				c.deregisterAll()
				c.registered.Store(false)
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
			// Throttle actual /health POSTs near HeartbeatInterval — the
			// 1 s tick above is for fast failover detection only. AMWA
			// test_05 enforces 5 s ± 0.5 s between heartbeats, so we
			// fire when the elapsed budget is within 0.5 s of due to
			// keep the tick-quantisation jitter inside the window.
			if time.Since(lastHeartbeat) < HeartbeatInterval-500*time.Millisecond {
				continue
			}
			lastHeartbeat = time.Now()
			err := c.sendHeartbeat(loopCtx)
			if errors.Is(err, ErrRegistryNotFound) {
				c.logger.Warn("provider/node: heartbeat 404 — re-registering")
				atomic.AddUint64(&c.reregister, 1)
				c.registered.Store(false)
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
				c.registered.Store(false)
				// Immediate failover loop — try the next-best Registry
				// right now rather than waiting another HeartbeatInterval.
				// AMWA test_15/16 cascade mocks down faster than 5 s.
				for i := 0; i < MaxFailoverChain; i++ {
					if _, ok := c.pickBase(); !ok {
						break
					}
					regErr := c.registerAll(loopCtx)
					if regErr == nil {
						lastHeartbeat = time.Time{}
						break
					}
					c.logger.Warn("provider/node: cascade re-register failed", "step", i, "err", regErr)
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
	c.registered.Store(true)
	return nil
}

// postResource POSTs one IS-04 resource. Per IS-04 v1.3.3 §4.0:
//   - 201 Created  → fresh registration, accept it.
//   - 200 OK       → Registry already had this UUID (stale state). The
//                    Node MUST DELETE the stale entry and re-POST as
//                    fresh — AMWA test_21 enforces this.
//   - other        → error.
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
	body, err := is04.EncodeRegistration(t, data)
	if err != nil {
		return 0, err
	}
	url := c.base + "/resource"
	req, err := stdhttp.NewRequestWithContext(ctx, stdhttp.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return 0, fmt.Errorf("provider/node: build POST: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
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
	c.registered.Store(false)
}

func (c *RegistrationClient) deleteResource(ctx context.Context, t is04.ResourceType, id string) {
	url := c.base + "/resource/" + t.Plural() + "/" + id
	req, err := stdhttp.NewRequestWithContext(ctx, stdhttp.MethodDelete, url, nil)
	if err != nil {
		c.logger.Warn("provider/node: build DELETE", "err", err)
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
