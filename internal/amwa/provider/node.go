package provider

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	stdhttp "net/http"
	"strconv"
	"sync"
	"sync/atomic"

	dnssdcodec "acp/internal/amwa/codec/dnssd"
	"acp/internal/amwa/codec/is04"
	dnssdsession "acp/internal/amwa/session/dnssd"
	httpsession "acp/internal/amwa/session/http"
)

// IS04NodeConfig is the runtime config for the Node API server. Bind
// address, advertise host, mDNS mode, priority — separate from
// NodeConfig (the file-loaded resource bundle).
type IS04NodeConfig struct {
	Bind          string
	AdvertiseHost string
	DiscoveryMode string // "mdns" | "static"
	Priority      int
	APIVer        string // default "v1.3"

	// RegistryURL — when non-empty the producer also registers itself
	// against this Registration API base (e.g. http://10.6.239.113:8235/).
	// When empty, mDNS-only / direct-Node mode (Mode D peers).
	RegistryURL string
}

// IS04NodeServer hosts the Node API endpoints + DNS-SD announce +
// (optionally) the registration client.
type IS04NodeServer struct {
	logger *slog.Logger
	cfg    IS04NodeConfig
	bundle *NodeConfig

	mu        sync.Mutex
	http      *httpsession.Server
	responder *dnssdsession.Responder
	cancel    context.CancelFunc
	regClient *RegistrationClient

	// Per-endpoint hit counters.
	indexHits     uint64
	selfHits      uint64
	deviceHits    uint64
	sourceHits    uint64
	flowHits      uint64
	senderHits    uint64
	receiverHits  uint64
}

// NewIS04NodeServer validates the Node bundle and prepares (but does
// not start) the server.
func NewIS04NodeServer(logger *slog.Logger, bundle *NodeConfig, cfg IS04NodeConfig) (*IS04NodeServer, error) {
	if logger == nil {
		logger = slog.Default()
	}
	if bundle == nil {
		return nil, errors.New("provider/node: nil bundle")
	}
	if err := validateBundle(bundle); err != nil {
		return nil, fmt.Errorf("provider/node: %w", err)
	}
	if cfg.Bind == "" {
		return nil, errors.New("provider/node: Bind required")
	}
	if cfg.APIVer == "" {
		cfg.APIVer = is04.APIVersion
	}
	return &IS04NodeServer{logger: logger, cfg: cfg, bundle: bundle}, nil
}

// Serve binds the HTTP listener, optionally announces via DNS-SD,
// optionally starts the Registration client, and blocks until ctx is
// cancelled.
func (s *IS04NodeServer) Serve(ctx context.Context) error {
	s.mu.Lock()
	if s.http != nil {
		s.mu.Unlock()
		return errors.New("provider/node: already serving")
	}

	srv := httpsession.NewServer(s.logger)
	s.installRoutes(srv)
	s.http = srv

	// DNS-SD announce.
	if s.cfg.DiscoveryMode == "" || s.cfg.DiscoveryMode == "mdns" {
		host, port := splitHostPort(s.cfg.AdvertiseHost, s.cfg.Bind)
		resp, err := dnssdsession.NewResponder(s.logger)
		if err != nil {
			s.mu.Unlock()
			return fmt.Errorf("provider/node: open mDNS responder: %w", err)
		}
		ctxAnnounce, cancel := context.WithCancel(ctx)
		s.responder = resp
		s.cancel = cancel
		ins := dnssdcodec.Instance{
			Name:    "dhs-nmos-node",
			Service: dnssdcodec.ServiceNode,
			Domain:  dnssdcodec.DefaultDomain,
			Host:    host,
			Port:    uint16(port),
			TXT: map[string]string{
				dnssdcodec.TXTKeyAPIProto: "http",
				dnssdcodec.TXTKeyAPIVer:   s.cfg.APIVer,
				dnssdcodec.TXTKeyAPIAuth:  "false",
				dnssdcodec.TXTKeyPriority: strconv.Itoa(s.cfg.Priority),
			},
		}
		if err := resp.Announce(ctxAnnounce, ins); err != nil {
			s.mu.Unlock()
			_ = resp.Close()
			return fmt.Errorf("provider/node: announce %s: %w", dnssdcodec.ServiceNode, err)
		}
		s.logger.Info("provider/node: mDNS announce active",
			"service", dnssdcodec.ServiceNode, "host", host, "port", port,
			"api_ver", s.cfg.APIVer, "pri", s.cfg.Priority)
	}

	// Registration client (optional).
	if s.cfg.RegistryURL != "" {
		rc := NewRegistrationClient(s.logger, s.cfg.RegistryURL, s.cfg.APIVer, s.bundle)
		s.regClient = rc
		go rc.Run(ctx)
	}

	s.mu.Unlock()
	return srv.Serve(ctx, s.cfg.Bind)
}

// Stop tears down everything. Idempotent.
func (s *IS04NodeServer) Stop() error {
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
	if s.regClient != nil {
		_ = s.regClient.Close()
	}
	return nil
}

// Stats exposes per-endpoint hit counters.
func (s *IS04NodeServer) Stats() map[string]uint64 {
	return map[string]uint64{
		"index":     atomic.LoadUint64(&s.indexHits),
		"self":      atomic.LoadUint64(&s.selfHits),
		"devices":   atomic.LoadUint64(&s.deviceHits),
		"sources":   atomic.LoadUint64(&s.sourceHits),
		"flows":     atomic.LoadUint64(&s.flowHits),
		"senders":   atomic.LoadUint64(&s.senderHits),
		"receivers": atomic.LoadUint64(&s.receiverHits),
	}
}

// installRoutes wires every IS-04 v1.3 Node API endpoint into the HTTP
// router. PUT /receivers/{id}/target is intentionally 501 — peers
// should use IS-05 connection management instead.
func (s *IS04NodeServer) installRoutes(srv *httpsession.Server) {
	base := "/x-nmos/node/" + s.cfg.APIVer

	srv.Handle(stdhttp.MethodGet, base+"/", func(ctx context.Context, r *stdhttp.Request) (int, any, error) {
		atomic.AddUint64(&s.indexHits, 1)
		return 0, []string{"self/", "devices/", "sources/", "flows/", "senders/", "receivers/"}, nil
	})
	srv.Handle(stdhttp.MethodGet, base+"/self", func(ctx context.Context, r *stdhttp.Request) (int, any, error) {
		atomic.AddUint64(&s.selfHits, 1)
		s.mu.Lock()
		defer s.mu.Unlock()
		return 0, &s.bundle.Node, nil
	})

	// Devices, Sources, Flows, Senders, Receivers each have a list
	// endpoint and a per-id endpoint. We register the list, and let
	// the dispatcher 404 on unknown ids — there's no wildcard support
	// in the route table, so per-id endpoints are installed at server
	// start by walking the bundle.
	s.installCollection(srv, base, "devices", func() any {
		s.mu.Lock(); defer s.mu.Unlock(); atomic.AddUint64(&s.deviceHits, 1); return s.bundle.Devices
	}, func(id string) (any, bool) {
		s.mu.Lock(); defer s.mu.Unlock()
		for i := range s.bundle.Devices {
			if s.bundle.Devices[i].ID == id {
				return &s.bundle.Devices[i], true
			}
		}
		return nil, false
	}, idsFromDevices(s.bundle.Devices))

	s.installCollection(srv, base, "sources", func() any {
		s.mu.Lock(); defer s.mu.Unlock(); atomic.AddUint64(&s.sourceHits, 1); return s.bundle.Sources
	}, func(id string) (any, bool) {
		s.mu.Lock(); defer s.mu.Unlock()
		for i := range s.bundle.Sources {
			if s.bundle.Sources[i].ID == id {
				return &s.bundle.Sources[i], true
			}
		}
		return nil, false
	}, idsFromSources(s.bundle.Sources))

	s.installCollection(srv, base, "flows", func() any {
		s.mu.Lock(); defer s.mu.Unlock(); atomic.AddUint64(&s.flowHits, 1); return s.bundle.Flows
	}, func(id string) (any, bool) {
		s.mu.Lock(); defer s.mu.Unlock()
		for i := range s.bundle.Flows {
			if s.bundle.Flows[i].ID == id {
				return &s.bundle.Flows[i], true
			}
		}
		return nil, false
	}, idsFromFlows(s.bundle.Flows))

	s.installCollection(srv, base, "senders", func() any {
		s.mu.Lock(); defer s.mu.Unlock(); atomic.AddUint64(&s.senderHits, 1); return s.bundle.Senders
	}, func(id string) (any, bool) {
		s.mu.Lock(); defer s.mu.Unlock()
		for i := range s.bundle.Senders {
			if s.bundle.Senders[i].ID == id {
				return &s.bundle.Senders[i], true
			}
		}
		return nil, false
	}, idsFromSenders(s.bundle.Senders))

	s.installCollection(srv, base, "receivers", func() any {
		s.mu.Lock(); defer s.mu.Unlock(); atomic.AddUint64(&s.receiverHits, 1); return s.bundle.Receivers
	}, func(id string) (any, bool) {
		s.mu.Lock(); defer s.mu.Unlock()
		for i := range s.bundle.Receivers {
			if s.bundle.Receivers[i].ID == id {
				return &s.bundle.Receivers[i], true
			}
		}
		return nil, false
	}, idsFromReceivers(s.bundle.Receivers))

	// PUT /receivers/{id}/target → 501 Not Implemented (deprecated;
	// use IS-05 instead). Fired per-id so the path matches exactly.
	for _, r := range s.bundle.Receivers {
		path := base + "/receivers/" + r.ID + "/target"
		srv.Handle(stdhttp.MethodPut, path, func(ctx context.Context, r *stdhttp.Request) (int, any, error) {
			return stdhttp.StatusNotImplemented, httpsession.ErrorBody{
				Code:  stdhttp.StatusNotImplemented,
				Error: "Not Implemented",
				Debug: "PUT /receivers/{id}/target deprecated in IS-04 v1.3 — use IS-05 instead",
			}, nil
		})
	}
}

// installCollection registers GET <base>/<plural> and per-id GETs.
func (s *IS04NodeServer) installCollection(
	srv *httpsession.Server, base, plural string,
	listFn func() any, getFn func(string) (any, bool), ids []string,
) {
	srv.Handle(stdhttp.MethodGet, base+"/"+plural, func(ctx context.Context, r *stdhttp.Request) (int, any, error) {
		return 0, listFn(), nil
	})
	for _, id := range ids {
		path := base + "/" + plural + "/" + id
		srv.Handle(stdhttp.MethodGet, path, func(ctx context.Context, r *stdhttp.Request) (int, any, error) {
			body, ok := getFn(id)
			if !ok {
				return stdhttp.StatusNotFound, httpsession.ErrorBody{
					Code: stdhttp.StatusNotFound, Error: "Not Found", Debug: id,
				}, nil
			}
			return 0, body, nil
		})
	}
}

func idsFromDevices(in []is04.Device) []string {
	out := make([]string, len(in))
	for i, d := range in {
		out[i] = d.ID
	}
	return out
}
func idsFromSources(in []is04.Source) []string {
	out := make([]string, len(in))
	for i, s := range in {
		out[i] = s.ID
	}
	return out
}
func idsFromFlows(in []is04.Flow) []string {
	out := make([]string, len(in))
	for i, f := range in {
		out[i] = f.ID
	}
	return out
}
func idsFromSenders(in []is04.Sender) []string {
	out := make([]string, len(in))
	for i, s := range in {
		out[i] = s.ID
	}
	return out
}
func idsFromReceivers(in []is04.Receiver) []string {
	out := make([]string, len(in))
	for i, r := range in {
		out[i] = r.ID
	}
	return out
}
