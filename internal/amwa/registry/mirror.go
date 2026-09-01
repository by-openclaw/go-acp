package registry

// Mirror — registry-to-registry bridge (the productized form of the
// VLAN600 lab bridge, see internal/amwa/docs/cerebrum-interop.md
// "Registry-to-registry bridge").
//
// Topology:
//
//	source Registry ──Query-WS grains──► Mirror ──Registration API──► target Registry
//	   (controller role toward source)          (node role toward target)
//
// The mirror subscribes to all six resource collections on the source
// Query API and forwards every change to the target Registration API:
// added/modified rows become POST /resource, removed rows become
// DELETE. One heartbeat per source node is proxied every
// HeartbeatInterval while that node exists in the source. No new wire
// behaviour is invented anywhere — toward the source the mirror is a
// standard Controller, toward the target a standard Node.
//
// Field-taught details baked in (measured against EVS Cerebrum
// 2.8.17, 2026-08-29):
//   - health POSTs carry an explicit empty body so Content-Length: 0
//     is always present — Cerebrum's registry answers 411 to bodyless
//     POSTs and then silently GCs everything;
//   - a 404 heartbeat triggers a full re-registration of the cached
//     catalogue in dependency order — their registration face
//     validates parent references, so order is mandatory.

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	stdhttp "net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"dhs/internal/amwa/codec/is04"
	"dhs/internal/amwa/session/query"
)

// MirrorHeartbeatInterval is the IS-04 §6.1 heartbeat cadence the
// mirror proxies per source node.
const MirrorHeartbeatInterval = 5 * time.Second

// mirrorTopics is every Query API collection, in registration
// dependency order (IS-04 referential integrity: sources before
// flows, flows before senders, …). Re-registration walks this order.
var mirrorTopics = []string{"nodes", "devices", "sources", "flows", "senders", "receivers"}

// mirrorSingular maps a collection to the Registration API type key.
var mirrorSingular = map[string]string{
	"nodes":     "node",
	"devices":   "device",
	"sources":   "source",
	"flows":     "flow",
	"senders":   "sender",
	"receivers": "receiver",
}

// MirrorOptions configures Run.
type MirrorOptions struct {
	// Source is the origin of the Registry whose catalogue is mirrored,
	// e.g. "http://10.6.250.101:8235". Query API side.
	Source string
	// Target is the origin of the Registry that receives the copy,
	// e.g. "http://10.6.250.5:8080". Registration API side.
	Target string
	// APIVer is the IS-04 wire version used on BOTH faces (default
	// v1.3). The mirror does not translate between minors.
	APIVer string
	// Logger receives operational events. nil = slog.Default().
	Logger *slog.Logger
	// AuditPath, when set, appends one JSONL AuditEvent per external-
	// registry observation — the evidence trail (mirror_audit.go).
	AuditPath string
	// StatusAddr, when set, serves /status.json — counters, cache
	// parity data and the recent audit ring.
	StatusAddr string
}

// MirrorStats is a snapshot of forward counters. JSON names are the
// /status.json contract amwa-validate-mirror.yml asserts on.
type MirrorStats struct {
	Forwarded  uint64 `json:"forwarded"`  // POSTs accepted by the target
	Deleted    uint64 `json:"deleted"`    // DELETEs accepted by the target
	Heartbeats uint64 `json:"heartbeats"` // health POSTs accepted by the target
	Resyncs    uint64 `json:"resyncs"`    // full re-registrations (target 404 recovery)
	Failures   uint64 `json:"failures"`   // requests the target refused
}

// Mirror bridges one source Registry into one target Registry.
type Mirror struct {
	opts   MirrorOptions
	logger *slog.Logger
	http   *stdhttp.Client

	mu sync.Mutex
	// cache holds the latest Post document per collection/id — the
	// mirror's authoritative copy of the source catalogue, used for
	// full re-registration after a target eviction.
	cache map[string]map[string]json.RawMessage
	stats MirrorStats
	// runCtx is the context passed to Run, captured so a debounced
	// resync fired from a timer goroutine can honour shutdown.
	runCtx context.Context
	// resyncTimer debounces an ordered re-registration triggered when
	// the target rejects a child whose parent has not landed yet
	// (Cerebrum answers 400, not 404, to a missing parent reference).
	// Non-nil while a resync is pending. Guarded by mu.
	resyncTimer *time.Timer

	// audit is the observation sink (mirror_audit.go); started stamps
	// the uptime the status endpoint reports.
	audit   *auditor
	started time.Time
}

// mirrorResyncDebounce collapses a burst of parent-missing 400s during
// the initial concurrent fan-out into ONE ordered resync once the burst
// settles.
const mirrorResyncDebounce = 750 * time.Millisecond

// NewMirror validates options and builds an unstarted mirror.
func NewMirror(opts MirrorOptions) (*Mirror, error) {
	if opts.Source == "" || opts.Target == "" {
		return nil, errors.New("registry/mirror: source and target are required")
	}
	if strings.TrimRight(opts.Source, "/") == strings.TrimRight(opts.Target, "/") {
		return nil, errors.New("registry/mirror: source and target must differ")
	}
	if opts.APIVer == "" {
		opts.APIVer = is04.APIVersion
	}
	if opts.Logger == nil {
		opts.Logger = slog.Default()
	}
	cache := make(map[string]map[string]json.RawMessage, len(mirrorTopics))
	for _, tp := range mirrorTopics {
		cache[tp] = map[string]json.RawMessage{}
	}
	return &Mirror{
		opts:   opts,
		logger: opts.Logger,
		http:   &stdhttp.Client{Timeout: 10 * time.Second},
		cache:  cache,
	}, nil
}

// Stats returns a snapshot of the forward counters.
func (m *Mirror) Stats() MirrorStats {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.stats
}

// Run drives the mirror until ctx ends: one watch goroutine per
// collection (resubscribing with backoff on socket loss — the source
// registry restarting must not kill the bridge) plus the heartbeat
// proxy loop. Returns ctx.Err() on normal shutdown.
func (m *Mirror) Run(ctx context.Context) error {
	codec, ok := is04.Get(m.opts.APIVer)
	if !ok {
		return fmt.Errorf("registry/mirror: unknown api-ver %q", m.opts.APIVer)
	}
	audit, err := newAuditor(m.opts.AuditPath)
	if err != nil {
		return err
	}
	m.mu.Lock()
	m.runCtx = ctx
	m.audit = audit
	m.started = time.Now()
	m.mu.Unlock()
	defer audit.close()
	audit.event("mirror_start", map[string]any{
		"source": m.opts.Source, "target": m.opts.Target, "api_ver": m.opts.APIVer,
	})
	qc, err := query.NewClient(m.opts.Source, codec)
	if err != nil {
		return fmt.Errorf("registry/mirror: source: %w", err)
	}
	if m.opts.StatusAddr != "" {
		go m.serveStatus(ctx, m.opts.StatusAddr)
	}

	var wg sync.WaitGroup
	for _, topic := range mirrorTopics {
		wg.Add(1)
		go func(topic string) {
			defer wg.Done()
			m.watchTopic(ctx, qc, topic)
		}(topic)
	}

	wg.Add(1)
	go func() {
		defer wg.Done()
		m.heartbeatLoop(ctx)
	}()

	wg.Wait()
	m.mu.Lock()
	if m.resyncTimer != nil {
		m.resyncTimer.Stop()
		m.resyncTimer = nil
	}
	m.mu.Unlock()
	return ctx.Err()
}

// watchTopic subscribes to one collection and forwards its grains,
// resubscribing with a flat 2 s backoff whenever the subscription or
// socket fails.
func (m *Mirror) watchTopic(ctx context.Context, qc *query.Client, topic string) {
	for {
		if ctx.Err() != nil {
			return
		}
		sub, err := qc.Subscribe(ctx, query.SubscribeRequest{ResourcePath: "/" + topic})
		if err != nil {
			m.logger.Warn("registry/mirror: subscribe failed", "topic", topic, "err", err)
			m.audit.event("ws_subscribe_failed", map[string]any{"topic": topic, "err": err.Error()})
			m.sleep(ctx, 2*time.Second)
			continue
		}
		err = query.Watch(ctx, sub.WSHref, func(g *is04.Grain) error {
			for _, row := range g.Grain.Data {
				m.forwardRow(ctx, topic, row)
			}
			return nil
		}, query.WatchOptions{})
		if ctx.Err() != nil {
			return
		}
		m.logger.Warn("registry/mirror: watch ended — resubscribing", "topic", topic, "err", err)
		m.audit.event("ws_reconnect", map[string]any{"topic": topic, "err": fmt.Sprint(err)})
		m.sleep(ctx, 2*time.Second)
	}
}

// forwardRow applies one grain row to the cache and the target.
func (m *Mirror) forwardRow(ctx context.Context, topic string, row is04.GrainDataRow) {
	switch row.Kind() {
	case is04.ChangeAdded, is04.ChangeModified:
		m.mu.Lock()
		m.cache[topic][row.Path] = row.Post
		m.mu.Unlock()
		// Live path: a 400 here means a parent has not been forwarded
		// yet (the six topics stream concurrently), so allow it to
		// trigger an ordered resync.
		m.postResource(ctx, topic, row.Post, true)
	case is04.ChangeRemoved:
		m.mu.Lock()
		delete(m.cache[topic], row.Path)
		m.mu.Unlock()
		m.deleteResource(ctx, topic, row.Path)
	default:
		m.logger.Warn("registry/mirror: grain row with neither pre nor post", "topic", topic, "id", row.Path)
	}
}

// postResource POSTs one document to the target Registration API.
//
// allowResync gates the parent-missing recovery: the live forward path
// passes true so a 400 (target rejects a child whose parent has not
// landed yet) schedules an ordered resync; resync itself passes false so
// a 400 during an already-ordered pass cannot trigger another resync (no
// runaway loop when a resource is genuinely un-postable).
func (m *Mirror) postResource(ctx context.Context, topic string, doc json.RawMessage, allowResync bool) {
	body, err := json.Marshal(map[string]any{
		"type": mirrorSingular[topic],
		"data": doc,
	})
	if err != nil {
		m.fail("encode", topic, err)
		return
	}
	url := m.registrationBase() + "/resource"
	req, err := stdhttp.NewRequestWithContext(ctx, stdhttp.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		m.fail("build POST", topic, err)
		return
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := m.http.Do(req)
	if err != nil {
		m.fail("POST", topic, err)
		return
	}
	drainClose(resp)
	if resp.StatusCode != stdhttp.StatusOK && resp.StatusCode != stdhttp.StatusCreated {
		m.fail("POST", topic, fmt.Errorf("HTTP %d", resp.StatusCode))
		// A target's registration face validates parent references and
		// answers 400 for a child whose parent has not been forwarded
		// yet. The six collections stream concurrently, so this is
		// expected during the initial fill; a debounced ordered resync
		// re-POSTs the whole cache node→device→source→flow→sender→
		// receiver, after which every parent precedes its children.
		if allowResync && resp.StatusCode == stdhttp.StatusBadRequest {
			m.scheduleResync()
		}
		return
	}
	m.mu.Lock()
	m.stats.Forwarded++
	m.mu.Unlock()
}

// scheduleResync arms (or extends) a debounced ordered resync. Repeated
// parent-missing 400s during the initial fan-out collapse into one pass
// once the burst goes quiet for mirrorResyncDebounce.
func (m *Mirror) scheduleResync() {
	m.mu.Lock()
	defer m.mu.Unlock()
	ctx := m.runCtx
	if ctx == nil {
		return
	}
	if m.resyncTimer != nil {
		m.resyncTimer.Reset(mirrorResyncDebounce)
		return
	}
	m.resyncTimer = time.AfterFunc(mirrorResyncDebounce, func() {
		m.mu.Lock()
		m.resyncTimer = nil
		rctx := m.runCtx
		m.mu.Unlock()
		if rctx != nil && rctx.Err() == nil {
			m.resync(rctx)
		}
	})
}

// deleteResource DELETEs one document from the target.
func (m *Mirror) deleteResource(ctx context.Context, topic, id string) {
	url := m.registrationBase() + "/resource/" + topic + "/" + id
	req, err := stdhttp.NewRequestWithContext(ctx, stdhttp.MethodDelete, url, nil)
	if err != nil {
		m.fail("build DELETE", topic, err)
		return
	}
	resp, err := m.http.Do(req)
	if err != nil {
		m.fail("DELETE", topic, err)
		return
	}
	drainClose(resp)
	// 404 is success for a delete — the target never had it.
	if resp.StatusCode != stdhttp.StatusNoContent && resp.StatusCode != stdhttp.StatusNotFound {
		m.fail("DELETE", topic, fmt.Errorf("HTTP %d", resp.StatusCode))
		return
	}
	m.mu.Lock()
	m.stats.Deleted++
	m.mu.Unlock()
}

// heartbeatLoop proxies one health POST per cached source node every
// MirrorHeartbeatInterval. A 404 answer means the target evicted us —
// re-register the whole cached catalogue in dependency order.
func (m *Mirror) heartbeatLoop(ctx context.Context) {
	ticker := time.NewTicker(MirrorHeartbeatInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			for _, id := range m.nodeIDs() {
				if m.sendHealth(ctx, id) == errMirrorEvicted {
					m.logger.Warn("registry/mirror: target evicted node — full resync", "node", id)
					m.audit.event("target_evicted", map[string]any{"node": id})
					m.resync(ctx)
					break
				}
			}
		}
	}
}

var errMirrorEvicted = errors.New("registry/mirror: target answered 404 to a heartbeat")

// sendHealth POSTs one heartbeat. The empty (non-nil sized) body is
// deliberate: it guarantees a Content-Length header, which some
// registries (EVS Cerebrum) demand on POST — without it they answer
// 411 and their GC silently sweeps the catalogue.
func (m *Mirror) sendHealth(ctx context.Context, nodeID string) error {
	url := m.registrationBase() + "/health/nodes/" + nodeID
	req, err := stdhttp.NewRequestWithContext(ctx, stdhttp.MethodPost, url, bytes.NewReader(nil))
	if err != nil {
		m.fail("build health", "nodes", err)
		return err
	}
	resp, err := m.http.Do(req)
	if err != nil {
		m.fail("health", "nodes", err)
		return err
	}
	drainClose(resp)
	switch resp.StatusCode {
	case stdhttp.StatusOK:
		m.mu.Lock()
		m.stats.Heartbeats++
		m.mu.Unlock()
		return nil
	case stdhttp.StatusNotFound:
		return errMirrorEvicted
	default:
		err := fmt.Errorf("HTTP %d", resp.StatusCode)
		m.fail("health", "nodes", err)
		return err
	}
}

// resync re-POSTs the whole cached catalogue in dependency order.
func (m *Mirror) resync(ctx context.Context) {
	m.audit.event("resync", nil)
	m.mu.Lock()
	m.stats.Resyncs++
	snapshot := make(map[string][]json.RawMessage, len(mirrorTopics))
	for _, topic := range mirrorTopics {
		ids := make([]string, 0, len(m.cache[topic]))
		for id := range m.cache[topic] {
			ids = append(ids, id)
		}
		sort.Strings(ids) // deterministic order for tests + logs
		docs := make([]json.RawMessage, 0, len(ids))
		for _, id := range ids {
			docs = append(docs, m.cache[topic][id])
		}
		snapshot[topic] = docs
	}
	m.mu.Unlock()

	for _, topic := range mirrorTopics {
		for _, doc := range snapshot[topic] {
			if ctx.Err() != nil {
				return
			}
			// allowResync=false: this pass is already in dependency
			// order, so a 400 here is a genuine reject, not a race.
			m.postResource(ctx, topic, doc, false)
		}
	}
}

// nodeIDs snapshots the cached source node ids.
func (m *Mirror) nodeIDs() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	ids := make([]string, 0, len(m.cache["nodes"]))
	for id := range m.cache["nodes"] {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

// registrationBase returns the target Registration API base URL.
func (m *Mirror) registrationBase() string {
	base := strings.TrimRight(m.opts.Target, "/")
	if !strings.Contains(base, "/x-nmos/registration/") {
		base += "/x-nmos/registration/" + m.opts.APIVer
	}
	return base
}

func (m *Mirror) fail(op, topic string, err error) {
	m.logger.Warn("registry/mirror: "+op+" failed", "topic", topic, "err", err)
	m.audit.event("forward_failed", map[string]any{"op": op, "topic": topic, "err": err.Error()})
	m.mu.Lock()
	m.stats.Failures++
	m.mu.Unlock()
}

// sleep waits d or until ctx ends, whichever is first.
func (m *Mirror) sleep(ctx context.Context, d time.Duration) {
	select {
	case <-ctx.Done():
	case <-time.After(d):
	}
}

// drainClose fully consumes and closes a response body so the
// transport can reuse the connection.
func drainClose(resp *stdhttp.Response) {
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<20))
	_ = resp.Body.Close()
}
