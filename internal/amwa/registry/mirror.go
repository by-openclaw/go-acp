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
	// APIVer is the IS-04 wire version used toward the TARGET
	// Registration API (default v1.3). Toward the SOURCE the mirror
	// subscribes at every registered minor in parallel — see the
	// version-fidelity comment on Run — and forwards each document
	// unchanged (no minor translation of the payloads themselves).
	APIVer string
	// Logger receives operational events. nil = slog.Default().
	Logger *slog.Logger
	// AuditPath, when set, appends one JSONL AuditEvent per external-
	// registry observation — the evidence trail (mirror_audit.go).
	AuditPath string
	// StatusAddr, when set, serves /status.json — counters, cache
	// parity data and the recent audit ring.
	StatusAddr string
	// ServeAddr, when set, serves the mirrored catalogue as a
	// read-only IS-04 Query API (REST + WS subscriptions) on this
	// address, so a controller reads the plant THROUGH the audited
	// mirror (mirror_serve.go). Empty disables the served face —
	// pre-#940 behaviour unchanged.
	ServeAddr string
	// ServeAdvertiseHost is the identity minted into the served face's
	// ws_href and mDNS announce, as "host" or "host:port". Precedence:
	// when set it wins outright (a bare host takes the bound serve
	// port); when empty the bound address decides — a concrete bind IP
	// advertises itself, an unspecified bind falls back to the OS
	// hostname, which peers off-link may not resolve (AMWA IS-04-02
	// test_22_2 et al fail exactly there). Set it to the address
	// controllers actually reach.
	ServeAdvertiseHost string
	// ServePri is the DNS-SD `pri` TXT for the served face's
	// _nmos-query._tcp announce. The CLI defaults it to 100 — the dev
	// range — because the plant mirror must never win a production
	// Registry election against its own source registry (pri 0).
	ServePri int
	// ServeAuthURL, when set, arms the served Query face with the
	// BCP-003-02 Bearer gate: tokens are validated against the
	// Authorization Server at this base URL, exactly the way the
	// standalone Registry's --auth-url arms its faces. Requires
	// ServeAddr — a gate with no served face guards nothing. The
	// mirror's OUTBOUND legs (source Query-WS, target forwards) are
	// untouched. Empty keeps the served face unauthenticated.
	ServeAuthURL string
}

// MirrorStats is a snapshot of forward counters. JSON names are the
// /status.json contract amwa-validate-mirror.yml asserts on.
type MirrorStats struct {
	Forwarded  uint64 `json:"forwarded"`  // POSTs accepted by the target
	Deleted    uint64 `json:"deleted"`    // DELETEs accepted by the target
	Heartbeats uint64 `json:"heartbeats"` // health POSTs accepted by the target
	Resyncs    uint64 `json:"resyncs"`    // full re-registrations (target 404 recovery)
	Failures   uint64 `json:"failures"`   // requests the target refused

	// Served Query face counters (--serve, mirror_serve.go). Zero when
	// serving is disabled.
	ServedQueries uint64 `json:"served_queries"` // REST requests answered on the served Query API
	ServedWSSubs  uint64 `json:"served_ws_subs"` // WS subscription sockets opened by consumers
	ServedRejects uint64 `json:"served_rejects"` // write attempts refused (the mirror serves Query only)
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
	// cacheVer holds each cached resource's REGISTERED wire minor —
	// the minor of the source subscription its row arrived on. The
	// embedded served store re-stamps resources with it so the IS-04
	// §6.1.5 downgrade semantics survive the mirrored hop (AMWA
	// IS-04-02 test_22/test_32). Same topic/id keys as cache.
	cacheVer map[string]map[string]string
	stats    MirrorStats
	// runCtx is the context passed to Run, captured so a debounced
	// resync fired from a timer goroutine can honour shutdown.
	runCtx context.Context
	// resyncTimer debounces an ordered re-registration triggered when
	// the target rejects a child whose parent has not landed yet
	// (Cerebrum answers 400, not 404, to a missing parent reference).
	// Non-nil while a resync is pending. Guarded by mu.
	resyncTimer *time.Timer

	// serve is the embedded read-only Query face (mirror_serve.go);
	// nil when opts.ServeAddr is empty. Assigned once in Run before the
	// watch goroutines start, immutable after.
	serve *mirrorServe
	// serveReplayTimer debounces the ordered replay of the cache into
	// the embedded store — the served-face twin of resyncTimer, fired
	// when a grain row's parent has not landed in the store yet (the
	// six topic streams race). Guarded by mu.
	serveReplayTimer *time.Timer

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
	if opts.ServeAuthURL != "" && opts.ServeAddr == "" {
		return nil, errors.New("registry/mirror: --auth-url guards the served Query face, and no face is being served — add --serve ADDR or drop --auth-url")
	}
	if opts.APIVer == "" {
		opts.APIVer = is04.APIVersion
	}
	if opts.Logger == nil {
		opts.Logger = slog.Default()
	}
	cache := make(map[string]map[string]json.RawMessage, len(mirrorTopics))
	cacheVer := make(map[string]map[string]string, len(mirrorTopics))
	for _, tp := range mirrorTopics {
		cache[tp] = map[string]json.RawMessage{}
		cacheVer[tp] = map[string]string{}
	}
	return &Mirror{
		opts:     opts,
		logger:   opts.Logger,
		http:     &stdhttp.Client{Timeout: 10 * time.Second},
		cache:    cache,
		cacheVer: cacheVer,
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
	if _, ok := is04.Get(m.opts.APIVer); !ok {
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
	// Version fidelity (AMWA IS-04-02 test_22/test_32): the source's
	// Query API hides a resource registered at v1.0 from a v1.3
	// subscription (IS-04 §6.1.5 no-downgrade-by-default), and a grain
	// row carries no version stamp — so the ONLY wire-true way to
	// mirror the whole catalogue AND learn each resource's registered
	// minor is one subscription per registered minor per topic. The
	// exact-match version gate on the source then partitions the
	// catalogue: the minor of the socket a row arrived on IS that
	// resource's registered minor.
	clients := make(map[string]*query.Client)
	for _, ver := range mirrorSourceVersions(m.opts.APIVer) {
		vcodec, ok := is04.Get(ver)
		if !ok {
			continue // minor without a registered codec cannot be dialled
		}
		qc, err := query.NewClient(m.opts.Source, vcodec)
		if err != nil {
			return fmt.Errorf("registry/mirror: source: %w", err)
		}
		clients[ver] = qc
	}
	if m.opts.StatusAddr != "" {
		go m.serveStatus(ctx, m.opts.StatusAddr)
	}
	if m.opts.ServeAddr != "" {
		// Started before the watch goroutines so forwardRow always sees
		// a fully-built embedded store.
		if err := m.startServe(ctx); err != nil {
			return err
		}
	}

	var wg sync.WaitGroup
	for _, topic := range mirrorTopics {
		for ver, qc := range clients {
			wg.Add(1)
			go func(topic, ver string, qc *query.Client) {
				defer wg.Done()
				m.watchTopic(ctx, qc, topic, ver)
			}(topic, ver, qc)
		}
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
	if m.serveReplayTimer != nil {
		m.serveReplayTimer.Stop()
		m.serveReplayTimer = nil
	}
	m.mu.Unlock()
	return ctx.Err()
}

// mirrorSourceVersions resolves the IS-04 minors the mirror subscribes
// at on the source — every registered codec minor (the served face's
// own fan-out, pickAPIVersions) with the target-leg primary guaranteed
// present. See the version-fidelity comment in Run for why one
// subscription per minor is mandatory, not an optimisation.
func mirrorSourceVersions(primary string) []string {
	vers := pickAPIVersions("")
	for _, v := range vers {
		if v == primary {
			return vers
		}
	}
	return append(vers, primary)
}

// watchTopic subscribes to one collection at one wire minor and
// forwards its grains, resubscribing with a flat 2 s backoff whenever
// the subscription or socket fails.
func (m *Mirror) watchTopic(ctx context.Context, qc *query.Client, topic, ver string) {
	for {
		if ctx.Err() != nil {
			return
		}
		sub, err := qc.Subscribe(ctx, query.SubscribeRequest{ResourcePath: "/" + topic})
		if err != nil {
			m.logger.Warn("registry/mirror: subscribe failed", "topic", topic, "ver", ver, "err", err)
			m.audit.event("ws_subscribe_failed", map[string]any{"topic": topic, "ver": ver, "err": err.Error()})
			m.sleep(ctx, 2*time.Second)
			continue
		}
		err = query.Watch(ctx, sub.WSHref, func(g *is04.Grain) error {
			for _, row := range g.Grain.Data {
				m.forwardRow(ctx, topic, ver, row)
			}
			return nil
		}, query.WatchOptions{})
		if ctx.Err() != nil {
			return
		}
		m.logger.Warn("registry/mirror: watch ended — resubscribing", "topic", topic, "ver", ver, "err", err)
		m.audit.event("ws_reconnect", map[string]any{"topic": topic, "ver": ver, "err": fmt.Sprint(err)})
		m.sleep(ctx, 2*time.Second)
	}
}

// forwardRow applies one grain row to the cache and the target. ver is
// the wire minor of the source subscription the row arrived on — the
// resource's registered minor, preserved into the served store's
// version stamp.
func (m *Mirror) forwardRow(ctx context.Context, topic, ver string, row is04.GrainDataRow) {
	switch row.Kind() {
	case is04.ChangeAdded, is04.ChangeModified:
		m.mu.Lock()
		m.cache[topic][row.Path] = row.Post
		m.cacheVer[topic][row.Path] = ver
		m.mu.Unlock()
		m.applyServeRow(topic, ver, row, true)
		// Live path: a 400 here means a parent has not been forwarded
		// yet (the six topics stream concurrently), so allow it to
		// trigger an ordered resync.
		m.postResource(ctx, topic, ver, row.Post, true)
	case is04.ChangeRemoved:
		m.mu.Lock()
		delete(m.cache[topic], row.Path)
		delete(m.cacheVer[topic], row.Path)
		m.mu.Unlock()
		m.applyServeRow(topic, ver, row, true)
		m.deleteResource(ctx, topic, ver, row.Path)
	default:
		m.logger.Warn("registry/mirror: grain row with neither pre nor post", "topic", topic, "id", row.Path)
	}
}

// postResource POSTs one document to the target Registration API at
// the resource's own registered minor — a v1.0 body legitimately
// lacks fields the v1.3 schema requires, so posting it to the v1.3
// URL would earn a 400 from any spec-strict target.
//
// allowResync gates the parent-missing recovery: the live forward path
// passes true so a 400 (target rejects a child whose parent has not
// landed yet) schedules an ordered resync; resync itself passes false so
// a 400 during an already-ordered pass cannot trigger another resync (no
// runaway loop when a resource is genuinely un-postable).
func (m *Mirror) postResource(ctx context.Context, topic, ver string, doc json.RawMessage, allowResync bool) {
	body, err := json.Marshal(map[string]any{
		"type": mirrorSingular[topic],
		"data": doc,
	})
	if err != nil {
		m.fail("encode", topic, err)
		return
	}
	url := m.registrationBase(ver) + "/resource"
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

// deleteResource DELETEs one document from the target, at the minor
// it was forwarded on.
func (m *Mirror) deleteResource(ctx context.Context, topic, ver, id string) {
	url := m.registrationBase(ver) + "/resource/" + topic + "/" + id
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
			for _, n := range m.nodeIDs() {
				if m.sendHealth(ctx, n.id, n.ver) == errMirrorEvicted {
					m.logger.Warn("registry/mirror: target evicted node — full resync", "node", n.id)
					m.audit.event("target_evicted", map[string]any{"node": n.id})
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
func (m *Mirror) sendHealth(ctx context.Context, nodeID, ver string) error {
	url := m.registrationBase(ver) + "/health/nodes/" + nodeID
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

// resync re-POSTs the whole cached catalogue in dependency order,
// each resource at its registered minor.
func (m *Mirror) resync(ctx context.Context) {
	m.audit.event("resync", nil)
	type verDoc struct {
		ver string
		doc json.RawMessage
	}
	m.mu.Lock()
	m.stats.Resyncs++
	snapshot := make(map[string][]verDoc, len(mirrorTopics))
	for _, topic := range mirrorTopics {
		ids := make([]string, 0, len(m.cache[topic]))
		for id := range m.cache[topic] {
			ids = append(ids, id)
		}
		sort.Strings(ids) // deterministic order for tests + logs
		docs := make([]verDoc, 0, len(ids))
		for _, id := range ids {
			docs = append(docs, verDoc{ver: m.cacheVer[topic][id], doc: m.cache[topic][id]})
		}
		snapshot[topic] = docs
	}
	m.mu.Unlock()

	for _, topic := range mirrorTopics {
		for _, vd := range snapshot[topic] {
			if ctx.Err() != nil {
				return
			}
			// allowResync=false: this pass is already in dependency
			// order, so a 400 here is a genuine reject, not a race.
			m.postResource(ctx, topic, vd.ver, vd.doc, false)
		}
	}
	// The served face is fed from the same authoritative cache, so a
	// full resync repopulates it too — an eviction-recovery pass must
	// leave both downstream copies (target AND embedded store) whole.
	m.serveReplay()
}

// nodeRef is one cached source node — id plus the wire minor its
// heartbeat is proxied at.
type nodeRef struct {
	id  string
	ver string
}

// nodeIDs snapshots the cached source node ids with their registered
// minors, id-sorted.
func (m *Mirror) nodeIDs() []nodeRef {
	m.mu.Lock()
	defer m.mu.Unlock()
	ids := make([]string, 0, len(m.cache["nodes"]))
	for id := range m.cache["nodes"] {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	out := make([]nodeRef, 0, len(ids))
	for _, id := range ids {
		out = append(out, nodeRef{id: id, ver: m.cacheVer["nodes"][id]})
	}
	return out
}

// registrationBase returns the target Registration API base URL for
// one wire minor. A target given as a bare origin gets the minor
// appended (empty minor falls back to the primary APIVer); a target
// the operator pinned to an explicit /x-nmos/registration/<ver> path
// is used verbatim for every resource — their pin outranks fidelity.
func (m *Mirror) registrationBase(ver string) string {
	base := strings.TrimRight(m.opts.Target, "/")
	if !strings.Contains(base, "/x-nmos/registration/") {
		if ver == "" {
			ver = m.opts.APIVer
		}
		base += "/x-nmos/registration/" + ver
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
