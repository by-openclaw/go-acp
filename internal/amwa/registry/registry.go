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
	"strconv"
	"strings"
	"sync"
	"sync/atomic"

	codec "acp/internal/amwa/codec/dnssd"
	session "acp/internal/amwa/session/dnssd"
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
}

// Serve advertises the Registry via mDNS and blocks until ctx is
// cancelled. opts.DiscoveryMode controls whether mDNS announce fires:
//
//   - "mdns" / "" — announce on the link.
//   - "unicast" / "static" — skip announce; the Registry is reached
//     via configured peers and does not advertise itself.
//
// Phase 1 step #1 does not start an HTTP listener; the BindAddrs and
// AdvertiseHost are interpreted only for the SRV records.
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
					codec.TXTKeyAPIVer:   "v1.3",
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
		r.logger.Info("registry/nmos: mDNS announce disabled by --discovery", "mode", mode)
	}

	<-ctx.Done()
	return r.Stop()
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
