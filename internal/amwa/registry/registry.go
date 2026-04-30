// Package registry is the Layer-3 NMOS Registry plugin — the
// dual-face middleware. Phase 1 step #1 ships only the discovery
// surface: it announces _nmos-register._tcp + _nmos-query._tcp via
// mDNS so Controllers see a Registry presence on the link, but it
// does not yet implement the Registration API or Query API REST
// surface. Those land in Phase 1 step #4.
package registry

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	stdhttp "net/http"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	codec "acp/internal/amwa/codec/dnssd"
	session "acp/internal/amwa/session/dnssd"
	httpsession "acp/internal/amwa/session/http"
	registryslot "acp/internal/registry"
)

// Plugin name registered with internal/registry/.
const PluginName = "nmos"

// init wires this plugin into the Tier-1 registry slot. cmd/dhs/main.go
// blank-imports the package so the Register call fires at process
// start — same pattern as internal/<proto>/consumer + provider.
func init() {
	registryslot.Register(&Factory{})
}

// Factory implements registryslot.Factory.
type Factory struct{}

// Meta returns the plugin's static identity.
func (Factory) Meta() registryslot.Meta {
	return registryslot.Meta{
		Name:        PluginName,
		Description: "AMWA NMOS Registry — IS-04 Registration API + Query API (dual-face).",
		DefaultPort: 8235,
	}
}

// New constructs an unstarted Registry. Logger may be nil.
func (Factory) New(logger *slog.Logger) registryslot.Registry {
	return &Registry{logger: logger}
}

// Registry implements registryslot.Registry. Phase 1 step #1 scope:
// mDNS announce only — no HTTP listener yet. Stats reflect what we
// can measure at the discovery layer (announcements emitted).
type Registry struct {
	logger *slog.Logger

	mu         sync.Mutex
	responder  *session.Responder
	cancel     context.CancelFunc
	announced  []codec.Instance
	announces  uint64 // atomic

	// Phase 1 #4: real HTTP face + store + GC + WS subscriptions.
	store    *Store
	subs     *SubscriptionManager
	apiVer   string
	httpSrv  *httpsession.Server
}

// Serve advertises the Registry via mDNS, mounts the Registration +
// Query API HTTP faces, runs the GC loop, and blocks until ctx is
// cancelled. opts.DiscoveryMode controls whether mDNS announce fires:
//
//   - "mdns" / "" — announce on the link.
//   - "unicast" / "static" — skip announce; records published via
//     authoritative DNS (e.g. pfSense Unbound).
//
// Phase 1 step #4: full Registration + Query API REST + WS
// subscriptions on top of the in-memory Store.
func (r *Registry) Serve(ctx context.Context, opts registryslot.ServeOptions) error {
	mode := strings.ToLower(opts.DiscoveryMode)
	if mode == "" {
		mode = "mdns"
	}

	host, port, err := pickAdvertiseHostPort(opts)
	if err != nil {
		return err
	}
	priority := opts.Priority
	if priority < 0 {
		priority = 0
	}
	apiVer := opts.APIVer
	if apiVer == "" {
		apiVer = "v1.3"
	}
	r.apiVer = apiVer

	// Initialise store + subscription manager. AdvertiseHost is what
	// we publish in ws_href so peers can reach us.
	advertise := opts.AdvertiseHost
	if advertise == "" {
		advertise = fmt.Sprintf("%s:%d", host, port)
	}
	r.store = NewStore()
	r.subs = NewSubscriptionManager(r.logger, r.store, advertise, apiVer)

	// HTTP routes — Registration + Query.
	srv := httpsession.NewServer(r.logger)
	regBase := "/x-nmos/registration/" + apiVer
	queryBase := "/x-nmos/query/" + apiVer
	installRegistrationRoutes(srv, r.store, regBase)
	installQueryRoutes(srv, r.store, r.subs, queryBase)

	// WebSocket upgrade route — registered as a prefix-matched GET.
	wsPrefix := queryBase + "/subscriptions/"
	upgradeHandler := r.subs.UpgradeHandler(queryBase)
	srv.HandlePrefix(wsPrefix, stdhttp.MethodGet, func(ctx context.Context, req *stdhttp.Request) (int, any, error) {
		// We can't return through the route table for an Upgrade —
		// the handler hijacks the connection. Use the raw upgrade
		// path: install a separate raw http.Handler via httpsession's
		// raw escape hatch. For now, return 0 + a sentinel and rely
		// on the dispatcher to call the upgrade. Since the
		// dispatcher writes a JSON response we can't easily hijack,
		// we accept the limitation: fall back to a 501 — peers can
		// fetch the resource list via REST during this phase. WS
		// dispatch lives on a parallel net/http mux, see installWS.
		_ = upgradeHandler
		return stdhttp.StatusNotImplemented, httpsession.ErrorBody{
			Code: 501, Error: "Not Implemented",
			Debug: "WebSocket upgrade handled on parallel mux; this route should not be hit",
		}, nil
	})

	r.mu.Lock()
	r.httpSrv = srv
	r.mu.Unlock()

	// HTTP server starts in a goroutine — we own ctx for full lifecycle.
	httpCtx, httpCancel := context.WithCancel(ctx)
	defer httpCancel()
	if len(opts.BindAddrs) == 0 {
		return fmt.Errorf("registry/nmos: BindAddrs required (use \":0\" for OS-allocated)")
	}
	bindAddr := opts.BindAddrs[0]
	httpErrCh := make(chan error, 1)
	go func() {
		// Wrap the route table with a single dispatcher: any path
		// matching `/x-nmos/query/<ver>/subscriptions/<id>/ws` is a
		// WebSocket upgrade; everything else falls through to the
		// route table verbatim. We deliberately avoid net/http
		// ServeMux subtree patterns here — registering
		// `/subscriptions/` would trigger ServeMux's 301 redirect
		// for POSTs to `/subscriptions` (no trailing slash), and
		// net/http converts POST→GET on 301.
		routeTable := srv.MuxHandler()
		dispatcher := stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, req *stdhttp.Request) {
			if strings.HasPrefix(req.URL.Path, wsPrefix) && strings.HasSuffix(req.URL.Path, "/ws") {
				upgradeHandler(w, req)
				return
			}
			routeTable.ServeHTTP(w, req)
		})
		s := &stdhttp.Server{
			Addr:              bindAddr,
			Handler:           dispatcher,
			ReadHeaderTimeout: 5 * time.Second,
		}
		httpErrCh <- runHTTPServer(httpCtx, s)
	}()

	if mode == "mdns" {
		resp, err := session.NewResponder(r.logger)
		if err != nil {
			return fmt.Errorf("registry/nmos: open mDNS responder: %w", err)
		}
		instCtx, cancel := context.WithCancel(ctx)

		r.mu.Lock()
		r.responder = resp
		r.cancel = cancel
		r.mu.Unlock()

		// Advertise both faces — Registration (left) and Query (right).
		ips := localIPv4Candidates(host)
		for _, svc := range []string{codec.ServiceRegister, codec.ServiceQuery} {
			ins := codec.Instance{
				Name:    "dhs-nmos-registry",
				Service: svc,
				Domain:  codec.DefaultDomain,
				Host:    host,
				Port:    port,
				IPv4:    ips,
				TXT: map[string]string{
					codec.TXTKeyAPIProto: "http",
					codec.TXTKeyAPIVer:   apiVer,
					codec.TXTKeyAPIAuth:  "false",
					codec.TXTKeyPriority: strconv.Itoa(priority),
				},
			}
			if err := resp.Announce(instCtx, ins); err != nil {
				_ = resp.Close()
				return fmt.Errorf("registry/nmos: announce %s: %w", svc, err)
			}
			r.mu.Lock()
			r.announced = append(r.announced, ins)
			atomic.AddUint64(&r.announces, 1)
			r.mu.Unlock()
		}
		if r.logger != nil {
			r.logger.Info("registry/nmos: mDNS announce active",
				"host", host, "port", port, "pri", priority,
				"register", codec.ServiceRegister, "query", codec.ServiceQuery)
		}
	} else if r.logger != nil {
		r.logger.Info("registry/nmos: mDNS announce disabled", "mode", mode)
	}

	// GC loop — evict Nodes that miss heartbeats past threshold.
	go runGC(httpCtx, r.store, r.logger, opts.GCInterval, opts.HeartbeatTimeout)

	if r.logger != nil {
		r.logger.Info("registry/nmos: HTTP face active",
			"bind", bindAddr,
			"reg", regBase, "query", queryBase, "ws", wsPrefix)
	}

	// Block until ctx cancels OR the HTTP server crashes.
	select {
	case <-ctx.Done():
	case err := <-httpErrCh:
		return err
	}
	return r.Stop()
}

// runHTTPServer adapts a stdhttp.Server to ctx-cancel semantics —
// returns when the server exits.
func runHTTPServer(ctx context.Context, srv *stdhttp.Server) error {
	errCh := make(chan error, 1)
	go func() {
		err := srv.ListenAndServe()
		if err == stdhttp.ErrServerClosed {
			err = nil
		}
		errCh <- err
	}()
	select {
	case <-ctx.Done():
		shutCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutCtx)
		return <-errCh
	case err := <-errCh:
		return err
	}
}

// Stop closes the mDNS responder. Idempotent.
func (r *Registry) Stop() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.cancel != nil {
		r.cancel()
		r.cancel = nil
	}
	if r.responder != nil {
		err := r.responder.Close()
		r.responder = nil
		return err
	}
	return nil
}

// Stats returns the discovery-layer counters available so far.
// Resources / Subscriptions / Heartbeats / Registrations stay zero
// until the HTTP face lands in Phase 1 step #4.
func (r *Registry) Stats() registryslot.Stats {
	return registryslot.Stats{
		// Announcements is a non-standard metric, but we surface it
		// through the Registrations counter for now since the only
		// thing this scaffold actually does is announce.
		Registrations: atomic.LoadUint64(&r.announces),
	}
}

// pickAdvertiseHostPort resolves opts.AdvertiseHost (preferred) or
// derives host:port from opts.BindAddrs[0]. Returns ("", 0, err) on
// malformed input.
func pickAdvertiseHostPort(opts registryslot.ServeOptions) (string, uint16, error) {
	src := opts.AdvertiseHost
	if src == "" && len(opts.BindAddrs) > 0 {
		src = opts.BindAddrs[0]
	}
	if src == "" {
		return "", 0, fmt.Errorf("registry/nmos: no AdvertiseHost or BindAddrs configured")
	}
	host, portStr, err := net.SplitHostPort(src)
	if err != nil {
		return "", 0, fmt.Errorf("registry/nmos: split %q: %w", src, err)
	}
	port, err := strconv.Atoi(portStr)
	if err != nil || port <= 0 || port > 65535 {
		return "", 0, fmt.Errorf("registry/nmos: bad port %q", portStr)
	}
	if host == "" || host == "0.0.0.0" || host == "::" {
		host = osHostname()
	}
	if !strings.HasSuffix(host, ".") && !strings.Contains(host, ".") {
		host = host + "." + codec.DefaultDomain
	}
	return host, uint16(port), nil
}

// localIPv4Candidates returns one or more IPv4 addresses the SRV
// announce should advertise. If host already resolves to a literal IP
// we use it; otherwise we pick non-loopback addresses from the OS.
func localIPv4Candidates(host string) []net.IP {
	if ip := net.ParseIP(strings.TrimSuffix(host, ".")); ip != nil && ip.To4() != nil {
		return []net.IP{ip.To4()}
	}
	ifs, err := net.InterfaceAddrs()
	if err != nil {
		return nil
	}
	var out []net.IP
	for _, a := range ifs {
		if ipnet, ok := a.(*net.IPNet); ok {
			ip4 := ipnet.IP.To4()
			if ip4 == nil || ip4.IsLoopback() || ip4.IsLinkLocalUnicast() {
				continue
			}
			out = append(out, ip4)
		}
	}
	return out
}

// osHostname returns the local hostname; falls back to "localhost"
// when net.InterfaceAddrs / os.Hostname are unavailable.
func osHostname() string {
	if h, err := osHostnameFn(); err == nil && h != "" {
		return h
	}
	return "localhost"
}

// osHostnameFn is a package-level seam for tests.
var osHostnameFn = func() (string, error) { return netHostname() }

// netHostname wraps net.LookupCNAME via os.Hostname; we keep this
// thin so we can stub it in unit tests without touching the OS.
func netHostname() (string, error) {
	return defaultHostname()
}
