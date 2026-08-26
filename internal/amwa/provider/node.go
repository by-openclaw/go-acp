package provider

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	stdhttp "net/http"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"

	dnssdcodec "dhs/internal/amwa/codec/dnssd"
	"dhs/internal/amwa/codec/is04"
	"dhs/internal/amwa/codec/is09"
	dnssdsession "dhs/internal/amwa/session/dnssd"
	httpsession "dhs/internal/amwa/session/http"
)

// encodeOne wraps a per-resource codec Encode method into a
// json.RawMessage suitable for handing back to the HTTP framework.
//
// A resource the served minor cannot express returns NIL, and the
// caller turns that into a 404.
//
// It used to return a JSON `null` at 200, on the theory that a
// schema-validation test would catch it. It does -- as an
// unattributable "Response schema validation error" three minors
// deep, which is a slow way to learn something the server already
// knew. And the behaviour is wrong on its own terms: the LIST endpoint
// drops a resource the minor cannot express, so serving it
// individually advertises a resource the Node does not list, and
// answers "here it is" with nothing.
func encodeOne[T any](enc func(T) ([]byte, error), v T) json.RawMessage {
	body, err := enc(v)
	if err != nil {
		return nil
	}
	return json.RawMessage(body)
}

// encodeList encodes every element of a slice via enc and returns a
// JSON array of the resulting blobs. Elements that fail to encode are
// dropped from the array — the alternative (returning null in-place)
// would corrupt the array's element type.
func encodeList[T any](enc func(T) ([]byte, error), in []T) json.RawMessage {
	out := []byte{'['}
	first := true
	for _, v := range in {
		body, err := enc(v)
		if err != nil {
			continue
		}
		if !first {
			out = append(out, ',')
		}
		out = append(out, body...)
		first = false
	}
	out = append(out, ']')
	return json.RawMessage(out)
}

// nodeInstanceName returns the DNS-SD instance name for a Node's
// _nmos-node._tcp announce. RFC 6763 §4.1.1 wants instance names to be
// human-readable AND unique on the link — using the Node's label
// satisfies both. Falls back to "dhs-nmos-node" only when the label
// is empty (config bug, but don't crash).
func nodeInstanceName(label string) string {
	if label == "" {
		return "dhs-nmos-node"
	}
	return label
}

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

	// NoConnectionAPI suppresses IS-05. Opt-OUT rather than opt-in: a
	// Node with senders and receivers but no Connection API is a valid
	// IS-04 Node that no controller can route, so the useful default
	// is to serve it.
	NoConnectionAPI bool

	// ConnectionAPIVer pins IS-05 to one wire minor. Empty mounts
	// every registered minor in parallel, which is what a real product
	// does — a v1.0-pinned controller and a v1.2 one must each find a
	// tree they can speak.
	ConnectionAPIVer string

	// NoChannelMappingAPI suppresses IS-08. Same opt-OUT reasoning as
	// IS-05: a multi-channel audio Node with no Channel Mapping API
	// can be connected but not patched, so the operator can only route
	// whole streams and never a single channel.
	NoChannelMappingAPI bool

	// ChannelMappingAPIVer pins IS-08 to one wire minor. Empty mounts
	// every registered minor.
	ChannelMappingAPIVer string

	// NoEventsAPI suppresses IS-07. Same opt-OUT reasoning again: a
	// Node with data Sources and no Event & Tally API publishes tally
	// state nothing can read.
	NoEventsAPI bool

	// EventsAPIVer pins IS-07 to one wire minor.
	EventsAPIVer string

	// SystemURL names an IS-09 System API as `host:port`, skipping
	// discovery. Empty means browse for one.
	SystemURL string

	// NoRegistry keeps the Node out of any Registry: no explicit
	// registration, and no browsing for one.
	//
	// This is Mode D (mDNS direct-Node) from internal/amwa/CLAUDE.md,
	// and it is a real deployment, not a test affordance -- EVS
	// Cerebrum runs registry-less peer-to-peer. It matters because
	// IS-04 §4.2.1 makes the two modes mutually exclusive on the wire:
	// a Node that has registered MUST stop advertising
	// _nmos-node._tcp, so a Node allowed to find a Registry cannot
	// also be a peer-to-peer Node.
	NoRegistry bool
}

// IS04NodeServer hosts the Node API endpoints + DNS-SD announce +
// (optionally) the registration client.
type IS04NodeServer struct {
	logger *slog.Logger
	cfg    IS04NodeConfig
	bundle *NodeConfig
	// codec encodes every Node-API response in the wire shape for the
	// configured api_ver. Without this downcast, GET /x-nmos/node/v1.0/...
	// would return the canonical (v1.3) JSON shape, which carries fields
	// (interfaces, attached_network_device, controls, caps.constraint_sets,
	// etc.) that the older minor's schema rejects — AMWA NMOS Testing
	// auto_node_11/12 fail "Response schema validation error" without it.
	// See root CLAUDE.md "AMWA NMOS strict" and #192.
	codec is04.Codec

	// connection is the IS-05 Connection API served alongside the Node
	// API. Nil disables it — a Node that only advertises resources is
	// still a valid IS-04 Node, it just cannot be routed.
	connection *IS05ConnectionServer

	// channelMapping is the IS-08 Channel Mapping API. Nil disables
	// it; a Node with no audio inputs or outputs gets an empty one,
	// which is the honest answer rather than a missing API.
	channelMapping *IS08ChannelMappingServer

	// events is the IS-07 Event & Tally API. Nil disables it.
	events *IS07EventsServer

	// systemGlobal is what an IS-09 System API told this Node at
	// startup, or nil if none answered. Guarded by mu.
	systemGlobal *is09.Global

	mu        sync.Mutex
	http      *httpsession.Server
	responder dnssdsession.Responder
	cancel    context.CancelFunc
	regClient *RegistrationClient
	watcher   *RegistryWatcher

	// announceInstance + announceCtx are kept around so the mDNS
	// _nmos-node._tcp announce can be torn down on registration
	// success (IS-04 §4.2.1: a registered Node MUST stop advertising
	// _nmos-node._tcp until registration is lost) and rebuilt
	// verbatim on lose-registration.
	announceInstance dnssdcodec.Instance
	announceCtx      context.Context

	// Per-endpoint hit counters.
	indexHits    uint64
	selfHits     uint64
	deviceHits   uint64
	sourceHits   uint64
	flowHits     uint64
	senderHits   uint64
	receiverHits uint64

	// IS-04 §3.1.1 Peer-to-Peer Node TXT counters. One per resource
	// type — incremented every time the matching Node API resource list
	// changes (POST/PUT/DELETE on /devices, /sources, /flows, /senders,
	// /receivers, or PATCH on /self via IS-05). Mode-D peers watch the
	// TXT records on `_nmos-node._tcp` to know when to re-fetch the
	// underlying Node API. Wraps mod 256 per spec; the wire form is the
	// uint8 truncation of the counter. Bumping is wired through
	// bumpResourceVersion which republishes the TXT via Responder.Update.
	verSelf     atomic.Uint64
	verDevice   atomic.Uint64
	verSource   atomic.Uint64
	verFlow     atomic.Uint64
	verSender   atomic.Uint64
	verReceiver atomic.Uint64
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
	codec, ok := is04.Get(cfg.APIVer)
	if !ok {
		return nil, fmt.Errorf("provider/node: no IS-04 codec registered for api_ver=%q (registered: %v)", cfg.APIVer, is04.SupportedVersions())
	}
	s := &IS04NodeServer{logger: logger, cfg: cfg, bundle: bundle, codec: codec}

	// IS-05 is served unless explicitly disabled. A Node carrying
	// senders and receivers but no Connection API can be discovered
	// and never routed — valid IS-04 and useless — so it is opt-OUT.
	if !cfg.NoConnectionAPI {
		s.connection = NewIS05ConnectionServer(logger, bundle, IS05ConnectionConfig{
			APIVer: cfg.ConnectionAPIVer,
		})
	}
	// IS-08 likewise. An audio Node that publishes no channel map
	// leaves a controller unable to say which channel goes where, so
	// the useful default is to serve it and let a Node with no audio
	// publish an empty one.
	if !cfg.NoChannelMappingAPI {
		s.channelMapping = NewIS08ChannelMappingServer(logger, bundle, IS08ChannelMappingConfig{
			APIVer: cfg.ChannelMappingAPIVer,
		})
	}
	if !cfg.NoEventsAPI {
		s.events = NewIS07EventsServer(logger, bundle, IS07EventsConfig{
			APIVer: cfg.EventsAPIVer,
		})
	}
	return s, nil
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

	// Rewrite Sender manifest_href to point at our /transportfile route
	// at the wire api_ver. v1.0/v1.1/v1.2 sender.json require a non-null
	// URI string; the matching transportfile handler is installed below.
	rewriteManifestHrefs(s.bundle.Senders, s.cfg.AdvertiseHost, s.cfg.APIVer)

	// Re-seed what IS-05 "auto" resolves to, now that the endpoint list
	// is real.
	//
	// The Connection API was constructed before expandNodeEndpoints
	// ran, so it took its address from whatever the bundle file
	// happened to declare -- often nothing. ACTIVE transport params
	// name the address a peer connects to, and a stale one there
	// points a controller at a host that is not us.
	if s.connection != nil {
		s.connection.Store().setNodeIP(firstNodeIP(s.bundle))
		s.connection.Store().setNodeBase(s.controlHost())
		s.connection.Store().reresolveActive()
	}

	srv := httpsession.NewServer(s.logger)
	s.installRoutes(srv)
	// IS-05 is attached BEFORE the first request can be served,
	// because attaching it rewrites device.controls[] — a controller
	// that fetched /devices first would cache a Device with no route
	// to the Connection API and never look again.
	s.attachConnectionAPI(srv)
	s.attachChannelMappingAPI(srv)
	s.attachEventsAPI(srv)
	s.http = srv

	// DNS-SD announce.
	if s.cfg.DiscoveryMode == "" || s.cfg.DiscoveryMode == "mdns" {
		host, port := splitHostPort(s.cfg.AdvertiseHost, s.cfg.Bind)
		// IS-04 §3.1 + AMWA test_12_01: api_ver TXT carries the FULL
		// comma-separated list of versions the Node supports (from the
		// bundle's `api.versions`), NOT just the wire api_ver.
		// Advertising only "v1.3" makes the Node look like v1.3+-only,
		// which test_12_01 flags as Warning even when registered.
		apiVerTXT := s.cfg.APIVer
		if len(s.bundle.Node.API.Versions) > 0 {
			apiVerTXT = strings.Join(s.bundle.Node.API.Versions, ",")
		}
		ins := dnssdcodec.Instance{
			Name:    nodeInstanceName(s.bundle.Node.Label),
			Service: dnssdcodec.ServiceNode,
			Domain:  dnssdcodec.DefaultDomain,
			Host:    host,
			Port:    uint16(port),
			TXT:     s.buildNodeTXTLocked(apiVerTXT),
		}
		s.announceInstance = ins
		// Save the parent context for later re-announce on
		// lose-registration; the per-announce sub-context is fresh
		// each time (Avahi auto-cancels on Close).
		s.announceCtx = ctx
		if err := s.startMDNSAnnounceLocked(); err != nil {
			s.mu.Unlock()
			return err
		}
	}

	// Registration: explicit URL wins (Mode B). Otherwise, when in
	// mDNS mode, browse `_nmos-register._tcp` and auto-register against
	// the highest-pri Registry — that's IS-04 §3.1 Mode A.
	if s.cfg.NoRegistry {
		s.logger.Info("provider/node: registry disabled, staying peer-to-peer",
			"plugin", "amwa", "api", "is-04", "mode", "direct-node")
	} else if s.cfg.RegistryURL != "" {
		rc := NewRegistrationClient(s.logger, s.cfg.RegistryURL, s.cfg.APIVer, s.bundle)
		rc.SetOnRegistered(s.onRegistrationStateChanged)
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
		rc.SetOnRegistered(s.onRegistrationStateChanged)
		s.regClient = rc
		go rc.Run(ctx)
	}

	// Scheduled activations need a clock running for the life of the
	// server. Without it an endpoint accepts a scheduled PATCH,
	// answers 202, and then never acts — the worst of the three
	// possible behaviours, because it looks correct to the controller
	// right up until the switch does not happen.
	go s.runActivationScheduler(ctx, 0)

	// Read the System API, if there is one.
	//
	// In its own goroutine: IS-09 discovery browses mDNS with a
	// timeout, and blocking the listener on it would mean a Node on a
	// network with no System API takes seconds to answer its first
	// request. The result is advisory (see system_client.go), so
	// nothing here needs to wait for it.
	go func() {
		s.fetchSystemGlobal(ctx)
	}()

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

// buildNodeTXTLocked returns a fresh TXT map for the `_nmos-node._tcp`
// announce: the four IS-04 §3.1.1 base keys plus the six `ver_*`
// counters (snapshotted from atomic state). Caller MUST hold s.mu —
// the only lock-free element is the atomic counter Load.
func (s *IS04NodeServer) buildNodeTXTLocked(apiVerTXT string) map[string]string {
	return map[string]string{
		dnssdcodec.TXTKeyAPIProto: "http",
		dnssdcodec.TXTKeyAPIVer:   apiVerTXT,
		dnssdcodec.TXTKeyAPIAuth:  "false",
		dnssdcodec.TXTKeyPriority: strconv.Itoa(s.cfg.Priority),
		dnssdcodec.TXTKeyVerSlf:   strconv.Itoa(int(uint8(s.verSelf.Load()))),
		dnssdcodec.TXTKeyVerDvc:   strconv.Itoa(int(uint8(s.verDevice.Load()))),
		dnssdcodec.TXTKeyVerSrc:   strconv.Itoa(int(uint8(s.verSource.Load()))),
		dnssdcodec.TXTKeyVerFlw:   strconv.Itoa(int(uint8(s.verFlow.Load()))),
		dnssdcodec.TXTKeyVerSnd:   strconv.Itoa(int(uint8(s.verSender.Load()))),
		dnssdcodec.TXTKeyVerRcv:   strconv.Itoa(int(uint8(s.verReceiver.Load()))),
	}
}

// counterForResource picks the matching atomic counter for a resource
// type. Returns nil for unknown types so the bump path stays safe.
func (s *IS04NodeServer) counterForResource(t is04.ResourceType) (*atomic.Uint64, string) {
	switch t {
	case is04.ResourceNode:
		return &s.verSelf, dnssdcodec.TXTKeyVerSlf
	case is04.ResourceDevice:
		return &s.verDevice, dnssdcodec.TXTKeyVerDvc
	case is04.ResourceSource:
		return &s.verSource, dnssdcodec.TXTKeyVerSrc
	case is04.ResourceFlow:
		return &s.verFlow, dnssdcodec.TXTKeyVerFlw
	case is04.ResourceSender:
		return &s.verSender, dnssdcodec.TXTKeyVerSnd
	case is04.ResourceReceiver:
		return &s.verReceiver, dnssdcodec.TXTKeyVerRcv
	}
	return nil, ""
}

// BumpResourceVersion increments the matching IS-04 §3.1.1 `ver_*`
// counter (mod 256) and republishes the `_nmos-node._tcp` TXT record so
// Mode-D peers learn the resource list has changed. Safe to call from
// any handler — the responder Update path is no-op when we're in
// registered mode (responder == nil because Node MUST suspend the P2P
// announce while registered, IS-04 §4.2.1).
//
// Exported so the IS-05 Connection API (and any future runtime
// mutation surface) can trigger a counter bump after PATCH /staged
// activations promote into the live bundle.
func (s *IS04NodeServer) BumpResourceVersion(t is04.ResourceType) {
	c, key := s.counterForResource(t)
	if c == nil {
		return
	}
	v := uint8(c.Add(1))
	s.mu.Lock()
	if s.announceInstance.Service == "" {
		// Not in mDNS mode (static discovery) — nothing to advertise.
		s.mu.Unlock()
		return
	}
	if s.announceInstance.TXT == nil {
		s.announceInstance.TXT = map[string]string{}
	}
	// Always stage the new value on the saved Instance so that a
	// later re-announce on lose-registration carries the up-to-date
	// counters, even if mutations happened while we were registered
	// and the responder was suspended.
	s.announceInstance.TXT[key] = strconv.Itoa(int(v))
	snapshot := s.announceInstance
	resp := s.responder
	s.mu.Unlock()
	if resp == nil {
		// Registered mode: counter is staged on announceInstance.TXT,
		// will be picked up on the next P2P re-announce.
		return
	}
	if err := resp.Update(s.announceCtx, snapshot); err != nil {
		s.logger.Warn("provider/node: republish ver_* TXT failed",
			"resource", t, "err", err)
	}
}

// startMDNSAnnounceLocked opens a fresh Responder + Announces the saved
// announceInstance. Caller MUST hold s.mu. Idempotent: a no-op if a
// responder is already active. Used both at first Serve and to
// re-announce after a lose-registration transition.
func (s *IS04NodeServer) startMDNSAnnounceLocked() error {
	if s.responder != nil {
		return nil
	}
	resp, err := dnssdsession.NewResponder(s.logger)
	if err != nil {
		return fmt.Errorf("provider/node: open mDNS responder: %w", err)
	}
	ctxAnnounce, cancel := context.WithCancel(s.announceCtx)
	if err := resp.Announce(ctxAnnounce, s.announceInstance); err != nil {
		cancel()
		_ = resp.Close()
		return fmt.Errorf("provider/node: announce %s: %w", dnssdcodec.ServiceNode, err)
	}
	s.responder = resp
	s.cancel = cancel
	s.logger.Info("provider/node: mDNS announce active",
		"service", dnssdcodec.ServiceNode,
		"host", s.announceInstance.Host, "port", s.announceInstance.Port,
		"api_ver", s.cfg.APIVer, "pri", s.cfg.Priority)
	return nil
}

// stopMDNSAnnounceLocked tears the responder down + emits goodbye
// packets. Caller MUST hold s.mu. Idempotent.
func (s *IS04NodeServer) stopMDNSAnnounceLocked() {
	if s.responder == nil {
		return
	}
	if s.cancel != nil {
		s.cancel()
		s.cancel = nil
	}
	_ = s.responder.Close()
	s.responder = nil
	s.logger.Info("provider/node: mDNS announce suspended (registered with Registry)",
		"service", dnssdcodec.ServiceNode)
}

// onRegistrationStateChanged toggles the _nmos-node._tcp announce on
// every registration transition. IS-04 v1.3 §4.2.1 (and AMWA test_12_01)
// require: registered Nodes MUST stop advertising via mDNS until
// registration is lost. Stub for v1.0/v1.1/v1.2 too — the spec rule
// is harmless on older minors and keeps behaviour uniform.
func (s *IS04NodeServer) onRegistrationStateChanged(registered bool) {
	if s.cfg.DiscoveryMode != "" && s.cfg.DiscoveryMode != "mdns" {
		return // static discovery — no responder to toggle
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if registered {
		s.stopMDNSAnnounceLocked()
	} else {
		if err := s.startMDNSAnnounceLocked(); err != nil {
			s.logger.Warn("provider/node: re-announce on lose-registration failed", "err", err)
		}
	}
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
	// The root lists every API TREE this host serves, not only the Node
	// API. auto_connection_1 fails outright when the Connection API is
	// served but unlisted — the same "served but not advertised is
	// absent" trap as device.controls, one level up.
	apiTrees := func() []string {
		trees := []string{"node/"}
		if s.connection != nil {
			trees = append(trees, "connection/")
		}
		if s.channelMapping != nil {
			trees = append(trees, "channelmapping/")
		}
		if s.events != nil {
			trees = append(trees, "events/")
		}
		return trees
	}
	srv.Handle(stdhttp.MethodGet, "/x-nmos", func(ctx context.Context, r *stdhttp.Request) (int, any, error) {
		return 0, apiTrees(), nil
	})
	srv.Handle(stdhttp.MethodGet, "/x-nmos/", func(ctx context.Context, r *stdhttp.Request) (int, any, error) {
		return 0, apiTrees(), nil
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
		body, err := s.codec.EncodeNode(s.bundle.Node)
		if err != nil {
			return stdhttp.StatusInternalServerError, httpsession.ErrorBody{Code: 500, Error: "Encode failed", Debug: err.Error()}, nil
		}
		return 0, json.RawMessage(body), nil
	})

	// Devices, Sources, Flows, Senders, Receivers each have a list
	// endpoint and a per-id endpoint. We register the list, and let
	// the dispatcher 404 on unknown ids — there's no wildcard support
	// in the route table, so per-id endpoints are installed at server
	// start by walking the bundle.
	//
	// Every response body is encoded via s.codec — the per-api_ver
	// IS-04 Codec — so the wire shape matches the URL's minor (#192).
	// Returning canonical structs through encoding/json would emit v1.3
	// fields that the older minor's schema rejects.
	s.installCollection(srv, base, "devices", func() any {
		s.mu.Lock()
		defer s.mu.Unlock()
		atomic.AddUint64(&s.deviceHits, 1)
		return encodeList(s.codec.EncodeDevice, s.bundle.Devices)
	}, func(id string) (any, bool) {
		s.mu.Lock()
		defer s.mu.Unlock()
		for i := range s.bundle.Devices {
			if s.bundle.Devices[i].ID == id {
				return encodeOne(s.codec.EncodeDevice, s.bundle.Devices[i]), true
			}
		}
		return nil, false
	}, idsFromDevices(s.bundle.Devices))

	s.installCollection(srv, base, "sources", func() any {
		s.mu.Lock()
		defer s.mu.Unlock()
		atomic.AddUint64(&s.sourceHits, 1)
		return encodeList(s.codec.EncodeSource, s.bundle.Sources)
	}, func(id string) (any, bool) {
		s.mu.Lock()
		defer s.mu.Unlock()
		for i := range s.bundle.Sources {
			if s.bundle.Sources[i].ID == id {
				return encodeOne(s.codec.EncodeSource, s.bundle.Sources[i]), true
			}
		}
		return nil, false
	}, idsFromSources(s.bundle.Sources))

	s.installCollection(srv, base, "flows", func() any {
		s.mu.Lock()
		defer s.mu.Unlock()
		atomic.AddUint64(&s.flowHits, 1)
		return encodeList(s.codec.EncodeFlow, s.bundle.Flows)
	}, func(id string) (any, bool) {
		s.mu.Lock()
		defer s.mu.Unlock()
		for i := range s.bundle.Flows {
			if s.bundle.Flows[i].ID == id {
				return encodeOne(s.codec.EncodeFlow, s.bundle.Flows[i]), true
			}
		}
		return nil, false
	}, idsFromFlows(s.bundle.Flows))

	s.installCollection(srv, base, "senders", func() any {
		s.mu.Lock()
		defer s.mu.Unlock()
		atomic.AddUint64(&s.senderHits, 1)
		return encodeList(s.codec.EncodeSender, s.bundle.Senders)
	}, func(id string) (any, bool) {
		s.mu.Lock()
		defer s.mu.Unlock()
		for i := range s.bundle.Senders {
			if s.bundle.Senders[i].ID == id {
				return encodeOne(s.codec.EncodeSender, s.bundle.Senders[i]), true
			}
		}
		return nil, false
	}, idsFromSenders(s.bundle.Senders))

	// IS-04 senders/{id}/transportfile — serves the SDP that describes
	// how to receive the Sender's flow. v1.0/v1.1/v1.2 schemas require
	// manifest_href to be a non-null URI; AMWA test_20_01 (v1.3) checks
	// the URL is actually reachable. We serve a minimal RFC 4566 SDP
	// per Sender — Content-Type application/sdp, status 200.
	//
	// The SDP comes from the IS-05 generator, not from a second one
	// living here. IS-05-02 test_13 fetches BOTH URLs and compares
	// them byte for byte, and it is right to: they are two routes to
	// one fact, and a Node that answers differently on each has told a
	// controller two different things about the same stream. Two
	// generators drift the moment either is touched.
	for _, snd := range s.bundle.Senders {
		sid := snd.ID
		sndCopy := snd
		path := base + "/senders/" + sid + "/transportfile"
		srv.Handle(stdhttp.MethodGet, path, func(ctx context.Context, r *stdhttp.Request) (int, any, error) {
			if sdp := s.senderSDP(sid); sdp != "" {
				return 0, &httpsession.RawBody{
					ContentType: "application/sdp",
					Body:        []byte(sdp),
				}, nil
			}
			// No Connection API mounted: fall back to the standalone
			// renderer so manifest_href still resolves.
			return 0, &httpsession.RawBody{
				ContentType: "application/sdp",
				Body:        []byte(sdpFor(sndCopy)),
			}, nil
		})
	}

	s.installCollection(srv, base, "receivers", func() any {
		s.mu.Lock()
		defer s.mu.Unlock()
		atomic.AddUint64(&s.receiverHits, 1)
		return encodeList(s.codec.EncodeReceiver, s.bundle.Receivers)
	}, func(id string) (any, bool) {
		s.mu.Lock()
		defer s.mu.Unlock()
		for i := range s.bundle.Receivers {
			if s.bundle.Receivers[i].ID == id {
				return encodeOne(s.codec.EncodeReceiver, s.bundle.Receivers[i]), true
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
			// Otherwise the payload is a Sender object — connect. Decode
			// via the per-api_ver codec so that a v1.0 PUT body (no caps,
			// no interface_bindings, no subscription) is accepted; the
			// canonical decoder uses DisallowUnknownFields against the v1.3
			// schema and would 400 on a v1.0-shaped body even though it's
			// spec-correct for the URL minor (#192).
			sender, err := s.codec.DecodeSender(body)
			if err != nil {
				return stdhttp.StatusBadRequest, httpsession.ErrorBody{
					Code: stdhttp.StatusBadRequest, Error: "Bad Request", Debug: err.Error(),
				}, nil
			}
			id := sender.ID
			rcv.Subscription.SenderID = &id
			rcv.Subscription.Active = true
			// Encode the response Sender via the same codec so the wire
			// shape matches the URL minor.
			respBody, err := s.codec.EncodeSender(sender)
			if err != nil {
				return stdhttp.StatusInternalServerError, httpsession.ErrorBody{
					Code: 500, Error: "Encode failed", Debug: err.Error(),
				}, nil
			}
			return stdhttp.StatusAccepted, json.RawMessage(respBody), nil
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
			// Not found, OR found and inexpressible at this minor --
			// the same answer either way. A controller asking a v1.0
			// Node for a resource that needs v1.2 vocabulary is asking
			// for something this Node does not have AT THIS VERSION,
			// which is what 404 means.
			if !ok || isNilBody(body) {
				return stdhttp.StatusNotFound, httpsession.ErrorBody{
					Code: stdhttp.StatusNotFound, Error: "Not Found", Debug: id,
				}, nil
			}
			return 0, body, nil
		})
	}
}

// isNilBody reports whether a getFn result carries no encodable
// resource. The typed nil hides inside an interface, so a plain
// `body == nil` misses it.
func isNilBody(body any) bool {
	if body == nil {
		return true
	}
	raw, isRaw := body.(json.RawMessage)
	return isRaw && len(raw) == 0
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
