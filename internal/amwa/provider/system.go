// Layer-3 IS-09 System API provider — loads + serves a /global resource
// and announces _nmos-system._tcp via DNS-SD.
//
// Spec: AMWA NMOS IS-09 v1.0.0 (https://specs.amwa.tv/is-09/releases/v1.0.0/).
// Stays strictly to the published surface — two endpoints, no extras:
//
//   GET /x-nmos/system/v1.0/         -> ["global/"]
//   GET /x-nmos/system/v1.0/global   -> the Global resource

package provider

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	stdhttp "net/http"
	"os"
	"strconv"
	"sync"
	"sync/atomic"

	dnssdcodec "dhs/internal/amwa/codec/dnssd"
	"dhs/internal/amwa/codec/is09"
	dnssdsession "dhs/internal/amwa/session/dnssd"
	httpsession "dhs/internal/amwa/session/http"
)

// IS09Config wires the System provider together. APIVer overrides the
// default `v1.0`; rarely needed today.
type IS09Config struct {
	// Bind address for the HTTP listener, e.g. ":10641".
	Bind string

	// AdvertiseHost+port placed in DNS-SD A/SRV records. When empty
	// the provider falls back to os.Hostname.
	AdvertiseHost string

	// DiscoveryMode: "mdns" announces via mDNS (default); "static"
	// skips announce — for unicast-only / peer-list deployments where
	// pfSense Unbound (or another authoritative resolver) already
	// publishes the records (see internal/amwa/docs/dns-sd-unbound.md).
	DiscoveryMode string

	// Priority in the DNS-SD `pri` TXT (0-99 production, 100+ dev).
	Priority int

	// APIVer is the IS-09 wire version to advertise. Defaults to v1.0.
	APIVer string
}

// IS09Server hosts the IS-09 endpoints + DNS-SD announce.
type IS09Server struct {
	logger *slog.Logger
	cfg    IS09Config
	global *is09.Global

	mu        sync.Mutex
	http      *httpsession.Server
	responder dnssdsession.Responder
	cancel    context.CancelFunc

	// Counters (atomic).
	indexHits  uint64
	globalHits uint64
}

// NewIS09Server validates `g` against the IS-09 schema, then prepares
// (but does not start) a server bound to the given config.
func NewIS09Server(logger *slog.Logger, g *is09.Global, cfg IS09Config) (*IS09Server, error) {
	if logger == nil {
		logger = slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))
	}
	if g == nil {
		return nil, errors.New("provider/system: nil Global")
	}
	if err := g.Validate(); err != nil {
		return nil, fmt.Errorf("provider/system: %w", err)
	}
	if cfg.Bind == "" {
		return nil, errors.New("provider/system: Bind required")
	}
	if cfg.APIVer == "" {
		cfg.APIVer = is09.APIVersion
	}
	return &IS09Server{logger: logger, cfg: cfg, global: g}, nil
}

// LoadIS09GlobalFromFile reads + validates a JSON config file.
func LoadIS09GlobalFromFile(path string) (*is09.Global, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("provider/system: read %s: %w", path, err)
	}
	g, err := is09.Decode(raw)
	if err != nil {
		return nil, fmt.Errorf("provider/system: %s: %w", path, err)
	}
	return g, nil
}

// Serve binds the HTTP listener, announces via DNS-SD if configured,
// and blocks until ctx is cancelled.
func (s *IS09Server) Serve(ctx context.Context) error {
	s.mu.Lock()
	if s.http != nil {
		s.mu.Unlock()
		return errors.New("provider/system: already serving")
	}

	srv := httpsession.NewServer(s.logger)
	indexPath := "/x-nmos/system/" + s.cfg.APIVer + "/"
	globalPath := "/x-nmos/system/" + s.cfg.APIVer + "/global"

	srv.Handle(stdhttp.MethodGet, indexPath, func(ctx context.Context, r *stdhttp.Request) (int, any, error) {
		atomic.AddUint64(&s.indexHits, 1)
		return 0, is09.IndexBody(), nil
	})
	srv.Handle(stdhttp.MethodGet, globalPath, func(ctx context.Context, r *stdhttp.Request) (int, any, error) {
		atomic.AddUint64(&s.globalHits, 1)
		// Re-marshal each call so a future mutating API can swap g
		// safely under a reader lock.
		s.mu.Lock()
		body := *s.global
		s.mu.Unlock()
		return 0, &body, nil
	})

	s.http = srv

	// Optional DNS-SD announce. Skipped in "static" mode — caller
	// publishes records via Unbound or similar.
	if s.cfg.DiscoveryMode == "" || s.cfg.DiscoveryMode == "mdns" {
		host, port := splitHostPort(s.cfg.AdvertiseHost, s.cfg.Bind)
		resp, err := dnssdsession.NewResponder(s.logger)
		if err != nil {
			s.mu.Unlock()
			return fmt.Errorf("provider/system: open mDNS responder: %w", err)
		}
		ctxAnnounce, cancel := context.WithCancel(ctx)
		s.responder = resp
		s.cancel = cancel
		ins := dnssdcodec.Instance{
			Name:    "dhs-nmos-system",
			Service: dnssdcodec.ServiceSystem,
			Domain:  dnssdcodec.DefaultDomain,
			Host:    host,
			Port:    uint16(port),
			TXT: map[string]string{
				dnssdcodec.TXTKeyAPIProto: "http",
				dnssdcodec.TXTKeyAPIVer:   s.cfg.APIVer,
				dnssdcodec.TXTKeyPriority: strconv.Itoa(s.cfg.Priority),
				// IS-09 v1.0 §3.1 does NOT define api_auth (predates
				// IS-10) — emit only what the spec lists.
			},
		}
		if err := resp.Announce(ctxAnnounce, ins); err != nil {
			s.mu.Unlock()
			_ = resp.Close()
			return fmt.Errorf("provider/system: announce %s: %w", dnssdcodec.ServiceSystem, err)
		}
		s.logger.Info("provider/system: mDNS announce active",
			"service", dnssdcodec.ServiceSystem, "host", host, "port", port, "api_ver", s.cfg.APIVer, "pri", s.cfg.Priority)
	} else {
		s.logger.Info("provider/system: DNS-SD announce disabled by config",
			"mode", s.cfg.DiscoveryMode)
	}

	s.mu.Unlock()
	return srv.Serve(ctx, s.cfg.Bind)
}

// Stop tears down both surfaces. Idempotent.
func (s *IS09Server) Stop() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cancel != nil {
		s.cancel()
		s.cancel = nil
	}
	if s.responder != nil {
		_ = s.responder.Close()
		s.responder = nil
	}
	return nil
}

// Stats exposes per-endpoint hit counters for observability.
func (s *IS09Server) Stats() (indexHits, globalHits uint64) {
	return atomic.LoadUint64(&s.indexHits), atomic.LoadUint64(&s.globalHits)
}

// MarshalGlobal renders the current Global as compact JSON. Used by
// debugging tools and tests.
func (s *IS09Server) MarshalGlobal() ([]byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return json.Marshal(s.global)
}

// splitHostPort returns the host advertised in DNS-SD records + the
// numeric port to put in the SRV record. AdvertiseHost wins over Bind
// for the host string; the port comes from whichever is set.
func splitHostPort(advertise, bind string) (string, int) {
	if advertise != "" {
		host, port := splitHP(advertise)
		if host != "" && port > 0 {
			return host, port
		}
		// AdvertiseHost without port — fall through to bind for port.
		if host != "" {
			_, bindPort := splitHP(bind)
			return host, bindPort
		}
	}
	host, port := splitHP(bind)
	if host == "" || host == "0.0.0.0" || host == "::" {
		if h, err := os.Hostname(); err == nil && h != "" {
			host = h
		} else {
			host = "localhost"
		}
	}
	return host, port
}

// splitHP is the lenient host:port splitter — bare host returns port 0,
// bare ":port" returns "" host.
func splitHP(s string) (string, int) {
	host := ""
	portStr := ""
	for i := len(s) - 1; i >= 0; i-- {
		if s[i] == ':' {
			host = s[:i]
			portStr = s[i+1:]
			break
		}
	}
	if portStr == "" {
		return s, 0
	}
	port, err := strconv.Atoi(portStr)
	if err != nil {
		return host, 0
	}
	return host, port
}
