package provider

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	stdhttp "net/http"
	"strconv"
	"strings"
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
	watcher   *RegistryWatcher

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

	// Auto-derive api.endpoints from runtime: every non-loopback IPv4
	// the Node binds, plus the --advertise-host hostname. IS-04 v1.3.3
	// §4.2.2 mandates that this list reflects every protocol/IP/port the
	// Node is reachable on — and AMWA NMOS Testing test_20 enforces it
	// against whatever URL the test reaches us at.
	expandNodeEndpoints(&s.bundle.Node, s.cfg.AdvertiseHost, s.cfg.Bind)

	// Manifest URLs that we cannot honour (no /transportfile handler
	// shipped today) MUST be null on the wire — leaving a stale URL
	// fails AMWA test_20_01 and is also wrong per spec.
	clearUnservedManifestHrefs(s.bundle.Senders)

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

	// Registration: explicit URL wins (Mode B). Otherwise, when in
	// mDNS mode, browse `_nmos-register._tcp` and auto-register against
	// the highest-pri Registry — that's IS-04 §3.1 Mode A.
	if s.cfg.RegistryURL != "" {
		rc := NewRegistrationClient(s.logger, s.cfg.RegistryURL, s.cfg.APIVer, s.bundle)
		s.regClient = rc
		go rc.Run(ctx)
	} else if s.cfg.DiscoveryMode == "" || s.cfg.DiscoveryMode == "mdns" {
		w, err := NewRegistryWatcher(s.logger, s.cfg.APIVer)
		if err != nil {
			s.mu.Unlock()
			return fmt.Errorf("provider/node: open registry watcher: %w", err)
		}
		if err := w.Run(ctx); err != nil {
			_ = w.Close()
			s.mu.Unlock()
			return fmt.Errorf("provider/node: start registry watcher: %w", err)
		}
		s.watcher = w
		rc := NewRegistrationClient(s.logger, "", s.cfg.APIVer, s.bundle)
		rc.SetWatcher(w)
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

	// IS-04 §4 — parent listings. The Node API root advertises which
	// NMOS API trees this host serves (we expose only "node/"). Each
	// API tree's root then advertises the supported version subtrees.
	// AMWA NMOS Testing's auto_node_1/auto_node_2 require both.
	srv.Handle(stdhttp.MethodGet, "/x-nmos", func(ctx context.Context, r *stdhttp.Request) (int, any, error) {
		return 0, []string{"node/"}, nil
	})
	srv.Handle(stdhttp.MethodGet, "/x-nmos/", func(ctx context.Context, r *stdhttp.Request) (int, any, error) {
		return 0, []string{"node/"}, nil
	})
	srv.Handle(stdhttp.MethodGet, "/x-nmos/node", func(ctx context.Context, r *stdhttp.Request) (int, any, error) {
		return 0, []string{s.cfg.APIVer + "/"}, nil
	})
	srv.Handle(stdhttp.MethodGet, "/x-nmos/node/", func(ctx context.Context, r *stdhttp.Request) (int, any, error) {
		return 0, []string{s.cfg.APIVer + "/"}, nil
	})

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

	// PUT /receivers/{id}/target — IS-04 §4.3.1 legacy connection
	// control. v1.0/v1.1 mandated this; v1.2+ recommends IS-05 instead
	// but the path remains. Body is either a Sender JSON object (to
	// connect — sets subscription.sender_id + active=true) or an empty
	// {} (to disconnect — sets subscription.sender_id=null + active=
	// false). Returns 202 Accepted with the resulting Sender object
	// (or empty object on disconnect) per spec.
	for _, r := range s.bundle.Receivers {
		rid := r.ID
		path := base + "/receivers/" + rid + "/target"
		srv.Handle(stdhttp.MethodPut, path, func(ctx context.Context, req *stdhttp.Request) (int, any, error) {
			body, err := io.ReadAll(io.LimitReader(req.Body, 1<<16))
			if err != nil {
				return stdhttp.StatusBadRequest, httpsession.ErrorBody{
					Code: stdhttp.StatusBadRequest, Error: "Bad Request", Debug: err.Error(),
				}, nil
			}
			trimmed := strings.TrimSpace(string(body))
			s.mu.Lock()
			defer s.mu.Unlock()
			rcv := findReceiverByID(s.bundle.Receivers, rid)
			if rcv == nil {
				return stdhttp.StatusNotFound, httpsession.ErrorBody{
					Code: stdhttp.StatusNotFound, Error: "Not Found", Debug: rid,
				}, nil
			}
			// Empty body or `{}` ⇒ disconnect.
			if trimmed == "" || trimmed == "{}" {
				rcv.Subscription.SenderID = nil
				rcv.Subscription.Active = false
				return stdhttp.StatusAccepted, struct{}{}, nil
			}
			// Otherwise the payload is a Sender object — connect.
			sender, err := is04.DecodeSender(body)
			if err != nil {
				return stdhttp.StatusBadRequest, httpsession.ErrorBody{
					Code: stdhttp.StatusBadRequest, Error: "Bad Request", Debug: err.Error(),
				}, nil
			}
			id := sender.ID
			rcv.Subscription.SenderID = &id
			rcv.Subscription.Active = true
			return stdhttp.StatusAccepted, sender, nil
		})
	}
}

// findReceiverByID locates a Receiver in the slice by UUID. Returns nil
// when the id is unknown — callers must respond 404.
func findReceiverByID(rs []is04.Receiver, id string) *is04.Receiver {
	for i := range rs {
		if rs[i].ID == id {
			return &rs[i]
		}
	}
	return nil
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
