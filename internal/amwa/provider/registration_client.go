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
	// `http://10.6.239.113:8235/x-nmos/registration/v1.3`.
	base   string
	bundle *NodeConfig

	http *stdhttp.Client

	mu          sync.Mutex
	cancelLoop  context.CancelFunc
	registered  atomic.Bool
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
	base := strings.TrimRight(registryURL, "/")
	// Accept both forms: bare host (we append /x-nmos/registration/v1.3)
	// or a URL that already includes the suffix.
	if !strings.Contains(base, "/x-nmos/registration/") {
		base = base + "/x-nmos/registration/" + apiVer
	}
	return &RegistrationClient{
		logger: logger,
		base:   base,
		bundle: bundle,
		http: &stdhttp.Client{
			Timeout: 10 * time.Second,
		},
		closed: make(chan struct{}),
	}
}

// Run drives the registration + heartbeat loop until ctx is
// cancelled. Performs initial registration, starts the heartbeat
// ticker, handles 404 → re-register, then deregisters on cancel.
func (c *RegistrationClient) Run(ctx context.Context) {
	defer close(c.closed)
	loopCtx, cancel := context.WithCancel(ctx)
	c.mu.Lock()
	c.cancelLoop = cancel
	c.mu.Unlock()

	if err := c.registerAll(loopCtx); err != nil {
		c.logger.Warn("provider/node: initial registration failed", "err", err)
		atomic.AddUint64(&c.failures, 1)
		// Keep trying via the heartbeat loop's reregister path.
	}

	ticker := time.NewTicker(HeartbeatInterval)
	defer ticker.Stop()

	for {
		select {
		case <-loopCtx.Done():
			c.deregisterAll()
			return
		case <-ticker.C:
			if !c.registered.Load() {
				if err := c.registerAll(loopCtx); err != nil {
					c.logger.Warn("provider/node: re-register attempt failed", "err", err)
					atomic.AddUint64(&c.failures, 1)
					continue
				}
			}
			err := c.sendHeartbeat(loopCtx)
			if errors.Is(err, ErrRegistryNotFound) {
				c.logger.Warn("provider/node: heartbeat 404 — re-registering")
				atomic.AddUint64(&c.reregister, 1)
				c.registered.Store(false)
			} else if err != nil {
				c.logger.Warn("provider/node: heartbeat failed", "err", err)
				atomic.AddUint64(&c.failures, 1)
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

func (c *RegistrationClient) postResource(ctx context.Context, t is04.ResourceType, data any) error {
	body, err := is04.EncodeRegistration(t, data)
	if err != nil {
		return err
	}
	url := c.base + "/resource"
	req, err := stdhttp.NewRequestWithContext(ctx, stdhttp.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("provider/node: build POST: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("provider/node: POST %s/resource: %w", c.base, err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != stdhttp.StatusOK && resp.StatusCode != stdhttp.StatusCreated {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("provider/node: POST resource (%s): HTTP %d: %s",
			t, resp.StatusCode, strings.TrimSpace(string(body)))
	}
	atomic.AddUint64(&c.registrations, 1)
	return nil
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
