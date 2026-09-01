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
	"os"
	"sort"
	"strings"
	"time"

	"log/slog"
	"strconv"

	codec "dhs/internal/amwa/codec/dnssd"
	"dhs/internal/amwa/codec/is04"
	authsession "dhs/internal/amwa/session/auth"
	session "dhs/internal/amwa/session/dnssd"
	httpsession "dhs/internal/amwa/session/http"
	registryslot "dhs/internal/registry"
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
	// The advertise identity minted into ws_href (and announced via
	// mDNS below). Precedence: an explicit --serve-advertise-host wins
	// outright — a bare host is completed with the bound serve port;
	// otherwise the bound address decides (concrete IP advertises
	// itself, unspecified bind falls back to the OS hostname).
	advertise := serveAdvertise(ln.Addr())
	if m.opts.ServeAdvertiseHost != "" {
		advertise = m.opts.ServeAdvertiseHost
		if _, _, err := net.SplitHostPort(advertise); err != nil {
			if _, boundPort, perr := net.SplitHostPort(ln.Addr().String()); perr == nil {
				advertise = net.JoinHostPort(advertise, boundPort)
			}
		}
	}
	store := NewStore()
	apiVers := pickAPIVersions("")
	srv := httpsession.NewServer(m.logger)
	// BCP-003-02 gate on the SERVED face only (issue #946) — the same
	// KeyCache + AuthGate wiring Registry.Serve arms with --auth-url.
	// The route table is covered via srv.Auth; the dispatcher branches
	// that bypass the route table (registration block, WS upgrades) are
	// gated explicitly below, mirroring registry.go's dispatcher.
	var authGate *httpsession.AuthGate
	if m.opts.ServeAuthURL != "" {
		kc := authsession.NewKeyCache(authsession.MetadataURL(m.opts.ServeAuthURL, ""), m.logger)
		if err := kc.Fetch(ctx); err != nil {
			m.logger.Warn("registry/mirror: initial JWKS fetch failed; served requests will 401 until keys arrive", "err", err)
		}
		go kc.Run(ctx)
		gateHosts := []string{advertiseHostOnly(advertise)}
		if hn, err := os.Hostname(); err == nil && hn != "" {
			gateHosts = append(gateHosts, hn, hn+".local")
		}
		authGate = &httpsession.AuthGate{Keys: kc, Hosts: gateHosts, Logger: m.logger}
		srv.Auth = authGate
	}
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
	// denyUnauthed applies the armed gate on the dispatcher branches
	// that bypass the route table (where srv.Auth lives). True means
	// the denial has been written — same body shape registry.go's WS
	// branch emits. Disarmed gate denies nothing.
	denyUnauthed := func(w stdhttp.ResponseWriter, req *stdhttp.Request) bool {
		if authGate == nil {
			return false
		}
		status, hdrs, body, _, ok := authGate.Check(req)
		if ok {
			return false
		}
		for hk, hv := range hdrs {
			w.Header().Set(hk, hv)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(body)
		return true
	}
	dispatcher := stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, req *stdhttp.Request) {
		if req.URL.Path == "/x-nmos/registration" || strings.HasPrefix(req.URL.Path, "/x-nmos/registration/") {
			// The armed face answers 401 before revealing anything —
			// even the "read-only mirror" refusal is behind the gate.
			if denyUnauthed(w, req) {
				return
			}
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
					// The spec says a server SHALL NOT upgrade on an
					// invalid token, and this branch bypasses the route
					// table where srv.Auth lives — gate it explicitly,
					// exactly like Registry.Serve's dispatcher.
					if denyUnauthed(w, req) {
						return
					}
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
	// DNS-SD announce of the served Query face (AMWA IS-04-02 test_02):
	// only with an operator-provided advertise identity — the
	// bound-address fallbacks (loopback binds, bare OS hostnames) are
	// exactly the identities off-link peers cannot resolve, and
	// announcing one of those helps nobody.
	if m.opts.ServeAdvertiseHost != "" {
		go m.announceServe(ctx, advertise, apiVers)
	}
	m.logger.Info("registry/mirror: serving mirrored Query API",
		"addr", ln.Addr().String(), "api_vers", apiVers,
		"auth", m.opts.ServeAuthURL != "")
	return nil
}

// newServeResponder is the mDNS seam — production uses the same
// session/dnssd backend detection Registry.Serve does; unit tests
// inject a recording fake (same seam pattern as osHostnameFn).
var newServeResponder = func(logger *slog.Logger) (session.Responder, error) {
	return session.NewResponder(logger)
}

// announceServe announces the served Query face as _nmos-query._tcp
// via the same Responder machinery Registry.Serve uses and keeps the
// announce alive until ctx ends. mDNS being unavailable is a warning,
// not a mirror failure — the REST face works without it.
func (m *Mirror) announceServe(ctx context.Context, advertise string, apiVers []string) {
	host, port, err := pickAdvertiseHostPort(registryslot.ServeOptions{AdvertiseHost: advertise})
	if err != nil {
		m.logger.Warn("registry/mirror: serve announce: bad advertise host",
			"advertise", advertise, "err", err)
		return
	}
	resp, err := newServeResponder(m.logger)
	if err != nil {
		m.logger.Warn("registry/mirror: serve announce: mDNS unavailable", "err", err)
		return
	}
	defer func() { _ = resp.Close() }()
	ins := serveAnnounceInstance(host, port, apiVers, m.opts.ServePri, m.opts.ServeAuthURL != "")
	if err := resp.Announce(ctx, ins); err != nil {
		m.logger.Warn("registry/mirror: serve announce failed", "err", err)
		return
	}
	m.logger.Info("registry/mirror: mDNS announce active for served Query API",
		"host", host, "port", port, "pri", m.opts.ServePri)
	<-ctx.Done()
}

// serveAnnounceInstance builds the served face's one DNS-SD instance:
// _nmos-query._tcp ONLY — the mirror's Query face is real, and there
// is deliberately no _nmos-register._tcp twin because the mirror
// refuses registrations. The TXT set mirrors Registry.Serve's
// announce: api_proto (the served face speaks plain HTTP today),
// api_ver per served minor, api_auth tracking the Bearer gate (#946),
// and pri — CLI-defaulted to 100, the dev range, so the plant mirror
// never wins a production Registry election against its own source
// registry at pri 0.
func serveAnnounceInstance(host string, port uint16, apiVers []string, pri int, auth bool) codec.Instance {
	if pri < 0 {
		pri = 0
	}
	return codec.Instance{
		Name:    "dhs-nmos-mirror",
		Service: codec.ServiceQuery,
		Domain:  codec.DefaultDomain,
		Host:    host,
		Port:    port,
		IPv4:    localIPv4Candidates(host),
		TXT: map[string]string{
			codec.TXTKeyAPIProto: "http",
			codec.TXTKeyAPIVer:   strings.Join(apiVers, ","),
			codec.TXTKeyAPIAuth:  strconv.FormatBool(auth),
			codec.TXTKeyPriority: strconv.Itoa(pri),
		},
	}
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

// advertiseHostOnly strips the port from a host:port advertise string —
// the auth gate's aud matching wants host identities, not endpoints.
func advertiseHostOnly(advertise string) string {
	host, _, err := net.SplitHostPort(advertise)
	if err != nil {
		return advertise
	}
	return host
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
// so the store fans the change out to WS subscribers as grains. ver is
// the resource's registered wire minor (the minor of the source
// subscription the row arrived on) — stamping it here is what keeps
// the IS-04 §6.1.5 downgrade view intact across the mirrored hop: a
// v1.0-registered resource must stay hidden from an un-downgraded
// v1.3 read of the served face and appear with query.downgrade=v1.0,
// exactly as it would on the source (AMWA IS-04-02 test_22/test_32).
//
// allowReplay gates the parent-ordering recovery: the six topic
// streams race, so a device can reach the store before its node; a
// debounced ordered replay of the cache repairs that. The replay pass
// itself runs with allowReplay=false — in dependency order a failure
// is a genuine reject, and rescheduling would loop forever.
func (m *Mirror) applyServeRow(topic, ver string, row is04.GrainDataRow, allowReplay bool) {
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
		if err := m.serve.store.IngestRegistrationVersioned(env, ver); err != nil {
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
// unchanged resource is a true no-op: the store's identical-put
// short-circuit emits NO grain for a byte-identical document, so a
// replay never leaks a same-body "modified" grain to subscribers
// (IS-04 §5.2 gives `pre` to modified/removed only — AMWA IS-04-02
// test_24_1 fails on anything else).
func (m *Mirror) serveReplay() {
	if m.serve == nil {
		return
	}
	type verDoc struct {
		ver string
		doc json.RawMessage
	}
	m.mu.Lock()
	snapshot := make(map[string][]verDoc, len(mirrorTopics))
	for _, topic := range mirrorTopics {
		docs := make([]verDoc, 0, len(m.cache[topic]))
		for _, id := range sortedKeys(m.cache[topic]) {
			ver := m.cacheVer[topic][id]
			if ver == "" {
				ver = m.opts.APIVer
			}
			docs = append(docs, verDoc{ver: ver, doc: m.cache[topic][id]})
		}
		snapshot[topic] = docs
	}
	m.mu.Unlock()
	for _, topic := range mirrorTopics {
		t, ok := singularFromPlural(topic)
		if !ok {
			continue
		}
		for _, vd := range snapshot[topic] {
			env := &is04.RegistrationRequest{Type: t, Data: vd.doc}
			// Re-stamp at the minor the row originally arrived on —
			// a replay must not flatten the downgrade view.
			if err := m.serve.store.IngestRegistrationVersioned(env, vd.ver); err != nil {
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
