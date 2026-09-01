package registry

// The mirror's SERVED Query face (--serve, issue #940).
//
// A mirror without this face is a one-way pump: it audits the source
// but a controller still has to read the plant from a registry the
// audit does not cover. With --serve the mirror feeds an EMBEDDED
// registry store from its source Query-WS stream and serves the full
// IS-04 Query API (REST + WebSocket subscriptions) from that store —
// the controller reads the plant THROUGH the audited path.
//
// Reuse over reimplementation: the served surface is the SAME code the
// standalone Registry runs — Store, installQueryRoutes, and
// SubscriptionManager. Rows are written through
// Store.IngestRegistrationVersioned / Store.DeleteResource, exactly
// the functions the Registration API handlers call, so mirror-driven
// updates fan out to WS subscribers via the same Store listener →
// SubscriptionManager.onChange → grain path registration-driven ones
// do. Nothing here is a hand-written Query subset.
//
// The one face NOT mounted is the Registration API: a controller must
// not register INTO the mirror. Any request under /x-nmos/registration
// answers 501 with an explanation and lands a mirror_write_rejected
// audit event.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	stdhttp "net/http"
	"sort"
	"strings"
	"time"

	"dhs/internal/amwa/codec/is04"
	httpsession "dhs/internal/amwa/session/http"
)

// mirrorServe is the embedded Query face's immutable wiring — built
// once by startServe before the watch goroutines run.
type mirrorServe struct {
	store *Store
	// addr is the actual bound address (":0" resolved), so tests and
	// operators can find the served face.
	addr string
}

// ServeAddr returns the served Query face's bound listen address, or
// "" while serving is disabled or not yet started.
func (m *Mirror) ServeAddr() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.serve == nil {
		return ""
	}
	return m.serve.addr
}

// startServe binds opts.ServeAddr, mounts the Query API for every
// registered IS-04 minor (same version fan-out as Registry.Serve) on
// an embedded Store, and serves until ctx ends. A bind failure is the
// operator's problem to see, not to discover later — it fails Run.
func (m *Mirror) startServe(ctx context.Context) error {
	ln, err := net.Listen("tcp", m.opts.ServeAddr)
	if err != nil {
		return fmt.Errorf("registry/mirror: serve listen %s: %w", m.opts.ServeAddr, err)
	}
	advertise := serveAdvertise(ln.Addr())
	store := NewStore()
	apiVers := pickAPIVersions("")
	srv := httpsession.NewServer(m.logger)
	wsPrefixes := make([]string, 0, len(apiVers))
	upgradeHandlers := make(map[string]stdhttp.HandlerFunc, len(apiVers))
	for _, apiVer := range apiVers {
		queryBase := "/x-nmos/query/" + apiVer
		mgr := NewSubscriptionManager(m.logger, store, advertise, apiVer)
		mgr.setWSLifecycleHooks(m.serveWSOpened, m.serveWSClosed)
		installQueryRoutes(srv, store, mgr, queryBase, apiVer)
		wsPrefix := queryBase + "/subscriptions/"
		wsPrefixes = append(wsPrefixes, wsPrefix)
		upgradeHandlers[wsPrefix] = mgr.UpgradeHandler(queryBase)
	}
	installMirrorServeRoots(srv, apiVers)
	routeTable := srv.MuxHandler()

	// Same dispatcher shape as Registry.Serve (WS upgrades hijack the
	// connection, so they bypass the route table), plus two mirror-only
	// concerns: the registration block and the per-request audit.
	dispatcher := stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, req *stdhttp.Request) {
		if req.URL.Path == "/x-nmos/registration" || strings.HasPrefix(req.URL.Path, "/x-nmos/registration/") {
			m.serveWriteRejected(req)
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(stdhttp.StatusNotImplemented)
			_ = json.NewEncoder(w).Encode(httpsession.ErrorBody{
				Code: stdhttp.StatusNotImplemented, Error: "Not Implemented",
				Debug: "read-only mirror: this Query API reflects " + m.opts.Source +
					" — register with the source registry, not the mirror",
			})
			return
		}
		if strings.HasSuffix(req.URL.Path, "/ws") {
			for _, prefix := range wsPrefixes {
				if strings.HasPrefix(req.URL.Path, prefix) {
					// Audited as serve_ws_subscribe/serve_ws_close via
					// the lifecycle hooks, not as serve_query — and the
					// upgrade must see the raw hijackable writer.
					upgradeHandlers[prefix](w, req)
					return
				}
			}
		}
		rec := &serveStatusRecorder{ResponseWriter: w, status: stdhttp.StatusOK}
		routeTable.ServeHTTP(rec, req)
		m.serveQueryAnswered(req, rec.status)
	})

	m.mu.Lock()
	m.serve = &mirrorServe{store: store, addr: ln.Addr().String()}
	m.mu.Unlock()

	httpSrv := &stdhttp.Server{Handler: dispatcher, ReadHeaderTimeout: 5 * time.Second}
	go func() {
		<-ctx.Done()
		shutCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = httpSrv.Shutdown(shutCtx)
	}()
	go func() {
		if err := httpSrv.Serve(ln); err != nil && err != stdhttp.ErrServerClosed {
			m.logger.Warn("registry/mirror: served query face failed", "addr", ln.Addr().String(), "err", err)
		}
	}()
	m.logger.Info("registry/mirror: serving mirrored Query API",
		"addr", ln.Addr().String(), "api_vers", apiVers)
	return nil
}

// serveAdvertise derives the host:port minted into ws_href. A bind on
// a concrete IP advertises that IP; an unspecified bind (":8335")
// advertises the OS hostname — the same fallback the Registry uses.
func serveAdvertise(addr net.Addr) string {
	host, port, err := net.SplitHostPort(addr.String())
	if err != nil {
		return addr.String()
	}
	if ip := net.ParseIP(host); host == "" || (ip != nil && ip.IsUnspecified()) {
		host = osHostname()
	}
	return net.JoinHostPort(host, port)
}

// installMirrorServeRoots is installAPIRootRoutes minus the
// registration face: the discovery roots list "query/" only, so a
// walking client never learns of a registration URL the mirror would
// refuse anyway.
func installMirrorServeRoots(srv *httpsession.Server, apiVers []string) {
	versionList := make([]string, 0, len(apiVers))
	for _, v := range apiVers {
		versionList = append(versionList, v+"/")
	}
	rootHandler := func(ctx context.Context, r *stdhttp.Request) (int, any, error) {
		return 0, []string{"query/"}, nil
	}
	srv.Handle(stdhttp.MethodGet, "/x-nmos", rootHandler)
	srv.Handle(stdhttp.MethodGet, "/x-nmos/", rootHandler)
	queryHandler := func(ctx context.Context, r *stdhttp.Request) (int, any, error) {
		return 0, versionList, nil
	}
	srv.Handle(stdhttp.MethodGet, "/x-nmos/query", queryHandler)
	srv.Handle(stdhttp.MethodGet, "/x-nmos/query/", queryHandler)
}

// serveStatusRecorder captures the status code the route table wrote
// so the audit event can report it. Plain REST responses only — WS
// upgrades never pass through it, so no Hijacker forwarding is needed.
type serveStatusRecorder struct {
	stdhttp.ResponseWriter
	status int
}

func (r *serveStatusRecorder) WriteHeader(status int) {
	r.status = status
	r.ResponseWriter.WriteHeader(status)
}

// serveQueryAnswered lands the per-REST-request audit event + counter.
func (m *Mirror) serveQueryAnswered(req *stdhttp.Request, status int) {
	m.mu.Lock()
	m.stats.ServedQueries++
	m.mu.Unlock()
	m.audit.event("serve_query", map[string]any{
		"method": req.Method, "path": req.URL.Path,
		"remote": req.RemoteAddr, "status": status,
	})
}

// serveWSOpened / serveWSClosed are the SubscriptionManager lifecycle
// hooks — one audit event per subscriber socket, each direction.
func (m *Mirror) serveWSOpened(resourcePath, remote string) {
	m.mu.Lock()
	m.stats.ServedWSSubs++
	m.mu.Unlock()
	m.audit.event("serve_ws_subscribe", map[string]any{"topic": resourcePath, "remote": remote})
}

func (m *Mirror) serveWSClosed(resourcePath, remote string) {
	m.audit.event("serve_ws_close", map[string]any{"topic": resourcePath, "remote": remote})
}

// serveWriteRejected records a refused registration attempt against
// the read-only face.
func (m *Mirror) serveWriteRejected(req *stdhttp.Request) {
	m.mu.Lock()
	m.stats.ServedRejects++
	m.mu.Unlock()
	m.audit.event("mirror_write_rejected", map[string]any{
		"method": req.Method, "path": req.URL.Path, "remote": req.RemoteAddr,
	})
}

// applyServeRow writes one grain row through to the embedded store —
// the same ingest/delete functions the Registration API handlers use,
// so the store fans the change out to WS subscribers as grains.
//
// allowReplay gates the parent-ordering recovery: the six topic
// streams race, so a device can reach the store before its node; a
// debounced ordered replay of the cache repairs that. The replay pass
// itself runs with allowReplay=false — in dependency order a failure
// is a genuine reject, and rescheduling would loop forever.
func (m *Mirror) applyServeRow(topic string, row is04.GrainDataRow, allowReplay bool) {
	if m.serve == nil {
		return
	}
	t, ok := singularFromPlural(topic)
	if !ok {
		m.logger.Warn("registry/mirror: serve: unknown topic", "topic", topic)
		return
	}
	switch row.Kind() {
	case is04.ChangeAdded, is04.ChangeModified:
		env := &is04.RegistrationRequest{Type: t, Data: row.Post}
		if err := m.serve.store.IngestRegistrationVersioned(env, m.opts.APIVer); err != nil {
			m.logger.Warn("registry/mirror: serve: store ingest failed",
				"topic", topic, "id", row.Path, "err", err)
			if allowReplay {
				m.scheduleServeReplay()
			}
		}
	case is04.ChangeRemoved:
		// ErrNotFound is fine: a node delete cascades in the store, so
		// the source's follow-up child-removal rows find nothing left.
		if err := m.serve.store.DeleteResource(t, row.Path); err != nil && !errors.Is(err, ErrNotFound) {
			m.logger.Warn("registry/mirror: serve: store delete failed",
				"topic", topic, "id", row.Path, "err", err)
		}
	}
}

// scheduleServeReplay arms (or extends) the debounced ordered replay —
// the embedded-store twin of scheduleResync.
func (m *Mirror) scheduleServeReplay() {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.runCtx == nil {
		return
	}
	if m.serveReplayTimer != nil {
		m.serveReplayTimer.Reset(mirrorResyncDebounce)
		return
	}
	m.serveReplayTimer = time.AfterFunc(mirrorResyncDebounce, func() {
		m.mu.Lock()
		m.serveReplayTimer = nil
		rctx := m.runCtx
		m.mu.Unlock()
		if rctx != nil && rctx.Err() == nil {
			m.serveReplay()
		}
	})
}

// serveReplay re-applies the whole cache to the embedded store in
// dependency order (node → device → source → flow → sender →
// receiver), so every parent precedes its children. Re-ingesting an
// unchanged resource is an idempotent update; subscribers may see a
// same-body modified grain on a replay, which is the honest rendering
// of "the mirror re-asserted its copy".
func (m *Mirror) serveReplay() {
	if m.serve == nil {
		return
	}
	m.mu.Lock()
	snapshot := make(map[string][]json.RawMessage, len(mirrorTopics))
	for _, topic := range mirrorTopics {
		docs := make([]json.RawMessage, 0, len(m.cache[topic]))
		for _, id := range sortedKeys(m.cache[topic]) {
			docs = append(docs, m.cache[topic][id])
		}
		snapshot[topic] = docs
	}
	m.mu.Unlock()
	for _, topic := range mirrorTopics {
		t, ok := singularFromPlural(topic)
		if !ok {
			continue
		}
		for _, doc := range snapshot[topic] {
			env := &is04.RegistrationRequest{Type: t, Data: doc}
			if err := m.serve.store.IngestRegistrationVersioned(env, m.opts.APIVer); err != nil {
				m.logger.Warn("registry/mirror: serve: replay ingest failed",
					"topic", topic, "err", err)
			}
		}
	}
}

// sortedKeys returns the map's keys in ascending order — deterministic
// replay order for tests + logs, same reason resync sorts.
func sortedKeys(docs map[string]json.RawMessage) []string {
	ids := make([]string, 0, len(docs))
	for id := range docs {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}
